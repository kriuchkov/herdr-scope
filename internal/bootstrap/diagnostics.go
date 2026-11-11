package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/adapters/config"
	"github.com/kriuchkov/herdr-scope/internal/adapters/filesystem"
	"github.com/kriuchkov/herdr-scope/internal/application"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// TestedProtocol is the newest Herdr protocol Scope was checked against.
const TestedProtocol = 22

func (r *Runtime) stamp() *filesystem.Stamp {
	return filesystem.NewStamp(r.Paths.StateDir, writeAtomic)
}

func (r *Runtime) off() *filesystem.Switch {
	return filesystem.NewSwitch(r.Paths.StateDir, writeAtomic)
}

func ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return fmt.Sprintf("%s (%s ago)", t.Local().Format(time.DateTime), time.Since(t).Round(time.Second))
}

func (r *Runtime) printStatus(health application.Health, status *ports.DaemonStatus, reason string) {
	line := func(k, v string) { fmt.Fprintf(r.Stdout, "%-18s %s\n", k, v) }
	switch {
	case health == application.HealthNone && r.off().Off():
		line("daemon", "not running — Scope is unconfigured (state-start or configure turns it back on)")
	case health == application.HealthNone:
		line("daemon", "not running")
	case health == application.HealthStalled:
		line("daemon", "stalled — "+reason)
	case health == application.HealthHealthy:
		line("daemon", fmt.Sprintf("running (pid %d, up %s, version %s)", status.PID, status.Uptime.Round(time.Second), status.Version))
		if status.Build != r.fingerprint() {
			line("build", "the daemon runs another build than bin/: the start action restarts it")
		}
	}
	if status != nil {
		line("herdr", status.ServerVersion)
		line("protocol", strconv.Itoa(status.Protocol))
		line("connected", strconv.FormatBool(status.Connected))
		line("agents", strconv.Itoa(status.Agents))
		line("order", status.Mode)
		line("last event", ago(status.LastEvent))
		line("last reconcile", ago(status.LastReconcile))
		line("last sidebar", ago(status.LastSidebar))
		line("frames/events", fmt.Sprintf("%d / %d", status.Frames, status.EventsReceived))
		if status.LastError != "" {
			line("last error", status.LastError)
		}
	} else if info, err := r.Observer.Ping(context.Background()); err == nil {
		line("herdr", info.Version)
		line("protocol", strconv.Itoa(info.Protocol))
	}
	line("state", r.State.Path())
	line("config", r.Plugin.Path())
	line("log", r.Paths.Log())
	line("endpoint", r.Endpoint)
}

type check struct {
	name, state, detail string
}

// doctor runs every check and fails when one fails; warnings do not.
func (r *Runtime) doctor(ctx context.Context) error {
	var checks []check
	add := func(name, state, detail string) { checks = append(checks, check{name, state, detail}) }

	if version, err := r.CLI.Version(ctx); err == nil {
		add("herdr binary", "ok", r.Paths.Binary+" — "+version)
	} else {
		add("herdr binary", "fail", err.Error())
	}
	if _, err := os.Stat(r.Paths.Socket); err == nil {
		add("socket", "ok", r.Paths.Socket)
	} else {
		add("socket", "fail", err.Error())
	}
	r.doctorProtocol(ctx, add)
	if agents, err := r.Observer.ListAgents(ctx); err == nil {
		add("agent list", "ok", fmt.Sprintf("%d agent(s)", len(agents)))
	} else {
		add("agent list", "fail", err.Error())
	}
	r.doctorEvents(ctx, add)
	if _, err := os.Stat(filepath.Join(r.Paths.PluginRoot, "herdr-plugin.toml")); err == nil {
		add("plugin directory", "ok", r.Paths.PluginRoot)
	} else {
		add("plugin directory", "warn", r.Paths.PluginRoot+": no herdr-plugin.toml (running outside the plugin?)")
	}
	probe := filepath.Join(r.Paths.StateDir, ".doctor")
	if err := writeAtomic(probe, []byte("ok\n")); err == nil {
		_ = os.Remove(probe)
		add("state directory", "ok", r.Paths.StateDir)
	} else {
		add("state directory", "fail", err.Error())
	}
	r.doctorConfig(add)
	if path, ok := r.Fonts.InstalledPath(); ok {
		add("font", "ok", path)
	} else {
		add("font", "warn", "not installed; marks use plain Unicode (herdr-scope font install)")
	}
	r.doctorTerminalFont(add)
	r.doctorSidebar(ctx, add)
	health, _, reason := application.DaemonHealth(ctx, r.Control)
	switch health {
	case application.HealthHealthy:
		add("daemon", "ok", "running")
	case application.HealthStalled:
		add("daemon", "fail", "stalled: "+reason)
	case application.HealthNone:
		if r.off().Off() {
			add("daemon", "ok", "off: Scope is unconfigured, hooks leave it off (state-start or configure turns it back on)")
			break
		}
		add("daemon", "warn", "not running (herdr plugin action invoke "+r.Paths.PluginID+".state-start)")
	}

	failed := 0
	for _, c := range checks {
		fmt.Fprintf(r.Stdout, "%-5s %-18s %s\n", c.state, c.name, c.detail)
		if c.state == "fail" {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("doctor: %d check(s) failed", failed)
	}
	return nil
}

func (r *Runtime) doctorProtocol(ctx context.Context, add func(string, string, string)) {
	info, err := application.CheckProtocol(ctx, r.Observer)
	var tooOld *application.ProtocolTooOldError
	switch {
	case errors.As(err, &tooOld):
		add("herdr protocol", "fail", tooOld.Error())
	case err != nil:
		add("herdr protocol", "fail", err.Error())
	case info.Protocol != TestedProtocol:
		add("herdr protocol", "warn", fmt.Sprintf("%d (herdr %s); tested against %d", info.Protocol, info.Version, TestedProtocol))
	default:
		add("herdr protocol", "ok", fmt.Sprintf("%d (herdr %s)", info.Protocol, info.Version))
	}
}

func (r *Runtime) doctorEvents(ctx context.Context, add func(string, string, string)) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	stream, err := r.Events.Subscribe(ctx, application.SubscribedKinds(r.Settings().MovesWorkspaces()))
	if err != nil {
		add("event subscription", "fail", err.Error())
		return
	}
	_ = stream.Close()
	add("event subscription", "ok", fmt.Sprintf("%d lifecycle kinds", len(application.SubscribedKinds(r.Settings().MovesWorkspaces()))))
}

func (r *Runtime) doctorConfig(add func(string, string, string)) {
	raw, err := os.ReadFile(r.Paths.PluginConfig())
	if errors.Is(err, os.ErrNotExist) {
		add("config", "ok", "no config.toml; defaults")
		return
	}
	if err != nil {
		add("config", "fail", err.Error())
		return
	}
	if _, warnings := config.Parse(string(raw)); len(warnings) > 0 {
		add("config", "warn", fmt.Sprintf("%d warning(s): %s", len(warnings), warnings[0]))
		return
	}
	add("config", "ok", r.Paths.PluginConfig())
}

// doctorTerminalFont checks the face the terminal draws icons with, where
// Scope can read it (iTerm2's default profile).
func (r *Runtime) doctorTerminalFont(add func(string, string, string)) {
	tf, ok := r.Fonts.TerminalFont()
	if !ok {
		return
	}
	face := tf.Terminal + " draws with " + tf.Name
	switch {
	case tf.Icons:
		add("terminal font", "ok", face+", which carries the icons")
	case r.Settings().Presentation.Variant == ports.VariantFont:
		add("terminal font", "warn", face+", which has no icons: variant = \"font\" shows boxes (herdr-scope font patch <its .ttf>)")
	default:
		add("terminal font", "ok", face+" (no icons: plain marks; herdr-scope font patch <its .ttf> adds them)")
	}
}

func (r *Runtime) doctorSidebar(ctx context.Context, add func(string, string, string)) {
	report, err := r.Managed.Inspect()
	switch {
	case err != nil:
		add("sidebar", "fail", err.Error())
		return
	case report.State != ports.ManagedInstalled:
		add("sidebar", "warn", fmt.Sprintf("managed blocks %s (herdr plugin action invoke %s.configure)", report.State, r.Paths.PluginID))
		return
	case !r.Managed.SidebarRowsOn():
		add("sidebar", "warn", "rows handed back to Herdr (agents_panel = herdr)")
		return
	}
	if parses, output := r.CLI.ConfigCheck(ctx); !parses {
		add("sidebar", "fail", "Herdr cannot parse config.toml: "+output)
		return
	}
	add("sidebar", "ok", "managed rows installed, config parses")
}
