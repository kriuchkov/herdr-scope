// Package bootstrap is the composition root: it resolves paths, builds every
// adapter, wires them into the application's use cases and exposes them as
// the herdr-scope command line. Nothing else in the module constructs
// adapters.
package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/adapters/appearance"
	"github.com/kriuchkov/herdr-scope/internal/adapters/config"
	"github.com/kriuchkov/herdr-scope/internal/adapters/control"
	"github.com/kriuchkov/herdr-scope/internal/adapters/filesystem"
	"github.com/kriuchkov/herdr-scope/internal/adapters/font"
	"github.com/kriuchkov/herdr-scope/internal/adapters/git"
	"github.com/kriuchkov/herdr-scope/internal/adapters/herdr"
	"github.com/kriuchkov/herdr-scope/internal/adapters/sidebar"
	"github.com/kriuchkov/herdr-scope/internal/adapters/state"
	"github.com/kriuchkov/herdr-scope/internal/application"
	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// Build identifies this binary.
type Build struct {
	Version string
}

// Runtime is every adapter, built once per process from the environment.
type Runtime struct {
	Build  Build
	Paths  filesystem.Paths
	Logger *slog.Logger
	Stdout io.Writer
	Stderr io.Writer

	settings *settingsHolder

	fingerprintOnce  sync.Once
	fingerprintValue string

	Client     *herdr.Client
	Observer   *herdr.Observer
	Events     *herdr.Events
	CLI        herdr.CLI
	State      *state.Repository
	SortModes  application.SortChoice
	Plugin     *config.PluginFile
	Managed    *config.Managed
	Fonts      *font.Installer
	Writer     *lazyWriter
	Appearance *appearance.Probe
	Control    control.Client
	Endpoint   string
	TabBar     *filesystem.TabBar
	Activity   *filesystem.Activity
}

// settingsHolder is the settings in force, swapped on reload.
type settingsHolder struct {
	mu       sync.RWMutex
	settings ports.Settings
}

func (h *settingsHolder) get() ports.Settings {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.settings
}

func (h *settingsHolder) set(s ports.Settings) {
	h.mu.Lock()
	h.settings = s
	h.mu.Unlock()
}

// writeAtomic is every file write Scope makes: atomic, private.
func writeAtomic(path string, data []byte) error {
	return state.WriteAtomic(path, data, 0o600)
}

// NewRuntime resolves paths and builds the adapters. It performs no I/O
// against Herdr.
func NewRuntime(ctx context.Context, build Build, stdout, stderr io.Writer) *Runtime {
	home, _ := os.UserHomeDir()
	exe, _ := os.Executable()
	paths := filesystem.ResolvePaths(os.Getenv, runtime.GOOS, home, filepath.Dir(exe))
	r := &Runtime{Build: build, Paths: paths, Stdout: stdout, Stderr: stderr, settings: &settingsHolder{}}
	r.Logger = slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	r.Plugin = config.NewPluginFile(paths.PluginConfig(), writeAtomic)
	settings, warnings, err := r.Plugin.Load(ctx)
	if err != nil {
		r.Logger.WarnContext(ctx, "config unreadable; using defaults", "path", paths.PluginConfig(), "error", err)
	}
	for _, w := range warnings {
		r.Logger.WarnContext(ctx, "config", "warning", w)
	}
	r.settings.set(settings)

	r.Client = herdr.NewClient(paths.Socket, "plugin:"+paths.PluginID)
	r.Observer = herdr.NewObserver(r.Client, herdr.Mapper{Home: home, Showing: sidebar.ReadBack})
	r.Events = herdr.NewEvents(r.Client, time.Now)
	r.CLI = herdr.CLI{Binary: paths.Binary}
	r.State = state.NewRepository(paths.StateDir, r.Logger, time.Now)
	r.SortModes = application.SortChoice{Store: r.State, Configured: r.Plugin.SortDefault}
	r.Fonts = font.New(font.Options{
		Source: paths.FontSource(), GOOS: runtime.GOOS, Home: home,
		XDGDataHome: os.Getenv("XDG_DATA_HOME"), XDGConfigHome: os.Getenv("XDG_CONFIG_HOME"),
		LocalAppData: os.Getenv("LOCALAPPDATA"),
	})
	r.Writer = &lazyWriter{build: func() *sidebar.Writer {
		return sidebar.NewWriter(r.Client, r.presentation(r.Settings()), time.Now)
	}}
	r.Appearance = appearance.NewProbe(runtime.GOOS, appearance.ExecRunner, time.Now)
	r.Endpoint = control.Endpoint(paths.RuntimeDir, paths.SessionSuffix)
	r.Control = control.Client{Endpoint: r.Endpoint}
	r.TabBar = filesystem.NewTabBar(r.Client, paths.TabBarCache(os.Getenv("HERDR_SESSION")), home, git.Branch, writeAtomic, time.Now)
	r.Activity = filesystem.NewActivity(os.Getenv, home, sqliteRunner())
	r.Managed = config.NewManaged(config.ManagedDeps{
		File:     paths.HerdrConfig,
		StateDir: paths.StateDir,
		Check:    r.CLI.ConfigCheck,
		Appearance: func() ports.Appearance {
			a, _ := r.Appearance.Current(context.Background())
			return a
		},
		Settings:     r.Settings,
		SidebarBlock: r.sidebarBlock,
		Windows:      runtime.GOOS == "windows",
		Write:        writeAtomic,
		Now:          time.Now,
	})
	return r
}

// Settings is the settings in force.
func (r *Runtime) Settings() ports.Settings { return r.settings.get() }

// presentation derives glyphs and indents from settings; auto uses the font
// only where the terminal can draw it (fontconfig on Linux, a codepoint map
// in a Ghostty/kitty config, or a patched face in iTerm2's default profile).
func (r *Runtime) presentation(settings ports.Settings) sidebar.Presentation {
	return sidebar.NewPresentation(settings, r.Fonts.Renderable)
}

// applySettings makes new settings the ones in force everywhere.
func (r *Runtime) applySettings(settings ports.Settings) {
	r.settings.set(settings)
	r.Writer.get().Configure(r.presentation(settings))
}

// sidebarBlock renders the managed [ui.sidebar.*] tables for an appearance,
// naming in rows_by_agent only the agent ids Herdr's detectors know.
func (r *Runtime) sidebarBlock(appearance ports.Appearance) (string, error) {
	layout, err := sidebar.NewLayout(r.Writer.get().Presentation(), appearance, filesystem.KnownAgents(r.Paths.HerdrStateDir))
	if err != nil {
		return "", err
	}
	return layout.TOML(), nil
}

// lazyWriter is the sidebar writer, built on first use. Building it resolves
// the presentation, and auto asks the terminal font for that — on macOS a
// `defaults` process and a scan of the font directories — which most
// commands never need: a key the daemon answers, status, a state-start that
// finds the daemon running.
type lazyWriter struct {
	once   sync.Once
	build  func() *sidebar.Writer
	writer *sidebar.Writer
}

func (l *lazyWriter) Apply(ctx context.Context, view domain.SidebarView) (time.Time, error) {
	return l.get().Apply(ctx, view) //nolint:wrapcheck // the writer's own error
}

func (l *lazyWriter) SetOrder(ctx context.Context, mode domain.SortMode, family []domain.WorkspaceID) error {
	return l.get().SetOrder(ctx, mode, family) //nolint:wrapcheck // the writer's own error
}

func (l *lazyWriter) Badge(view domain.SidebarView) string { return l.get().Badge(view) }

func (l *lazyWriter) Clear(ctx context.Context, purge bool) error {
	return l.get().Clear(ctx, purge) //nolint:wrapcheck // the writer's own error
}

func (l *lazyWriter) Resync() { l.get().Resync() }

func (l *lazyWriter) get() *sidebar.Writer {
	l.once.Do(func() { l.writer = l.build() })
	return l.writer
}

// observerDeps wires the observer's ports.
func (r *Runtime) observerDeps(logger ports.Logger) application.ObserverDeps {
	return application.ObserverDeps{
		Agents: r.Observer, Session: r.Observer, Activity: r.Activity, Repositories: git.Resolver{},
		State: r.State, Sidebar: r.Writer, Host: r.Managed, Mover: r.Client, Folds: r.State,
		Clock: clock{}, Logger: logger,
	}
}

func (r *Runtime) toggle() application.SortModeToggle {
	return application.SortModeToggle{Sidebar: r.Writer, Store: r.SortModes, Managed: r.Managed, Reloader: r.CLI}
}

// build is this binary's setup identity: a new version sets up again (setup
// is idempotent: installed blocks are kept, a present font is not copied).
func (r *Runtime) build() string {
	return "herdr-scope " + r.Build.Version
}

// fingerprint is a short hash of this process's executable, read once per
// process: it tells two builds of one version apart. A daemon takes it at
// start, before anyone can replace the file. Empty when the file is
// unreadable, which disables the restart on a new build.
func (r *Runtime) fingerprint() string {
	r.fingerprintOnce.Do(func() {
		path, err := os.Executable()
		if err != nil {
			return
		}
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		h := sha256.New()
		if _, copyErr := io.Copy(h, f); copyErr == nil {
			r.fingerprintValue = hex.EncodeToString(h.Sum(nil))[:16]
		}
	})
	return r.fingerprintValue
}

type clock struct{}

func (clock) Now() time.Time { return time.Now() }

var _ ports.SidebarWriter = (*lazyWriter)(nil)
