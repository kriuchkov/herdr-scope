package application

import (
	"context"
	"fmt"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// SettingsRequest is what the settings popup saves: the config file's values
// plus the two live switches it shows beside them.
type SettingsRequest struct {
	Settings ports.Settings
	// PanelOn says whether the Agents panel's rows are Scope's; PanelChanged
	// whether the popup changed it.
	PanelOn      bool
	PanelChanged bool
	// Order is the panel order; OrderChanged whether the popup changed it.
	Order        domain.SortMode
	OrderChanged bool
}

// SaveSettings is the popup's save: validate everything first, write the
// config file, switch the panel (rows first, then the order, as the native
// toggle does), and have the daemon apply the new settings — no Herdr
// restart, no daemon restart.
type SaveSettings struct {
	Update UpdateConfiguration
	// Rows installs or removes the sidebar rows when the panel switches.
	Rows SortModeToggle
	// View switches the order through the daemon when one answers, so its
	// own re-assert cannot undo the switch.
	View ToggleSortMode
}

// SaveOutcome reports what took effect.
type SaveOutcome struct {
	UpdateOutcome

	// Mode is the panel order now in force.
	Mode domain.SortMode
}

// Execute saves the request. Nothing is written when validation fails.
func (u SaveSettings) Execute(ctx context.Context, request SettingsRequest) (SaveOutcome, error) {
	if err := request.Settings.Validate(); err != nil {
		return SaveOutcome{}, fmt.Errorf("%w: %w", domain.ErrConfiguration, err)
	}
	outcome := SaveOutcome{Mode: request.Order}
	if request.PanelChanged && u.Rows.Managed != nil {
		if err := u.Rows.switchRows(ctx, request.PanelOn); err != nil {
			return outcome, err
		}
	}
	if request.PanelChanged || request.OrderChanged {
		mode := request.Order
		if !request.PanelOn {
			// Herdr's own panel has no override: the order is off, whatever
			// the order row said.
			mode = domain.SortOff
		}
		next, err := u.View.Execute(ctx, "", mode, false)
		if err != nil {
			return outcome, fmt.Errorf("could not switch the Agents panel order: %w", err)
		}
		outcome.Mode = next
	}
	updated, err := u.Update.Execute(ctx, request.Settings)
	outcome.UpdateOutcome = updated
	return outcome, err
}
