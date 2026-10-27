package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/ports"
)

type managedFixture struct {
	t          *testing.T
	file       string
	state      string
	parses     bool
	checks     int
	appearance ports.Appearance
	asked      int
	settings   ports.Settings
	variant    ports.Variant
	managed    *Managed
}

func newManaged(t *testing.T, config string) *managedFixture {
	t.Helper()
	dir := t.TempDir()
	f := &managedFixture{
		t: t, file: filepath.Join(dir, "config.toml"), state: filepath.Join(dir, "state"),
		parses: true, appearance: ports.AppearanceDark, settings: ports.DefaultSettings(), variant: ports.VariantText,
	}
	if config != "" {
		require.NoError(t, os.WriteFile(f.file, []byte(config), 0o600))
	}
	clock := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	f.managed = NewManaged(ManagedDeps{
		File: f.file, StateDir: f.state,
		Check: func(context.Context) (bool, string) {
			f.checks++
			if f.parses {
				return true, "config: ok"
			}
			return false, "config: issues found\nconfig parse error: duplicate key `custom`\n; using defaults"
		},
		Appearance: func() ports.Appearance {
			f.asked++
			return f.appearance
		},
		Settings: func() ports.Settings { return f.settings },
		SidebarBlock: func(a ports.Appearance) (string, error) {
			return "[ui.sidebar.agents]\n# logo glyphs: " + string(
				f.variant,
			) + "\nrows = [[\"$title_idle\"]]\n\n[ui.sidebar.spaces]\nrow_gap = 0\n# appearance " + string(
				a,
			), nil
		},
		Write: plainWrite,
		Now: func() time.Time {
			clock = clock.Add(time.Millisecond)
			return clock
		},
	})
	return f
}

func (f *managedFixture) text() string {
	raw, err := os.ReadFile(f.file)
	require.NoError(f.t, err)
	return string(raw)
}

const userConfig = "# my herdr config\n[ui]\nsidebar_width = 30 # wide\n\n[ui.toast]\ndelivery = \"herdr\"\n\n[theme]\nname = \"tokyo-night\"\nauto_switch = true\nlight_name = \"tokyo-night-day\"\n"

func TestApplyInstallsBlocksAndKeepsTheRest(t *testing.T) {
	f := newManaged(t, userConfig)
	report, err := f.managed.Inspect()
	require.NoError(t, err)
	assert.Equal(t, ports.ManagedInstallable, report.State)

	result, err := f.managed.Apply()
	require.NoError(t, err)
	require.True(t, result.OK, result.Message)
	assert.Contains(t, result.Message, "installed")
	text := f.text()
	assert.True(
		t,
		strings.HasPrefix(text, "# my herdr config\n[ui]\n# >>> HERDR-SCOPE-BEGIN tab-bar\n"),
		"the tab-bar block sits in [ui]",
	)
	assert.Contains(t, text, "sidebar_width = 30 # wide\n", "the user's keys and comments survive")
	assert.Contains(t, text, "# >>> HERDR-SCOPE-BEGIN theme\n"+
		"[theme.custom.light]\nactive_row_bg = \"#b9cdf2\"\n[theme.custom.dark]\nactive_row_bg = \"#232636\"\n"+
		"# <<< HERDR-SCOPE-END theme", "under auto_switch both fills go in and Herdr picks")
	assert.Contains(t, text, "# appearance dark", "the desktop stands in for the terminal")
	assert.Contains(t, text, "interval_seconds = 3, timeout_seconds = 2")
	var parsed map[string]any
	_, err = toml.Decode(text, &parsed)
	require.NoError(t, err, text)
	assert.Equal(t, 1, f.checks, "every write is checked by Herdr")

	report, _ = f.managed.Inspect()
	assert.Equal(t, ports.ManagedInstalled, report.State)
	assert.Equal(t, []string{BlockTabBar, BlockTheme, BlockSidebar}, report.Blocks)

	result, err = f.managed.Apply()
	require.NoError(t, err)
	assert.Contains(t, result.Message, "refreshed")
	assert.False(t, result.Changed, "a repeat install changes nothing")
	assert.Equal(t, text, f.text())
}

func TestApplyRefusals(t *testing.T) {
	result, err := newManaged(t, "").managed.Apply()
	require.NoError(t, err)
	assert.False(t, result.OK)
	assert.Contains(t, result.Message, "not found")

	result, _ = newManaged(t, "[ui]\ntab_bar_right = []\n").managed.Apply()
	assert.Contains(t, result.Message, "tab_bar_right is already set by hand")

	f := newManaged(t, "[theme]\nname = \"x\"\n")
	result, _ = f.managed.Apply()
	assert.Contains(t, result.Message, "no [ui] table")
	assert.Equal(t, "[theme]\nname = \"x\"\n", f.text(), "a refusal writes nothing")
}

func TestForeignTablesKeepTheirBlockOut(t *testing.T) {
	f := newManaged(t, "[ui]\nstatus_indicators = \"symbols\"\n\n[ui.sidebar.spaces]\nrow_gap = 1\n")
	result, err := f.managed.Apply()
	require.NoError(t, err)
	require.True(t, result.OK)
	assert.Equal(t, []string{"ui.sidebar.spaces"}, result.Skipped)
	assert.Contains(t, result.Message, "sidebar skipped")
	assert.NotContains(t, f.text(), "HERDR-SCOPE-BEGIN sidebar")
	assert.Contains(t, f.text(), "HERDR-SCOPE-BEGIN tab-bar", "the rest still installs")

	g := newManaged(t, "[ui]\n[theme]\ncustom.accent = \"#fff\"\n")
	result, err = g.managed.Apply()
	require.NoError(t, err)
	assert.Equal(t, []string{"theme.custom"}, result.Skipped)
	assert.NotContains(t, g.text(), "HERDR-SCOPE-BEGIN theme")
}

func TestUnparsableWriteIsRestored(t *testing.T) {
	f := newManaged(t, userConfig)
	f.parses = false
	result, err := f.managed.Apply()
	require.NoError(t, err)
	assert.False(t, result.OK)
	assert.Contains(t, result.Message, "duplicate key `custom`")
	assert.NotContains(t, result.Message, "using defaults")
	assert.Equal(t, userConfig, f.text(), "the broken write was undone")
	backups, _ := filepath.Glob(filepath.Join(f.state, "backups", "config.toml.*"))
	assert.Len(t, backups, 1)
}

func TestNoThemeBlockForAHostFollowingTheme(t *testing.T) {
	f := newManaged(t, "[ui]\n[theme]\nname = \"terminal\"\n")
	f.appearance = ports.AppearanceLight
	result, err := f.managed.Apply()
	require.NoError(t, err)
	assert.Contains(t, result.Message, "theme skipped")
	assert.NotContains(t, f.text(), "HERDR-SCOPE-BEGIN theme")
	assert.Contains(t, f.text(), "# appearance light", "the desktop stands in for the terminal")

	f = newManaged(t, "[ui]\n[theme]\nname = \"terminal\"\n")
	f.appearance = ports.AppearanceUnknown
	_, err = f.managed.Apply()
	require.NoError(t, err)
	assert.Contains(t, f.text(), "# appearance dark", "and dark without one")
}

// The case behind the change: no [theme] table, a light desktop, a dark
// terminal. Herdr runs catppuccin, a dark theme, so the rows must be dark.
func TestRowsFollowHerdrsThemeNotTheDesktop(t *testing.T) {
	for _, follow := range []bool{false, true} {
		f := newManaged(t, "[ui]\nstatus_indicators = \"symbols\"\n")
		f.appearance = ports.AppearanceLight
		f.settings.FollowAppearance = follow
		result, err := f.managed.Apply()
		require.NoError(t, err)
		assert.Contains(t, result.Message, "theme (dark)", "follow %t", follow)
		assert.Contains(t, f.text(), "# appearance dark", "with no [theme] to drive, the desktop decides nothing")
		assert.Contains(t, f.text(), "[theme.custom]\nactive_row_bg = \"#232636\"")
	}

	cases := map[string]ports.Appearance{
		"latte": ports.AppearanceLight, "Rose Pine Dawn": ports.AppearanceLight, "gruvbox-light": ports.AppearanceLight,
		"tokyo-night": ports.AppearanceDark, "no-such-theme": ports.AppearanceDark,
	}
	for name, want := range cases {
		f := newManaged(t, "[ui]\n[theme]\nname = \""+name+"\"\n")
		f.appearance = ports.AppearanceUnknown
		_, err := f.managed.Apply()
		require.NoError(t, err)
		assert.Contains(t, f.text(), "# appearance "+string(want), name)
	}
}

func TestFollowAppearanceLetsTheDesktopDecide(t *testing.T) {
	f := newManaged(t, "[ui]\n[theme]\nname = \"catppuccin\"\n")
	f.appearance = ports.AppearanceLight
	f.settings.FollowAppearance = true
	_, err := f.managed.Apply()
	require.NoError(t, err)
	assert.Contains(t, f.text(), "# appearance light")
	assert.Contains(t, f.text(), "[theme.custom]\nactive_row_bg = \"#b9cdf2\"")
}

// Asking the desktop runs a process (defaults, gsettings, reg); only the
// cases it decides may pay for it.
func TestTheDesktopIsAskedOnlyWhenItDecides(t *testing.T) {
	f := newManaged(t, "")
	for _, c := range []struct {
		text   string
		follow bool
		asks   bool
	}{
		{"[ui]\n", false, false},
		{"[ui]\n", true, false},
		{"[theme]\nname = \"latte\"\n", false, false},
		{"[theme]\nname = \"latte\"\n", true, true},
		{"[theme]\nname = \"tokyo-night\"\nauto_switch = true\n", false, true},
		{"[theme]\nname = \"terminal\"\n", false, true},
	} {
		f.asked = 0
		f.settings.FollowAppearance = c.follow
		f.managed.sideOf(c.text)
		assert.Equal(t, c.asks, f.asked > 0, "%q, follow %t", c.text, c.follow)
		assert.LessOrEqual(t, f.asked, 1, "asked once at most")
	}
}

func TestEmptyColourOverrideDropsTheThemeBlock(t *testing.T) {
	f := newManaged(t, userConfig)
	f.settings.Presentation.ActiveRowBgDark = ports.ColorOverride{Set: true, Value: ""}
	_, err := f.managed.Apply()
	require.NoError(t, err)
	assert.NotContains(t, f.text(), "[theme.custom.dark]", "the theme's own value shows through")
	assert.Contains(t, f.text(), "[theme.custom.light]")

	f = newManaged(t, "[ui]\n")
	f.settings.Presentation.ActiveRowBgDark = ports.ColorOverride{Set: true, Value: ""}
	_, err = f.managed.Apply()
	require.NoError(t, err)
	assert.NotContains(t, f.text(), "HERDR-SCOPE-BEGIN theme", "nothing left to override, no block")
}

func TestApplyAppearanceDrivesTheThemeAndRemoveRestoresIt(t *testing.T) {
	f := newManaged(t, userConfig)
	_, err := f.managed.Apply()
	require.NoError(t, err)

	result, err := f.managed.ApplyAppearance(ports.AppearanceLight)
	require.NoError(t, err)
	require.True(t, result.OK, result.Message)
	assert.Equal(t, "herdr: light (tokyo-night-day)", result.Message)
	text := f.text()
	assert.Contains(t, text, "[theme]\nname = \"tokyo-night-day\"\nauto_switch = false\n")
	assert.Contains(t, text, "active_row_bg = \"#b9cdf2\"")
	assert.Contains(t, text, "# appearance light", "the sidebar block follows the appearance")

	result, _ = f.managed.ApplyAppearance(ports.AppearanceLight)
	assert.False(t, result.Changed, "the same appearance twice writes once")

	result, _ = f.managed.ApplyAppearance(ports.AppearanceDark)
	assert.Contains(t, result.Message, "(catppuccin)", "no dark_name falls back to catppuccin")

	result, err = f.managed.Remove()
	require.NoError(t, err)
	assert.True(t, result.OK)
	text = f.text()
	assert.NotContains(t, text, "HERDR-SCOPE")
	assert.Contains(
		t,
		text,
		"[theme]\nname = \"tokyo-night\"\nauto_switch = true\nlight_name = \"tokyo-night-day\"\n",
		"[theme] is what it was",
	)
	assert.Contains(t, text, "sidebar_width = 30 # wide")
}

func TestApplyAppearanceNeedsATheme(t *testing.T) {
	f := newManaged(t, "[ui]\n")
	result, err := f.managed.ApplyAppearance(ports.AppearanceDark)
	require.NoError(t, err)
	assert.Equal(t, "herdr: no [theme] table", result.Message)
}

func TestSidebarRowsToggle(t *testing.T) {
	f := newManaged(t, userConfig)
	_, err := f.managed.Apply()
	require.NoError(t, err)
	assert.True(t, f.managed.SidebarRowsOn())
	result, err := f.managed.SetSidebarRows(false)
	require.NoError(t, err)
	assert.True(t, result.Changed)
	assert.False(t, f.managed.SidebarRowsOn(), "the panel is Herdr's again")
	assert.NotContains(t, f.text(), "HERDR-SCOPE-BEGIN sidebar")
	assert.Contains(t, f.text(), "HERDR-SCOPE-BEGIN tab-bar", "only the rows go")

	result, _ = f.managed.SetSidebarRows(false)
	assert.False(t, result.Changed)

	result, err = f.managed.Refresh()
	require.NoError(t, err)
	assert.NotContains(t, f.text(), "HERDR-SCOPE-BEGIN sidebar", "a refresh never puts handed-back rows back")
	_ = result

	result, _ = f.managed.SetSidebarRows(true)
	assert.True(t, result.Changed)
	assert.Contains(t, f.text(), "HERDR-SCOPE-BEGIN sidebar")
	assert.True(t, f.managed.SidebarRowsOn())
	assert.False(t, newManaged(t, "").managed.SidebarRowsOn(), "no config.toml, no rows")
}

func TestRefreshRewritesADriftedBlock(t *testing.T) {
	f := newManaged(t, userConfig)
	_, err := f.managed.Apply()
	require.NoError(t, err)
	result, err := f.managed.Refresh()
	require.NoError(t, err)
	assert.False(t, result.Changed)

	f.variant = ports.VariantFont
	result, err = f.managed.Refresh()
	require.NoError(t, err)
	assert.True(t, result.Changed, "a block written for another glyph table is rewritten")
	assert.Contains(t, f.text(), "# logo glyphs: font")
}

func TestPanelGrouped(t *testing.T) {
	assert.True(t, newManaged(t, "[ui]\n").managed.PanelGrouped())
	assert.False(t, newManaged(t, "[ui]\nagent_panel_sort = \"priority\"\n").managed.PanelGrouped())
	assert.True(t, newManaged(t, "").managed.PanelGrouped(), "unreadable is Herdr's default")
}

func TestBackupsArePruned(t *testing.T) {
	f := newManaged(t, userConfig)
	for range keepBackups + 5 {
		_, _ = f.managed.SetSidebarRows(true)
		_, _ = f.managed.SetSidebarRows(false)
	}
	backups, _ := filepath.Glob(filepath.Join(f.state, "backups", "config.toml.*"))
	assert.Len(t, backups, keepBackups)
}
