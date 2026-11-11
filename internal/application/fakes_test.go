package application

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

var errDown = errors.New("socket down")

// fakeClock is a settable wall clock.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// fakeHerdr serves both the agent list and the session snapshot.
type fakeHerdr struct {
	mu        sync.Mutex
	session   domain.Session
	fail      bool
	lists     int
	snapshots int
}

func (h *fakeHerdr) ListAgents(context.Context) ([]domain.Agent, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lists++
	if h.fail {
		return nil, errDown
	}
	return append([]domain.Agent(nil), h.session.Agents...), nil
}

func (h *fakeHerdr) Snapshot(context.Context) (domain.Session, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.snapshots++
	if h.fail {
		return domain.Session{}, errDown
	}
	s := h.session
	s.Agents = append([]domain.Agent(nil), h.session.Agents...)
	return s, nil
}

func (h *fakeHerdr) setStatus(pane domain.PaneID, status domain.AgentStatus) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.session.Agents {
		if h.session.Agents[i].Pane == pane {
			h.session.Agents[i].Status = status
		}
	}
}

func (h *fakeHerdr) update(f func(*domain.Session)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f(&h.session)
}

// fakeSidebar records every view it was given.
type fakeSidebar struct {
	mu       sync.Mutex
	views    []domain.SidebarView
	orders   []domain.SortMode
	clears   []bool
	resyncs  int
	fail     bool
	families [][]domain.WorkspaceID
}

func (s *fakeSidebar) Apply(_ context.Context, view domain.SidebarView) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return time.Time{}, errors.New("write refused")
	}
	s.views = append(s.views, view)
	for _, g := range view.Groups {
		for _, r := range g.Rows {
			if r.Display.Animated() {
				return view.At.Add(150 * time.Millisecond), nil
			}
		}
	}
	return time.Time{}, nil
}

func (s *fakeSidebar) SetOrder(_ context.Context, mode domain.SortMode, family []domain.WorkspaceID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orders = append(s.orders, mode)
	s.families = append(s.families, family)
	return nil
}

func (s *fakeSidebar) Badge(view domain.SidebarView) string {
	asking, finished := view.WaitingCounts()
	if asking+finished == 0 {
		return ""
	}
	return fmt.Sprintf("?%d ✓%d", asking, finished)
}

func (s *fakeSidebar) Clear(_ context.Context, purge bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clears = append(s.clears, purge)
	return nil
}

func (s *fakeSidebar) Resync() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resyncs++
}

func (s *fakeSidebar) last() domain.SidebarView {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.views) == 0 {
		return domain.SidebarView{}
	}
	return s.views[len(s.views)-1]
}

func (s *fakeSidebar) display(pane domain.PaneID) domain.DisplayState {
	for _, g := range s.last().Groups {
		for _, r := range g.Rows {
			if r.Pane == pane {
				return r.Display
			}
		}
	}
	return ""
}

// fakeState is an in-memory StateRepository.
type fakeState struct {
	mu      sync.Mutex
	records []domain.AgentSession
	saves   int
	fail    bool
}

func (s *fakeState) Load(context.Context) ([]domain.AgentSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.AgentSession(nil), s.records...), nil
}

func (s *fakeState) Save(_ context.Context, records []domain.AgentSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("disk full")
	}
	s.records = records
	s.saves++
	return nil
}

// fakeActivity answers recovery lookups from a map.
type fakeActivity struct {
	stamps  map[string]time.Time
	lookups int
}

func (a *fakeActivity) LastActivity(_ context.Context, _ domain.AgentKind, session string) (time.Time, error) {
	a.lookups++
	return a.stamps[session], nil
}

type fakeHost struct{ grouped bool }

func (h fakeHost) PanelGrouped() bool { return h.grouped }

type fakeStore struct {
	mode  domain.SortMode
	ok    bool
	saves int
}

func (s *fakeStore) LoadSortMode() (domain.SortMode, bool, error) { return s.mode, s.ok, nil }

func (s *fakeStore) SaveSortMode(mode domain.SortMode) error {
	s.mode, s.ok = mode, true
	s.saves++
	return nil
}

type fakeFolds struct{ keys []string }

func (s *fakeFolds) LoadFolds() ([]string, error) { return s.keys, nil }

func (s *fakeFolds) SaveFolds(keys []string) error {
	s.keys = keys
	return nil
}

type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

// fixture is an observer wired to fakes.
type fixture struct {
	clock    *fakeClock
	herdr    *fakeHerdr
	sidebar  *fakeSidebar
	state    *fakeState
	activity *fakeActivity
	observer *Observer
}

func newFixture(agents ...domain.Agent) *fixture {
	f := &fixture{
		clock:    newClock(),
		herdr:    &fakeHerdr{},
		sidebar:  &fakeSidebar{},
		state:    &fakeState{},
		activity: &fakeActivity{stamps: map[string]time.Time{}},
	}
	f.herdr.session = domain.Session{
		Server:     domain.ServerInfo{Version: "0.9.5", Protocol: 22},
		Workspaces: []domain.Workspace{{ID: "w1", Label: "project"}},
		Tabs:       []domain.Tab{{ID: "w1:t1", Workspace: "w1", Label: "1"}},
		Agents:     agents,
	}
	f.observer = f.newObserver(nil)
	return f
}

func (f *fixture) newObserver(records []domain.AgentSession) *Observer {
	settings := ports.DefaultSettings()
	settings.Policy.IdleGrace = 0
	return NewObserver(ObserverDeps{
		Agents: f.herdr, Session: f.herdr, Activity: f.activity, State: f.state,
		Sidebar: f.sidebar, Host: fakeHost{grouped: true}, Clock: f.clock, Logger: nopLogger{},
	}, settings, domain.SortActive, records)
}

func agentAt(id domain.AgentID, pane domain.PaneID, status domain.AgentStatus) domain.Agent {
	terminal := domain.TerminalID("term-" + string(id))
	return domain.Agent{
		ID: id, Pane: pane, Tab: "w1:t1", Workspace: "w1", Terminal: terminal,
		Kind: "claude", Session: string(id), Title: "task", Status: status,
	}
}
