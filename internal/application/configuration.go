package application

import (
	"context"
	"fmt"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// UpdateConfiguration validates and saves settings, then tells the daemon to
// apply them. Herdr's server is never restarted for it.
type UpdateConfiguration struct {
	Config  ports.ConfigRepository
	Control ports.DaemonControl
}

// UpdateOutcome says how far a save got.
type UpdateOutcome struct {
	Saved bool
	// Applied is true when a running daemon took the new settings.
	Applied bool
}

// Execute refuses invalid settings before anything is written.
func (u UpdateConfiguration) Execute(ctx context.Context, settings ports.Settings) (UpdateOutcome, error) {
	if err := settings.Validate(); err != nil {
		return UpdateOutcome{}, fmt.Errorf("%w: %w", domain.ErrConfiguration, err)
	}
	if err := u.Config.Save(ctx, settings); err != nil {
		return UpdateOutcome{}, fmt.Errorf("%w: save: %w", domain.ErrConfiguration, err)
	}
	outcome := UpdateOutcome{Saved: true}
	if u.Control == nil {
		return outcome, nil
	}
	reply, err := u.Control.Request(ctx, ports.ControlRequest{Cmd: ports.ControlReload})
	outcome.Applied = err == nil && reply.OK
	return outcome, nil
}

// AppearanceFollower drives Herdr's theme from the desktop's light/dark.
// Herdr's own auto_switch only notices a change on the next attach, so the
// daemon checks once a minute and rewrites the managed blocks on a flip.
type AppearanceFollower struct {
	Source   ports.AppearanceSource
	Managed  ports.ManagedConfig
	Reloader ports.HostReloader

	applied ports.Appearance
}

// Sync applies the current appearance when it changed since the last sync.
// It reports whether Herdr's config was rewritten.
func (f *AppearanceFollower) Sync(ctx context.Context) (bool, error) {
	appearance, err := f.Source.Current(ctx)
	if err != nil {
		return false, fmt.Errorf("appearance: %w", err)
	}
	if appearance == ports.AppearanceUnknown || appearance == f.applied {
		return false, nil
	}
	result, err := f.Managed.ApplyAppearance(appearance)
	if err != nil {
		return false, fmt.Errorf("apply appearance: %w", err)
	}
	if !result.OK {
		return false, fmt.Errorf("%w: %s", domain.ErrConfiguration, result.Message)
	}
	f.applied = appearance
	if result.Changed && f.Reloader != nil {
		if reloadErr := f.Reloader.ReloadConfig(ctx); reloadErr != nil {
			return true, fmt.Errorf("reload: %w", reloadErr)
		}
	}
	return result.Changed, nil
}
