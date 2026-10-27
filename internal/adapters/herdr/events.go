package herdr

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	herdrapi "github.com/cyperx84/herdr-api"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// Events opens event subscriptions; it implements ports.EventSource.
type Events struct {
	client *Client
	now    func() time.Time
}

// NewEvents subscribes through client; now stamps received events.
func NewEvents(client *Client, now func() time.Time) *Events {
	return &Events{client: client, now: now}
}

// SubscriptionType is the dotted subscription name of a kind:
// pane_agent_detected → pane.agent_detected.
func SubscriptionType(kind domain.EventKind) string {
	return strings.Replace(string(kind), "_", ".", 1)
}

// Subscribe opens one stream. The connection stays write-silent after the
// request: client bytes on a streaming connection read as a disconnect.
func (e *Events) Subscribe(ctx context.Context, kinds []domain.EventKind) (ports.EventStream, error) {
	subs := make([]herdrapi.Subscription, 0, len(kinds))
	for _, kind := range kinds {
		subs = append(subs, herdrapi.Subscription{Type: SubscriptionType(kind)})
	}
	streamCtx, cancel := context.WithCancel(ctx)
	raw, err := e.client.api.Subscribe(streamCtx, subs...)
	if err != nil {
		cancel()
		return nil, classify("events.subscribe", err)
	}
	s := &stream{raw: raw, cancel: cancel, events: make(chan domain.AgentEvent, eventBuffer), done: make(chan struct{})}
	go s.pump(e.now)
	return s, nil
}

const eventBuffer = 256

type stream struct {
	raw    *herdrapi.Stream
	cancel context.CancelFunc
	events chan domain.AgentEvent
	done   chan struct{}
	once   sync.Once
}

func (s *stream) pump(now func() time.Time) {
	defer close(s.events)
	for ev := range s.raw.Events() {
		select {
		case s.events <- Normalize(ev.Kind, ev.Data, now()):
		case <-s.done:
			return
		}
	}
}

func (s *stream) Events() <-chan domain.AgentEvent { return s.events }

func (s *stream) Err() error { return s.raw.Err() }

func (s *stream) Close() error {
	var err error
	s.once.Do(func() {
		close(s.done)
		s.cancel()
		err = s.raw.Close()
	})
	return err
}

// Normalize turns a wire event into a domain event. kind arrives either
// underscored (lifecycle subscriptions) or dotted (per-pane ones); both map
// to the same EventKind.
func Normalize(kind string, data json.RawMessage, at time.Time) domain.AgentEvent {
	event := domain.AgentEvent{Kind: knownKind(strings.ReplaceAll(kind, ".", "_")), Timestamp: at}
	var payload wireEventData
	if len(data) > 0 && json.Unmarshal(data, &payload) == nil {
		event.PaneID = domain.PaneID(payload.PaneID)
		event.Workspace = domain.WorkspaceID(payload.WorkspaceID)
		if payload.Pane != nil {
			event.PaneID = domain.PaneID(payload.Pane.PaneID)
			event.Workspace = domain.WorkspaceID(payload.Pane.WorkspaceID)
		}
		if payload.Workspace != nil && event.Workspace == "" {
			event.Workspace = domain.WorkspaceID(payload.Workspace.WorkspaceID)
		}
		if payload.AgentStatus != "" {
			event.Status = domain.ParseAgentStatus(payload.AgentStatus)
		}
	}
	return event
}

func knownKind(kind string) domain.EventKind {
	switch k := domain.EventKind(kind); k {
	case domain.EventPaneCreated, domain.EventPaneClosed, domain.EventPaneExited, domain.EventPaneMoved,
		domain.EventPaneFocused, domain.EventAgentDetected, domain.EventAgentStatusChanged,
		domain.EventWorkspaceCreated, domain.EventWorkspaceClosed, domain.EventWorkspaceRenamed,
		domain.EventWorkspaceFocused, domain.EventWorkspaceMoved, domain.EventWorkspaceReordered,
		domain.EventTabCreated, domain.EventTabClosed, domain.EventTabRenamed, domain.EventTabMoved:
		return k
	case domain.EventStreamStarted, domain.EventOther:
		return domain.EventOther
	}
	return domain.EventOther
}
