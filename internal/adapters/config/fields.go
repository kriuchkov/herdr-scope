package config

import (
	"fmt"
	"strconv"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// FieldKind is how a setting is edited.
type FieldKind string

// Field kinds.
const (
	KindBool   FieldKind = "bool"
	KindEnum   FieldKind = "enum"
	KindNumber FieldKind = "number"
	KindGlyph  FieldKind = "glyph"
	KindColor  FieldKind = "color"
)

// Field is one row of the settings popup and one key of the config file.
// Values travel as strings so one editor handles every kind.
type Field struct {
	Key   string
	Table string
	Kind  FieldKind
	// Options are an enum's values, in cycling order.
	Options []string
	// Min, Max and Step bound a number.
	Min, Max, Step float64
	Help           string
	// Virtual fields are live state, not config keys: the popup reads and
	// switches them through other ports.
	Virtual bool
	// Get reads the value out of settings; Set writes it back.
	Get func(ports.Settings) string
	Set func(ports.Settings, string) (ports.Settings, error)
}

// Name is the key as the file spells it.
func (f Field) Name() string {
	if f.Table != "" {
		return f.Table + "." + f.Key
	}
	return f.Key
}

// Default is the value with nothing set. group_gap's follows the layout, so
// it is the one for the layout s has.
func (f Field) Default(s ports.Settings) string {
	if f.Get == nil {
		if len(f.Options) > 0 {
			return f.Options[0]
		}
		return ""
	}
	def := ports.DefaultSettings()
	def.Policy.GroupGap = groupGapDefault(s.Presentation.Layout)
	return f.Get(def)
}

// literal is a value as TOML text: numbers and booleans bare, the rest
// quoted. A done_hold of seconds is written as a number.
func (f Field) literal(value string) string {
	switch f.Kind {
	case KindBool, KindNumber:
		return value
	case KindEnum:
		if _, err := strconv.ParseFloat(value, 64); err == nil {
			return value
		}
	case KindGlyph, KindColor:
	}
	return tomlString(value)
}

// tableColors holds the row-fill overrides; untilSeen is done_hold's
// acknowledge-on-focus value.
const (
	tableColors = "colors"
	untilSeen   = "until_seen"
)

// Virtual field keys.
const (
	FieldAgentsPanel = "agents_panel"
	FieldOrder       = "order"
)

// FormatNumber spells a number as the file and the popup do: no trailing
// zeros, no exponent.
func FormatNumber(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func parseNumber(f Field, value string) (float64, error) {
	n, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %s is not a number", domain.ErrConfiguration, value)
	}
	if n < f.Min || n > f.Max {
		return 0, fmt.Errorf("%w: %s must be within %s..%s", domain.ErrConfiguration, f.Key, FormatNumber(f.Min), FormatNumber(f.Max))
	}
	return n, nil
}

func boolField(key string, help string, get func(ports.Settings) bool, set func(*ports.Settings, bool)) Field {
	return Field{
		Key: key, Kind: KindBool, Help: help,
		Get: func(s ports.Settings) string { return strconv.FormatBool(get(s)) },
		Set: func(s ports.Settings, v string) (ports.Settings, error) {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return s, fmt.Errorf("%w: %s must be true or false", domain.ErrConfiguration, key)
			}
			set(&s, b)
			return s, nil
		},
	}
}

func numberField(
	key string,
	lo, hi, step float64,
	help string,
	get func(ports.Settings) float64,
	set func(*ports.Settings, float64),
) Field {
	f := Field{Key: key, Kind: KindNumber, Min: lo, Max: hi, Step: step, Help: help}
	f.Get = func(s ports.Settings) string { return FormatNumber(get(s)) }
	f.Set = func(s ports.Settings, v string) (ports.Settings, error) {
		n, err := parseNumber(f, v)
		if err != nil {
			return s, err
		}
		set(&s, n)
		return s, nil
	}
	return f
}

func colorField(key, help string, pick func(*ports.PresentationSettings) *ports.ColorOverride, appearance ports.Appearance) Field {
	return Field{
		Key: key, Table: tableColors, Kind: KindColor, Help: help,
		Get: func(s ports.Settings) string {
			c := pick(&s.Presentation)
			if !c.Set {
				return defaultActiveRowBg(appearance)
			}
			return c.Value
		},
		Set: func(s ports.Settings, v string) (ports.Settings, error) {
			*pick(&s.Presentation) = ports.ColorOverride{Set: true, Value: v}
			return s, nil
		},
	}
}

// defaultActiveRowBg is Scope's selected-row fill for each side, which the
// theme block writes into [theme.custom]; the popup shows it as the colour
// rows' default.
func defaultActiveRowBg(appearance ports.Appearance) string {
	if appearance == ports.AppearanceDark {
		return "#232636"
	}
	return "#b9cdf2"
}

// Fields is every setting, in the popup's order.
func Fields() []Field {
	return []Field{
		{
			Key: FieldAgentsPanel, Kind: KindEnum, Options: []string{"plugin", "herdr"}, Virtual: true,
			Help: "Whose Agents panel: the plugin's rows and order, or Herdr's own. Rarely changed, so it lives here, not on a key.",
		},
		{
			Key:     FieldOrder,
			Kind:    KindEnum,
			Options: []string{"active", "recent", "here", "off"},
			Virtual: true,
			Help: "Agents panel order: active (grouped, busiest first, stale last), recent (flat, by activity), here " +
				"(active, only the focused workspace and its family) or off (Herdr's own order). Applies while agents_panel is plugin.",
		},
		boolField(
			"reorder_workspaces",
			"Make Herdr workspace indices follow Scope's activity order, so prefix+shift+1..9 follows the panel.",
			func(s ports.Settings) bool { return s.ReorderWorkspaces },
			func(s *ports.Settings, b bool) { s.ReorderWorkspaces = b },
		),
		variantField(),
		layoutField(),
		spaceMarkField(),
		logoColorField(),
		doneHoldField(),
		boolField("blocked_hold", "Keep the question mark until the agent works again.",
			func(s ports.Settings) bool { return s.Policy.BlockedHold }, func(s *ports.Settings, b bool) { s.Policy.BlockedHold = b }),
		numberField(
			"idle_grace_seconds",
			0,
			domain.MaxIdleGrace.Seconds(),
			0.5,
			"Idle must persist this long before a turn counts as ended (absorbs detection flicker).",
			func(s ports.Settings) float64 { return s.Policy.IdleGrace.Seconds() },
			func(s *ports.Settings, n float64) { s.Policy.IdleGrace = time.Duration(n * float64(time.Second)) },
		),
		numberField("activity_fresh_minutes", domain.MinFreshFor.Minutes(), domain.MaxFreshFor.Minutes(), 5,
			"An idle pane reads as fresh for this long after its last turn.",
			func(s ports.Settings) float64 { return s.Policy.FreshFor.Minutes() },
			func(s *ports.Settings, n float64) { s.Policy.FreshFor = time.Duration(n * float64(time.Minute)) }),
		numberField("activity_stale_minutes", domain.MinStaleAfter.Minutes(), domain.MaxStaleAfter.Minutes(), 30,
			"After this long without a turn an idle pane fades to stale.",
			func(s ports.Settings) float64 { return s.Policy.StaleAfter.Minutes() },
			func(s *ports.Settings, n float64) { s.Policy.StaleAfter = time.Duration(n * float64(time.Minute)) }),
		numberField(
			"group_indent",
			0,
			domain.MaxGroupIndent,
			1,
			"Spaces members sit in under a workspace header; 0 keeps headers and gaps but draws no tree lines.",
			func(s ports.Settings) float64 { return float64(s.Policy.GroupIndent) },
			func(s *ports.Settings, n float64) { s.Policy.GroupIndent = int(n) },
		),
		boolField("group_gap", "A blank row between workspace groups.",
			func(s ports.Settings) bool { return s.Policy.GroupGap }, func(s *ports.Settings, b bool) { s.Policy.GroupGap = b }),
		boolField("split_corner", "Hang the other panes of a split screen off the first with a corner; off draws them as plain rows.",
			func(s ports.Settings) bool { return s.Policy.SplitCorner }, func(s *ports.Settings, b bool) { s.Policy.SplitCorner = b }),
		showAgeField(),
		boolField("nest_folders", "Hang a workspace opened inside another workspace's folder under it, in both columns, "+
			"and keep it right below that workspace in Herdr's list (Scope moves it there).",
			func(s ports.Settings) bool { return s.Policy.NestFolders }, func(s *ports.Settings, b bool) { s.Policy.NestFolders = b }),
		boolField("mark_tabs", "End a tab's name with the wait's mark (? or ✓) while an agent in it waits on you. "+
			"Herdr then counts the tab as named, so Scope keeps a bare number in step with the tab's place.",
			func(s ports.Settings) bool { return s.Policy.MarkTabs }, func(s *ports.Settings, b bool) { s.Policy.MarkTabs = b }),
		{
			Key: "row_label", Kind: KindEnum, Options: []string{"title", "tab", "both"},
			Help: "What names an agent row: the session's title, its tab's name, or both.",
			Get:  func(s ports.Settings) string { return string(s.Policy.RowLabel) },
			Set: func(s ports.Settings, v string) (ports.Settings, error) {
				s.Policy.RowLabel = domain.RowLabel(v)
				return s, nil
			},
		},
		boolField(
			"trim_group_prefix",
			"Drop the workspace name from a title when the header above already shows it.",
			func(s ports.Settings) bool { return s.Policy.TrimGroupPrefix },
			func(s *ports.Settings, b bool) { s.Policy.TrimGroupPrefix = b },
		),
		{
			Key: "worktree_mark", Kind: KindGlyph,
			Help: "The mark on a worktree header, after the branch corner. Enter a codepoint like U+F418, or empty for none.",
			Get:  func(s ports.Settings) string { return s.Presentation.WorktreeMark },
			Set: func(s ports.Settings, v string) (ports.Settings, error) {
				s.Presentation.WorktreeMark = ParseGlyph(v)
				return s, nil
			},
		},
		boolField(
			"follow_appearance",
			"Switch Herdr's theme with the desktop's light/dark (once a minute). Only for a terminal that follows the desktop too: the sidebar sits on the terminal's background. Needs a [theme] table.",
			func(s ports.Settings) bool { return s.FollowAppearance },
			func(s *ports.Settings, b bool) { s.FollowAppearance = b },
		),
		colorField("active_row_bg_light", "Selected-row fill written to [theme.custom] for a light theme. Empty = keep the theme's own.",
			func(p *ports.PresentationSettings) *ports.ColorOverride { return &p.ActiveRowBgLight }, ports.AppearanceLight),
		colorField("active_row_bg_dark", "Selected-row fill for a dark theme. Empty = keep the theme's own.",
			func(p *ports.PresentationSettings) *ports.ColorOverride { return &p.ActiveRowBgDark }, ports.AppearanceDark),
	}
}

func variantField() Field {
	return Field{
		Key:     "variant",
		Kind:    KindEnum,
		Options: []string{"auto", "font", "text", "none"},
		Help: "Vendor logos and state marks from the icon font, plain Unicode, or none. auto uses the font only where " +
			"the terminal can draw it: fontconfig on Linux, a Ghostty/kitty codepoint map, or a patched face in iTerm2's default profile.",
		Get: func(s ports.Settings) string { return string(s.Presentation.Variant) },
		Set: func(s ports.Settings, v string) (ports.Settings, error) {
			s.Presentation.Variant = ports.Variant(v)
			return s, nil
		},
	}
}

func layoutField() Field {
	return Field{
		Key:     "layout",
		Kind:    KindEnum,
		Options: []string{string(ports.LayoutCompact), string(ports.LayoutClassic)},
		Help: "compact draws each entry as one cell (logo, mark, title), so Herdr puts no \" · \" inside it; classic " +
			"splits it into cells: the logo in its own cell and vendor colour, the agents listed under each Space.",
		Get: func(s ports.Settings) string { return string(s.Presentation.Layout) },
		Set: func(s ports.Settings, v string) (ports.Settings, error) {
			s.Presentation.Layout = ports.Layout(v)
			return s, nil
		},
	}
}

func spaceMarkField() Field {
	return Field{
		Key: "space_mark", Kind: KindEnum, Options: []string{string(ports.SpaceMarkLogo), string(ports.SpaceMarkState)},
		Help: "What marks a workspace in Spaces: logo, its agent's logo in the state's colour (a question blinks ? " +
			"with the logo, a result keeps ✓), one line per workspace; or state, the state mark with the agents named below it.",
		Get: func(s ports.Settings) string { return string(s.Presentation.SpaceMark) },
		Set: func(s ports.Settings, v string) (ports.Settings, error) {
			s.Presentation.SpaceMark = ports.SpaceMark(v)
			return s, nil
		},
	}
}

func logoColorField() Field {
	return Field{
		Key: "logo_color", Kind: KindEnum, Options: []string{string(ports.LogoColorActive), string(ports.LogoColorAlways)},
		Help: "When a logo wears its vendor's colour: active, while its agent works or waits on you (an idle " +
			"one takes its title's grey); or always.",
		Get: func(s ports.Settings) string { return string(s.Presentation.LogoColor) },
		Set: func(s ports.Settings, v string) (ports.Settings, error) {
			s.Presentation.LogoColor = ports.LogoColor(v)
			return s, nil
		},
	}
}

func showAgeField() Field {
	return Field{
		Key: "show_age", Kind: KindEnum, Options: []string{string(ports.AgeWaiting), string(ports.AgeAll), string(ports.AgeOff)},
		Help: "Put how long a row has been in its state in front of its title: waiting on rows that wait on you " +
			"(? 4m, ✓ 12m), all on every row (the turn, the idle time too), or off.",
		Get: func(s ports.Settings) string { return string(s.Presentation.ShowAge) },
		Set: func(s ports.Settings, v string) (ports.Settings, error) {
			s.Presentation.ShowAge = ports.AgeMode(v)
			return s, nil
		},
	}
}

func doneHoldField() Field {
	return Field{
		Key: "done_hold", Kind: KindEnum, Options: []string{untilSeen, "6", "15", "30", "60", "120"},
		Help: "How long the done tick stays: until you focus the pane, or a number of seconds.",
		Get: func(s ports.Settings) string {
			if s.Policy.DoneHold.UntilSeen {
				return untilSeen
			}
			return FormatNumber(s.Policy.DoneHold.For.Seconds())
		},
		Set: func(s ports.Settings, v string) (ports.Settings, error) {
			if v == untilSeen {
				s.Policy.DoneHold = domain.DoneHold{UntilSeen: true, For: domain.DefaultDisplayPolicy().DoneHold.For}
				return s, nil
			}
			n, err := strconv.ParseFloat(v, 64)
			if err != nil || n <= 0 {
				return s, fmt.Errorf("%w: done_hold must be until_seen or a number of seconds", domain.ErrConfiguration)
			}
			s.Policy.DoneHold = domain.DoneHold{For: time.Duration(n * float64(time.Second))}
			return s, nil
		},
	}
}

// fieldByName finds a field by its file name ("colors.active_row_bg_dark").
func fieldByName(name string) (Field, bool) {
	for _, f := range Fields() {
		if f.Name() == name {
			return f, true
		}
	}
	return Field{}, false
}
