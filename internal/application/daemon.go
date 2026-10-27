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

// MinProtocol is the oldest Herdr socket protocol Scope supports.
const MinProtocol = 22

// Scheduler and housekeeping timings.
const (
	// frameFloor keeps our own token writes, which may echo back as events,
	// from scheduling frames forever.
	frameFloor = 120 * time.Millisecond
	// wakeDebounce lets a burst of events collapse into one frame.
	wakeDebounce = 50 * time.Millisecond
	// housekeeping is the tick that runs periodic tasks.
	housekeeping = time.Second
	// viewReassert is how long the panel can sit in Herdr's own order after
	// the server lost the sort override.
	viewReassert = 15 * time.Second
	// tabBarRefresh is how often the tab-bar line is recomputed.
	tabBarRefresh = 2 * time.Second
	// appearanceProbe is how often the desktop appearance is checked.
	appearanceProbe = time.Minute
	// eventBuffer is how many events may queue before the pump coalesces.
	eventBuffer = 256
	retryBase   = 500 * time.Millisecond
	retryCap    = 30 * time.Second
)

// ErrServerGone means Herdr's endpoint disappeared: the server is not coming
// back on it, and the next server start runs the startup hook again.
var ErrServerGone = errors.New("herdr server is gone")

// SubscribedKinds are the lifecycle events the daemon wakes for. Status and
// title changes have no cheap event (see docs/protocol.md); the heartbeat
// covers them.
func SubscribedKinds(movesWorkspaces bool) []domain.EventKind {
	kinds := []domain.EventKind{
		domain.EventPaneCreated, domain.EventPaneClosed, domain.EventPaneExited,
		domain.EventAgentDetected, domain.EventPaneFocused, domain.EventPaneMoved,
		domain.EventWorkspaceCreated, domain.EventWorkspaceClosed, domain.EventWorkspaceRenamed,
		domain.EventWorkspaceFocused,
		domain.EventTabCreated, domain.EventTabClosed, domain.EventTabRenamed, domain.EventTabMoved,
	}
	if movesWorkspaces {
		kinds = append(kinds, domain.EventWorkspaceMoved, domain.EventWorkspaceReordered)
	}
	return kinds
}

// DaemonDeps are the daemon's collaborators. Everything after Logger is
// optional.
type DaemonDeps struct {
	Observer  *Observer
	Events    ports.EventSource
	Probe     ports.ServerProbe
	Control   ports.ControlServer
	Config    ports.ConfigRepository
	SortModes ports.SortModeStore
	Sidebar   ports.SidebarWriter
	Clock     ports.Clock
	Logger    ports.Logger

	Managed    ports.ManagedConfig
	Reloader   ports.HostReloader
	Appearance *AppearanceFollower
	TabBar     ports.TabBarPublisher
	// OnSettings is told about every settings change, the initial one too.
	OnSettings []func(ports.Settings)

	Version    string
	Build      string
	PID        int
	StatePath  string
	ConfigPath string
}

// Daemon is the resident observer: one loop that owns the observer and the
// sidebar for as long as Herdr runs.
type Daemon struct {
	deps DaemonDeps

	requests chan controlCall
	events   chan pumpSignal
	// stopPump retires the event subscription; pumps counts the ones started
	// and pumpMoves says whether the current one hears workspace moves. Only
	// the loop touches them.
	stopPump  context.CancelFunc
	pumps     int
	pumpMoves bool

	mu      sync.Mutex
	status  ports.DaemonStatus
	started time.Time
	fireAt  time.Time
	frameAt time.Time
	inFrame bool
}

type controlCall struct {
	request ports.ControlRequest
	reply   chan ports.ControlReply
}

type pumpSignal struct {
	// pump is the subscription that speaks (Daemon.pumps); a retired one's
	// word is ignored.
	pump      int
	event     *domain.AgentEvent
	connected bool
	reconnect bool
	gone      bool
}

// NewDaemon wires a daemon.
func NewDaemon(deps DaemonDeps) *Daemon {
	return &Daemon{
		deps:     deps,
		requests: make(chan controlCall),
		events:   make(chan pumpSignal, eventBuffer),
	}
}

// ProtocolTooOldError refuses a Herdr whose protocol predates what Scope
// speaks: running a partial implementation against it would be guesswork.
type ProtocolTooOldError struct {
	Required, Detected int
}

func (e *ProtocolTooOldError) Error() string {
	return fmt.Sprintf("Herdr Scope requires Herdr protocol %d+.\nDetected protocol %d.", e.Required, e.Detected)
}

// Unwrap classifies it as a protocol error.
func (e *ProtocolTooOldError) Unwrap() error { return domain.ErrProtocol }

// CheckProtocol refuses a Herdr that is too old to run against. A newer one
// is accepted: Herdr's clients tolerate unknown fields, and a real break
// surfaces as a decode error naming the method.
func CheckProtocol(ctx context.Context, probe ports.ServerProbe) (domain.ServerInfo, error) {
	info, err := probe.Ping(ctx)
	if err != nil {
		return info, fmt.Errorf("ping: %w", err)
	}
	if info.Protocol < MinProtocol {
		return info, &ProtocolTooOldError{Required: MinProtocol, Detected: info.Protocol}
	}
	return info, nil
}

// Run is StartObserver: it runs the daemon until ctx ends, a stop request
// arrives, or the server goes away. The startup order is the one the
// protocol needs: protocol check, agent list, session snapshot, event
// subscription, reconcile, live processing.
func (d *Daemon) Run(ctx context.Context) error {
	info, err := CheckProtocol(ctx, d.deps.Probe)
	if err != nil {
		return err
	}
	now := d.deps.Clock.Now()
	d.mu.Lock()
	d.started, d.fireAt = now, now
	d.status.ServerVersion, d.status.Protocol = info.Version, info.Protocol
	d.mu.Unlock()
	d.deps.Logger.Info("daemon starting", "herdr_version", info.Version, "protocol", info.Protocol, "pid", d.deps.PID)

	obs := d.deps.Observer
	if listErr := obs.ObserveAgents(ctx); listErr != nil {
		d.deps.Logger.Warn("initial agent list failed", "error", listErr)
	}
	if snapErr := obs.ReconcileAgents(ctx); snapErr != nil {
		d.deps.Logger.Warn("initial snapshot failed", "error", snapErr)
	}
	d.notifySettings(obs.Settings())
	d.refreshManaged(ctx)
	d.applyOrder(ctx)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	d.subscribe(runCtx, obs.Settings().MovesWorkspaces())
	if d.deps.Control != nil {
		go func() {
			if serveErr := d.deps.Control.Serve(runCtx, d.handle); serveErr != nil && runCtx.Err() == nil {
				d.deps.Logger.Error("control channel failed", "error", serveErr)
			}
		}()
	}
	return d.loop(runCtx)
}

func (d *Daemon) loop(ctx context.Context) error {
	obs := d.deps.Observer
	sched := scheduler{floor: frameFloor, debounce: wakeDebounce}
	sched.wake(d.deps.Clock.Now())
	timer := time.NewTimer(0)
	defer timer.Stop()
	tick := time.NewTicker(housekeeping)
	defer tick.Stop()
	tasks := d.periodicTasks()
	configVersion := ""
	if d.deps.Config != nil {
		configVersion = d.deps.Config.Version()
	}

	for {
		timer.Reset(sched.delay(d.deps.Clock.Now()))
		select {
		case <-ctx.Done():
			obs.Flush(context.WithoutCancel(ctx))
			d.deps.Logger.Info("daemon stopping", "reason", "signal")
			return nil
		case signal := <-d.events:
			if d.handleSignal(ctx, signal) {
				obs.Flush(context.WithoutCancel(ctx))
				return ErrServerGone
			}
			sched.wake(d.deps.Clock.Now())
		case call := <-d.requests:
			reply, stop := d.handleCommand(ctx, call.request)
			if stop {
				obs.Flush(context.WithoutCancel(ctx))
				call.reply <- reply
				return nil
			}
			call.reply <- reply
			sched.wake(d.deps.Clock.Now())
		case <-tick.C:
			now := d.deps.Clock.Now()
			d.mu.Lock()
			d.fireAt = now
			d.mu.Unlock()
			for i := range tasks {
				tasks[i].maybeRun(ctx, now)
			}
			if d.deps.Config != nil && d.deps.Config.Version() != configVersion {
				configVersion = d.deps.Config.Version()
				d.reload(ctx)
				sched.wake(now)
			}
		case <-timer.C:
			d.frame(ctx, &sched)
		}
	}
}

func (d *Daemon) frame(ctx context.Context, sched *scheduler) {
	now := d.deps.Clock.Now()
	d.mu.Lock()
	d.fireAt, d.frameAt, d.inFrame = now, now, true
	d.mu.Unlock()
	result, err := d.deps.Observer.Frame(ctx)
	if err != nil {
		d.deps.Logger.Warn("frame skipped", "error", err)
	}
	after := d.deps.Clock.Now()
	sched.ran(after, result.Next)
	d.publishStatus(after)
}

// handleSignal reacts to the event pump. It reports whether the server is
// gone.
func (d *Daemon) handleSignal(ctx context.Context, signal pumpSignal) bool {
	if signal.pump != d.pumps {
		// A retired subscription: the one that replaced it reads everything
		// back when it connects.
		return false
	}
	switch {
	case signal.gone:
		d.deps.Logger.Info("daemon stopping", "reason", "server gone")
		return true
	case signal.connected:
		d.setConnected(true)
		d.deps.Observer.Invalidate()
		if signal.reconnect {
			// A live handoff keeps the socket path and swaps the server; the
			// new one holds none of our tokens or the sort override.
			d.deps.Logger.Info("event stream reconnected; resyncing")
			d.deps.Sidebar.Resync()
			d.applyOrder(ctx)
			d.refreshManaged(ctx)
		}
	case signal.event != nil:
		d.deps.Observer.ProcessAgentEvent(*signal.event)
	default:
		d.setConnected(false)
	}
	return false
}

func (d *Daemon) handleCommand(ctx context.Context, request ports.ControlRequest) (ports.ControlReply, bool) {
	switch request.Cmd {
	case ports.ControlView:
		return d.switchView(ctx, request), false
	case ports.ControlReload:
		d.reload(ctx)
		return ports.ControlReply{OK: true}, false
	case ports.ControlStop:
		if err := d.deps.Sidebar.Clear(ctx, request.Purge); err != nil {
			d.deps.Logger.Warn("clearing the sidebar failed", "error", err)
		}
		d.deps.Logger.Info("daemon stopping", "reason", "stop request", "purge", request.Purge)
		return ports.ControlReply{OK: true, Stopped: true}, true
	case ports.ControlPing:
		return ports.ControlReply{OK: true, Status: d.Status()}, false
	case ports.ControlWaiting:
		return ports.ControlReply{OK: true, Panes: d.deps.Observer.Waiting()}, false
	case ports.ControlFold:
		family, folded, err := d.deps.Observer.ToggleFold(request.Workspace)
		reply := ports.ControlReply{OK: err == nil, Family: family, Folded: folded, Mode: string(d.deps.Observer.Mode())}
		if err != nil {
			d.deps.Logger.Warn("fold failed", "error", err)
			reply.Error = err.Error()
		}
		return reply, false
	}
	return ports.ControlReply{OK: false, Error: "unknown_command"}, false
}

// switchView is ToggleSortMode inside the daemon.
func (d *Daemon) switchView(ctx context.Context, request ports.ControlRequest) ports.ControlReply {
	obs := d.deps.Observer
	toggle := SortModeToggle{
		Sidebar: d.deps.Sidebar, Store: d.deps.SortModes, Managed: d.deps.Managed, Reloader: d.deps.Reloader,
		Family: obs.HereFamily,
	}
	next, err := toggle.Switch(ctx, obs.Mode(), request.Op, request.Set)
	if err != nil {
		d.deps.Logger.Warn("view switch failed", "error", err)
		return ports.ControlReply{OK: true, Mode: string(obs.Mode()), Applied: false, Error: err.Error()}
	}
	obs.SetMode(next)
	d.mu.Lock()
	d.status.Mode = string(next)
	d.mu.Unlock()
	return ports.ControlReply{OK: true, Mode: string(next), Applied: true}
}

// reload is UpdateConfiguration's daemon half: re-read, apply, repaint.
func (d *Daemon) reload(ctx context.Context) {
	if d.deps.Config == nil {
		return
	}
	settings, warnings, err := d.deps.Config.Load(ctx)
	if err != nil {
		d.deps.Logger.Warn("config reload failed; keeping the previous configuration", "error", err)
		return
	}
	for _, warning := range warnings {
		d.deps.Logger.Warn("config", "warning", warning)
	}
	d.deps.Observer.UpdateSettings(settings)
	d.notifySettings(settings)
	if settings.MovesWorkspaces() != d.pumpMoves {
		// A stream hears the kinds it was opened for, and whether Scope moves
		// workspaces decides whether it must hear others move them.
		d.subscribe(ctx, settings.MovesWorkspaces())
	}
	if d.deps.SortModes != nil {
		d.deps.Observer.SetMode(CurrentSortMode(d.deps.SortModes))
		d.applyOrder(ctx)
	}
	d.deps.Sidebar.Resync()
	d.refreshManaged(ctx)
	d.deps.Logger.Info("configuration reloaded")
}

func (d *Daemon) notifySettings(settings ports.Settings) {
	for _, listener := range d.deps.OnSettings {
		listener(settings)
	}
}

// refreshManaged rewrites installed managed blocks that drifted from what
// this version and configuration write (glyph variant, colours, tab bar).
func (d *Daemon) refreshManaged(ctx context.Context) {
	if d.deps.Managed == nil {
		return
	}
	result, err := d.deps.Managed.Refresh()
	if err != nil {
		d.deps.Logger.Warn("managed block refresh failed", "error", err)
		return
	}
	if result.Changed && d.deps.Reloader != nil {
		if reloadErr := d.deps.Reloader.ReloadConfig(ctx); reloadErr != nil {
			d.deps.Logger.Warn("config reload failed", "error", reloadErr)
		}
	}
}

func (d *Daemon) applyOrder(ctx context.Context) {
	if err := d.deps.Sidebar.SetOrder(ctx, d.deps.Observer.Mode(), d.deps.Observer.HereFamily()); err != nil {
		d.deps.Logger.Warn("panel order not applied", "error", err)
	}
}

// subscribe starts an event pump, retiring the one before: it ends with its
// context, and what it may still say is ignored (pumpSignal.pump).
func (d *Daemon) subscribe(ctx context.Context, movesWorkspaces bool) {
	if d.stopPump != nil {
		d.stopPump()
	}
	pumpCtx, stop := context.WithCancel(ctx)
	d.pumps++
	d.stopPump, d.pumpMoves = stop, movesWorkspaces
	go d.pump(pumpCtx, d.pumps, movesWorkspaces)
}

// pump owns the event subscription: it reconnects forever with exponential
// backoff, because a reload hiccup or a live handoff is a disconnect too.
func (d *Daemon) pump(ctx context.Context, id int, movesWorkspaces bool) {
	send := func(signal pumpSignal) {
		signal.pump = id
		d.send(ctx, signal)
	}
	attempts := 0
	connectedOnce := false
	for ctx.Err() == nil {
		stream, err := d.deps.Events.Subscribe(ctx, SubscribedKinds(movesWorkspaces))
		if err != nil {
			if d.deps.Probe.Gone() {
				send(pumpSignal{gone: true})
				return
			}
			attempts++
			d.deps.Logger.Debug("event subscription failed", "error", err, "attempt", attempts)
			if !sleepCtx(ctx, backoff(attempts)) {
				return
			}
			continue
		}
		attempts = 0
		send(pumpSignal{connected: true, reconnect: connectedOnce})
		connectedOnce = true
		retire := context.AfterFunc(ctx, func() { _ = stream.Close() })
		for event := range stream.Events() {
			send(pumpSignal{event: &event})
		}
		retire()
		_ = stream.Close()
		send(pumpSignal{})
		if d.deps.Probe.Gone() {
			send(pumpSignal{gone: true})
			return
		}
		d.deps.Logger.Debug("event stream ended", "error", stream.Err())
		attempts++
		if !sleepCtx(ctx, backoff(attempts)) {
			return
		}
	}
}

func (d *Daemon) send(ctx context.Context, signal pumpSignal) {
	select {
	case d.events <- signal:
	case <-ctx.Done():
	}
}

// handle answers control requests. Ping is answered from the published
// status so a daemon stuck in a slow frame still says so; everything else
// runs on the loop, between frames.
func (d *Daemon) handle(ctx context.Context, request ports.ControlRequest) ports.ControlReply {
	if request.Cmd == ports.ControlPing {
		return ports.ControlReply{OK: true, Status: d.Status()}
	}
	call := controlCall{request: request, reply: make(chan ports.ControlReply, 1)}
	select {
	case d.requests <- call:
	case <-ctx.Done():
		return ports.ControlReply{OK: false, Error: "stopping"}
	}
	select {
	case reply := <-call.reply:
		return reply
	case <-ctx.Done():
		return ports.ControlReply{OK: false, Error: "stopping"}
	}
}

// Status is the daemon's self-report.
func (d *Daemon) Status() *ports.DaemonStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.deps.Clock.Now()
	status := d.status
	status.PID = d.deps.PID
	status.Version = d.deps.Version
	status.Build = d.deps.Build
	status.Uptime = now.Sub(d.started)
	status.FireAge = now.Sub(d.fireAt)
	if d.inFrame {
		status.FrameRunning = now.Sub(d.frameAt)
	}
	status.StatePath, status.ConfigPath = d.deps.StatePath, d.deps.ConfigPath
	return &status
}

func (d *Daemon) publishStatus(now time.Time) {
	stats := d.deps.Observer.Stats()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inFrame = false
	d.fireAt = now
	d.status.Agents = stats.Agents
	d.status.Mode = string(d.deps.Observer.Mode())
	d.status.LastEvent = stats.LastEvent
	d.status.LastReconcile = stats.LastReconcile
	d.status.LastSidebar = stats.LastSidebar
	d.status.LastError = stats.LastError
	d.status.EventsReceived = stats.Events
	d.status.Frames = stats.Frames
	if stats.Server.Protocol != 0 {
		d.status.ServerVersion, d.status.Protocol = stats.Server.Version, stats.Server.Protocol
	}
}

func (d *Daemon) setConnected(connected bool) {
	d.mu.Lock()
	d.status.Connected = connected
	d.mu.Unlock()
}

// periodic is a task run every interval from the housekeeping tick.
type periodic struct {
	every time.Duration
	last  time.Time
	run   func(ctx context.Context)
}

func (p *periodic) maybeRun(ctx context.Context, now time.Time) {
	if !p.last.IsZero() && now.Sub(p.last) < p.every {
		return
	}
	p.last = now
	p.run(ctx)
}

func (d *Daemon) periodicTasks() []periodic {
	tasks := []periodic{{every: viewReassert, last: d.deps.Clock.Now(), run: d.applyOrder}}
	if d.deps.TabBar != nil {
		tasks = append(tasks, periodic{every: tabBarRefresh, run: func(ctx context.Context) {
			if err := d.deps.TabBar.Publish(ctx, d.deps.Sidebar.Badge(d.deps.Observer.View())); err != nil {
				d.deps.Logger.Debug("tab bar refresh failed", "error", err)
			}
		}})
	}
	if d.deps.Managed != nil {
		// The rows are written for the side Herdr's theme is on; a name edited
		// by hand, or the desktop standing in under auto_switch, moves it.
		// Refresh writes only when the blocks drifted. With follow_appearance
		// on, the follower below does this on a flip instead.
		tasks = append(tasks, periodic{every: appearanceProbe, last: d.deps.Clock.Now(), run: func(ctx context.Context) {
			if !d.deps.Observer.Settings().FollowAppearance {
				d.refreshManaged(ctx)
			}
		}})
	}
	if d.deps.Appearance != nil {
		tasks = append(tasks, periodic{every: appearanceProbe, run: func(ctx context.Context) {
			if !d.deps.Observer.Settings().FollowAppearance {
				return
			}
			if _, err := d.deps.Appearance.Sync(ctx); err != nil {
				d.deps.Logger.Debug("appearance sync failed", "error", err)
			}
		}})
	}
	return tasks
}

// scheduler aims one timer at the earliest thing that needs doing. Wakes
// pull it closer (debounced); a finished frame pushes it out to the next real
// deadline; a floor separates frames.
type scheduler struct {
	floor, debounce time.Duration
	due             time.Time
	lastFrame       time.Time
}

func (s *scheduler) wake(now time.Time) {
	s.at(now.Add(s.debounce))
}

func (s *scheduler) at(t time.Time) {
	if s.due.IsZero() || t.Before(s.due) {
		s.due = t
	}
}

func (s *scheduler) ran(now, next time.Time) {
	s.lastFrame = now
	s.due = next
}

func (s *scheduler) delay(now time.Time) time.Duration {
	due := s.due
	if floorAt := s.lastFrame.Add(s.floor); !s.lastFrame.IsZero() && due.Before(floorAt) {
		due = floorAt
	}
	return max(0, due.Sub(now))
}

func backoff(attempts int) time.Duration {
	delay := retryBase << min(attempts, 6)
	return min(delay, retryCap)
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
