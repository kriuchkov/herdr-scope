package config

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// Managed block names.
const (
	BlockTabBar  = "tab-bar"
	BlockTheme   = "theme"
	BlockSidebar = "sidebar"
)

// markers fence one managed block. Nothing outside them is ever edited.
func markers(block string) (string, string) {
	return "# >>> HERDR-SCOPE-BEGIN " + block, "# <<< HERDR-SCOPE-END " + block
}

// The tables the sidebar block declares. TOML allows each once, so a copy the
// user wrote outside the markers keeps the block that would declare it out —
// never merged — and the rest still installs. The theme block's tables depend
// on the theme (themeSide.tables).
func sidebarTables() []string {
	return []string{"ui.sidebar.agents", "ui.sidebar.agents.rows_by_agent", "ui.sidebar.spaces"}
}

// The [theme] table and its name key, which the appearance switch drives.
const (
	tableTheme = "theme"
	keyName    = "name"
)

// keepBackups bounds the backups directory; every write makes one.
const keepBackups = 20

// tabBarInterval is how often Herdr runs the tab-bar command — it types out a
// file, so a short interval costs a cat; the line leads with who waits, which
// should not lag long. The timeout is derived so it can never reach the
// interval (an overlap feeds itself).
const (
	tabBarInterval      = 3
	tabBarTimeoutMargin = 1
)

// ManagedDeps are what the managed blocks are generated from.
type ManagedDeps struct {
	// File is Herdr's config.toml.
	File string
	// StateDir holds backups, the theme origin and the tab-bar cache.
	StateDir string
	// Check is Herdr's own verdict on the file: parses is false only for a
	// parse error (Herdr then runs on defaults, taking every plugin down).
	Check func(ctx context.Context) (parses bool, output string)
	// Appearance is the desktop's appearance, or unknown.
	Appearance func() ports.Appearance
	// Settings are the plugin settings in force.
	Settings func() ports.Settings
	// SidebarBlock renders the [ui.sidebar.*] tables for one appearance.
	SidebarBlock func(appearance ports.Appearance) (string, error)
	// Windows switches the tab-bar command to cmd syntax.
	Windows bool
	Write   func(path string, data []byte) error
	Now     func() time.Time
}

// Managed implements ports.ManagedConfig and ports.HostConfig.
type Managed struct {
	deps ManagedDeps

	mu        sync.Mutex
	groupedAt time.Time
	grouped   bool
}

// panelCache bounds how often the grouped/priority toggle is re-read; the
// observer asks every frame.
const panelCache = 2 * time.Second

// NewManaged binds the managed blocks to Herdr's config file.
func NewManaged(deps ManagedDeps) *Managed {
	return &Managed{deps: deps}
}

var (
	uiHeader      = regexp.MustCompile(`(?m)^(\s*\[ui\]\s*)$`)
	tabBarRight   = regexp.MustCompile(`(?m)^\s*tab_bar_right\s*=`)
	panelSortLine = regexp.MustCompile(`(?m)^\s*agent_panel_sort\s*=\s*"([^"]+)"`)
)

func (m *Managed) read() (string, bool, error) {
	raw, err := os.ReadFile(m.deps.File)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("%w: read %s: %w", domain.ErrConfiguration, m.deps.File, err)
	}
	return string(raw), true, nil
}

// Inspect describes what an install would find.
func (m *Managed) Inspect() (ports.ManagedReport, error) {
	text, exists, err := m.read()
	if err != nil {
		return ports.ManagedReport{File: m.deps.File}, err
	}
	report := ports.ManagedReport{File: m.deps.File, State: inspectState(text, exists)}
	for _, block := range []string{BlockTabBar, BlockTheme, BlockSidebar} {
		if start, _ := markers(block); strings.Contains(text, start) {
			report.Blocks = append(report.Blocks, block)
		}
	}
	return report, nil
}

// SidebarRowsOn reports whether the Agents panel's rows are Scope's: the
// sidebar block is installed. An unreadable file reads as off.
func (m *Managed) SidebarRowsOn() bool {
	report, err := m.Inspect()
	return err == nil && slices.Contains(report.Blocks, BlockSidebar)
}

func inspectState(text string, exists bool) ports.ManagedState {
	start, _ := markers(BlockTabBar)
	switch {
	case !exists:
		return ports.ManagedMissing
	case strings.Contains(text, start):
		return ports.ManagedInstalled
	case tabBarRight.MatchString(text):
		return ports.ManagedForeignEntry
	case !uiHeader.MatchString(text):
		return ports.ManagedNoUITable
	}
	return ports.ManagedInstallable
}

// PanelGrouped reads Herdr's own grouped/priority toggle, which persists in
// config.toml and nowhere else. Unreadable reads as Herdr's default, grouped.
func (m *Managed) PanelGrouped() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.deps.Now()
	if !m.groupedAt.IsZero() && now.Sub(m.groupedAt) < panelCache {
		return m.grouped
	}
	m.groupedAt, m.grouped = now, true
	if text, _, err := m.read(); err == nil {
		match := panelSortLine.FindStringSubmatch(text)
		m.grouped = match == nil || match[1] != "priority"
	}
	return m.grouped
}

// foreignTables are the tables the file claims outside the managed blocks.
func foreignTables(text string, tables []string) []string {
	outside := text
	for _, block := range []string{BlockTheme, BlockSidebar} {
		start, end := markers(block)
		outside = dropBlock(outside, start, end)
	}
	var claimed []string
	for _, t := range tables {
		if claimsTable(outside, t) {
			claimed = append(claimed, t)
		}
	}
	return claimed
}

func yours(tables []string) string {
	return fmt.Sprintf("[%s] is already in your config.toml, and TOML allows a table once", strings.Join(tables, "], ["))
}

// tabBarBlock is the [ui] entry that types out the cached tab-bar line. The
// command picks the per-session file from HERDR_SESSION, which only a named
// session's server has.
func (m *Managed) tabBarBlock() string {
	start, end := markers(BlockTabBar)
	plain := filepath.Join(m.deps.StateDir, "tabbar.txt")
	var command string
	if m.deps.Windows {
		named := filepath.Join(m.deps.StateDir, "tabbar-%HERDR_SESSION%.txt")
		command = fmt.Sprintf(`if defined HERDR_SESSION (type "%s") else (type "%s")`, named, plain)
	} else {
		command = fmt.Sprintf(`cat "%s"`, strings.TrimSuffix(filepath.ToSlash(plain), ".txt")+"${HERDR_SESSION:+-$HERDR_SESSION}.txt")
	}
	return strings.Join([]string{
		start,
		`tab_bar_right_separator = " · "`,
		"tab_bar_right = [",
		fmt.Sprintf("  { type = 'command', command = '%s', interval_seconds = %d, timeout_seconds = %d },",
			command, tabBarInterval, tabBarInterval-tabBarTimeoutMargin),
		"]",
		end,
	}, "\n")
}

// themeSide is which side of the palette the rows and the row fill are
// written for, and how Herdr's theme picks its own.
type themeSide struct {
	appearance ports.Appearance
	// follows: auto_switch is on, so Herdr picks its side from the terminal
	// and the row fill goes in per side ([theme.custom.light|dark]).
	follows bool
	// host: the "terminal" theme draws in the terminal's own colours, so
	// there is no row fill to override.
	host bool
}

// tables are the tables this side's theme block declares.
func (t themeSide) tables() []string {
	if t.follows {
		return []string{"theme.custom.light", "theme.custom.dark"}
	}
	return []string{"theme.custom"}
}

// sideOf reads the side off Herdr's [theme]. Herdr leaves the sidebar's
// background to the terminal (sidebar_bg is Reset in every built-in theme)
// while it colours its own text from the theme, so the theme in force is the
// best witness to the side the rows sit on — the desktop is not: a dark
// terminal on a light desktop is common, and rows written for the desktop
// then read dark on dark.
//
//   - follow_appearance drives [theme] name from the desktop, so there the
//     desktop decides — provided there is a [theme] table to drive;
//   - under auto_switch or the host-following "terminal" theme Herdr goes by
//     the terminal, which Scope cannot ask; the desktop stands in, as a
//     terminal that switches usually switches with it, and dark without one;
//   - otherwise the configured name, Herdr's own catppuccin when unset.
//
// Asking the desktop runs a process, so only those first two cases ask.
func (m *Managed) sideOf(text string) themeSide {
	_, hasTheme := findTable(text, tableTheme)
	followed := m.deps.Settings().FollowAppearance && hasTheme
	raw, ok := themeRaw(text, "auto_switch")
	autoSwitch := ok && raw == "true"
	name, ok := themeValue(text, keyName)
	if !ok {
		name = herdrDefaultTheme
	}
	name = strings.NewReplacer(" ", "-", "_", "-").Replace(strings.ToLower(name))
	desktop := ports.AppearanceUnknown
	if (followed || autoSwitch || name == "terminal") && m.deps.Appearance != nil {
		desktop = m.deps.Appearance()
	}
	if followed && desktop != ports.AppearanceUnknown {
		return themeSide{appearance: desktop}
	}
	guess := cmp.Or(desktop, ports.AppearanceDark)
	switch {
	case autoSwitch:
		return themeSide{appearance: guess, follows: true}
	case name == "terminal":
		return themeSide{appearance: guess, host: true}
	case slices.Contains(lightThemes(), name):
		return themeSide{appearance: ports.AppearanceLight}
	}
	return themeSide{appearance: ports.AppearanceDark}
}

// herdrDefaultTheme is the theme Herdr runs with no [theme] name.
const herdrDefaultTheme = "catppuccin"

// lightThemes are Herdr's light built-ins under every name it accepts for
// them (canonical_theme_name); every other name, unknown ones included —
// Herdr falls back to catppuccin — is dark. The list lives here alone: the
// sidebar block is handed the side it picks, never the theme's name.
func lightThemes() []string {
	return []string{
		"catppuccin-latte", "latte", "light",
		"tokyo-night-day", "tokyo-day", "tokyonight-day",
		"gruvbox-light",
		"one-light", "onelight",
		"solarized-light",
		"kanagawa-lotus", "lotus",
		"rose-pine-dawn", "rosepine-dawn", "dawn",
	}
}

// themeBlock overrides one chrome token, active_row_bg, and nothing else: a
// whole copied palette would pin every colour and stop theme switches
// working. Under auto_switch both sides go in and Herdr picks; otherwise the
// one side in force. Empty when the user's overrides drop every fill.
func (m *Managed) themeBlock(side themeSide) string {
	pres := m.deps.Settings().Presentation
	fill := func(appearance ports.Appearance) string {
		override := pres.ActiveRowBgLight
		if appearance == ports.AppearanceDark {
			override = pres.ActiveRowBgDark
		}
		if override.Set {
			return override.Value
		}
		return defaultActiveRowBg(appearance)
	}
	var lines []string
	if side.follows {
		for _, appearance := range []ports.Appearance{ports.AppearanceLight, ports.AppearanceDark} {
			if value := fill(appearance); value != "" {
				lines = append(lines, "[theme.custom."+string(appearance)+"]", "active_row_bg = "+tomlString(value))
			}
		}
	} else if value := fill(side.appearance); value != "" {
		lines = append(lines, "[theme.custom]", "active_row_bg = "+tomlString(value))
	}
	if len(lines) == 0 {
		return ""
	}
	start, end := markers(BlockTheme)
	return strings.Join(slices.Concat([]string{start}, lines, []string{end}), "\n")
}

// withThemeBlock installs, rewrites or drops the theme block for side.
func (m *Managed) withThemeBlock(text string, side themeSide) string {
	start, end := markers(BlockTheme)
	if body := m.themeBlock(side); body != "" && !side.host && len(foreignTables(text, side.tables())) == 0 {
		return upsertTail(text, start, end, body)
	}
	return dropBlock(text, start, end)
}

func (m *Managed) sidebarBody(appearance ports.Appearance) (string, error) {
	body, err := m.deps.SidebarBlock(appearance)
	if err != nil {
		return "", err
	}
	start, end := markers(BlockSidebar)
	return start + "\n" + body + "\n" + end, nil
}

// Apply installs or repairs the managed blocks.
func (m *Managed) Apply() (ports.ManagedResult, error) {
	text, exists, err := m.read()
	if err != nil {
		return ports.ManagedResult{}, err
	}
	switch state := inspectState(text, exists); state {
	case ports.ManagedMissing:
		return ports.ManagedResult{Message: "herdr: config.toml not found at " + m.deps.File}, nil
	case ports.ManagedForeignEntry:
		return ports.ManagedResult{Message: "herdr: refused — tab_bar_right is already set by hand; merge it yourself"}, nil
	case ports.ManagedNoUITable:
		return ports.ManagedResult{Message: "herdr: refused — no [ui] table to extend"}, nil
	case ports.ManagedInstalled, ports.ManagedInstallable:
	}
	installed := strings.Contains(text, firstMarker(BlockTabBar))
	side := m.sideOf(text)
	foreignTheme := foreignTables(text, side.tables())
	foreignSidebar := foreignTables(text, sidebarTables())

	next := text
	if installed {
		next = replaceBlock(next, BlockTabBar, m.tabBarBlock())
	} else {
		// Right under the header, so its keys sit in [ui]; dropInline is the
		// exact inverse.
		loc := uiHeader.FindStringSubmatchIndex(next)
		next = next[:loc[3]] + "\n" + m.tabBarBlock() + next[loc[3]:]
	}
	next = m.withThemeBlock(next, side)
	sidebarStart, sidebarEnd := markers(BlockSidebar)
	if len(foreignSidebar) > 0 {
		next = dropBlock(next, sidebarStart, sidebarEnd)
	} else {
		body, bodyErr := m.sidebarBody(side.appearance)
		if bodyErr != nil {
			return ports.ManagedResult{}, bodyErr
		}
		next = upsertTail(next, sidebarStart, sidebarEnd, body)
	}
	result, err := m.checkedWrite(text, next)
	if err != nil || !result.OK {
		return result, err
	}

	themeNote := fmt.Sprintf("theme (%s)", side.appearance)
	switch {
	case len(foreignTheme) > 0:
		themeNote = "theme skipped — " + yours(foreignTheme)
	case side.host:
		themeNote = "theme skipped — the terminal theme draws in the terminal's own colours"
	case side.follows:
		themeNote = fmt.Sprintf("theme (both sides, auto_switch picks; rows %s)", side.appearance)
	}
	sidebarNote := "sidebar"
	if len(foreignSidebar) > 0 {
		sidebarNote = "sidebar skipped — " + yours(foreignSidebar) +
			", so the Agents panel stays Herdr's own; delete that table and run configure again for the plugin's"
	}
	verb := "installed"
	if installed {
		verb = "refreshed"
	}
	result.Skipped = slices.Concat(foreignTheme, foreignSidebar)
	result.Message = fmt.Sprintf("herdr: tab-bar, %s and %s %s", sidebarNote, themeNote, verb)
	return result, nil
}

// Refresh rewrites installed blocks that drifted from what this version and
// configuration write — the glyph variant, the palette, the row colours, the
// tab-bar command. It never installs an absent block: with the panel handed
// back to Herdr, the sidebar block's absence is the state.
func (m *Managed) Refresh() (ports.ManagedResult, error) {
	text, exists, err := m.read()
	if err != nil || !exists {
		return ports.ManagedResult{OK: err == nil, Message: "herdr: nothing to refresh"}, err
	}
	next := text
	if strings.Contains(next, firstMarker(BlockTabBar)) {
		next = replaceBlock(next, BlockTabBar, m.tabBarBlock())
	}
	side := m.sideOf(text)
	if strings.Contains(next, firstMarker(BlockTheme)) {
		next = m.withThemeBlock(next, side)
	}
	if strings.Contains(next, firstMarker(BlockSidebar)) {
		start, end := markers(BlockSidebar)
		if len(foreignTables(text, sidebarTables())) > 0 {
			next = dropBlock(next, start, end)
		} else {
			body, bodyErr := m.sidebarBody(side.appearance)
			if bodyErr != nil {
				return ports.ManagedResult{}, bodyErr
			}
			next = upsertTail(next, start, end, body)
		}
	}
	if next == text {
		return ports.ManagedResult{OK: true, Message: "herdr: managed blocks current"}, nil
	}
	result, err := m.checkedWrite(text, next)
	if err == nil && result.OK {
		result.Message = "herdr: managed blocks refreshed"
	}
	return result, err
}

// SetSidebarRows installs or removes only the sidebar block — whether the
// Agents panel's rows are Scope's at all.
func (m *Managed) SetSidebarRows(on bool) (ports.ManagedResult, error) {
	text, exists, err := m.read()
	if err != nil {
		return ports.ManagedResult{}, err
	}
	if !exists {
		return ports.ManagedResult{Message: "herdr: config.toml not found"}, nil
	}
	start, end := markers(BlockSidebar)
	if strings.Contains(text, start) == on {
		return ports.ManagedResult{OK: true, Message: fmt.Sprintf("herdr: sidebar rows already %s", onOff(on))}, nil
	}
	next := dropBlock(text, start, end)
	if on {
		if foreign := foreignTables(text, sidebarTables()); len(foreign) > 0 {
			return ports.ManagedResult{Message: "herdr: refused — " + yours(foreign) + "; delete it and try again"}, nil
		}
		body, bodyErr := m.sidebarBody(m.sideOf(text).appearance)
		if bodyErr != nil {
			return ports.ManagedResult{}, bodyErr
		}
		next = upsertTail(text, start, end, body)
	}
	result, err := m.checkedWrite(text, next)
	if err == nil && result.OK {
		result.Message = fmt.Sprintf("herdr: sidebar rows %s", onOff(on))
	}
	return result, err
}

// ApplyAppearance puts Herdr in the desktop's appearance: its own theme for
// that side, chosen by light_name/dark_name, and the row fill to match.
// auto_switch goes off — Herdr only checks it on attach — and its earlier
// value is recorded so unconfigure can put it back.
func (m *Managed) ApplyAppearance(appearance ports.Appearance) (ports.ManagedResult, error) {
	if appearance != ports.AppearanceLight && appearance != ports.AppearanceDark {
		return ports.ManagedResult{Message: "herdr: unknown appearance"}, nil
	}
	text, exists, err := m.read()
	if err != nil || !exists {
		return ports.ManagedResult{Message: "herdr: config.toml not found"}, err
	}
	key, fallback := "light_name", "catppuccin-latte"
	if appearance == ports.AppearanceDark {
		key, fallback = "dark_name", "catppuccin"
	}
	wanted, ok := themeValue(text, key)
	if !ok {
		wanted = fallback
	}
	next, ok := editTable(text, tableTheme, []edit{{Key: keyName, Value: tomlString(wanted)}, {Key: "auto_switch", Value: "false"}})
	if !ok {
		return ports.ManagedResult{Message: "herdr: no [theme] table"}, nil
	}
	next = m.withThemeBlock(next, themeSide{appearance: appearance})
	sidebarStart, sidebarEnd := markers(BlockSidebar)
	switch {
	case len(foreignTables(text, sidebarTables())) > 0:
		next = dropBlock(next, sidebarStart, sidebarEnd)
	case strings.Contains(next, sidebarStart):
		body, bodyErr := m.sidebarBody(appearance)
		if bodyErr != nil {
			return ports.ManagedResult{}, bodyErr
		}
		next = upsertTail(next, sidebarStart, sidebarEnd, body)
	}
	if next == text {
		return ports.ManagedResult{OK: true, Message: "herdr: appearance current"}, nil
	}
	if err = m.recordThemeOrigin(text); err != nil {
		return ports.ManagedResult{}, err
	}
	result, err := m.checkedWrite(text, next)
	if err == nil && result.OK {
		result.Message = fmt.Sprintf("herdr: %s (%s)", appearance, wanted)
	}
	return result, err
}

// Remove takes every managed block out and restores what [theme] said
// before Scope drove it.
func (m *Managed) Remove() (ports.ManagedResult, error) {
	text, exists, err := m.read()
	if err != nil || !exists {
		return ports.ManagedResult{OK: err == nil, Message: "herdr: nothing to remove"}, err
	}
	start, end := markers(BlockTabBar)
	next := dropInline(text, start, end)
	for _, block := range []string{BlockTheme, BlockSidebar} {
		start, end = markers(block)
		next = dropBlock(next, start, end)
	}
	next = m.restoreThemeOrigin(next)
	if next == text {
		return ports.ManagedResult{OK: true, Message: "herdr: no managed blocks to remove"}, nil
	}
	if _, err = m.backup(text); err != nil {
		return ports.ManagedResult{}, err
	}
	if err = m.deps.Write(m.deps.File, []byte(next)); err != nil {
		return ports.ManagedResult{}, fmt.Errorf("%w: write %s: %w", domain.ErrConfiguration, m.deps.File, err)
	}
	return ports.ManagedResult{OK: true, Changed: true, Message: "herdr: managed blocks removed, [theme] restored"}, nil
}

// checkedWrite writes next and asks Herdr whether it still parses; if not,
// the previous text goes back at once and Herdr's diagnostics become the
// message. A config.toml that fails to parse takes every plugin down.
func (m *Managed) checkedWrite(previous, next string) (ports.ManagedResult, error) {
	if next == previous {
		return ports.ManagedResult{OK: true}, nil
	}
	saved, err := m.backup(previous)
	if err != nil {
		return ports.ManagedResult{}, err
	}
	if err = m.deps.Write(m.deps.File, []byte(next)); err != nil {
		return ports.ManagedResult{}, fmt.Errorf("%w: write %s: %w", domain.ErrConfiguration, m.deps.File, err)
	}
	parses, output := true, ""
	if m.deps.Check != nil {
		parses, output = m.deps.Check(context.Background())
	}
	if parses {
		return ports.ManagedResult{OK: true, Changed: true}, nil
	}
	if err = m.deps.Write(m.deps.File, []byte(previous)); err != nil {
		return ports.ManagedResult{}, fmt.Errorf("%w: restore %s: %w", domain.ErrConfiguration, m.deps.File, err)
	}
	why := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(output, "config: issues found")), "; using defaults"))
	return ports.ManagedResult{Message: fmt.Sprintf(
		"herdr: refused — the config would not parse with the managed blocks added; restored from %s. %s",
		filepath.ToSlash(saved), why)}, nil
}

func (m *Managed) backup(text string) (string, error) {
	dir := filepath.Join(m.deps.StateDir, "backups")
	stamp := strings.NewReplacer(":", "-", ".", "-").Replace(m.deps.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
	target := filepath.Join(dir, filepath.Base(m.deps.File)+"."+stamp)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("%w: backup: %w", domain.ErrConfiguration, err)
	}
	if err := os.WriteFile(target, []byte(text), 0o600); err != nil {
		return "", fmt.Errorf("%w: backup: %w", domain.ErrConfiguration, err)
	}
	pruneBackups(dir, filepath.Base(m.deps.File)+".")
	return target, nil
}

func pruneBackups(dir, prefix string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keepBackups {
		_ = os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

type themeOrigin struct {
	Name       *string `json:"name"`
	AutoSwitch *string `json:"auto_switch"`
}

func (m *Managed) originPath() string { return filepath.Join(m.deps.StateDir, "theme-origin.json") }

// recordThemeOrigin keeps what [theme] said before the first appearance
// write. name and auto_switch are the user's keys, driven only while Scope is
// installed; a missing key is recorded as null and deleted on restore.
func (m *Managed) recordThemeOrigin(text string) error {
	if _, err := os.Stat(m.originPath()); err == nil {
		return nil
	}
	origin := themeOrigin{}
	if v, ok := themeRaw(text, keyName); ok {
		origin.Name = &v
	}
	if v, ok := themeRaw(text, "auto_switch"); ok {
		origin.AutoSwitch = &v
	}
	raw, err := json.MarshalIndent(origin, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: theme origin: %w", domain.ErrConfiguration, err)
	}
	if err = m.deps.Write(m.originPath(), append(raw, '\n')); err != nil {
		return fmt.Errorf("%w: theme origin: %w", domain.ErrConfiguration, err)
	}
	return nil
}

func (m *Managed) restoreThemeOrigin(text string) string {
	raw, err := os.ReadFile(m.originPath())
	if err != nil {
		return text
	}
	var origin themeOrigin
	if json.Unmarshal(raw, &origin) != nil {
		return text
	}
	edits := []edit{{Key: keyName, Delete: origin.Name == nil}, {Key: "auto_switch", Delete: origin.AutoSwitch == nil}}
	if origin.Name != nil {
		edits[0].Value = *origin.Name
	}
	if origin.AutoSwitch != nil {
		edits[1].Value = *origin.AutoSwitch
	}
	restored, ok := editTable(text, tableTheme, edits)
	_ = os.Remove(m.originPath())
	if !ok {
		return text
	}
	return restored
}

func replaceBlock(text, block, body string) string {
	start, end := markers(block)
	pattern := regexp.MustCompile(regexp.QuoteMeta(start) + `[\s\S]*?` + regexp.QuoteMeta(end))
	return replaceFirst(pattern, text, body)
}

func firstMarker(block string) string {
	start, _ := markers(block)
	return start
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

var (
	_ ports.ManagedConfig = (*Managed)(nil)
	_ ports.HostConfig    = (*Managed)(nil)
)
