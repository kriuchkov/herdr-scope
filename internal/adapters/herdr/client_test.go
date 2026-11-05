package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

// fakeServer speaks Herdr's framing: one request per connection, except
// events.subscribe, which acks and then streams.
type fakeServer struct {
	t        *testing.T
	path     string
	mu       sync.Mutex
	requests []map[string]any
	answer   func(method string, params map[string]any) (any, *struct{ Code, Message string })
	events   chan string
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	// Not t.TempDir: a Unix socket path must stay under 104 bytes.
	dir, err := os.MkdirTemp("", "hs") //nolint:usetesting // socket path length
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	s := &fakeServer{t: t, path: filepath.Join(dir, "herdr.sock"), events: make(chan string, 8)}
	listener, err := net.Listen("unix", s.path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return s
}

func (s *fakeServer) serve(conn net.Conn) {
	defer conn.Close()
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return
	}
	var req struct {
		ID     string         `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if json.Unmarshal(line, &req) != nil {
		return
	}
	s.mu.Lock()
	s.requests = append(s.requests, map[string]any{"method": req.Method, "params": req.Params})
	s.mu.Unlock()
	if req.Method == "events.subscribe" {
		_, _ = conn.Write([]byte(`{"id":"` + req.ID + `","result":{"type":"subscription_started"}}` + "\n"))
		for ev := range s.events {
			if _, err = conn.Write([]byte(ev + "\n")); err != nil {
				return
			}
		}
		return
	}
	result, refusal := s.answer(req.Method, req.Params)
	var reply any = map[string]any{"id": req.ID, "result": result}
	if refusal != nil {
		reply = map[string]any{"id": req.ID, "error": map[string]string{"code": refusal.Code, "message": refusal.Message}}
	}
	raw, _ := json.Marshal(reply)
	_, _ = conn.Write(append(raw, '\n'))
}

func (s *fakeServer) last() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[len(s.requests)-1]
}

func TestPingAndProtocol(t *testing.T) {
	s := newFakeServer(t)
	s.answer = func(string, map[string]any) (any, *struct{ Code, Message string }) {
		return map[string]any{"type": "pong", "version": "0.9.5", "protocol": 22, "capabilities": map[string]any{"live_handoff": true}}, nil
	}
	c := NewClient(s.path, "plugin:test")
	info, err := c.Ping(context.Background())
	require.NoError(t, err)
	assert.Equal(t, domain.ServerInfo{Version: "0.9.5", Protocol: 22}, info)
	assert.False(t, c.Gone())
}

func TestReportPaneSendsNullsAndTreatsNotFoundAsLanded(t *testing.T) {
	s := newFakeServer(t)
	s.answer = func(_ string, params map[string]any) (any, *struct{ Code, Message string }) {
		if params["pane_id"] == "w9:p9" {
			return nil, &struct{ Code, Message string }{"pane_not_found", "pane w9:p9 not found"}
		}
		return map[string]any{"type": "ok"}, nil
	}
	c := NewClient(s.path, "plugin:test")
	title := "✓ done"
	require.NoError(t, c.ReportPane(context.Background(), "w1:p1", map[string]*string{"title_done": &title, "title_idle": nil}))
	params := s.last()["params"].(map[string]any)
	assert.Equal(t, "plugin:test", params["source"])
	tokens := params["tokens"].(map[string]any)
	assert.Equal(t, "✓ done", tokens["title_done"])
	assert.Contains(t, tokens, "title_idle")
	assert.Nil(t, tokens["title_idle"], "a clear is an explicit null")

	require.NoError(t, c.ReportPane(context.Background(), "w9:p9", map[string]*string{"x": nil}), "a gone pane is done, not failed")

	tooMany := map[string]*string{}
	for i := range 17 {
		tooMany[string(rune('a'+i))] = nil
	}
	require.ErrorIs(t, c.ReportPane(context.Background(), "w1:p1", tooMany), domain.ErrProjection)
}

func TestRefusalsAndTransportErrorsAreClassified(t *testing.T) {
	s := newFakeServer(t)
	s.answer = func(string, map[string]any) (any, *struct{ Code, Message string }) {
		return nil, &struct{ Code, Message string }{"invalid_params", "bad"}
	}
	c := NewClient(s.path, "plugin:test")
	err := c.SetAgentView(context.Background(), "active", []string{"ws_key"}, "", false, nil)
	require.ErrorIs(t, err, domain.ErrRefused)
	require.NotErrorIs(t, err, domain.ErrProtocol, "one refusal is no reason to stop")
	assert.True(t, IsCode(err, "invalid_params"))

	gone := NewClient(filepath.Join(filepath.Dir(s.path), "missing.sock"), "plugin:test")
	_, err = gone.Ping(context.Background())
	require.ErrorIs(t, err, domain.ErrTransport)
	assert.True(t, gone.Gone())
}

func TestSetAgentViewShape(t *testing.T) {
	s := newFakeServer(t)
	s.answer = func(string, map[string]any) (any, *struct{ Code, Message string }) {
		return map[string]any{"type": "agent_view", "active": true}, nil
	}
	c := NewClient(s.path, "plugin:test")
	require.NoError(t, c.SetAgentView(context.Background(), "active", []string{"ws_key", "tab_key", "sort_key"}, "", false, nil))
	params := s.last()["params"].(map[string]any)
	assert.Equal(t, "active", params["label"])
	sorts := params["sort"].([]any)
	require.Len(t, sorts, 3)
	first := sorts[0].(map[string]any)
	assert.Equal(t, "desc", first["order"])
	assert.Equal(t, map[string]any{"token": "ws_key"}, first["field"])
}

func TestSnapshotDecodesThroughTheSocket(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "tests", "fixtures", "herdr", "session.snapshot.json"))
	require.NoError(t, err)
	var recorded struct {
		Result json.RawMessage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &recorded))
	s := newFakeServer(t)
	s.answer = func(string, map[string]any) (any, *struct{ Code, Message string }) { return recorded.Result, nil }
	obs := NewObserver(NewClient(s.path, "plugin:test"), Mapper{})
	session, err := obs.Snapshot(context.Background())
	require.NoError(t, err)
	assert.Len(t, session.Agents, 5)
	assert.Equal(t, "session.snapshot", s.last()["method"])
}

func TestSubscribeStreamsNormalisedEvents(t *testing.T) {
	s := newFakeServer(t)
	events := NewEvents(NewClient(s.path, "plugin:test"), time.Now)
	stream, err := events.Subscribe(t.Context(), []domain.EventKind{domain.EventPaneCreated, domain.EventAgentDetected})
	require.NoError(t, err)
	subs := s.last()["params"].(map[string]any)["subscriptions"].([]any)
	assert.Equal(t, map[string]any{"type": "pane.created"}, subs[0])
	assert.Equal(t, map[string]any{"type": "pane.agent_detected"}, subs[1])

	s.events <- `{"event":"pane_agent_detected","data":{"type":"pane_agent_detected","pane_id":"w1:p1","workspace_id":"w1","agent":"claude"}}`
	s.events <- `{"event":"pane.agent_status_changed","data":{"pane_id":"w1:p1","workspace_id":"w1","agent_status":"working"}}`
	first := <-stream.Events()
	assert.Equal(t, domain.EventAgentDetected, first.Kind)
	assert.Equal(t, domain.PaneID("w1:p1"), first.PaneID)
	assert.False(t, first.Timestamp.IsZero())
	second := <-stream.Events()
	assert.Equal(t, domain.EventAgentStatusChanged, second.Kind)
	assert.Equal(t, domain.StatusWorking, second.Status)

	require.NoError(t, stream.Close())
	for {
		if _, open := <-stream.Events(); !open {
			break
		}
	}
	close(s.events)
}

func TestTabsAndRenameTabShape(t *testing.T) {
	s := newFakeServer(t)
	s.answer = func(method string, params map[string]any) (any, *struct{ Code, Message string }) {
		if method == "tab.list" {
			return map[string]any{"type": "tab_list", "tabs": []map[string]any{
				{"tab_id": "w1:t1", "workspace_id": "w1", "number": 1, "label": "1"},
				{"tab_id": "", "workspace_id": "w1", "label": "ghost"},
				{"tab_id": "w1:t2", "workspace_id": "w1", "number": 2, "label": "api"},
			}}, nil
		}
		return map[string]any{"type": "tab_info", "tab": map[string]any{"tab_id": params["tab_id"], "label": params["label"]}}, nil
	}
	c := NewClient(s.path, "plugin:test")
	tabs, err := c.Tabs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []domain.Tab{{ID: "w1:t1", Workspace: "w1", Label: "1"}, {ID: "w1:t2", Workspace: "w1", Label: "api"}}, tabs)

	require.NoError(t, c.RenameTab(context.Background(), "w1:t2", "api ?"))
	assert.Equal(t, map[string]any{"method": "tab.rename", "params": map[string]any{"tab_id": "w1:t2", "label": "api ?"}}, s.last())

	count, err := c.TabCount(context.Background(), "w1")
	require.NoError(t, err)
	assert.Equal(t, 2, count, "a tab without an id is no tab")
	count, err = c.TabCount(context.Background(), "w2")
	require.NoError(t, err)
	assert.Zero(t, count)
}

func TestRenameTabTreatsAGoneTabAsLanded(t *testing.T) {
	s := newFakeServer(t)
	s.answer = func(string, map[string]any) (any, *struct{ Code, Message string }) {
		return nil, &struct{ Code, Message string }{"tab_not_found", "tab w1:t9 not found"}
	}
	c := NewClient(s.path, "plugin:test")
	require.NoError(t, c.RenameTab(context.Background(), "w1:t9", "9"), "a gone tab is done, not failed")
}

func TestAResultOfTheWrongShapeIsADecodeError(t *testing.T) {
	s := newFakeServer(t)
	s.answer = func(string, map[string]any) (any, *struct{ Code, Message string }) {
		return map[string]any{"type": "agent_list", "agents": "none"}, nil
	}
	_, err := NewObserver(NewClient(s.path, "plugin:test"), Mapper{}).ListAgents(context.Background())
	require.ErrorIs(t, err, domain.ErrDecode)
	assert.NotErrorIs(t, err, domain.ErrTransport)
}

func TestSetAgentViewFilters(t *testing.T) {
	s := newFakeServer(t)
	s.answer = func(string, map[string]any) (any, *struct{ Code, Message string }) {
		return map[string]any{"type": "agent_view", "active": true}, nil
	}
	c := NewClient(s.path, "plugin:test")
	// The shapes agent.view takes, as the sidebar sent them before the view
	// was described to the adapter rather than spelled out by it.
	unfolded := `{"op":"not","filter":{"op":"exists","field":{"token":"folded"}}}`
	current := `{"op":"eq","field":"workspace_id","value":{"context":"current_workspace_id"}}`
	members := `{"op":"in","field":"workspace_id","values":["w1","w4","w5"]}`
	family := `{"op":"any","filters":[` + current + `,` + members + `]}`
	unfoldedAnd := func(filter string) string { return `{"op":"all","filters":[` + filter + `,` + unfolded + `]}` }
	cases := []struct {
		name   string
		hide   string
		here   bool
		family []domain.WorkspaceID
		want   string
	}{
		{"every agent", "", false, nil, ""},
		{"what a fold hides left out", "folded", false, nil, unfolded},
		{"here alone: Herdr follows the focus", "folded", true, []domain.WorkspaceID{"w1"}, unfoldedAnd(current)},
		{"here with a family: its members named", "folded", true, []domain.WorkspaceID{"w1", "w4", "w5"}, unfoldedAnd(family)},
	}
	for _, tc := range cases {
		require.NoError(t, c.SetAgentView(context.Background(), "view", []string{"ws_key"}, tc.hide, tc.here, tc.family), tc.name)
		params := s.last()["params"].(map[string]any)
		if tc.want == "" {
			assert.NotContains(t, params, "filter", tc.name)
			continue
		}
		raw, err := json.Marshal(params["filter"])
		require.NoError(t, err)
		assert.JSONEq(t, tc.want, string(raw), tc.name)
	}
}
