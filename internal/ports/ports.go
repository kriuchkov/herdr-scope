// Package ports declares what the application needs from the outside world.
// Adapters implement these; the application depends on nothing else.
package ports

import (
	"context"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

// AgentSource reads the live agent list — the cheap per-frame refresh.
type AgentSource interface {
	ListAgents(ctx context.Context) ([]domain.Agent, error)
}

// SessionSource reads the whole session — the authoritative reconcile.
type SessionSource interface {
	Snapshot(ctx context.Context) (domain.Session, error)
}

// EventSource opens event subscriptions. One stream is one connection;
// reconnecting is the caller's job.
type EventSource interface {
	Subscribe(ctx context.Context, kinds []domain.EventKind) (EventStream, error)
}

// EventStream delivers normalised events until it ends.
type EventStream interface {
	// Events closes when the stream ends.
	Events() <-chan domain.AgentEvent
	// Err says why the stream ended; meaningful once Events is closed.
	Err() error
	Close() error
}

// ServerProbe identifies the host and tells a hiccup from a server that is
// gone for good.
type ServerProbe interface {
	Ping(ctx context.Context) (domain.ServerInfo, error)
	// Gone reports whether the host's endpoint has disappeared.
	Gone() bool
}

// ActivitySource recovers when an agent last worked from the agent's own
// session record, for panes older than the observer. A zero time with a nil
// error means no record was found.
type ActivitySource interface {
	LastActivity(ctx context.Context, kind domain.AgentKind, session string) (time.Time, error)
}

// RepositoryResolver says which git checkout a path belongs to. It returns
// domain.ErrNotRepository for a path outside any checkout.
type RepositoryResolver interface {
	Resolve(ctx context.Context, path string) (domain.RepositoryInfo, error)
}

// StateRepository persists the agent memory. Save is handed the memory every
// few seconds while the daemon runs, and may skip a write whose only changes
// are small moves of its times (a working agent's activity within its
// minute, say).
type StateRepository interface {
	Load(ctx context.Context) ([]domain.AgentSession, error)
	Save(ctx context.Context, sessions []domain.AgentSession) error
}

// SortModeStore persists the chosen panel order. ok is false when nobody has
// chosen one yet, which is not the same as having chosen off.
type SortModeStore interface {
	LoadSortMode() (mode domain.SortMode, ok bool, err error)
	SaveSortMode(mode domain.SortMode) error
}

// FoldStore persists the families folded in the Agents panel, by
// domain.FoldKey.
type FoldStore interface {
	LoadFolds() ([]string, error)
	SaveFolds(keys []string) error
}

// SidebarWriter projects a SidebarView onto the host.
type SidebarWriter interface {
	// Apply writes what changed since the last successful Apply and returns
	// when the projection next needs repainting for its own animation (zero
	// when nothing animates).
	Apply(ctx context.Context, view domain.SidebarView) (time.Time, error)
	// SetOrder installs (or, for off, removes) the panel order. family is
	// what the here view shows besides the focused workspace (nil: that
	// workspace alone); the other orders ignore it.
	SetOrder(ctx context.Context, mode domain.SortMode, family []domain.WorkspaceID) error
	// Badge is the tab bar's summary of who waits on the user, in the
	// sidebar's own marks; empty when nobody does.
	Badge(view domain.SidebarView) string
	// Clear takes Scope's paint off the host. A plain clear keeps titles and
	// sort keys so a stopped plugin leaves a readable list; purge takes all.
	Clear(ctx context.Context, purge bool) error
	// Resync forgets what was sent, so the next Apply writes everything —
	// after a reconnect the server may be a new one with none of it.
	Resync()
}

// HostConfig is what the observer reads from the host's own configuration.
type HostConfig interface {
	// PanelGrouped reports whether Herdr's own Agents panel is in its grouped
	// order; it persists only in the host's config file.
	PanelGrouped() bool
}

// ManagedConfig owns Scope's managed blocks in the host's config file.
type ManagedConfig interface {
	Inspect() (ManagedReport, error)
	Apply() (ManagedResult, error)
	Remove() (ManagedResult, error)
	SetSidebarRows(on bool) (ManagedResult, error)
	ApplyAppearance(appearance Appearance) (ManagedResult, error)
	// Refresh rewrites installed blocks that no longer match what this
	// version and configuration would write; it never installs a block.
	Refresh() (ManagedResult, error)
}

// ManagedReport describes the host config file.
type ManagedReport struct {
	File  string
	State ManagedState
	// Blocks lists the managed blocks present, by name.
	Blocks []string
}

// ManagedState is what an install would find.
type ManagedState string

// Managed states.
const (
	ManagedMissing      ManagedState = "missing"
	ManagedInstalled    ManagedState = "installed"
	ManagedInstallable  ManagedState = "installable"
	ManagedForeignEntry ManagedState = "foreign-entry"
	ManagedNoUITable    ManagedState = "no-ui-table"
)

// ManagedResult is the outcome of a managed-block operation.
type ManagedResult struct {
	OK      bool
	Changed bool
	Message string
	// Skipped lists the host tables a user already declares, which kept the
	// block that would declare them out.
	Skipped []string
}

// HostReloader asks the host to reload its config file.
type HostReloader interface {
	ReloadConfig(ctx context.Context) error
}

// Notifier puts a line in front of the user.
type Notifier interface {
	Notify(ctx context.Context, title, body string)
}

// ConfigRepository loads and saves the plugin's own settings.
type ConfigRepository interface {
	// Load returns the effective settings; problems with individual values
	// fall back to defaults and are reported as warnings.
	Load(ctx context.Context) (Settings, []string, error)
	Save(ctx context.Context, settings Settings) error
	// Version changes whenever the stored configuration may have changed.
	Version() string
}

// Appearance is the desktop's light/dark mode.
type Appearance string

// Appearances.
const (
	AppearanceUnknown Appearance = ""
	AppearanceLight   Appearance = "light"
	AppearanceDark    Appearance = "dark"
)

// AppearanceSource probes the desktop's appearance.
type AppearanceSource interface {
	Current(ctx context.Context) (Appearance, error)
}

// TabBarPublisher keeps the tab-bar line current; badge leads it.
type TabBarPublisher interface {
	Publish(ctx context.Context, badge string) error
}

// PaneFocus reads and moves Herdr's focus.
type PaneFocus interface {
	// FocusedPane is the focused pane and its workspace.
	FocusedPane(ctx context.Context) (domain.PaneID, domain.WorkspaceID, error)
	// FocusPane focuses a pane, its tab and its workspace.
	FocusPane(ctx context.Context, pane domain.PaneID) error
}

// WorkspaceMover reorders Herdr's workspaces.
type WorkspaceMover interface {
	MoveWorkspaces(ctx context.Context, order []domain.WorkspaceID) ([]domain.WorkspaceID, error)
}

// Clock tells the wall-clock time. The observer compares it with persisted
// activity stamps, so it must be wall clock; scheduling uses durations.
type Clock interface {
	Now() time.Time
}

// Logger is structured logging. *slog.Logger satisfies it.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}
