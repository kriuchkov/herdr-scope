package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// fakeManaged records managed-block operations in order.
type fakeManaged struct {
	calls   *[]string
	state   ports.ManagedState
	changed bool
	ok      bool
	skipped []string
}

func (m fakeManaged) record(call string) {
	if m.calls != nil {
		*m.calls = append(*m.calls, call)
	}
}

func (m fakeManaged) result() ports.ManagedResult {
	return ports.ManagedResult{OK: m.ok, Changed: m.changed, Message: "msg", Skipped: m.skipped}
}

func (m fakeManaged) Inspect() (ports.ManagedReport, error) {
	m.record("inspect")
	return ports.ManagedReport{State: m.state}, nil
}

func (m fakeManaged) Apply() (ports.ManagedResult, error) {
	m.record("apply")
	return m.result(), nil
}

func (m fakeManaged) Remove() (ports.ManagedResult, error) {
	m.record("remove")
	return m.result(), nil
}

func (m fakeManaged) SetSidebarRows(on bool) (ports.ManagedResult, error) {
	if on {
		m.record("rows-on")
	} else {
		m.record("rows-off")
	}
	return m.result(), nil
}

func (m fakeManaged) ApplyAppearance(a ports.Appearance) (ports.ManagedResult, error) {
	m.record("appearance-" + string(a))
	return m.result(), nil
}

func (m fakeManaged) Refresh() (ports.ManagedResult, error) {
	m.record("refresh")
	return m.result(), nil
}

type fakeReloader struct{ calls *[]string }

func (r fakeReloader) ReloadConfig(context.Context) error {
	*r.calls = append(*r.calls, "reload")
	return nil
}

type orderRecorder struct {
	fakeSidebar

	calls *[]string
}

func (s *orderRecorder) SetOrder(ctx context.Context, mode domain.SortMode, family []domain.WorkspaceID) error {
	*s.calls = append(*s.calls, "order-"+string(mode))
	return s.fakeSidebar.SetOrder(ctx, mode, family)
}

func TestNativeToggleSwitchesRowsBeforeTheOrder(t *testing.T) {
	var calls []string
	sidebar := &orderRecorder{calls: &calls}
	toggle := SortModeToggle{
		Sidebar: sidebar, Store: &fakeStore{},
		Managed:  fakeManaged{calls: &calls, ok: true, changed: true},
		Reloader: fakeReloader{calls: &calls},
	}
	next, err := toggle.Switch(context.Background(), domain.SortActive, domain.SortNative, "")
	require.NoError(t, err)
	assert.Equal(t, domain.SortOff, next)
	assert.Equal(t, []string{"rows-off", "reload", "order-off"}, calls, "the socket call has to be the last word")

	calls = nil
	next, err = toggle.Switch(context.Background(), domain.SortOff, domain.SortNative, "")
	require.NoError(t, err)
	assert.Equal(t, domain.SortActive, next)
	assert.Equal(t, []string{"rows-on", "reload", "order-active"}, calls)
}

func TestFlipNeverTouchesTheRows(t *testing.T) {
	var calls []string
	toggle := SortModeToggle{Sidebar: &orderRecorder{calls: &calls}, Managed: fakeManaged{calls: &calls, ok: true}}
	next, err := toggle.Switch(context.Background(), domain.SortActive, domain.SortFlip, "")
	require.NoError(t, err)
	assert.Equal(t, domain.SortRecent, next)
	assert.Equal(t, []string{"order-recent"}, calls)
}

type fakeControl struct {
	reply    ports.ControlReply
	err      error
	requests []ports.ControlRequest
}

func (c *fakeControl) Request(_ context.Context, r ports.ControlRequest) (ports.ControlReply, error) {
	c.requests = append(c.requests, r)
	return c.reply, c.err
}

func TestToggleSortModeGoesThroughTheDaemon(t *testing.T) {
	control := &fakeControl{reply: ports.ControlReply{OK: true, Applied: true, Mode: "recent"}}
	sidebar := &fakeSidebar{}
	u := ToggleSortMode{Control: control, Store: &fakeStore{}, Toggle: SortModeToggle{Sidebar: sidebar}}
	mode, err := u.Execute(context.Background(), domain.SortFlip, "", false)
	require.NoError(t, err)
	assert.Equal(t, domain.SortRecent, mode)
	assert.Empty(t, sidebar.orders, "the daemon applied it; the client did not")
}

func TestReapplyDoesNothingWhileUnconfigured(t *testing.T) {
	control := &fakeControl{reply: ports.ControlReply{OK: true, Applied: true, Mode: "active"}}
	sidebar := &fakeSidebar{}
	u := ToggleSortMode{Control: control, Store: &fakeStore{}, Toggle: SortModeToggle{Sidebar: sidebar}, Off: &fakeOff{off: true}}
	_, err := u.Execute(context.Background(), "", "", true)
	require.ErrorIs(t, err, ErrNothingToReapply)
	assert.Empty(t, control.requests)
	assert.Empty(t, sidebar.orders, "the startup hook leaves Herdr's own order alone")
}

func TestToggleSortModeStandaloneStartsADaemon(t *testing.T) {
	store := &fakeStore{}
	sidebar := &fakeSidebar{}
	started := false
	u := ToggleSortMode{
		Control: &fakeControl{err: errors.New("no daemon")}, Store: store,
		Toggle: SortModeToggle{Sidebar: sidebar, Store: store},
		Start:  func(context.Context) error { started = true; return nil },
	}
	mode, err := u.Execute(context.Background(), domain.SortFlip, "", false)
	require.NoError(t, err)
	assert.Equal(t, domain.SortRecent, mode)
	assert.Equal(t, []domain.SortMode{domain.SortRecent}, sidebar.orders)
	assert.Equal(t, domain.SortRecent, store.mode)
	assert.True(t, started)
}

func TestReapplyRestoresWithoutChangingTheChoice(t *testing.T) {
	store := &fakeStore{mode: domain.SortRecent, ok: true}
	sidebar := &fakeSidebar{}
	u := ToggleSortMode{
		Control: &fakeControl{err: errors.New("no daemon")},
		Store:   store,
		Toggle:  SortModeToggle{Sidebar: sidebar, Store: store},
	}
	mode, err := u.Execute(context.Background(), "", "", true)
	require.NoError(t, err)
	assert.Equal(t, domain.SortRecent, mode)
	assert.Zero(t, store.saves)

	store.mode = domain.SortOff
	_, err = u.Execute(context.Background(), "", "", true)
	require.ErrorIs(t, err, ErrNothingToReapply)
	assert.Equal(t, domain.SortActive, CurrentSortMode(&fakeStore{}), "never chosen is active, not off")
}

func TestAKeyChoiceOutranksTheConfiguredOrder(t *testing.T) {
	store := &fakeStore{}
	choice := SortChoice{Store: store, Configured: func() (domain.SortMode, bool) { return domain.SortRecent, true }}
	assert.Equal(t, domain.SortRecent, CurrentSortMode(choice), "the file's display.sort before the default")

	require.NoError(t, choice.SaveSortMode(domain.SortOff))
	assert.Equal(t, domain.SortOff, CurrentSortMode(choice), "a choice made with the keys wins")
	assert.Equal(t, domain.SortActive, CurrentSortMode(SortChoice{Store: &fakeStore{}}), "with neither, the default")
}

func TestUpdateConfigurationValidatesBeforeSaving(t *testing.T) {
	config := &fakeConfig{settings: ports.DefaultSettings()}
	control := &fakeControl{reply: ports.ControlReply{OK: true}}
	u := UpdateConfiguration{Config: config, Control: control}

	bad := ports.DefaultSettings()
	bad.Presentation.Variant = "emoji"
	_, err := u.Execute(context.Background(), bad)
	require.ErrorIs(t, err, domain.ErrConfiguration)
	assert.Empty(t, config.version, "nothing was written")

	good := ports.DefaultSettings()
	good.Policy.GroupGap = false
	outcome, err := u.Execute(context.Background(), good)
	require.NoError(t, err)
	assert.True(t, outcome.Saved)
	assert.True(t, outcome.Applied)
	assert.Equal(t, ports.ControlReload, control.requests[0].Cmd)
}

type fakeAppearance struct{ value ports.Appearance }

func (a *fakeAppearance) Current(context.Context) (ports.Appearance, error) { return a.value, nil }

func TestAppearanceFollowerActsOncePerFlip(t *testing.T) {
	var calls []string
	source := &fakeAppearance{value: ports.AppearanceDark}
	f := AppearanceFollower{
		Source:   source,
		Managed:  fakeManaged{calls: &calls, ok: true, changed: true},
		Reloader: fakeReloader{calls: &calls},
	}
	changed, err := f.Sync(context.Background())
	require.NoError(t, err)
	assert.True(t, changed)
	_, _ = f.Sync(context.Background())
	source.value = ports.AppearanceLight
	_, _ = f.Sync(context.Background())
	source.value = ports.AppearanceUnknown
	_, _ = f.Sync(context.Background())
	assert.Equal(t, []string{"appearance-dark", "reload", "appearance-light", "reload"}, calls)
}

type fakeStamp struct {
	value string
	ok    bool
}

func (s *fakeStamp) Read() (string, bool) { return s.value, s.ok }
func (s *fakeStamp) Write(v string) error {
	s.value, s.ok = v, true
	return nil
}

type fakeFonts struct{ installed, installs int }

func (f *fakeFonts) Installed() bool { return f.installed > 0 }
func (f *fakeFonts) Install() ([]string, error) {
	f.installed++
	f.installs++
	return []string{"font: installed"}, nil
}
func (f *fakeFonts) Uninstall() ([]string, error)            { return nil, nil }
func (f *fakeFonts) ConfigureTerminals() ([]string, error)   { return []string{"ghostty: mapped"}, nil }
func (f *fakeFonts) UnconfigureTerminals() ([]string, error) { return nil, nil }

func TestSetupRunsOncePerBuild(t *testing.T) {
	var calls []string
	stamp := &fakeStamp{}
	fonts := &fakeFonts{}
	s := Setup{
		Managed:  fakeManaged{calls: &calls, state: ports.ManagedInstallable, ok: true},
		Fonts:    fonts,
		Stamp:    stamp,
		Reloader: fakeReloader{calls: &calls},
		Build:    "b1",
	}
	notes := s.Ensure(context.Background(), false)
	assert.Contains(t, notes, "font: installed")
	assert.Equal(t, []string{"inspect", "apply", "reload"}, calls)

	calls = nil
	assert.Empty(t, s.Ensure(context.Background(), false), "a stamped build does nothing")
	assert.Empty(t, calls)

	s.Build = "b2"
	s.Managed = fakeManaged{calls: &calls, state: ports.ManagedInstalled, ok: true}
	s.Ensure(context.Background(), false)
	assert.Equal(t, []string{"inspect"}, calls, "installed blocks are not rewritten by setup")
	assert.Equal(t, 1, fonts.installs)
}

func TestOnlyAnInstallTurnsScopeBackOn(t *testing.T) {
	var calls []string
	off := &fakeOff{off: true}
	s := Setup{
		Managed:  fakeManaged{calls: &calls, state: ports.ManagedInstalled, ok: true},
		Stamp:    &fakeStamp{},
		Reloader: fakeReloader{calls: &calls},
		Off:      off,
		Build:    "b1",
	}
	s.Ensure(context.Background(), true)
	assert.True(t, off.off, "the startup hook's setup leaves the off switch alone")

	_, err := s.Install(context.Background(), false)
	require.NoError(t, err)
	assert.False(t, off.off, "an install or update is the user asking for Scope")
}

type fakeProcess struct {
	spawns     []string
	terminated []int
}

func (p *fakeProcess) Spawn(_ context.Context, note string) error {
	p.spawns = append(p.spawns, note)
	return nil
}

func (p *fakeProcess) Terminate(_ context.Context, pid int) error {
	p.terminated = append(p.terminated, pid)
	return nil
}

func noSleep(context.Context, time.Duration) bool { return true }

func TestEnsureObserver(t *testing.T) {
	healthy := &fakeControl{reply: ports.ControlReply{OK: true, Status: &ports.DaemonStatus{PID: 7}}}
	process := &fakeProcess{}
	notes, err := EnsureObserver{Control: healthy, Process: process, Sleep: noSleep}.Execute(context.Background())
	require.NoError(t, err)
	assert.Contains(t, notes, "daemon: already running")
	assert.Empty(t, process.spawns)

	none := &fakeControl{err: errors.New("refused")}
	_, err = EnsureObserver{Control: none, Process: process, Sleep: noSleep}.Execute(context.Background())
	require.NoError(t, err)
	assert.Len(t, process.spawns, 1)

	stalled := &fakeControl{reply: ports.ControlReply{OK: true, Status: &ports.DaemonStatus{PID: 9, FireAge: time.Minute}}}
	notes, err = EnsureObserver{Control: stalled, Process: process, Sleep: noSleep}.Execute(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []int{9}, process.terminated, "a stalled daemon is ended by the pid it reported")
	assert.Len(t, process.spawns, 2)
	assert.Contains(t, notes[len(notes)-1], "replaced a stalled daemon: pid 9")
}

func TestEnsureObserverRestartsADaemonOfAnotherBuild(t *testing.T) {
	for _, tc := range []struct {
		name, running string
		restart       bool
	}{
		{"same build", "new", false},
		{"rebuilt bin/", "old", true},
		{"a daemon older than the field", "", true},
	} {
		control := &fakeControl{reply: ports.ControlReply{OK: true, Status: &ports.DaemonStatus{PID: 7, Build: tc.running}}}
		process := &fakeProcess{}
		stops := 0
		notes, err := EnsureObserver{
			Control: control, Process: process, Sleep: noSleep, Build: "new",
			Stop: func(context.Context) error { stops++; return nil },
		}.Execute(context.Background())
		require.NoError(t, err, tc.name)
		if !tc.restart {
			assert.Contains(t, notes, "daemon: already running", tc.name)
			assert.Zero(t, stops, tc.name)
			continue
		}
		assert.Equal(t, 1, stops, tc.name)
		assert.Equal(t, []string{NoteRestarted}, process.spawns, tc.name)
		assert.Contains(t, notes, NoteRestarted, tc.name)
	}
}

type fakeOff struct{ off bool }

func (o *fakeOff) Off() bool { return o.off }
func (o *fakeOff) SetOff(off bool) error {
	o.off = off
	return nil
}

func TestEnsureObserverLeavesAnUnconfiguredScopeToTheUser(t *testing.T) {
	var calls []string
	setup := &Setup{
		Managed: fakeManaged{calls: &calls, state: ports.ManagedInstallable, ok: true},
		Stamp:   &fakeStamp{value: "b1", ok: true}, Build: "b1",
	}
	none := &fakeControl{err: errors.New("refused")}
	process := &fakeProcess{}
	off := &fakeOff{off: true}

	notes, err := EnsureObserver{Setup: setup, Control: none, Process: process, Off: off, Hook: true, Sleep: noSleep}.
		Execute(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{NoteOff}, notes, "a watchdog or startup hook does not bring it back")
	assert.Empty(t, process.spawns)
	assert.Empty(t, calls, "nor does it touch config.toml")
	assert.True(t, off.off)

	notes, err = EnsureObserver{Setup: setup, Control: none, Process: process, Off: off, Sleep: noSleep}.
		Execute(context.Background())
	require.NoError(t, err)
	assert.False(t, off.off, "the user's start turns it back on")
	assert.Equal(t, []string{"inspect", "apply"}, calls, "with its blocks, although this build was set up before")
	assert.Len(t, process.spawns, 1)
	assert.Contains(t, notes, "daemon: started")
}

func TestDaemonHealthThresholds(t *testing.T) {
	hung := &fakeControl{reply: ports.ControlReply{OK: true, Status: &ports.DaemonStatus{FrameRunning: 6 * time.Minute}}}
	health, _, reason := DaemonHealth(context.Background(), hung)
	assert.Equal(t, HealthStalled, health)
	assert.Contains(t, reason, "one frame has been running")
	slow := &fakeControl{reply: ports.ControlReply{OK: true, Status: &ports.DaemonStatus{FrameRunning: time.Minute}}}
	health, _, _ = DaemonHealth(context.Background(), slow)
	assert.Equal(t, HealthHealthy, health, "a slow Herdr is not a stall")
}

func TestStopObserverClearsDirectlyWithoutADaemon(t *testing.T) {
	sidebar := &fakeSidebar{}
	err := StopObserver{
		Control: &fakeControl{err: errors.New("refused")},
		Process: &fakeProcess{},
		Sidebar: sidebar,
		Sleep:   noSleep,
	}.Execute(
		context.Background(),
		true,
	)
	require.NoError(t, err)
	assert.Equal(t, []bool{true}, sidebar.clears)
}

func TestUnconfigureStopsThenRemoves(t *testing.T) {
	var calls []string
	u := Unconfigure{
		Stop: StopObserver{
			Control: &fakeControl{err: errors.New("refused")},
			Process: &fakeProcess{},
			Sidebar: &fakeSidebar{},
			Sleep:   noSleep,
		},
		Managed:  fakeManaged{calls: &calls, ok: true},
		Reloader: fakeReloader{calls: &calls},
	}
	notes, err := u.Execute(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "daemon: stopped, tokens cleared", notes[0])
	assert.Equal(t, []string{"remove", "reload"}, calls)

	off := &fakeOff{}
	u.Off = off
	_, err = u.Execute(context.Background())
	require.NoError(t, err)
	assert.True(t, off.off, "Herdr's hooks must leave it off from now on")

	configure := Configure{Managed: fakeManaged{calls: &calls, ok: true}, Off: off}
	_, err = configure.Execute(context.Background())
	require.NoError(t, err)
	assert.False(t, off.off, "configure turns it back on")
}

func TestConfigureNotifiesOnSkippedBlocks(t *testing.T) {
	var calls []string
	notifier := &fakeNotifier{}
	u := Configure{
		Managed:  fakeManaged{calls: &calls, ok: true, skipped: []string{"ui.sidebar.agents"}},
		Reloader: fakeReloader{calls: &calls},
		Notifier: notifier,
	}
	_, err := u.Execute(context.Background())
	require.NoError(t, err)
	assert.Len(t, notifier.notes, 1)
	assert.Equal(t, []string{"apply", "reload"}, calls)
}

type fakeNotifier struct{ notes []string }

func (n *fakeNotifier) Notify(_ context.Context, title, _ string) { n.notes = append(n.notes, title) }

type fakeFocus struct {
	focused   domain.PaneID
	workspace domain.WorkspaceID
	moved     []domain.PaneID
}

func (f *fakeFocus) FocusedPane(context.Context) (domain.PaneID, domain.WorkspaceID, error) {
	return f.focused, f.workspace, nil
}

func (f *fakeFocus) FocusPane(_ context.Context, pane domain.PaneID) error {
	f.moved = append(f.moved, pane)
	f.focused = pane
	return nil
}

func TestJumpToWaitingGoesRoundTheList(t *testing.T) {
	control := &fakeControl{reply: ports.ControlReply{OK: true, Panes: []domain.PaneID{"ask1", "ask2", "done1"}}}
	focus := &fakeFocus{focused: "elsewhere"}
	jump := JumpToWaiting{Control: control, Focus: focus}
	for range 4 {
		_, err := jump.Execute(context.Background())
		require.NoError(t, err)
	}
	assert.Equal(t, []domain.PaneID{"ask1", "ask2", "done1", "ask1"}, focus.moved,
		"from outside the list to its head, then each next one, round again")
	assert.Equal(t, ports.ControlWaiting, control.requests[0].Cmd)

	control.reply.Panes = nil
	note, err := jump.Execute(context.Background())
	require.NoError(t, err)
	assert.Equal(t, NoteNobodyWaits, note)
	assert.Len(t, focus.moved, 4, "nobody waits: the focus stays")

	_, err = JumpToWaiting{Control: &fakeControl{err: errors.New("refused")}, Focus: focus}.Execute(context.Background())
	require.ErrorIs(t, err, ErrNotRunning)
	_, err = JumpToWaiting{Control: &fakeControl{reply: ports.ControlReply{OK: false, Error: "unknown_command"}}, Focus: focus}.
		Execute(context.Background())
	require.ErrorIs(t, err, ErrNotRunning, "a daemon too old to answer")
}

func TestFoldFamilyNamesTheFocusedWorkspace(t *testing.T) {
	control := &fakeControl{reply: ports.ControlReply{OK: true, Family: "acme", Folded: true, Mode: "active"}}
	fold := FoldFamily{Control: control, Focus: &fakeFocus{workspace: "w7"}}
	note, err := fold.Execute(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "fold: acme folded", note)
	assert.Equal(t, ports.ControlRequest{Cmd: ports.ControlFold, Workspace: "w7"}, control.requests[0])

	control.reply = ports.ControlReply{OK: true, Family: "acme", Mode: "recent"}
	note, err = fold.Execute(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "fold: acme unfolded (the Agents panel folds in the active and here orders)", note)

	_, err = FoldFamily{Control: &fakeControl{err: errors.New("refused")}}.Execute(context.Background())
	require.ErrorIs(t, err, ErrNotRunning)
	_, err = FoldFamily{Control: &fakeControl{reply: ports.ControlReply{Error: "no workspace"}}}.Execute(context.Background())
	require.ErrorIs(t, err, domain.ErrConfiguration)
}
