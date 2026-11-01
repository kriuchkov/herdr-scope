package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// Health thresholds for a daemon that answers but may not be drawing.
const (
	// StalledFireAge: the daemon's housekeeping tick fires every second
	// whatever else happens, so this long without is a dead loop.
	StalledFireAge = 30 * time.Second
	// HungFrame: a frame waits on host calls that each time out in seconds;
	// one this long is past any frame that is still making progress.
	HungFrame = 5 * time.Minute
	// stallConfirm is how long a stall is re-checked before acting: right
	// after a system resume a healthy daemon can look silent.
	stallConfirm = 3 * time.Second
	exitWait     = 3 * time.Second
	exitPoll     = 100 * time.Millisecond
)

// Health is a daemon's state as its control endpoint reports it.
type Health string

// Health states.
const (
	HealthNone    Health = "none"
	HealthHealthy Health = "healthy"
	HealthStalled Health = "stalled"
)

// DaemonHealth asks the daemon itself. A pid file can name a recycled pid;
// only our daemon can answer our endpoint.
func DaemonHealth(ctx context.Context, control ports.DaemonControl) (Health, *ports.DaemonStatus, string) {
	reply, err := control.Request(ctx, ports.ControlRequest{Cmd: ports.ControlPing})
	if err != nil || !reply.OK || reply.Status == nil {
		return HealthNone, nil, ""
	}
	status := reply.Status
	if status.FireAge > StalledFireAge {
		return HealthStalled, status, fmt.Sprintf("its timer has not fired for %s", status.FireAge.Round(time.Second))
	}
	if status.FrameRunning > HungFrame {
		return HealthStalled, status, fmt.Sprintf("one frame has been running for %s", status.FrameRunning.Round(time.Second))
	}
	return HealthHealthy, status, ""
}

// Setup is first-run setup: what `herdr plugin install` alone does not do.
// It writes the managed blocks, installs the font and maps codepoints, then
// stamps the build so a later start does not redo — or undo — what the user
// changed by hand since.
type Setup struct {
	Managed  ports.ManagedConfig
	Fonts    ports.FontInstaller
	Stamp    ports.SetupStamp
	Reloader ports.HostReloader
	Notifier ports.Notifier
	// Off is cleared by Install: an install or update is the user asking
	// for Scope.
	Off ports.OffSwitch
	// Build identifies this release; a new one sets up again.
	Build string
}

// Install is setup run by an install or update: it turns Scope back on
// before Ensure.
func (s Setup) Install(ctx context.Context, force bool) ([]string, error) {
	if s.Off != nil {
		if err := s.Off.SetOff(false); err != nil {
			return nil, fmt.Errorf("setup: %w", err)
		}
	}
	return s.Ensure(ctx, force), nil
}

// Ensure runs setup when it has not run for this build, or when forced. It
// returns notes for the plugin log.
func (s Setup) Ensure(ctx context.Context, force bool) []string {
	var notes []string
	done, stamped := s.Stamp.Read()
	fontMissing := s.Fonts != nil && !s.Fonts.Installed()
	if !force && stamped && done == s.Build && !fontMissing {
		return notes
	}
	notes = append(notes, s.configure(ctx)...)
	if fontMissing {
		installed, err := s.Fonts.Install()
		notes = append(notes, installed...)
		if err != nil {
			notes = append(notes, "font: "+err.Error())
		} else {
			mapped, mapErr := s.Fonts.ConfigureTerminals()
			notes = append(notes, mapped...)
			if mapErr != nil {
				notes = append(notes, "terminal: "+mapErr.Error())
			}
		}
	}
	if err := s.Stamp.Write(s.Build); err != nil {
		notes = append(notes, "setup: stamp not written: "+err.Error())
	}
	return notes
}

func (s Setup) configure(ctx context.Context) []string {
	report, err := s.Managed.Inspect()
	if err != nil {
		return []string{"herdr: " + err.Error()}
	}
	switch report.State {
	case ports.ManagedInstallable:
		result, applyErr := s.Managed.Apply()
		if applyErr != nil {
			return []string{"herdr: " + applyErr.Error()}
		}
		notes := []string{result.Message}
		if result.OK && s.Reloader != nil {
			if reloadErr := s.Reloader.ReloadConfig(ctx); reloadErr == nil {
				notes = append(notes, "herdr: config reloaded")
			}
		}
		if (!result.OK || len(result.Skipped) > 0) && s.Notifier != nil {
			s.Notifier.Notify(ctx, "herdr-scope: configure", result.Message)
		}
		return notes
	case ports.ManagedInstalled:
		return nil
	case ports.ManagedMissing, ports.ManagedForeignEntry, ports.ManagedNoUITable:
	}
	note := fmt.Sprintf("herdr: managed blocks not written (%s); run the configure action once that is fixed", report.State)
	if s.Notifier != nil {
		s.Notifier.Notify(ctx, "herdr-scope: not configured", note)
	}
	return []string{note}
}

// EnsureObserver is the state-start action: make sure exactly one healthy
// daemon runs, on this build. It replaces a stalled one, restarts one that
// another build started, and starts one when none answers.
type EnsureObserver struct {
	Setup   *Setup
	Control ports.DaemonControl
	Process ports.DaemonProcess
	// Off is the unconfigure switch; nil means never off.
	Off ports.OffSwitch
	// Hook is true when Herdr runs this from a startup or event hook rather
	// than for the user: a hook never brings an unconfigured Scope back.
	Hook bool
	// Sleep waits between health checks; nil means time.Sleep honouring ctx.
	Sleep func(ctx context.Context, d time.Duration) bool
	// Build fingerprints this executable. A healthy daemon that reports
	// another one — a rebuilt bin/ — is stopped and started again from this
	// one. Empty, or Stop nil, never restarts.
	Build string
	Stop  func(ctx context.Context) error
}

// NoteRestarted is what a start logs when it replaced a daemon of another build.
const NoteRestarted = "daemon: restarted on a new build"

// NoteOff is what a hook logs instead of starting an unconfigured Scope.
const NoteOff = "daemon: not started — Scope is unconfigured; the state-start or configure action turns it back on"

// Execute returns notes for the plugin log.
func (u EnsureObserver) Execute(ctx context.Context) ([]string, error) {
	var notes []string
	force := false
	if u.Off != nil && u.Off.Off() {
		if u.Hook {
			return []string{NoteOff}, nil
		}
		// Asked for by the user: back on, blocks and all.
		if err := u.Off.SetOff(false); err != nil {
			return nil, fmt.Errorf("%w: %w", domain.ErrPersistence, err)
		}
		force = true
	}
	if u.Setup != nil {
		notes = append(notes, u.Setup.Ensure(ctx, force)...)
	}
	sleep := u.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	health, status, reason := DaemonHealth(ctx, u.Control)
	if health == HealthStalled {
		sleep(ctx, stallConfirm)
		health, status, reason = DaemonHealth(ctx, u.Control)
	}
	switch health {
	case HealthHealthy:
		if u.Build == "" || u.Stop == nil || status == nil || status.Build == u.Build {
			return append(notes, "daemon: already running"), nil
		}
		if err := u.Stop(ctx); err != nil {
			return notes, fmt.Errorf("restart on a new build: %w", err)
		}
		notes = append(notes, NoteRestarted)
		return notes, u.spawn(ctx, NoteRestarted)
	case HealthStalled:
		note := fmt.Sprintf("replaced a stalled daemon: pid %d, %s", status.PID, reason)
		if err := u.Process.Terminate(ctx, status.PID); err != nil {
			note += " (it did not exit)"
		}
		notes = append(notes, note)
		return notes, u.spawn(ctx, note)
	case HealthNone:
	}
	return append(notes, "daemon: started"), u.spawn(ctx, "")
}

func (u EnsureObserver) spawn(ctx context.Context, note string) error {
	if err := u.Process.Spawn(ctx, note); err != nil {
		return fmt.Errorf("spawn daemon: %w", err)
	}
	return nil
}

// StopObserver is the state-stop action: ask the daemon to clear its marks
// and exit; end it if it will not; clear directly when none answers.
type StopObserver struct {
	Control ports.DaemonControl
	Process ports.DaemonProcess
	// Sidebar clears tokens when no daemon is there to do it.
	Sidebar ports.SidebarWriter
	Sleep   func(ctx context.Context, d time.Duration) bool
}

// ErrDaemonStillRunning means the daemon outlived every way of ending it.
var ErrDaemonStillRunning = errors.New("daemon still running")

// Execute stops the daemon; purge also clears titles, sort keys and logos —
// the uninstall path, where the blocks that render them are about to go.
func (u StopObserver) Execute(ctx context.Context, purge bool) error {
	sleep := u.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	reply, err := u.Control.Request(ctx, ports.ControlRequest{Cmd: ports.ControlStop, Purge: purge})
	stoppedByDaemon := err == nil && reply.OK && reply.Stopped
	if !u.waitForExit(ctx, sleep) {
		// The reply can arrive while the exit it promises never does.
		if health, status, _ := DaemonHealth(ctx, u.Control); health != HealthNone && status != nil {
			_ = u.Process.Terminate(ctx, status.PID)
		}
	}
	if !stoppedByDaemon && u.Sidebar != nil {
		if clearErr := u.Sidebar.Clear(ctx, purge); clearErr != nil {
			return fmt.Errorf("%w: %w", domain.ErrProjection, clearErr)
		}
	}
	if health, _, _ := DaemonHealth(ctx, u.Control); health != HealthNone {
		return ErrDaemonStillRunning
	}
	return nil
}

func (u StopObserver) waitForExit(ctx context.Context, sleep func(context.Context, time.Duration) bool) bool {
	for waited := time.Duration(0); waited < exitWait; waited += exitPoll {
		if health, _, _ := DaemonHealth(ctx, u.Control); health == HealthNone {
			return true
		}
		if !sleep(ctx, exitPoll) {
			return false
		}
	}
	return false
}

// Configure installs or repairs the managed blocks and reloads Herdr.
type Configure struct {
	Managed  ports.ManagedConfig
	Reloader ports.HostReloader
	Notifier ports.Notifier
	// Off is cleared by a successful configure; nil to leave it.
	Off ports.OffSwitch
}

// Execute returns the user-facing message.
func (u Configure) Execute(ctx context.Context) (ports.ManagedResult, error) {
	result, err := u.Managed.Apply()
	if err != nil {
		return result, fmt.Errorf("%w: %w", domain.ErrConfiguration, err)
	}
	if (!result.OK || len(result.Skipped) > 0) && u.Notifier != nil {
		u.Notifier.Notify(ctx, "herdr-scope: configure", result.Message)
	}
	if result.OK && u.Off != nil {
		if offErr := u.Off.SetOff(false); offErr != nil {
			return result, fmt.Errorf("%w: %w", domain.ErrPersistence, offErr)
		}
	}
	if result.OK && u.Reloader != nil {
		if reloadErr := u.Reloader.ReloadConfig(ctx); reloadErr != nil {
			return result, fmt.Errorf("reload: %w", reloadErr)
		}
	}
	return result, nil
}

// Unconfigure stops the daemon, clears every token it wrote and removes the
// managed blocks. It needs the plugin still installed, so it comes first.
type Unconfigure struct {
	Stop     StopObserver
	Managed  ports.ManagedConfig
	Reloader ports.HostReloader
	// Off is set once the blocks are gone, so Herdr's hooks leave Scope off.
	Off ports.OffSwitch
}

// Execute returns the user-facing messages.
func (u Unconfigure) Execute(ctx context.Context) ([]string, error) {
	var notes []string
	if err := u.Stop.Execute(ctx, true); err != nil {
		notes = append(notes, "daemon: "+err.Error())
	} else {
		notes = append(notes, "daemon: stopped, tokens cleared")
	}
	result, err := u.Managed.Remove()
	if err != nil {
		return notes, fmt.Errorf("%w: %w", domain.ErrConfiguration, err)
	}
	notes = append(notes, result.Message)
	if u.Off != nil {
		if offErr := u.Off.SetOff(true); offErr != nil {
			return notes, fmt.Errorf("%w: %w", domain.ErrPersistence, offErr)
		}
		notes = append(notes, "scope: off until the state-start or configure action")
	}
	if u.Reloader != nil {
		if reloadErr := u.Reloader.ReloadConfig(ctx); reloadErr != nil {
			return notes, fmt.Errorf("reload: %w", reloadErr)
		}
		notes = append(notes, "herdr: config reloaded")
	}
	return notes, nil
}

// JumpToWaiting is the jump action: focus the next pane that waits on the
// user — a question before a result — after the focused one, round the list.
// The daemon says who waits: it has the frame on screen.
type JumpToWaiting struct {
	Control ports.DaemonControl
	Focus   ports.PaneFocus
}

// NoteNobodyWaits is the jump action's answer when no pane waits.
const NoteNobodyWaits = "jump: nobody is waiting"

// ErrNotRunning means no daemon answered a request only it can answer.
var ErrNotRunning = errors.New("the Scope daemon is not running (state-start)")

// Execute focuses the pane and says which.
func (u JumpToWaiting) Execute(ctx context.Context) (string, error) {
	reply, err := u.Control.Request(ctx, ports.ControlRequest{Cmd: ports.ControlWaiting})
	if err != nil || !reply.OK {
		return "", ErrNotRunning
	}
	if len(reply.Panes) == 0 {
		return NoteNobodyWaits, nil
	}
	focused, _, err := u.Focus.FocusedPane(ctx)
	if err != nil {
		focused = ""
	}
	target := domain.NextWaiting(reply.Panes, focused)
	if err = u.Focus.FocusPane(ctx, target); err != nil {
		return "", fmt.Errorf("%w: focus %s: %w", domain.ErrTransport, target, err)
	}
	return "jump: " + string(target), nil
}

// FoldFamily is the fold action: the daemon folds the focused workspace's
// family in the Agents panel, or unfolds it.
type FoldFamily struct {
	Control ports.DaemonControl
	Focus   ports.PaneFocus
}

// Execute asks the daemon and says what it did.
func (u FoldFamily) Execute(ctx context.Context) (string, error) {
	var ws domain.WorkspaceID
	if u.Focus != nil {
		if _, focused, err := u.Focus.FocusedPane(ctx); err == nil {
			ws = focused
		}
	}
	reply, err := u.Control.Request(ctx, ports.ControlRequest{Cmd: ports.ControlFold, Workspace: ws})
	if err != nil {
		return "", ErrNotRunning
	}
	if !reply.OK {
		return "", fmt.Errorf("%w: fold: %s", domain.ErrConfiguration, reply.Error)
	}
	note := "fold: " + reply.Family + " unfolded"
	if reply.Folded {
		note = "fold: " + reply.Family + " folded"
	}
	if mode, ok := domain.ParseSortMode(reply.Mode); ok && !mode.Folds() {
		note += " (the Agents panel folds in the active and here orders)"
	}
	return note, nil
}
