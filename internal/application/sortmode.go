package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// SortModeToggle switches the panel order. The daemon runs it on a control
// request; the thin client runs it itself when no daemon answers.
type SortModeToggle struct {
	Sidebar ports.SidebarWriter
	Store   ports.SortModeStore
	// Managed and Reloader are needed only for the native toggle, which also
	// installs or removes the sidebar rows.
	Managed  ports.ManagedConfig
	Reloader ports.HostReloader
	// Family is the focused workspace's family for the here view; nil
	// leaves it to Herdr's focus alone (no daemon to know the family).
	Family func() []domain.WorkspaceID
}

// Switch lands on set when given, otherwise on current.Apply(op), applies it
// and persists the choice.
func (t SortModeToggle) Switch(
	ctx context.Context,
	current domain.SortMode,
	op domain.SortOp,
	set domain.SortMode,
) (domain.SortMode, error) {
	next := current
	switch {
	case set != "":
		next = set
	case op != "":
		next = current.Apply(op)
	}
	// Only the native toggle owns both layers. Rows go first: the reload it
	// triggers is the one event that could drop a socket-installed override,
	// so the socket call has to be the last word.
	if op == domain.SortNative && t.Managed != nil {
		if err := t.switchRows(ctx, next != domain.SortOff); err != nil {
			return current, err
		}
	}
	var family []domain.WorkspaceID
	if t.Family != nil {
		family = t.Family()
	}
	if err := t.Sidebar.SetOrder(ctx, next, family); err != nil {
		return current, fmt.Errorf("panel order: %w", err)
	}
	if t.Store != nil {
		if err := t.Store.SaveSortMode(next); err != nil {
			// The panel switched; only remembering it failed.
			return next, fmt.Errorf("%w: sort mode not saved: %w", domain.ErrPersistence, err)
		}
	}
	return next, nil
}

func (t SortModeToggle) switchRows(ctx context.Context, on bool) error {
	result, err := t.Managed.SetSidebarRows(on)
	if err != nil {
		return fmt.Errorf("sidebar rows: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("%w: %s", domain.ErrConfiguration, result.Message)
	}
	if result.Changed && t.Reloader != nil {
		if reloadErr := t.Reloader.ReloadConfig(ctx); reloadErr != nil {
			return fmt.Errorf("reload: %w", reloadErr)
		}
	}
	return nil
}

// ToggleSortMode is the key binding's use case: ask the daemon, which applies
// the order and repaints on its next frame; with no daemon, switch here and
// start one.
type ToggleSortMode struct {
	Control ports.DaemonControl
	Store   ports.SortModeStore
	Toggle  SortModeToggle
	// Start brings a daemon up after a standalone switch; nil to skip.
	Start func(ctx context.Context) error
	// Off is the unconfigure switch: reapply does nothing while it is on.
	Off ports.OffSwitch
}

// ErrNothingToReapply means the chosen order is Herdr's own, or Scope is
// unconfigured.
var ErrNothingToReapply = errors.New("nothing to reapply")

// Execute performs op (or set). With reapply it re-installs the persisted
// order without changing it — the startup hook's job, since the override
// dies with the server.
func (u ToggleSortMode) Execute(ctx context.Context, op domain.SortOp, set domain.SortMode, reapply bool) (domain.SortMode, error) {
	current := CurrentSortMode(u.Store)
	if reapply {
		if current == domain.SortOff || (u.Off != nil && u.Off.Off()) {
			return current, ErrNothingToReapply
		}
		set = current
	}
	if u.Control != nil {
		reply, err := u.Control.Request(ctx, ports.ControlRequest{Cmd: ports.ControlView, Op: op, Set: set})
		if err == nil && reply.OK && reply.Applied {
			mode, _ := domain.ParseSortMode(reply.Mode)
			return mode, nil
		}
	}
	toggle := u.Toggle
	if reapply {
		toggle.Store = nil
	}
	next, err := toggle.Switch(ctx, current, op, set)
	if err != nil {
		return next, err
	}
	if u.Start != nil {
		if startErr := u.Start(ctx); startErr != nil {
			return next, fmt.Errorf("start daemon: %w", startErr)
		}
	}
	return next, nil
}

// SortChoice is the order the user chose, in precedence: the one made with
// the keys (persisted in Store), then the configured default (Configured, the
// config file's display.sort), and with neither nobody chose. Saving records
// a key choice.
type SortChoice struct {
	Store      ports.SortModeStore
	Configured func() (domain.SortMode, bool)
}

// LoadSortMode implements ports.SortModeStore.
func (c SortChoice) LoadSortMode() (domain.SortMode, bool, error) {
	mode, ok, err := c.Store.LoadSortMode()
	if err != nil || ok {
		return mode, ok, err //nolint:wrapcheck // the store's own error
	}
	if c.Configured != nil {
		if configured, has := c.Configured(); has {
			return configured, true, nil
		}
	}
	return "", false, nil
}

// SaveSortMode implements ports.SortModeStore.
func (c SortChoice) SaveSortMode(mode domain.SortMode) error {
	return c.Store.SaveSortMode(mode) //nolint:wrapcheck // the store's own error
}

// CurrentSortMode is the persisted choice, or the default when nobody chose.
func CurrentSortMode(store ports.SortModeStore) domain.SortMode {
	if store == nil {
		return domain.DefaultSortMode
	}
	mode, ok, err := store.LoadSortMode()
	if err != nil || !ok {
		return domain.DefaultSortMode
	}
	return mode
}
