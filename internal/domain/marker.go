package domain

import "time"

// CompletionMarker is the held ✓: a turn ended and nobody has looked yet.
type CompletionMarker struct {
	SetAt time.Time
	// UntilSeen holds it until the pane is focused; otherwise Until does.
	UntilSeen bool
	Until     time.Time
}

// Active reports whether the marker still shows at now.
func (m CompletionMarker) Active(now time.Time) bool {
	return m.UntilSeen || now.Before(m.Until)
}

// BlockedMarker is the held ?: the agent asked something and has not gone
// back to work since.
type BlockedMarker struct {
	Since time.Time
	// HadWorked records whether the agent had ever worked when it asked. A
	// question before any work is a startup prompt (a trust dialog), which
	// drops straight back to idle without a turn once answered.
	HadWorked bool
}

func newCompletionMarker(now time.Time, hold DoneHold) *CompletionMarker {
	if hold.UntilSeen {
		return &CompletionMarker{SetAt: now, UntilSeen: true}
	}
	return &CompletionMarker{SetAt: now, Until: now.Add(hold.For)}
}
