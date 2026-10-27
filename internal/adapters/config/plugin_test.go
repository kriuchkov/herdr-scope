package config

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

func plainWrite(path string, data []byte) error { return os.WriteFile(path, data, 0o600) }

func TestEmptyFileIsTheDefaults(t *testing.T) {
	s, warnings := Parse("")
	assert.Empty(t, warnings)
	assert.Equal(t, ports.DefaultSettings(), s)
	assert.True(t, s.Policy.DoneHold.UntilSeen)
	assert.Equal(t, 2500*time.Millisecond, s.Policy.IdleGrace)
	assert.Equal(t, "\uF418", s.Presentation.WorktreeMark)
}

func TestGroupGapDefaultFollowsTheLayout(t *testing.T) {
	classic, _ := Parse("")
	assert.Equal(t, ports.LayoutClassic, classic.Presentation.Layout, "the classic layout is the default")
	assert.True(t, classic.Policy.GroupGap, "the classic layout keeps its blank line")
	compact, _ := Parse("layout = \"compact\"\n")
	assert.False(t, compact.Policy.GroupGap, "compact separates groups by their headers")
	forced, _ := ParseLayout("", ports.LayoutCompact)
	assert.False(t, forced.Policy.GroupGap, "a forced layout brings its own default")
	chosen, _ := Parse("group_gap = false\n")
	assert.False(t, chosen.Policy.GroupGap, "a choice in the file wins")
}

func TestShowAgeIsForWaitingRowsUnlessChosen(t *testing.T) {
	def, _ := Parse("")
	assert.Equal(t, ports.AgeWaiting, def.Presentation.ShowAge)
	for text, want := range map[string]ports.AgeMode{
		"show_age = \"all\"\n": ports.AgeAll,
		"show_age = \"off\"\n": ports.AgeOff,
		"show_age = true\n":    ports.AgeAll,
		"show_age = false\n":   ports.AgeOff,
	} {
		s, warnings := Parse(text)
		assert.Empty(t, warnings, text)
		assert.Equal(t, want, s.Presentation.ShowAge, text)
	}
	_, warnings := Parse("show_age = \"sometimes\"\n")
	assert.NotEmpty(t, warnings)
}

func TestSpaceMarkAndLogoColour(t *testing.T) {
	def, _ := Parse("")
	assert.Equal(t, ports.SpaceMarkLogo, def.Presentation.SpaceMark)
	assert.Equal(t, ports.LogoColorActive, def.Presentation.LogoColor)
	chosen, warnings := Parse("space_mark = \"state\"\nlogo_color = \"always\"\n")
	assert.Empty(t, warnings)
	assert.Equal(t, ports.SpaceMarkState, chosen.Presentation.SpaceMark)
	assert.Equal(t, ports.LogoColorAlways, chosen.Presentation.LogoColor)
	bad, warnings := Parse("space_mark = \"ring\"\nlogo_color = \"never\"\n")
	assert.Equal(t, []string{
		`space_mark must be logo or state, not "ring"; using the default`,
		`logo_color must be active or always, not "never"; using the default`,
	}, warnings, "each invalid value says what it falls back to")
	assert.Equal(t, def.Presentation, bad.Presentation)
}

func TestNestFoldersIsOnUnlessTurnedOff(t *testing.T) {
	on, _ := Parse("")
	assert.True(t, on.Policy.NestFolders)
	assert.True(t, on.MovesWorkspaces(), "nesting moves workspaces, so their moves must be heard")
	off, warnings := Parse("nest_folders = false\n")
	assert.Empty(t, warnings)
	assert.False(t, off.Policy.NestFolders)
	assert.False(t, off.MovesWorkspaces())
}

func TestTopLevelKeysFile(t *testing.T) {
	s, warnings := Parse(`
variant = "text"
done_hold = 30
blocked_hold = false
idle_grace_seconds = 0
activity_fresh_minutes = 5
activity_stale_minutes = 60
group_indent = 0
group_gap = false
split_corner = true
show_tab = true
trim_group_prefix = false
worktree_mark = ""
follow_appearance = false
reorder_workspaces = true
led_glyph = "●"

[colors]
active_row_bg_light = ""
active_row_bg_dark = "#000000"

[glyphs]
done = "✔"
`)
	assert.Empty(t, warnings)
	assert.Equal(t, ports.VariantText, s.Presentation.Variant)
	assert.Equal(t, domain.DoneHold{For: 30 * time.Second}, s.Policy.DoneHold)
	assert.False(t, s.Policy.BlockedHold)
	assert.Zero(t, s.Policy.IdleGrace)
	assert.Equal(t, 5*time.Minute, s.Policy.FreshFor)
	assert.Equal(t, time.Hour, s.Policy.StaleAfter)
	assert.Zero(t, s.Policy.GroupIndent)
	assert.False(t, s.Policy.GroupGap)
	assert.True(t, s.Policy.SplitCorner)
	assert.Equal(t, domain.RowLabelBoth, s.Policy.RowLabel, "show_tab = true reads as both")
	assert.False(t, s.Policy.TrimGroupPrefix)
	assert.Empty(t, s.Presentation.WorktreeMark)
	assert.False(t, s.FollowAppearance)
	assert.True(t, s.ReorderWorkspaces)
	assert.Equal(t, ports.ColorOverride{Set: true, Value: ""}, s.Presentation.ActiveRowBgLight, "empty keeps the theme's own")
	assert.Equal(t, ports.ColorOverride{Set: true, Value: "#000000"}, s.Presentation.ActiveRowBgDark)
	assert.Len(t, s.Presentation.Glyphs, 3)
	assert.Equal(t, "✔", s.Presentation.Glyphs[domain.DisplayDone])
	assert.Equal(t, "●", s.Presentation.Glyphs[domain.DisplayBlocked], "led_glyph marks blocked")
	assert.Equal(t, "●", s.Presentation.Glyphs[domain.DisplayIdle], "and idle")
}

func TestSpecStyleTables(t *testing.T) {
	s, warnings := Parse(`
[observer]
reconcile_interval = "45s"
heartbeat_interval = "2s"

[display]
sort = "recent"
done_hold = "2m"
row_label = "tab"
worktree_mark = "U+E725"
`)
	assert.Empty(t, warnings)
	assert.Equal(t, 45*time.Second, s.Observer.ReconcileInterval)
	assert.Equal(t, 2*time.Second, s.Observer.Heartbeat)
	assert.Equal(t, domain.DoneHold{For: 2 * time.Minute}, s.Policy.DoneHold)
	assert.Equal(t, domain.RowLabelTab, s.Policy.RowLabel)
	assert.Equal(t, "\uE725", s.Presentation.WorktreeMark, "a codepoint is read as its character")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[display]\nsort = \"recent\"\n"), 0o600))
	mode, ok := NewPluginFile(path, plainWrite).SortDefault()
	assert.True(t, ok)
	assert.Equal(t, domain.SortRecent, mode)
}

func TestBadValuesFallBackWithWarnings(t *testing.T) {
	s, warnings := Parse(`
variant = 3
done_hold = "soon"
group_gap = "no"
row_label = "pane"
render_hook = "./hook.js"

[glyphs]
sparkle = "*"
`)
	assert.Equal(t, ports.DefaultSettings().Policy, s.Policy)
	assert.Len(t, warnings, 6)
	_, warnings = Parse("this is = = not toml")
	assert.Len(t, warnings, 1)
}

func TestOutOfRangeValuesAreReported(t *testing.T) {
	s, warnings := Parse("idle_grace_seconds = 99\nactivity_stale_minutes = 5\n")
	require.Len(t, warnings, 2)
	assert.Contains(t, warnings[0], "idle_grace_seconds")
	assert.Equal(t, domain.MaxIdleGrace, s.Policy.IdleGrace, "a number past its range takes the nearest limit")
	assert.Contains(t, warnings[1], "activity_stale_minutes must not be below activity_fresh_minutes")
	assert.Equal(t, s.Policy.FreshFor, s.Policy.StaleAfter, "a pane cannot go stale before it stops being fresh")
}

func TestABadFileNeverReachesTheDaemon(t *testing.T) {
	s, warnings := Parse(`
variant = "fancy"
layout = "wide"
space_mark = "ring"
logo_color = "never"
show_age = "sometimes"
log_level = "loud"
idle_grace_seconds = nan
activity_fresh_minutes = 0
activity_stale_minutes = 1e30
group_indent = -3

[colors]
active_row_bg_light = "#b9cdf2"
active_row_bg_dark = "#12"

[observer]
heartbeat_interval = "1ms"
reconcile_interval = "0s"
`)
	require.NoError(t, s.Validate())
	assert.Len(t, warnings, 13, "every value that falls back says so")
	def := ports.DefaultSettings()
	assert.Equal(t, def.Presentation.Variant, s.Presentation.Variant, "an unknown word is its default")
	assert.Equal(t, def.Presentation.Layout, s.Presentation.Layout)
	assert.Equal(t, def.Presentation.SpaceMark, s.Presentation.SpaceMark)
	assert.Equal(t, def.Presentation.LogoColor, s.Presentation.LogoColor)
	assert.Equal(t, def.Presentation.ShowAge, s.Presentation.ShowAge)
	assert.Equal(t, def.LogLevel, s.LogLevel)
	assert.True(t, s.Policy.GroupGap, "an unknown layout is the classic one, blank line and all")
	assert.Equal(t, ports.ColorOverride{Set: true, Value: "#b9cdf2"}, s.Presentation.ActiveRowBgLight, "a valid colour stays")
	assert.Equal(t, def.Presentation.ActiveRowBgDark, s.Presentation.ActiveRowBgDark, "an invalid one is Scope's default")
	assert.Equal(t, def.Policy.IdleGrace, s.Policy.IdleGrace, "nan is not a number")
	assert.Equal(t, domain.MinFreshFor, s.Policy.FreshFor)
	assert.Equal(t, domain.MaxStaleAfter, s.Policy.StaleAfter)
	assert.Zero(t, s.Policy.GroupIndent)
	assert.Equal(t, ports.MinHeartbeat, s.Observer.Heartbeat, "no agent.list storm")
	assert.Equal(t, ports.MinReconcile, s.Observer.ReconcileInterval, "no snapshot every frame")

	_, warnings = Parse("[observer]\nheartbeat_interval = \"2h\"\nreconcile_interval = 86400\n")
	assert.Equal(t, []string{
		"observer.heartbeat_interval must be within 250ms..1m0s; using 1m0s",
		"observer.reconcile_interval must be within 1s..1h0m0s; using 1h0m0s",
	}, warnings)
}

func TestLoadReturnsOnlyValidSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("group_indent = 99\nlayout = \"wide\"\n[observer]\nreconcile_interval = \"0s\"\n"), 0o600))
	s, warnings, err := NewPluginFile(path, plainWrite).Load(context.Background())
	require.NoError(t, err)
	require.NoError(t, s.Validate())
	assert.Len(t, warnings, 3)
	assert.Equal(t, domain.MaxGroupIndent, s.Policy.GroupIndent)
}

func TestGroupGapSurvivesALayoutSwitch(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name, file string
		layout     ports.Layout
	}{
		{"classic to compact", "", ports.LayoutCompact},
		{"compact to classic", "layout = \"compact\"\n", ports.LayoutClassic},
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, os.WriteFile(path, []byte(c.file), 0o600))
		file := NewPluginFile(path, plainWrite)
		s, _, err := file.Load(ctx)
		require.NoError(t, err)

		switched := s
		switched.Presentation.Layout = c.layout
		require.NoError(t, file.Save(ctx, switched))
		reloaded, warnings, err := file.Load(ctx)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		assert.Equal(t, switched, reloaded, "%s keeps the old layout's group_gap", c.name)
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(raw), "group_gap = "+strconv.FormatBool(s.Policy.GroupGap), c.name)

		require.NoError(t, os.WriteFile(path, []byte(c.file), 0o600))
		switched.Policy.GroupGap = groupGapDefault(c.layout)
		require.NoError(t, file.Save(ctx, switched))
		raw, err = os.ReadFile(path)
		require.NoError(t, err)
		assert.NotContains(t, string(raw), "group_gap", "%s: the new layout's default needs no line", c.name)
		reloaded, _, err = file.Load(ctx)
		require.NoError(t, err)
		assert.Equal(t, switched, reloaded, c.name)
	}
}

func TestFieldDefaultsFollowTheLayout(t *testing.T) {
	compact := ports.DefaultSettings()
	compact.Presentation.Layout = ports.LayoutCompact
	gap, ok := fieldByName("group_gap")
	require.True(t, ok)
	assert.Equal(t, "true", gap.Default(ports.DefaultSettings()))
	assert.Equal(t, "false", gap.Default(compact), "compact's group_gap is off unless chosen")
	layout, _ := fieldByName("layout")
	assert.Equal(t, "classic", layout.Default(compact), "the layout keeps its own default")
}

func TestSaveWritesOnlyChangesAndKeepsComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := "# my settings\ngroup_gap = true # keep\n\n[colors]\n# fills\nactive_row_bg_dark = \"#414868\"\n"
	require.NoError(t, os.WriteFile(path, []byte(original), 0o600))
	file := NewPluginFile(path, plainWrite)
	s, _, err := file.Load(context.Background())
	require.NoError(t, err)

	s.Policy.GroupGap = false
	s.Policy.DoneHold = domain.DoneHold{For: 15 * time.Second}
	s.Presentation.ActiveRowBgLight = ports.ColorOverride{Set: true, Value: ""}
	s.Presentation.WorktreeMark = "\uE725"
	require.NoError(t, file.Save(context.Background(), s))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(raw)
	assert.Contains(t, text, "# my settings\ngroup_gap = false\n")
	assert.Contains(t, text, "done_hold = 15\n", "seconds are written as a number")
	assert.Contains(t, text, `worktree_mark = "\uE725"`, "a private-use mark is written visibly")
	assert.Contains(t, text, "# fills\nactive_row_bg_dark = \"#414868\"\nactive_row_bg_light = \"\"\n")
	assert.NotContains(t, text, "variant", "unchanged defaults are not written")

	reloaded, warnings, err := file.Load(context.Background())
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, s.Policy, reloaded.Policy)
	assert.Equal(t, s.Presentation.WorktreeMark, reloaded.Presentation.WorktreeMark)

	before := file.Version()
	require.NoError(t, file.Save(context.Background(), reloaded))
	assert.Equal(t, before, file.Version(), "saving what the file already says writes nothing")
}

func TestSaveCreatesTheFileAndRefusesInvalidSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	file := NewPluginFile(path, plainWrite)
	s := ports.DefaultSettings()
	s.Observer.ReconcileInterval = time.Minute
	require.NoError(t, file.Save(context.Background(), s))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "[observer]\nreconcile_interval = \"1m0s\"\n", string(raw))

	s.Policy.GroupIndent = 99
	require.ErrorIs(t, file.Save(context.Background(), s), domain.ErrConfiguration)
}

func TestFieldsRoundTrip(t *testing.T) {
	s := ports.DefaultSettings()
	for _, f := range Fields() {
		if f.Virtual {
			continue
		}
		updated, err := f.Set(s, f.Get(s))
		require.NoError(t, err, f.Name())
		assert.Equal(t, f.Get(s), f.Get(updated), f.Name())
	}
	f, ok := fieldByName("idle_grace_seconds")
	require.True(t, ok)
	_, err := f.Set(s, "31")
	require.ErrorIs(t, err, domain.ErrConfiguration)
	f, _ = fieldByName("colors.active_row_bg_dark")
	assert.Equal(t, "#232636", f.Default(s))
	assert.Equal(t, "U+F418", Codepoint("\uF418"))
}
