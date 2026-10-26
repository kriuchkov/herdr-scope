package application

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

type fakeStream struct {
	events chan domain.AgentEvent
	kinds  []domain.EventKind
	once   sync.Once
}

func (s *fakeStream) Events() <-chan domain.AgentEvent { return s.events }
func (s *fakeStream) Err() error                       { return nil }
func (s *fakeStream) Close() error {
	s.once.Do(func() { close(s.events) })
	return nil
}

type fakeEvents struct {
	mu      sync.Mutex
	streams []*fakeStream
	opened  chan *fakeStream
}

func newFakeEvents() *fakeEvents { return &fakeEvents{opened: make(chan *fakeStream, 8)} }

func (e *fakeEvents) Subscribe(_ context.Context, kinds []domain.EventKind) (ports.EventStream, error) {
	s := &fakeStream{events: make(chan domain.AgentEvent, 16), kinds: kinds}
	e.mu.Lock()
	e.streams = append(e.streams, s)
	e.mu.Unlock()
	e.opened <- s
	return s, nil
}

type fakeProbe struct {
	mu       sync.Mutex
	protocol int
	gone     bool
}

func (p *fakeProbe) Ping(context.Context) (domain.ServerInfo, error) {
	return domain.ServerInfo{Version: "0.9.5", Protocol: p.protocol}, nil
}

func (p *fakeProbe) Gone() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gone
}

type fakeControlServer struct {
	handler chan ports.ControlHandler
}

func (s *fakeControlServer) Serve(ctx context.Context, handler ports.ControlHandler) error {
	s.handler <- handler
	<-ctx.Done()
	return nil
}

type fakeConfig struct {
	mu       sync.Mutex
	settings ports.Settings
	version  string
}

func (c *fakeConfig) Load(context.Context) (ports.Settings, []string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.settings, nil, nil
}

func (c *fakeConfig) Save(_ context.Context, s ports.Settings) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settings = s
	c.version += "+"
	return nil
}

func (c *fakeConfig) Version() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.version
}

type daemonFixture struct {
	*fixture

	events   *fakeEvents
	probe    *fakeProbe
	control  *fakeControlServer
	config   *fakeConfig
	store    *fakeStore
	daemon   *Daemon
	settings []ports.Settings
	done     chan error
	handler  ports.ControlHandler
}

func newDaemonFixture(t *testing.T) *daemonFixture {
	t.Helper()
	f := &daemonFixture{
		fixture: newFixture(agentAt("a1", "w1:p1", domain.StatusIdle)),
		events:  newFakeEvents(),
		probe:   &fakeProbe{protocol: 22},
		control: &fakeControlServer{handler: make(chan ports.ControlHandler, 1)},
		config:  &fakeConfig{settings: ports.DefaultSettings()},
		store:   &fakeStore{},
		done:    make(chan error, 1),
	}
	f.fixture.clock = nil // the loop runs on real time
	f.observer = NewObserver(ObserverDeps{
		Agents: f.herdr, Session: f.herdr, State: f.state, Sidebar: f.sidebar,
		Host: fakeHost{grouped: true}, Clock: realClock{}, Logger: nopLogger{},
	}, ports.DefaultSettings(), domain.SortActive, nil)
	f.daemon = NewDaemon(DaemonDeps{
		Observer: f.observer, Events: f.events, Probe: f.probe, Control: f.control,
		Config: f.config, SortModes: f.store, Sidebar: f.sidebar, Clock: realClock{}, Logger: nopLogger{},
		OnSettings: []func(ports.Settings){func(s ports.Settings) { f.settings = append(f.settings, s) }},
		Version:    "test", PID: 4242,
	})
	return f
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (f *daemonFixture) start(t *testing.T) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { f.done <- f.daemon.Run(ctx) }()
	select {
	case f.handler = <-f.control.handler:
	case <-time.After(2 * time.Second):
		t.Fatal("control channel never served")
	}
	return cancel
}

func (f *daemonFixture) stream(t *testing.T) *fakeStream {
	t.Helper()
	select {
	case s := <-f.events.opened:
		return s
	case <-time.After(2 * time.Second):
		t.Fatal("no subscription opened")
	}
	return nil
}

func eventually(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	require.Eventually(t, cond, 3*time.Second, 10*time.Millisecond, msg)
}

func TestProtocolGateRefusesAnOldHerdr(t *testing.T) {
	_, err := CheckProtocol(context.Background(), &fakeProbe{protocol: 19})
	require.ErrorIs(t, err, domain.ErrProtocol)
	assert.Equal(t, "Herdr Scope requires Herdr protocol 22+.\nDetected protocol 19.", err.Error())

	_, err = CheckProtocol(context.Background(), &fakeProbe{protocol: 23})
	require.NoError(t, err, "a newer protocol is accepted")
}

func TestDaemonPaintsAndAppliesTheOrderAtStart(t *testing.T) {
	f := newDaemonFixture(t)
	cancel := f.start(t)
	defer cancel()
	f.stream(t)
	eventually(t, func() bool { return f.sidebar.display("w1:p1") == domain.DisplayIdle }, "the first frame never painted")
	f.sidebar.mu.Lock()
	assert.Equal(t, domain.SortActive, f.sidebar.orders[0])
	f.sidebar.mu.Unlock()
	assert.Len(t, f.settings, 1, "listeners hear the initial settings")
}

func TestDaemonWakesOnEventsAndResyncsOnReconnect(t *testing.T) {
	f := newDaemonFixture(t)
	cancel := f.start(t)
	defer cancel()
	first := f.stream(t)
	eventually(t, func() bool { return f.sidebar.display("w1:p1") == domain.DisplayIdle }, "no first frame")

	f.herdr.setStatus("w1:p1", domain.StatusWorking)
	first.events <- domain.AgentEvent{Kind: domain.EventAgentDetected, PaneID: "w1:p1"}
	eventually(t, func() bool { return f.sidebar.display("w1:p1") == domain.DisplayWorking }, "the event did not wake a frame")

	_ = first.Close()
	f.stream(t)
	eventually(t, func() bool {
		f.sidebar.mu.Lock()
		defer f.sidebar.mu.Unlock()
		return f.sidebar.resyncs == 1 && len(f.sidebar.orders) >= 2
	}, "a reconnect did not resync the writer and re-apply the order")
}

func TestDaemonExitsWhenTheServerIsGone(t *testing.T) {
	f := newDaemonFixture(t)
	cancel := f.start(t)
	defer cancel()
	s := f.stream(t)
	f.probe.mu.Lock()
	f.probe.gone = true
	f.probe.mu.Unlock()
	_ = s.Close()
	select {
	case err := <-f.done:
		require.ErrorIs(t, err, ErrServerGone)
	case <-time.After(3 * time.Second):
		t.Fatal("the daemon outlived its server")
	}
}

func TestDaemonStopClearsAndExits(t *testing.T) {
	f := newDaemonFixture(t)
	cancel := f.start(t)
	defer cancel()
	f.stream(t)
	eventually(t, func() bool { return f.sidebar.display("w1:p1") != "" }, "no first frame")
	reply := f.handler(context.Background(), ports.ControlRequest{Cmd: ports.ControlStop, Purge: true})
	assert.True(t, reply.Stopped)
	select {
	case err := <-f.done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("stop did not end the daemon")
	}
	assert.Equal(t, []bool{true}, f.sidebar.clears)
	f.state.mu.Lock()
	defer f.state.mu.Unlock()
	assert.NotZero(t, f.state.saves, "memory is flushed on the way out")
}

func TestDaemonViewFlip(t *testing.T) {
	f := newDaemonFixture(t)
	cancel := f.start(t)
	defer cancel()
	f.stream(t)
	reply := f.handler(context.Background(), ports.ControlRequest{Cmd: ports.ControlView, Op: domain.SortFlip})
	require.True(t, reply.Applied)
	assert.Equal(t, "recent", reply.Mode)
	assert.Equal(t, domain.SortRecent, f.store.mode, "the choice is persisted")
	eventually(t, func() bool { return f.sidebar.last().Mode == domain.SortRecent }, "the flip did not repaint")
}

func TestDaemonFoldsAFamily(t *testing.T) {
	f := newDaemonFixture(t)
	cancel := f.start(t)
	defer cancel()
	f.stream(t)
	reply := f.handler(context.Background(), ports.ControlRequest{Cmd: ports.ControlFold, Workspace: "w1"})
	require.True(t, reply.OK, reply.Error)
	assert.Equal(t, "project", reply.Family)
	assert.True(t, reply.Folded)
	assert.Equal(t, "active", reply.Mode)
	eventually(t, func() bool {
		groups := f.sidebar.last().Groups
		return len(groups) > 0 && groups[0].Rows[0].Fold != nil
	}, "the fold did not repaint")

	reply = f.handler(context.Background(), ports.ControlRequest{Cmd: ports.ControlFold, Workspace: "w9"})
	assert.False(t, reply.OK)
	assert.NotEmpty(t, reply.Error)
}

func TestDaemonPingAnswersFromStatus(t *testing.T) {
	f := newDaemonFixture(t)
	cancel := f.start(t)
	defer cancel()
	f.stream(t)
	eventually(t, func() bool {
		reply := f.handler(context.Background(), ports.ControlRequest{Cmd: ports.ControlPing})
		return reply.OK && reply.Status.Frames > 0 && reply.Status.Connected
	}, "ping never reported a connected, drawing daemon")
	reply := f.handler(context.Background(), ports.ControlRequest{Cmd: ports.ControlPing})
	assert.Equal(t, 4242, reply.Status.PID)
	assert.Equal(t, 22, reply.Status.Protocol)
	assert.Less(t, reply.Status.FireAge, StalledFireAge)
}

func TestDaemonReloadAppliesNewSettings(t *testing.T) {
	f := newDaemonFixture(t)
	cancel := f.start(t)
	defer cancel()
	f.stream(t)
	changed := ports.DefaultSettings()
	changed.Policy.RowLabel = domain.RowLabelTab
	require.NoError(t, f.config.Save(context.Background(), changed))
	reply := f.handler(context.Background(), ports.ControlRequest{Cmd: ports.ControlReload})
	require.True(t, reply.OK)
	assert.Equal(t, domain.RowLabelTab, f.observer.Settings().Policy.RowLabel)
	assert.Len(t, f.settings, 2)
}

func TestDaemonResubscribesWhenItStartsMovingWorkspaces(t *testing.T) {
	f := newDaemonFixture(t)
	still := ports.DefaultSettings()
	still.Policy.NestFolders = false
	f.observer.UpdateSettings(still)
	require.NoError(t, f.config.Save(context.Background(), still))
	cancel := f.start(t)
	defer cancel()
	first := f.stream(t)
	assert.NotContains(t, first.kinds, domain.EventWorkspaceMoved, "Scope moves no workspace: nobody else's move is worth a read")

	require.NoError(t, f.config.Save(context.Background(), ports.DefaultSettings()))
	require.True(t, f.handler(context.Background(), ports.ControlRequest{Cmd: ports.ControlReload}).OK)
	second := f.stream(t)
	assert.Contains(t, second.kinds, domain.EventWorkspaceMoved, "nesting moves workspaces: a move by hand must be heard")
	assert.Contains(t, second.kinds, domain.EventWorkspaceReordered)
	eventually(t, func() bool {
		select {
		case _, open := <-first.events:
			return !open
		default:
			return false
		}
	}, "the retired stream was not closed")
	eventually(t, func() bool { return f.daemon.Status().Connected }, "the new stream never connected")

	require.True(t, f.handler(context.Background(), ports.ControlRequest{Cmd: ports.ControlReload}).OK)
	select {
	case <-f.events.opened:
		t.Fatal("a reload that changes nothing about moves resubscribed")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestARetiredPumpIsNotListenedTo(t *testing.T) {
	f := newDaemonFixture(t)
	f.daemon.pumps = 2
	f.daemon.setConnected(true)
	assert.False(t, f.daemon.handleSignal(context.Background(), pumpSignal{pump: 1}))
	assert.False(t, f.daemon.handleSignal(context.Background(), pumpSignal{pump: 1, gone: true}))
	assert.True(t, f.daemon.Status().Connected, "the old stream's end is not the new one's")
	assert.True(t, f.daemon.handleSignal(context.Background(), pumpSignal{pump: 2, gone: true}))
}

func TestSchedulerFloorAndDebounce(t *testing.T) {
	now := time.Now()
	s := scheduler{floor: frameFloor, debounce: wakeDebounce}
	s.wake(now)
	assert.Equal(t, wakeDebounce, s.delay(now))
	s.ran(now, now.Add(time.Hour))
	s.wake(now)
	assert.Equal(t, frameFloor, s.delay(now), "a wake right after a frame waits out the floor")
	assert.Equal(t, time.Duration(0), s.delay(now.Add(time.Second)))
}

func TestBackoffIsExponentialAndCapped(t *testing.T) {
	assert.Equal(t, time.Second, backoff(1))
	assert.Equal(t, 4*time.Second, backoff(3))
	assert.Equal(t, 30*time.Second, backoff(6))
	assert.Equal(t, 30*time.Second, backoff(50))
}
