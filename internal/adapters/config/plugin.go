package config

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/BurntSushi/toml"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// PluginFile is the plugin's own config.toml. Every key has a default, so a
// missing or partly broken file degrades to defaults rather than failing.
type PluginFile struct {
	path  string
	write func(path string, data []byte) error
}

// NewPluginFile reads and writes path; write must replace it atomically.
func NewPluginFile(path string, write func(path string, data []byte) error) *PluginFile {
	return &PluginFile{path: path, write: write}
}

// Path is the file's path.
func (f *PluginFile) Path() string { return f.path }

// Version changes whenever the file may have changed (size and mtime).
func (f *PluginFile) Version() string {
	info, err := os.Stat(f.path)
	if err != nil {
		return "absent"
	}
	return fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
}

func (f *PluginFile) read() (string, error) {
	raw, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("%w: read %s: %w", domain.ErrConfiguration, f.path, err)
	}
	return string(raw), nil
}

// Load returns the effective settings and a warning per value that fell back
// to its default.
func (f *PluginFile) Load(_ context.Context) (ports.Settings, []string, error) {
	text, err := f.read()
	if err != nil {
		return ports.DefaultSettings(), nil, err
	}
	settings, warnings := Parse(text)
	return settings, warnings, nil
}

// SortDefault is the panel order the file asks for when nobody has chosen
// one with the key yet (the spec's display.sort).
func (f *PluginFile) SortDefault() (domain.SortMode, bool) {
	text, err := f.read()
	if err != nil {
		return "", false
	}
	raw, _ := decode(text)
	for _, key := range []string{"sort", "order"} {
		if v, ok := lookup(raw, key).(string); ok {
			if mode, valid := domain.ParseSortMode(v); valid {
				return mode, true
			}
		}
	}
	return "", false
}

func decode(text string) (map[string]any, error) {
	raw := map[string]any{}
	if strings.TrimSpace(text) == "" {
		return raw, nil
	}
	if _, err := toml.Decode(text, &raw); err != nil {
		return map[string]any{}, fmt.Errorf("parse: %w", err)
	}
	return raw, nil
}

// lookup finds a key at the top level or in [display]; [display] wins.
func lookup(raw map[string]any, key string) any {
	if display, ok := raw["display"].(map[string]any); ok {
		if v, has := display[key]; has {
			return v
		}
	}
	return raw[key]
}

func table(raw map[string]any, name string) map[string]any {
	if t, ok := raw[name].(map[string]any); ok {
		return t
	}
	return map[string]any{}
}

// Parse interprets a config file's text.
func Parse(text string) (ports.Settings, []string) {
	return ParseLayout(text, "")
}

// ParseLayout is Parse with the layout forced (when not empty), before the
// defaults that follow it: a replay can draw either layout whatever the
// scenario's file says. The settings always pass
// Validate: a number out of range takes its nearest limit, an unknown word
// or an invalid colour its default, each with a warning.
func ParseLayout(text string, layout ports.Layout) (ports.Settings, []string) {
	s := ports.DefaultSettings()
	raw, err := decode(text)
	if err != nil {
		return s, []string{fmt.Sprintf("config.toml does not parse, using defaults: %v", err)}
	}
	p := parser{raw: raw}
	p.presentation(&s.Presentation)
	if layout != "" {
		s.Presentation.Layout = layout
	}
	s.FollowAppearance = p.boolean("follow_appearance", false)
	s.ReorderWorkspaces = p.boolean("reorder_workspaces", false)
	if v, ok := p.str("log_level"); ok {
		s.LogLevel = v
	}
	observer := table(raw, "observer")
	if d, ok := p.duration(observer, "observer.heartbeat_interval"); ok {
		s.Observer.Heartbeat = clamp(&p, "observer.heartbeat_interval", d, ports.MinHeartbeat, ports.MaxHeartbeat)
	}
	if d, ok := p.duration(observer, "observer.reconcile_interval"); ok {
		s.Observer.ReconcileInterval = clamp(&p, "observer.reconcile_interval", d, ports.MinReconcile, ports.MaxReconcile)
	}
	if lookup(raw, "render_hook") != nil {
		p.warnf("render_hook is not supported by Herdr Scope and is ignored")
	}
	// Before the policy: group_gap's default follows the settled layout.
	p.settle(&s)
	p.policy(&s.Policy, s.Presentation.Layout)
	if err = s.Validate(); err != nil {
		// settle and the clamps cover every rule Validate has; a rule added
		// without them still never reaches the daemon.
		p.warnf("%s; using the defaults", err.Error())
		return ports.DefaultSettings(), p.warnings
	}
	return s, p.warnings
}

type parser struct {
	raw      map[string]any
	warnings []string
}

func (p *parser) warnf(format string, args ...any) {
	p.warnings = append(p.warnings, fmt.Sprintf(format, args...))
}

// boolean reads a default-true key as on unless it is exactly false, and a
// default-false key as off unless it is exactly true.
func (p *parser) boolean(key string, def bool) bool {
	v := lookup(p.raw, key)
	if v == nil {
		return def
	}
	b, ok := v.(bool)
	if !ok {
		p.warnf("%s must be true or false; using %t", key, def)
		return def
	}
	return b
}

func (p *parser) number(key string) (float64, bool) {
	switch v := lookup(p.raw, key).(type) {
	case nil:
		return 0, false
	case int64:
		return float64(v), true
	case float64:
		if !math.IsNaN(v) {
			return v, true
		}
	}
	p.warnf("%s must be a number; using the default", key)
	return 0, false
}

// clamp brings v within lo..hi, with a warning when it was outside.
func clamp[T cmp.Ordered](p *parser, key string, v, lo, hi T) T {
	if v < lo || v > hi {
		v = min(hi, max(lo, v))
		p.warnf("%s must be within %v..%v; using %v", key, lo, hi, v)
	}
	return v
}

// settle puts the default back for each word and colour Validate refuses,
// with Validate's own words as the warning: each value is checked alone on
// the defaults, so the rules stay Validate's.
func (p *parser) settle(s *ports.Settings) {
	def := ports.DefaultSettings()
	for _, value := range []func(to *ports.Settings, from ports.Settings){
		func(to *ports.Settings, from ports.Settings) { to.Presentation.Variant = from.Presentation.Variant },
		func(to *ports.Settings, from ports.Settings) { to.Presentation.Layout = from.Presentation.Layout },
		func(to *ports.Settings, from ports.Settings) { to.Presentation.SpaceMark = from.Presentation.SpaceMark },
		func(to *ports.Settings, from ports.Settings) { to.Presentation.LogoColor = from.Presentation.LogoColor },
		func(to *ports.Settings, from ports.Settings) { to.Presentation.ShowAge = from.Presentation.ShowAge },
		func(to *ports.Settings, from ports.Settings) {
			to.Presentation.ActiveRowBgLight = from.Presentation.ActiveRowBgLight
		},
		func(to *ports.Settings, from ports.Settings) {
			to.Presentation.ActiveRowBgDark = from.Presentation.ActiveRowBgDark
		},
		func(to *ports.Settings, from ports.Settings) { to.LogLevel = from.LogLevel },
	} {
		alone := def
		value(&alone, *s)
		if err := alone.Validate(); err != nil {
			p.warnf("%s; using the default", strings.TrimPrefix(err.Error(), ports.ErrInvalidSettings.Error()+": "))
			value(s, def)
		}
	}
}

func (p *parser) str(key string) (string, bool) {
	switch v := lookup(p.raw, key).(type) {
	case nil:
		return "", false
	case string:
		return v, true
	default:
		p.warnf("%s must be a string; using the default", key)
		return "", false
	}
}

// groupGapDefault is group_gap with nothing set, which follows the layout. A
// blank line between groups is the classic layout's default. The compact layout separates
// groups by their headers alone: the blank is a line of the group's last
// entry, so a one-agent group's selection carried it too.
func groupGapDefault(layout ports.Layout) bool { return layout == ports.LayoutClassic }

func (p *parser) policy(policy *domain.DisplayPolicy, layout ports.Layout) {
	if hold, ok := p.doneHold(); ok {
		policy.DoneHold = hold
	}
	policy.BlockedHold = p.boolean("blocked_hold", true)
	if v, ok := p.number("idle_grace_seconds"); ok {
		v = clamp(p, "idle_grace_seconds", v, 0, domain.MaxIdleGrace.Seconds())
		policy.IdleGrace = time.Duration(v * float64(time.Second))
	}
	if v, ok := p.number("activity_fresh_minutes"); ok {
		v = clamp(p, "activity_fresh_minutes", v, domain.MinFreshFor.Minutes(), domain.MaxFreshFor.Minutes())
		policy.FreshFor = time.Duration(v * float64(time.Minute))
	}
	if v, ok := p.number("activity_stale_minutes"); ok {
		v = clamp(p, "activity_stale_minutes", v, domain.MinStaleAfter.Minutes(), domain.MaxStaleAfter.Minutes())
		policy.StaleAfter = time.Duration(v * float64(time.Minute))
	}
	if policy.StaleAfter < policy.FreshFor {
		p.warnf("activity_stale_minutes must not be below activity_fresh_minutes; using %s", FormatNumber(policy.FreshFor.Minutes()))
		policy.StaleAfter = policy.FreshFor
	}
	if v, ok := p.number("group_indent"); ok {
		policy.GroupIndent = int(math.Round(clamp(p, "group_indent", v, 0, domain.MaxGroupIndent)))
	}
	policy.GroupGap = p.boolean("group_gap", groupGapDefault(layout))
	policy.SplitCorner = p.boolean("split_corner", false)
	policy.NestFolders = p.boolean("nest_folders", true)
	policy.MarkTabs = p.boolean("mark_tabs", true)
	policy.TrimGroupPrefix = p.boolean("trim_group_prefix", true)
	policy.RowLabel = p.rowLabel()
}

// doneHold reads untilSeen, a number of seconds, or a duration ("30s").
func (p *parser) doneHold() (domain.DoneHold, bool) {
	var hold time.Duration
	switch v := lookup(p.raw, "done_hold").(type) {
	case nil:
		return domain.DoneHold{}, false
	case string:
		if v == untilSeen {
			return domain.DoneHold{UntilSeen: true, For: domain.DefaultDisplayPolicy().DoneHold.For}, true
		}
		hold, _ = time.ParseDuration(v)
	case int64:
		hold = time.Duration(v) * time.Second
	case float64:
		hold = time.Duration(v * float64(time.Second))
	}
	// Not positive also covers a number past what a duration holds.
	if hold > 0 {
		return domain.DoneHold{For: hold}, true
	}
	p.warnf("done_hold must be \"until_seen\", a number of seconds or a duration; using until_seen")
	return domain.DoneHold{}, false
}

// rowLabel is row_label when it names a mode; the older show_tab = true reads
// as both.
func (p *parser) rowLabel() domain.RowLabel {
	if v, ok := p.str("row_label"); ok {
		switch label := domain.RowLabel(v); label {
		case domain.RowLabelTitle, domain.RowLabelTab, domain.RowLabelBoth:
			return label
		}
		p.warnf("row_label must be title, tab or both; using title")
	}
	if b, ok := lookup(p.raw, "show_tab").(bool); ok && b {
		return domain.RowLabelBoth
	}
	return domain.RowLabelTitle
}

func (p *parser) presentation(pres *ports.PresentationSettings) {
	if v, ok := p.str("variant"); ok {
		pres.Variant = ports.Variant(v)
	}
	if v, ok := p.str("layout"); ok {
		pres.Layout = ports.Layout(v)
	}
	if v, ok := p.str("worktree_mark"); ok {
		pres.WorktreeMark = ParseGlyph(v)
	}
	if v, ok := p.str("space_mark"); ok {
		pres.SpaceMark = ports.SpaceMark(v)
	}
	if v, ok := p.str("logo_color"); ok {
		pres.LogoColor = ports.LogoColor(v)
	}
	pres.ShowAge = p.ageMode(pres.ShowAge)
	colors := table(p.raw, tableColors)
	for key, target := range map[string]*ports.ColorOverride{
		"active_row_bg_light": &pres.ActiveRowBgLight,
		"active_row_bg_dark":  &pres.ActiveRowBgDark,
	} {
		switch v := colors[key].(type) {
		case nil:
		case string:
			*target = ports.ColorOverride{Set: true, Value: v}
		default:
			p.warnf("colors.%s must be a string; using the default", key)
		}
	}
	glyphs := table(p.raw, "glyphs")
	pres.Glyphs = map[domain.DisplayState]string{}
	for name, v := range glyphs {
		state, known := domain.ParseDisplayState(name)
		glyph, isString := v.(string)
		if !known || !isString || glyph == "" {
			p.warnf("glyphs.%s is not a state mark Scope draws; ignored", name)
			continue
		}
		pres.Glyphs[state] = glyph
	}
	// led_glyph is upstream's single knob for the blocked/idle mark.
	if led, ok := p.str("led_glyph"); ok && led != "" {
		for _, state := range []domain.DisplayState{domain.DisplayBlocked, domain.DisplayIdle} {
			if _, set := pres.Glyphs[state]; !set {
				pres.Glyphs[state] = led
			}
		}
	}
	if len(pres.Glyphs) == 0 {
		pres.Glyphs = nil
	}
}

// ageMode reads show_age: a mode, or the older true (every row) and false.
func (p *parser) ageMode(def ports.AgeMode) ports.AgeMode {
	switch v := lookup(p.raw, "show_age").(type) {
	case nil:
		return def
	case string:
		return ports.AgeMode(v)
	case bool:
		if v {
			return ports.AgeAll
		}
		return ports.AgeOff
	}
	p.warnf("show_age must be waiting, all or off; using %s", def)
	return def
}

func (p *parser) duration(t map[string]any, name string) (time.Duration, bool) {
	key := name[strings.LastIndex(name, ".")+1:]
	switch v := t[key].(type) {
	case nil:
		return 0, false
	case string:
		if d, err := time.ParseDuration(v); err == nil {
			return d, true
		}
	case int64:
		return time.Duration(v) * time.Second, true
	case float64:
		return time.Duration(v * float64(time.Second)), true
	}
	p.warnf("%s must be a duration such as \"30s\"; using the default", name)
	return 0, false
}

// ParseGlyph reads a mark written as a codepoint ("U+F418") or as the
// character itself; empty means none. The U+ form works in a hand-written
// file as well as in the popup.
func ParseGlyph(v string) string {
	v = strings.TrimSpace(v)
	hex, prefixed := strings.CutPrefix(strings.ToUpper(v), "U+")
	if prefixed && len(hex) >= 4 && len(hex) <= 6 {
		if n, err := strconv.ParseUint(hex, 16, 32); err == nil && n <= unicode.MaxRune {
			return string(rune(n))
		}
	}
	return v
}

// Codepoint spells a mark as U+XXXX for display.
func Codepoint(glyph string) string {
	for _, r := range glyph {
		return fmt.Sprintf("U+%04X", r)
	}
	return ""
}

// Save writes only the values that differ from what the file says now, as
// line edits: the user's comments and ordering survive. Keys go where they
// already are ([display] when the file uses it, the top level otherwise).
func (f *PluginFile) Save(_ context.Context, settings ports.Settings) error {
	if err := settings.Validate(); err != nil {
		return fmt.Errorf("%w: %w", domain.ErrConfiguration, err)
	}
	text, err := f.read()
	if err != nil {
		return err
	}
	current, _ := Parse(text)
	raw, _ := decode(text)
	// group_gap's default follows the layout: one the file leaves unset reads
	// back with the layout being saved, so that is what it is compared with.
	if _, set := lookup(raw, "group_gap").(bool); !set {
		current.Policy.GroupGap = groupGapDefault(settings.Presentation.Layout)
	}
	var top, display, colors, observer []edit
	place := func(key, value string, changed bool) {
		if !changed {
			return
		}
		if _, inDisplay := table(raw, "display")[key]; inDisplay {
			display = append(display, edit{Key: key, Value: value})
			return
		}
		top = append(top, edit{Key: key, Value: value})
	}
	for _, field := range Fields() {
		if field.Virtual || field.Get == nil {
			continue
		}
		want, have := field.Get(settings), field.Get(current)
		switch field.Table {
		case tableColors:
			if want != have {
				colors = append(colors, edit{Key: field.Key, Value: tomlString(want)})
			}
		default:
			place(field.Key, field.literal(want), want != have)
		}
	}
	if settings.Observer.Heartbeat != current.Observer.Heartbeat {
		observer = append(observer, edit{Key: "heartbeat_interval", Value: tomlString(settings.Observer.Heartbeat.String())})
	}
	if settings.Observer.ReconcileInterval != current.Observer.ReconcileInterval {
		observer = append(observer, edit{Key: "reconcile_interval", Value: tomlString(settings.Observer.ReconcileInterval.String())})
	}
	place("log_level", tomlString(settings.LogLevel), settings.LogLevel != current.LogLevel)

	next := editTopLevel(text, top)
	for _, t := range []struct {
		name  string
		edits []edit
	}{{"display", display}, {tableColors, colors}, {"observer", observer}} {
		if len(t.edits) == 0 {
			continue
		}
		if edited, ok := editTable(next, t.name, t.edits); ok {
			next = edited
			continue
		}
		lines := make([]string, 0, len(t.edits))
		for _, e := range t.edits {
			lines = append(lines, e.Key+" = "+e.Value)
		}
		next = strings.TrimRight(next, "\n") + "\n\n[" + t.name + "]\n" + strings.Join(lines, "\n") + "\n"
	}
	if next == text {
		return nil
	}
	if err = os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return fmt.Errorf("%w: %w", domain.ErrConfiguration, err)
	}
	if err = f.write(f.path, []byte(strings.TrimLeft(next, "\n"))); err != nil {
		return fmt.Errorf("%w: write %s: %w", domain.ErrConfiguration, f.path, err)
	}
	return nil
}

var _ ports.ConfigRepository = (*PluginFile)(nil)
