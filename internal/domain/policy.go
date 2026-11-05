package domain

import (
	"errors"
	"fmt"
	"time"
)

// DoneHold says how long the ✓ survives after a turn ends.
type DoneHold struct {
	// UntilSeen keeps it until the pane is focused.
	UntilSeen bool
	// For is the hold when UntilSeen is false.
	For time.Duration
}

// RowLabel picks what names an agent row.
type RowLabel string

// Row labels.
const (
	// RowLabelTitle shows the session's own title.
	RowLabelTitle RowLabel = "title"
	// RowLabelTab shows the tab's name, or the title when the tab was never
	// named.
	RowLabelTab RowLabel = "tab"
	// RowLabelBoth puts the tab's name in front of the title.
	RowLabelBoth RowLabel = "both"
)

// DisplayPolicy is every setting that changes what the sidebar says or how
// it is structured. Colours and glyphs are presentation and live outside the
// domain.
type DisplayPolicy struct {
	DoneHold DoneHold
	// BlockedHold keeps the ? until the agent works again.
	BlockedHold bool
	// IdleGrace is how long idle must persist to count as a finished turn;
	// it absorbs Herdr's screen detector flapping to idle mid-turn.
	IdleGrace time.Duration
	// FreshFor is how long after its last turn an idle pane reads as fresh.
	FreshFor time.Duration
	// StaleAfter is how long without a turn before an idle pane goes stale.
	StaleAfter time.Duration
	// RowLabel names agent rows.
	RowLabel RowLabel
	// TrimGroupPrefix drops the workspace name from a title when the group
	// header above already shows it.
	TrimGroupPrefix bool
	// GroupIndent is the spaces per tree level; 0 keeps headers and gaps but
	// draws no tree lines.
	GroupIndent int
	// GroupGap puts a blank row between groups.
	GroupGap bool
	// SplitCorner hangs the other panes of a split screen off the first.
	SplitCorner bool
	// NestFolders hangs a workspace opened inside another workspace's folder
	// under that workspace, in both columns, and keeps it right below it in
	// Herdr's workspace list.
	NestFolders bool
	// MarkTabs puts the mark of a wait at the end of the tab's name while an
	// agent in it waits on the user.
	MarkTabs bool
}

// Policy limits, matching the ranges the settings popup enforces.
const (
	MaxIdleGrace   = 30 * time.Second
	MinFreshFor    = time.Minute
	MaxFreshFor    = 1440 * time.Minute
	MinStaleAfter  = time.Minute
	MaxStaleAfter  = 10080 * time.Minute
	MaxGroupIndent = 8
	defaultDoneFor = 6 * time.Second
)

// DefaultDisplayPolicy is the out-of-the-box behaviour (docs/design.md).
func DefaultDisplayPolicy() DisplayPolicy {
	return DisplayPolicy{
		DoneHold:        DoneHold{UntilSeen: true, For: defaultDoneFor},
		BlockedHold:     true,
		IdleGrace:       2500 * time.Millisecond,
		FreshFor:        15 * time.Minute,
		StaleAfter:      120 * time.Minute,
		RowLabel:        RowLabelTitle,
		TrimGroupPrefix: true,
		GroupIndent:     2,
		GroupGap:        true,
		SplitCorner:     false,
		NestFolders:     true,
		MarkTabs:        true,
	}
}

// ErrInvalidPolicy wraps every policy validation failure.
var ErrInvalidPolicy = errors.New("invalid display policy")

// Validate reports every value outside its range.
func (p DisplayPolicy) Validate() error {
	var problems []error
	check := func(ok bool, format string, args ...any) {
		if !ok {
			problems = append(problems, fmt.Errorf("%w: "+format, append([]any{ErrInvalidPolicy}, args...)...))
		}
	}
	check(p.DoneHold.UntilSeen || p.DoneHold.For > 0, "done_hold must be until_seen or a positive duration")
	check(p.IdleGrace >= 0 && p.IdleGrace <= MaxIdleGrace, "idle_grace_seconds must be within 0..%v", MaxIdleGrace.Seconds())
	check(p.FreshFor >= MinFreshFor && p.FreshFor <= MaxFreshFor,
		"activity_fresh_minutes must be within %v..%v", MinFreshFor.Minutes(), MaxFreshFor.Minutes())
	check(p.StaleAfter >= MinStaleAfter && p.StaleAfter <= MaxStaleAfter,
		"activity_stale_minutes must be within %v..%v", MinStaleAfter.Minutes(), MaxStaleAfter.Minutes())
	check(p.StaleAfter >= p.FreshFor, "activity_stale_minutes must not be below activity_fresh_minutes")
	check(p.GroupIndent >= 0 && p.GroupIndent <= MaxGroupIndent, "group_indent must be within 0..%d", MaxGroupIndent)
	check(p.RowLabel == RowLabelTitle || p.RowLabel == RowLabelTab || p.RowLabel == RowLabelBoth,
		"row_label must be title, tab or both")
	return errors.Join(problems...)
}

// treeLines reports whether the indent draws tree corners at all.
func (p DisplayPolicy) treeLines() bool {
	return p.GroupIndent > 0
}
