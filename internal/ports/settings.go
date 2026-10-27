package ports

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

// Settings is the plugin configuration as the application sees it.
type Settings struct {
	Policy       domain.DisplayPolicy
	Observer     ObserverSettings
	Presentation PresentationSettings
	// FollowAppearance drives Herdr's theme from the desktop's light/dark.
	// Off by default: Herdr draws the sidebar on the terminal's own
	// background, and a terminal need not follow the desktop.
	FollowAppearance bool
	// ReorderWorkspaces makes Herdr's workspace order follow Scope's.
	ReorderWorkspaces bool
	// LogLevel is debug, info, warn or error.
	LogLevel string
}

// ObserverSettings tune how often the observer reads the host.
type ObserverSettings struct {
	// Heartbeat is the longest the agent list goes unread: Herdr offers no
	// cheap event for status or title changes.
	Heartbeat time.Duration
	// ReconcileInterval is the longest the whole session goes unread.
	ReconcileInterval time.Duration
}

// Variant chooses where marks come from.
type Variant string

// Variants.
const (
	// VariantAuto uses the icon font when the terminal can draw it, text
	// otherwise.
	VariantAuto Variant = "auto"
	VariantFont Variant = "font"
	VariantText Variant = "text"
	// VariantNone draws no vendor logos; state marks stay.
	VariantNone Variant = "none"
)

// Layout is how an agent or workspace entry is split into sidebar cells.
type Layout string

// Layouts.
const (
	// LayoutCompact draws each line as one cell — logo, mark and title
	// together — because Herdr separates every pair of cells with " · ".
	LayoutCompact Layout = "compact"
	// LayoutClassic splits the line into cells: the logo in a cell of its own
	// in its vendor's colour, and a second Spaces line naming the agents in
	// each workspace.
	LayoutClassic Layout = "classic"
)

// SpaceMark is what marks a workspace in the Spaces column.
type SpaceMark string

// Space marks.
const (
	// SpaceMarkLogo is the logo of the agent the mark speaks for, in its
	// state's colour, so a workspace takes one line: a question blinks the
	// question mark with the logo, a result keeps its tick.
	SpaceMarkLogo SpaceMark = "logo"
	// SpaceMarkState is the state mark, with the agents named on a second
	// line in the classic layout.
	SpaceMarkState SpaceMark = "state"
)

// LogoColor is when an agent's logo wears its vendor's colour.
type LogoColor string

// Logo colourings.
const (
	// LogoColorActive colours a logo while its agent works or waits on the
	// user; an idle one takes its title's colour.
	LogoColorActive LogoColor = "active"
	// LogoColorAlways is the vendor's colour whatever the state.
	LogoColorAlways LogoColor = "always"
)

// AgeMode is which rows show how long they have been in their state.
type AgeMode string

// Age modes.
const (
	// AgeWaiting shows it on rows that wait on the user: a question, a result.
	AgeWaiting AgeMode = "waiting"
	// AgeAll shows it on every row: the turn, the wait, the idle time.
	AgeAll AgeMode = "all"
	AgeOff AgeMode = "off"
)

// ColorOverride replaces one theme colour. Set with an empty Value means
// "keep the theme's own"; unset means Scope's default.
type ColorOverride struct {
	Set   bool
	Value string
}

// PresentationSettings are the look: glyphs and colours. None of it reaches
// the domain.
type PresentationSettings struct {
	Variant Variant
	Layout  Layout
	// WorktreeMark is the mark on a worktree header; empty for none.
	WorktreeMark string
	// ActiveRowBgLight and ActiveRowBgDark fill the selected row.
	ActiveRowBgLight ColorOverride
	ActiveRowBgDark  ColorOverride
	// Glyphs overrides state marks by display state name.
	Glyphs map[domain.DisplayState]string
	// SpaceMark is what marks a workspace in the Spaces column.
	SpaceMark SpaceMark
	// LogoColor is when a logo wears its vendor's colour.
	LogoColor LogoColor
	// ShowAge is which rows put how long they have been in their state in
	// front of their title.
	ShowAge AgeMode
}

// colorPattern accepts what Herdr's theme accepts:
// #rgb, #rrggbb, rgb(r,g,b) or a colour name.
var colorPattern = regexp.MustCompile(`^(?i:#[0-9a-f]{6}|#[0-9a-f]{3}|rgb\(\s*\d+\s*,\s*\d+\s*,\s*\d+\s*\)|[a-z]+)$`)

// ValidColor reports whether s is a colour Herdr's theme accepts.
func ValidColor(s string) bool {
	return colorPattern.MatchString(s)
}

// ErrInvalidSettings wraps every settings validation failure.
var ErrInvalidSettings = errors.New("invalid settings")

// Observer interval limits.
const (
	MinHeartbeat = 250 * time.Millisecond
	MaxHeartbeat = time.Minute
	MinReconcile = time.Second
	MaxReconcile = time.Hour
)

// Validate reports every invalid value.
func (s Settings) Validate() error {
	problems := []error{s.Policy.Validate()}
	invalid := func(format string, args ...any) {
		problems = append(problems, fmt.Errorf("%w: "+format, append([]any{ErrInvalidSettings}, args...)...))
	}
	switch s.Presentation.Variant {
	case VariantAuto, VariantFont, VariantText, VariantNone:
	default:
		invalid("variant must be auto, font, text or none, not %q", s.Presentation.Variant)
	}
	switch s.Presentation.Layout {
	case LayoutCompact, LayoutClassic:
	default:
		invalid("layout must be compact or classic, not %q", s.Presentation.Layout)
	}
	switch s.Presentation.SpaceMark {
	case SpaceMarkLogo, SpaceMarkState:
	default:
		invalid("space_mark must be logo or state, not %q", s.Presentation.SpaceMark)
	}
	switch s.Presentation.LogoColor {
	case LogoColorActive, LogoColorAlways:
	default:
		invalid("logo_color must be active or always, not %q", s.Presentation.LogoColor)
	}
	switch s.Presentation.ShowAge {
	case AgeWaiting, AgeAll, AgeOff:
	default:
		invalid("show_age must be waiting, all or off, not %q", s.Presentation.ShowAge)
	}
	for name, color := range map[string]ColorOverride{
		"colors.active_row_bg_light": s.Presentation.ActiveRowBgLight,
		"colors.active_row_bg_dark":  s.Presentation.ActiveRowBgDark,
	} {
		if color.Set && color.Value != "" && !ValidColor(color.Value) {
			invalid("%s must be a colour (hex, rgb(...) or a name), not %q", name, color.Value)
		}
	}
	if s.Observer.Heartbeat < MinHeartbeat || s.Observer.Heartbeat > MaxHeartbeat {
		invalid("observer.heartbeat_interval must be within %v..%v", MinHeartbeat, MaxHeartbeat)
	}
	if s.Observer.ReconcileInterval < MinReconcile || s.Observer.ReconcileInterval > MaxReconcile {
		invalid("observer.reconcile_interval must be within %v..%v", MinReconcile, MaxReconcile)
	}
	switch s.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		invalid("log_level must be debug, info, warn or error, not %q", s.LogLevel)
	}
	return errors.Join(problems...)
}

// DefaultWorktreeMark is Octicons' git-branch, U+F418.
const DefaultWorktreeMark = "\uF418"

// MovesWorkspaces is whether Scope moves Herdr's workspaces at all — into its
// activity order, or a nested folder under its parent — and so must hear
// when somebody else moves them.
func (s Settings) MovesWorkspaces() bool {
	return s.ReorderWorkspaces || s.Policy.NestFolders
}

// DefaultSettings is the out-of-the-box configuration (docs/design.md,
// "Look").
func DefaultSettings() Settings {
	return Settings{
		Policy: domain.DefaultDisplayPolicy(),
		Observer: ObserverSettings{
			Heartbeat:         time.Second,
			ReconcileInterval: 30 * time.Second,
		},
		Presentation: PresentationSettings{
			Variant:      VariantAuto,
			Layout:       LayoutClassic,
			WorktreeMark: DefaultWorktreeMark,
			SpaceMark:    SpaceMarkLogo,
			LogoColor:    LogoColorActive,
			ShowAge:      AgeWaiting,
		},
		FollowAppearance:  false,
		ReorderWorkspaces: false,
		LogLevel:          "info",
	}
}
