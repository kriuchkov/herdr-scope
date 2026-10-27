package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kriuchkov/herdr-scope/internal/adapters/config"
	"github.com/kriuchkov/herdr-scope/internal/adapters/control"
	"github.com/kriuchkov/herdr-scope/internal/adapters/filesystem"
	"github.com/kriuchkov/herdr-scope/internal/adapters/tui"
	"github.com/kriuchkov/herdr-scope/internal/application"
	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// Main runs the command line and returns the exit code.
func Main(ctx context.Context, build Build, args []string, stdout, stderr io.Writer) int {
	root := NewRootCommand(ctx, build, stdout, stderr)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// NewRootCommand builds the command tree. The runtime is created lazily so
// `version` and `help` need nothing from the environment.
func NewRootCommand(ctx context.Context, build Build, stdout, stderr io.Writer) *cobra.Command {
	var rt *Runtime
	runtime := func() *Runtime {
		if rt == nil {
			rt = NewRuntime(ctx, build, stdout, stderr)
		}
		return rt
	}
	root := &cobra.Command{
		Use:           "herdr-scope",
		Short:         "Herdr Scope: who is working, who is waiting on you, who is done — in the Herdr sidebar",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(
		versionCommand(build),
		&cobra.Command{
			Use: "run", Short: "Be the daemon (Herdr's startup hook runs state-start, which starts this)",
			RunE: func(cmd *cobra.Command, _ []string) error { return runtime().RunDaemon(cmd.Context()) },
		},
		&cobra.Command{
			Use: "state-start", Short: "Make sure one healthy daemon runs (setup on first start)",
			RunE: func(cmd *cobra.Command, _ []string) error { return runtime().stateStart(cmd.Context()) },
		},
		stateStopCommand(runtime),
		statusCommand(runtime),
		&cobra.Command{
			Use: "doctor", Short: "Check Herdr, the socket, events, state, config, font and sidebar",
			RunE: func(cmd *cobra.Command, _ []string) error { return runtime().doctor(cmd.Context()) },
		},
		configCommand(runtime),
		viewCommand(runtime),
		&cobra.Command{
			Use: "jump", Short: "Focus the next agent waiting on you: a question first, then a finished turn (prefix+u)",
			RunE: func(cmd *cobra.Command, _ []string) error {
				scope := runtime()
				note, err := application.JumpToWaiting{Control: scope.Control, Focus: scope.Client}.Execute(cmd.Context())
				if err != nil {
					return err //nolint:wrapcheck // the use case says what failed
				}
				scope.note(note)
				return nil
			},
		},
		&cobra.Command{
			Use: "fold", Short: "Fold the focused workspace's folder in the Agents panel, or unfold it (prefix+m)",
			RunE: func(cmd *cobra.Command, _ []string) error {
				scope := runtime()
				note, err := application.FoldFamily{Control: scope.Control, Focus: scope.Client}.Execute(cmd.Context())
				if err != nil {
					return err //nolint:wrapcheck // the use case says what failed
				}
				scope.note(note)
				return nil
			},
		},
		settingsCommand(runtime),
		configureCommand(runtime),
		&cobra.Command{
			Use: "unconfigure", Short: "Stop the daemon, clear its tokens and remove the managed blocks",
			RunE: func(cmd *cobra.Command, _ []string) error { return runtime().unconfigure(cmd.Context()) },
		},
		fontCommand(runtime),
		setupCommand(runtime),
		themeSyncCommand(runtime),
		renderCommand(),
	)
	return root
}

func versionCommand(build Build) *cobra.Command {
	return &cobra.Command{
		Use: "version", Short: "Print the version",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "herdr-scope %s (protocol %d+)\n", build.Version, application.MinProtocol)
		},
	}
}

func (r *Runtime) note(lines ...string) {
	for _, line := range lines {
		fmt.Fprintln(r.Stdout, line)
	}
}

func (r *Runtime) setup() *application.Setup {
	return &application.Setup{
		Managed: r.Managed, Fonts: r.Fonts, Stamp: r.stamp(), Reloader: r.CLI, Notifier: r.CLI, Off: r.off(),
		Build: r.build(),
	}
}

// lifecycleLock serialises everything that starts or stops the daemon, across
// processes: the startup hook, the watchdog and an action can all run
// state-start at once (and the first may run setup, which edits Herdr's
// config.toml), and `herdr plugin action invoke` returns before the command
// ends, so a stop and the start that follows it can overlap — the start then
// finds the old daemon still answering, and nothing runs once it exits.
const lifecycleLock = "lifecycle.lock"

func (r *Runtime) stateStart(ctx context.Context) error {
	return withLock(ctx, r.Paths.RuntimeDir, lifecycleLock, func() error {
		notes, err := application.EnsureObserver{
			Setup: r.setup(), Control: r.Control, Process: process{runtime: r},
			Off: r.off(), Hook: filesystem.InvokedByHook(os.Getenv),
			Build: r.fingerprint(), Stop: func(ctx context.Context) error { return r.stopper().Execute(ctx, false) },
		}.Execute(ctx)
		r.note(notes...)
		return err //nolint:wrapcheck // the use case names what failed
	})
}

func (r *Runtime) stopper() application.StopObserver {
	return application.StopObserver{Control: r.Control, Process: process{runtime: r}, Sidebar: r.Writer}
}

func stateStopCommand(runtime func() *Runtime) *cobra.Command {
	var purge bool
	cmd := &cobra.Command{
		Use: "state-stop", Short: "Stop the daemon and clear its marks (titles and sort keys stay)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := runtime()
			return withLock(cmd.Context(), rt.Paths.RuntimeDir, lifecycleLock, func() error {
				if err := rt.stopper().Execute(cmd.Context(), purge); err != nil {
					return err //nolint:wrapcheck // the use case names what failed
				}
				rt.note("daemon: stopped")
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "clear every token, titles and sort keys included")
	return cmd
}

func configCommand(runtime func() *Runtime) *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Inspect the plugin configuration"}
	var file string
	validate := &cobra.Command{
		Use: "validate", Short: "Validate config.toml and report every problem",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := file
			if path == "" {
				path = runtime().Paths.PluginConfig()
			}
			raw, err := os.ReadFile(path)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("read %s: %w", path, err)
			}
			settings, warnings := config.Parse(string(raw))
			for _, w := range warnings {
				fmt.Fprintln(cmd.OutOrStdout(), "warning:", w)
			}
			if err = settings.Validate(); err != nil || len(warnings) > 0 {
				return fmt.Errorf("%s: %d problem(s)", path, max(1, len(warnings)))
			}
			fmt.Fprintln(cmd.OutOrStdout(), path+": ok")
			return nil
		},
	}
	validate.Flags().StringVar(&file, "file", "", "config file to check (default: the plugin's)")
	cmd.AddCommand(validate)
	return cmd
}

func viewCommand(runtime func() *Runtime) *cobra.Command {
	var flip, cycle, native, here, active, recent, off, reapply bool
	cmd := &cobra.Command{
		Use: "view", Short: "Switch the Agents panel order (prefix+a runs --flip)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			op, set := domain.SortCycle, domain.SortMode("")
			switch {
			case flip:
				op = domain.SortFlip
			case native:
				op = domain.SortNative
			case here:
				op = domain.SortHereToggle
			case active:
				op, set = "", domain.SortActive
			case recent:
				op, set = "", domain.SortRecent
			case off:
				op, set = "", domain.SortOff
			case reapply:
				op = ""
			case cycle:
			}
			rt := runtime()
			toggle := application.ToggleSortMode{
				Control: rt.Control, Store: rt.SortModes, Toggle: rt.toggle(),
				Start: func(ctx context.Context) error { return rt.stateStart(ctx) },
				Off:   rt.off(),
			}
			mode, err := toggle.Execute(cmd.Context(), op, set, reapply)
			if errors.Is(err, application.ErrNothingToReapply) {
				rt.note("agent view: off or Scope unconfigured; nothing to reapply")
				return nil
			}
			if err != nil {
				return err //nolint:wrapcheck // the use case names what failed
			}
			rt.note(describeMode(mode))
			return nil
		},
	}
	cmd.Flags().BoolVar(&flip, "flip", false, "active <-> recent")
	cmd.Flags().BoolVar(&cycle, "cycle", false, "off -> active -> recent -> off (the default)")
	cmd.Flags().BoolVar(&native, "native", false, "Herdr's panel <-> Scope's")
	cmd.Flags().BoolVar(&here, "here", false, "only the focused workspace and its family <-> active (prefix+f)")
	cmd.Flags().BoolVar(&active, "active", false, "grouped, busiest first")
	cmd.Flags().BoolVar(&recent, "recent", false, "flat, by activity")
	cmd.Flags().BoolVar(&off, "off", false, "Herdr's own order")
	cmd.Flags().BoolVar(&reapply, "reapply", false, "restore the chosen order (startup hook)")
	return cmd
}

func describeMode(mode domain.SortMode) string {
	switch mode {
	case domain.SortActive:
		return "agent view: active (grouped, recent first)"
	case domain.SortRecent:
		return "agent view: recent (flat)"
	case domain.SortOff:
		return "agent view: back to panel order"
	case domain.SortHere:
		return "agent view: here (the focused workspace and its family)"
	}
	return "agent view: " + string(mode)
}

func settingsCommand(runtime func() *Runtime) *cobra.Command {
	var open bool
	cmd := &cobra.Command{
		Use: "settings", Short: "The settings popup (prefix+comma runs --open)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := runtime()
			if open {
				return rt.CLI.OpenPluginPane(
					cmd.Context(),
					rt.Paths.PluginID,
					"settings",
					rt.Paths.PluginRoot,
				) //nolint:wrapcheck // names the call
			}
			return rt.runSettings(cmd.Context())
		},
	}
	cmd.Flags().BoolVar(&open, "open", false, "ask Herdr to open the popup")
	return cmd
}

func (r *Runtime) runSettings(ctx context.Context) error {
	save := application.SaveSettings{
		Update: application.UpdateConfiguration{Config: r.Plugin, Control: r.Control},
		Rows:   application.SortModeToggle{Managed: r.Managed, Reloader: r.CLI},
		View: application.ToggleSortMode{
			Control: r.Control, Store: r.SortModes,
			Toggle: application.SortModeToggle{Sidebar: r.Writer, Store: r.SortModes},
		},
	}
	return tui.Run(ctx, os.Stdin, os.Stdout, tui.Deps{ //nolint:wrapcheck // the popup reports its own errors
		Fields: config.Fields(),
		Load: func(ctx context.Context) (ports.Settings, error) {
			s, _, err := r.Plugin.Load(ctx)
			return s, err //nolint:wrapcheck // shown in the popup
		},
		PanelOn:    r.Managed.SidebarRowsOn,
		Order:      func() domain.SortMode { return application.CurrentSortMode(r.SortModes) },
		Save:       save.Execute,
		Title:      "herdr-scope settings",
		PluginID:   r.Paths.PluginID,
		ConfigPath: r.Plugin.Path(),
	})
}

func contains(list []string, item string) bool {
	return slices.Contains(list, item)
}

func configureCommand(runtime func() *Runtime) *cobra.Command {
	var check, apply, uninstall, rowsOn, rowsOff, keys, reload bool
	cmd := &cobra.Command{
		Use: "configure", Short: "Install, inspect or remove the managed blocks in Herdr's config.toml",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := runtime()
			ctx := cmd.Context()
			switch {
			case keys:
				rt.note(keybindings(rt.Paths.PluginID))
				return nil
			case uninstall:
				return rt.unconfigure(ctx)
			case apply:
				use := application.Configure{Managed: rt.Managed, Notifier: rt.CLI, Off: rt.off()}
				if reload {
					use.Reloader = rt.CLI
				}
				result, err := use.Execute(ctx)
				rt.note(result.Message)
				if err != nil {
					return err //nolint:wrapcheck // names the step
				}
				if !result.OK {
					return errors.New("configure failed")
				}
				return nil
			case rowsOn || rowsOff:
				result, err := rt.Managed.SetSidebarRows(rowsOn)
				if err != nil {
					return err //nolint:wrapcheck // names the file
				}
				rt.note(result.Message)
				if result.Changed && reload {
					return rt.CLI.ReloadConfig(ctx) //nolint:wrapcheck // names the call
				}
				return nil
			case check:
			}
			return rt.inspect()
		},
	}
	f := cmd.Flags()
	f.BoolVar(&check, "check", false, "show what is installed (the default)")
	f.BoolVar(&apply, "apply", false, "install or repair the managed blocks")
	f.BoolVar(&uninstall, "uninstall", false, "stop the daemon, clear tokens, remove the blocks")
	f.BoolVar(&rowsOn, "rows-on", false, "install only the sidebar rows")
	f.BoolVar(&rowsOff, "rows-off", false, "remove only the sidebar rows")
	f.BoolVar(&keys, "keys", false, "print a key-binding snippet to paste")
	f.BoolVar(&reload, "reload", false, "reload Herdr's config afterwards")
	return cmd
}

func (r *Runtime) inspect() error {
	report, err := r.Managed.Inspect()
	if err != nil {
		return err //nolint:wrapcheck // names the file
	}
	has := func(block string) string {
		if contains(report.Blocks, block) {
			return "installed"
		}
		return "absent"
	}
	r.note(
		fmt.Sprintf("herdr   %-16s %s", report.State, report.File),
		fmt.Sprintf("tab-bar %-16s [ui] tab_bar_right", has(config.BlockTabBar)),
		fmt.Sprintf("theme   %-16s [theme.custom]", has(config.BlockTheme)),
		fmt.Sprintf("sidebar %-16s [ui.sidebar.*]", has(config.BlockSidebar)),
		fmt.Sprintf("state   %-16s %s", "", r.Paths.StateDir),
	)
	return nil
}

// unconfigure holds the lifecycle lock too: a watchdog start between its stop
// and the removal of the blocks would leave a daemon painting for no rows.
func (r *Runtime) unconfigure(ctx context.Context) error {
	return withLock(ctx, r.Paths.RuntimeDir, lifecycleLock, func() error {
		notes, err := application.Unconfigure{Stop: r.stopper(), Managed: r.Managed, Reloader: r.CLI, Off: r.off()}.Execute(ctx)
		r.note(notes...)
		return err //nolint:wrapcheck // the use case names what failed
	})
}

// keybindings is the snippet `configure --keys` prints. Key bindings are the
// user's config, not a managed block; all go through the prefix so nothing
// running inside a pane receives them.
func keybindings(pluginID string) string {
	lines := []string{
		"# herdr-scope — paste into Herdr's config.toml and adjust the keys to taste.",
		"# All go through the prefix (ctrl+b by default) so they cannot collide",
		"# with anything running inside a pane.",
	}
	for _, k := range []struct{ key, action, description string }{
		{"prefix+a", "view-flip", "Agents: active <-> recent"},
		{"prefix+u", "jump", "Agents: next one waiting on you"},
		{"prefix+f", "view-here", "Agents: only this workspace <-> all"},
		{"prefix+m", "fold", "Agents: fold <-> unfold this folder"},
		{"prefix+comma", "settings", "herdr-scope settings"},
	} {
		lines = append(lines, "",
			"[[keys.command]]",
			fmt.Sprintf("key = %q", k.key),
			`type = "plugin_action"`,
			fmt.Sprintf("command = %q", pluginID+"."+k.action),
			fmt.Sprintf("description = %q", k.description),
		)
	}
	return strings.Join(lines, "\n")
}

func fontCommand(runtime func() *Runtime) *cobra.Command {
	cmd := &cobra.Command{Use: "font", Short: "Install, remove or check the bundled icon font"}
	cmd.AddCommand(fontPatchCommand(runtime))
	cmd.AddCommand(
		&cobra.Command{
			Use:   "install",
			Short: "Install for this user and map codepoints in Ghostty/kitty",
			RunE: func(*cobra.Command, []string) error {
				rt := runtime()
				notes, err := rt.Fonts.Install()
				rt.note(notes...)
				if err != nil {
					return err //nolint:wrapcheck // names the step
				}
				mapped, err := rt.Fonts.ConfigureTerminals()
				rt.note(mapped...)
				rt.note("done. New terminal windows pick the font up; some terminals need a full restart.")
				return err //nolint:wrapcheck // names the step
			},
		},
		&cobra.Command{Use: "uninstall", Short: "Remove the font and the codepoint maps", RunE: func(*cobra.Command, []string) error {
			rt := runtime()
			notes, err := rt.Fonts.Uninstall()
			rt.note(notes...)
			unmapped, mapErr := rt.Fonts.UnconfigureTerminals()
			rt.note(unmapped...)
			return errors.Join(err, mapErr)
		}},
		&cobra.Command{Use: "check", Short: "Say whether the font is installed", RunE: func(*cobra.Command, []string) error {
			rt := runtime()
			if path, ok := rt.Fonts.InstalledPath(); ok {
				rt.note("font    installed  " + path)
				return nil
			}
			rt.note("font    absent     " + rt.Fonts.UserFontDir())
			return nil
		}},
	)
	return cmd
}

func fontPatchCommand(runtime func() *Runtime) *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "patch FONT.ttf...",
		Short: "Copy your terminal font with the icons in, for terminals with no codepoint map (iTerm2, Terminal.app, Windows Terminal)",
		Long: "patch writes a copy of each TrueType face given, with Scope's icons grafted in and \" Herdr\" added to its\n" +
			"family name, and installs it for this user (or writes it to --out). Pick the new family in the terminal's\n" +
			"profile: variant = \"auto\" then finds it in iTerm2's default profile; elsewhere set variant = \"font\".",
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			rt := runtime()
			notes, err := rt.Fonts.PatchFonts(args, out)
			rt.note(notes...)
			if err != nil {
				return err //nolint:wrapcheck // names the face
			}
			rt.note("done. Choose the new family in your terminal's profile; a running terminal may need a restart to list it.")
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "write the copies into this directory instead of installing them")
	return cmd
}

func setupCommand(runtime func() *Runtime) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use: "setup", Short: "First-run setup: managed blocks, font, codepoint maps (the install hook)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := runtime()
			notes, err := rt.setup().Install(cmd.Context(), force)
			if err != nil {
				return err //nolint:wrapcheck // names setup and the file
			}
			if len(notes) == 0 {
				notes = []string{"setup: nothing to do"}
			}
			rt.note(notes...)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "redo the setup even if it ran for this build")
	return cmd
}

func themeSyncCommand(runtime func() *Runtime) *cobra.Command {
	var force, check bool
	cmd := &cobra.Command{
		Use: "theme-sync", Short: "Follow the desktop's light/dark now (the daemon does it every minute)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := runtime()
			current, err := rt.Appearance.Current(cmd.Context())
			if err != nil {
				return err //nolint:wrapcheck // names the probe
			}
			if check {
				rt.note("appearance: " + string(current))
				return nil
			}
			if !rt.Settings().FollowAppearance && !force {
				rt.note("follow_appearance = false; nothing to do (use --force to apply once)")
				return nil
			}
			follower := application.AppearanceFollower{Source: rt.Appearance, Managed: rt.Managed, Reloader: rt.CLI}
			changed, err := follower.Sync(cmd.Context())
			if err != nil {
				return err //nolint:wrapcheck // names the step
			}
			if changed {
				rt.note("theme block -> " + string(current))
			} else {
				rt.note("unchanged (" + string(current) + ")")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "apply even with follow_appearance = false")
	cmd.Flags().BoolVar(&check, "check", false, "print the appearance only")
	return cmd
}

func statusCommand(runtime func() *Runtime) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use: "status", Short: "Daemon, Herdr, connection, agents and the last event, reconcile and sidebar update",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := runtime()
			health, status, reason := application.DaemonHealth(cmd.Context(), rt.Control)
			if asJSON {
				out := map[string]any{"daemon": health, "reason": reason, "status": control.StatusJSON(status),
					"state_path": rt.State.Path(), "config_path": rt.Plugin.Path(), "endpoint": rt.Endpoint}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(out) //nolint:wrapcheck // stdout
			}
			rt.printStatus(health, status, reason)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}
