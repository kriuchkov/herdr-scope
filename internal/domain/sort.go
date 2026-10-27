package domain

// SortMode is the Agents panel order.
type SortMode string

// Sort modes. Both of Scope's orders rank by activity and nothing else: a
// question left hanging on purpose sinks with time like everything else.
const (
	// SortActive keeps the workspace groups and ranks by activity at both
	// levels: the busiest project first, stale rows last.
	SortActive SortMode = "active"
	// SortRecent drops the grouping and ranks every row flat.
	SortRecent SortMode = "recent"
	// SortOff leaves the panel in Herdr's own order.
	SortOff SortMode = "off"
	// SortHere is the active order narrowed to the focused workspace and its
	// family: the folders nested in it, its worktrees.
	SortHere SortMode = "here"
)

// DefaultSortMode is the order nobody has chosen yet. Sorting by activity is
// the point, so a fresh install gets it without a key press.
const DefaultSortMode = SortActive

// ParseSortMode accepts the user-facing words and the internal names.
func ParseSortMode(raw string) (SortMode, bool) {
	switch raw {
	case string(SortActive), "grouped":
		return SortActive, true
	case string(SortRecent):
		return SortRecent, true
	case string(SortOff):
		return SortOff, true
	case string(SortHere):
		return SortHere, true
	default:
		return "", false
	}
}

// SortOp is a transition between sort modes.
type SortOp string

// Sort transitions, one table for the daemon and the thin client so a key
// does the same thing whether or not a daemon answered it.
const (
	// SortFlip toggles active ↔ recent and never lands on off.
	SortFlip SortOp = "flip"
	// SortCycle walks off → active → recent → off.
	SortCycle SortOp = "cycle"
	// SortNative toggles between Herdr's panel and Scope's; coming back in
	// always lands on active.
	SortNative SortOp = "native"
	// SortHereToggle narrows the panel to the focused workspace and back to
	// active.
	SortHereToggle SortOp = "here"
)

// Apply returns the mode a transition lands on.
func (m SortMode) Apply(op SortOp) SortMode {
	switch op {
	case SortFlip:
		if m == SortActive {
			return SortRecent
		}
		return SortActive
	case SortCycle:
		switch m {
		case SortOff:
			return SortActive
		case SortActive, SortHere:
			return SortRecent
		case SortRecent:
			return SortOff
		}
		return SortActive
	case SortNative:
		if m == SortOff {
			return SortActive
		}
		return SortOff
	case SortHereToggle:
		if m == SortHere {
			return SortActive
		}
		return SortHere
	}
	return m
}

// Grouped reports whether rows sit under workspace headers. Scope's active
// order groups; recent never does; off groups only when Herdr's own panel is
// in its grouped ("spaces") order.
func (m SortMode) Grouped(hostGrouped bool) bool {
	switch m {
	case SortActive, SortHere:
		return true
	case SortRecent:
		return false
	case SortOff:
		return hostGrouped
	}
	return hostGrouped
}
