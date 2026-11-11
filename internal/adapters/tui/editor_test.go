package tui

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/adapters/config"
	"github.com/kriuchkov/herdr-scope/internal/application"
	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// fakeBackend stands in for the config file, the two live switches and the
// save use case; a save it accepts becomes what the next load reads.
type fakeBackend struct {
	settings ports.Settings
	panelOn  bool
	order    domain.SortMode
	outcome  application.SaveOutcome
	saveErr  error
	loads    int
	requests []application.SettingsRequest
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		settings: ports.DefaultSettings(),
		panelOn:  true,
		order:    domain.SortActive,
		outcome:  application.SaveOutcome{UpdateOutcome: application.UpdateOutcome{Saved: true, Applied: true}},
	}
}

func (b *fakeBackend) deps() Deps {
	return Deps{
		Fields: config.Fields(),
		Load: func(context.Context) (ports.Settings, error) {
			b.loads++
			return b.settings, nil
		},
		PanelOn: func() bool { return b.panelOn },
		Order:   func() domain.SortMode { return b.order },
		Save: func(_ context.Context, req application.SettingsRequest) (application.SaveOutcome, error) {
			b.requests = append(b.requests, req)
			if b.saveErr != nil {
				return application.SaveOutcome{}, b.saveErr
			}
			b.settings, b.panelOn, b.order = req.Settings, req.PanelOn, req.Order
			return b.outcome, nil
		},
		Title:      "herdr-scope settings",
		PluginID:   "herdr-scope",
		ConfigPath: "/cfg/config.toml",
	}
}

func newTestEditor(t *testing.T, b *fakeBackend) *editor {
	t.Helper()
	e, err := newEditor(t.Context(), b.deps())
	require.NoError(t, err)
	return e
}

func rowIndex(t *testing.T, e *editor, name string) int {
	t.Helper()
	for i, f := range e.deps.Fields {
		if f.Name() == name {
			return i
		}
	}
	require.Failf(t, "no such row", "%s", name)
	return -1
}

// goTo walks the cursor down to a row, the way a user would.
func goTo(t *testing.T, e *editor, name string) {
	t.Helper()
	want := rowIndex(t, e, name)
	for e.cursor != want {
		require.False(t, e.Key(t.Context(), "j"))
	}
}

func value(t *testing.T, e *editor, name string) string {
	t.Helper()
	return e.values[rowIndex(t, e, name)]
}

func press(t *testing.T, e *editor, keys ...string) {
	t.Helper()
	for _, k := range keys {
		require.False(t, e.Key(t.Context(), k), "key %q closed the popup", k)
	}
}

// typeText replaces the edit buffer with text.
func typeText(t *testing.T, e *editor, text string) {
	t.Helper()
	for e.buffer != "" {
		press(t, e, "\x7f")
	}
	for _, r := range text {
		press(t, e, string(r))
	}
}

func TestNewEditorReadsEveryRow(t *testing.T) {
	b := newFakeBackend()
	b.panelOn = false
	b.order = domain.SortRecent
	e := newTestEditor(t, b)
	assert.Equal(t, "herdr", value(t, e, config.FieldAgentsPanel))
	assert.Equal(t, "recent", value(t, e, config.FieldOrder))
	assert.Equal(t, "auto", value(t, e, "variant"))
	assert.Equal(t, "2.5", value(t, e, "idle_grace_seconds"))
	assert.Equal(t, ports.DefaultWorktreeMark, value(t, e, "worktree_mark"))
	assert.False(t, e.dirty())
}

func TestNewEditorFailsWhenLoadFails(t *testing.T) {
	deps := newFakeBackend().deps()
	deps.Load = func(context.Context) (ports.Settings, error) { return ports.Settings{}, errors.New("broken toml") }
	_, err := newEditor(t.Context(), deps)
	require.ErrorContains(t, err, "broken toml")
}

func TestNavigationWrapsAround(t *testing.T) {
	e := newTestEditor(t, newFakeBackend())
	last := len(e.deps.Fields) - 1

	press(t, e, "\x1b[A")
	assert.Equal(t, last, e.cursor, "up from the first row lands on the last")
	press(t, e, "\x1b[B")
	assert.Equal(t, 0, e.cursor, "down from the last row lands on the first")
	press(t, e, "j", "j")
	assert.Equal(t, 2, e.cursor)
	press(t, e, "k")
	assert.Equal(t, 1, e.cursor)
}

func TestStepCyclesEnumBothWays(t *testing.T) {
	e := newTestEditor(t, newFakeBackend())
	goTo(t, e, "variant")

	press(t, e, "\x1b[C")
	assert.Equal(t, "font", value(t, e, "variant"))
	press(t, e, "l", " ", "+")
	assert.Equal(t, "auto", value(t, e, "variant"), "stepping past the last option wraps to the first")
	press(t, e, "\x1b[D")
	assert.Equal(t, "none", value(t, e, "variant"), "stepping back from the first option wraps to the last")
	press(t, e, "h", "-")
	assert.Equal(t, "font", value(t, e, "variant"))
	press(t, e, "\r")
	assert.Equal(t, "text", value(t, e, "variant"), "enter steps an enum forward")
	assert.False(t, e.editing)
}

func TestStepTogglesBool(t *testing.T) {
	e := newTestEditor(t, newFakeBackend())
	goTo(t, e, "reorder_workspaces")

	press(t, e, "\x1b[C")
	assert.Equal(t, "true", value(t, e, "reorder_workspaces"))
	press(t, e, "\x1b[D")
	assert.Equal(t, "false", value(t, e, "reorder_workspaces"))
	press(t, e, "\r")
	assert.Equal(t, "true", value(t, e, "reorder_workspaces"), "enter toggles a bool")
	assert.False(t, e.editing)
}

func TestStepNumberMovesByStepWithinBounds(t *testing.T) {
	e := newTestEditor(t, newFakeBackend())
	goTo(t, e, "idle_grace_seconds")

	press(t, e, "+")
	assert.Equal(t, "3", value(t, e, "idle_grace_seconds"))
	for range 100 {
		press(t, e, "+")
	}
	assert.Equal(t, "30", value(t, e, "idle_grace_seconds"), "clamped to Max")
	for range 100 {
		press(t, e, "-")
	}
	assert.Equal(t, "0", value(t, e, "idle_grace_seconds"), "clamped to Min")

	goTo(t, e, "activity_fresh_minutes")
	press(t, e, "-", "-")
	assert.Equal(t, "5", value(t, e, "activity_fresh_minutes"))
	press(t, e, "-")
	assert.Equal(t, "1", value(t, e, "activity_fresh_minutes"), "a step below Min lands on Min")
}

func TestEditNumberCommitsClampedOrRejects(t *testing.T) {
	e := newTestEditor(t, newFakeBackend())
	goTo(t, e, "idle_grace_seconds")

	press(t, e, "\r")
	require.True(t, e.editing)
	assert.Equal(t, "2.5", e.buffer, "editing starts from the current value")
	assert.Contains(t, e.Render(84, 26), "type a value · ↵ confirm · esc cancel")

	typeText(t, e, "99")
	press(t, e, "\r")
	assert.False(t, e.editing)
	assert.Equal(t, "30", value(t, e, "idle_grace_seconds"), "clamped to Max")

	press(t, e, "\r")
	typeText(t, e, " 1.25 ")
	press(t, e, "\r")
	assert.Equal(t, "1.25", value(t, e, "idle_grace_seconds"), "trimmed, and not rounded to the step")

	press(t, e, "\r")
	typeText(t, e, "soon")
	press(t, e, "\r")
	assert.Equal(t, "1.25", value(t, e, "idle_grace_seconds"))
	assert.Contains(t, e.status, "not a number")
	assert.Contains(t, e.Render(84, 26), sgrWarn+"not a number")

	press(t, e, "\r")
	typeText(t, e, "Inf")
	press(t, e, "\r")
	assert.Equal(t, "1.25", value(t, e, "idle_grace_seconds"), "an infinity is not a number either")

	press(t, e, "\r")
	typeText(t, e, "7")
	press(t, e, "\x1b")
	assert.False(t, e.editing)
	assert.Equal(t, "1.25", value(t, e, "idle_grace_seconds"), "esc drops the typed text")
}

func TestEditGlyphTakesCodepointCharacterOrNothing(t *testing.T) {
	e := newTestEditor(t, newFakeBackend())
	goTo(t, e, "worktree_mark")

	press(t, e, "\x1b[C")
	require.True(t, e.editing, "stepping a glyph opens the editor")
	assert.Equal(t, "U+F418", e.buffer)

	typeText(t, e, "u+e0a0")
	press(t, e, "\r")
	assert.Equal(t, "\uE0A0", value(t, e, "worktree_mark"))

	press(t, e, "\r")
	typeText(t, e, "→x")
	press(t, e, "\r")
	assert.Equal(t, "→", value(t, e, "worktree_mark"), "only the first character is kept")

	press(t, e, "\r")
	typeText(t, e, "")
	press(t, e, "\r")
	assert.Empty(t, value(t, e, "worktree_mark"))
	assert.Contains(t, e.Render(84, 26), sgrDim+"none"+sgrReset)
}

func TestEditColorAcceptsHerdrsFormats(t *testing.T) {
	e := newTestEditor(t, newFakeBackend())
	goTo(t, e, "colors.active_row_bg_dark")

	for _, ok := range []string{"#123", "#A1b2C3", "rgb(1, 2, 3)", "navy"} {
		press(t, e, "\r")
		typeText(t, e, ok)
		press(t, e, "\r")
		assert.Equal(t, ok, value(t, e, "colors.active_row_bg_dark"))
		assert.Empty(t, e.status)
	}

	press(t, e, "\r")
	typeText(t, e, "#12345")
	press(t, e, "\r")
	assert.Equal(t, "navy", value(t, e, "colors.active_row_bg_dark"))
	assert.Contains(t, e.status, "not a colour (hex, rgb(...) or a name)")

	press(t, e, "\r")
	typeText(t, e, "")
	press(t, e, "\r")
	assert.Empty(t, value(t, e, "colors.active_row_bg_dark"))
	assert.Contains(t, e.Render(84, 26), sgrDim+"theme's own"+sgrReset)
}

func TestEditBackspaceRemovesWholeCharacter(t *testing.T) {
	e := newTestEditor(t, newFakeBackend())
	goTo(t, e, "worktree_mark")
	press(t, e, "\r")
	typeText(t, e, "a→")
	press(t, e, "\b")
	assert.Equal(t, "a", e.buffer)
	press(t, e, "\x1b[A", "\x03")
	assert.Equal(t, "a", e.buffer, "escape sequences and control keys are not typed")
	assert.True(t, e.editing, "ctrl-c does not close the popup mid-edit")
}

func TestResetRestoresTheDefault(t *testing.T) {
	b := newFakeBackend()
	b.panelOn = false
	b.order = domain.SortOff
	b.settings.Presentation.Variant = ports.VariantText
	e := newTestEditor(t, b)

	goTo(t, e, config.FieldAgentsPanel)
	press(t, e, "r")
	assert.Equal(t, "plugin", value(t, e, config.FieldAgentsPanel))
	goTo(t, e, config.FieldOrder)
	press(t, e, "r")
	assert.Equal(t, "active", value(t, e, config.FieldOrder))
	goTo(t, e, "variant")
	press(t, e, "r")
	assert.Equal(t, "auto", value(t, e, "variant"))
	goTo(t, e, "colors.active_row_bg_light")
	press(t, e, "r")
	assert.Equal(t, "#b9cdf2", value(t, e, "colors.active_row_bg_light"))
	assert.True(t, e.dirty())
}

func TestGroupGapDefaultFollowsTheLayoutRow(t *testing.T) {
	b := newFakeBackend()
	b.settings.Presentation.Layout = ports.LayoutCompact
	b.settings.Policy.GroupGap = false
	e := newTestEditor(t, b)
	gap := "\n     group_gap" + strings.Repeat(" ", 28-len("group_gap")+1)
	assert.Contains(t, e.Render(84, 40), gap+sgrDim+"off  (default)"+sgrReset, "compact's own default")

	goTo(t, e, "group_gap")
	press(t, e, "l", "r")
	assert.Equal(t, "false", value(t, e, "group_gap"), "r resets to compact's default, not classic's")

	goTo(t, e, "layout")
	press(t, e, "l")
	require.Equal(t, "classic", value(t, e, "layout"))
	assert.Contains(t, e.Render(84, 40), gap+"off\n", "under classic off is a choice")
	goTo(t, e, "group_gap")
	press(t, e, "r")
	assert.Equal(t, "true", value(t, e, "group_gap"))
}

func TestOrderOffersHere(t *testing.T) {
	b := newFakeBackend()
	e := newTestEditor(t, b)
	goTo(t, e, config.FieldOrder)
	press(t, e, "l", "l")
	assert.Equal(t, "here", value(t, e, config.FieldOrder))
	press(t, e, "s")
	require.Len(t, b.requests, 1)
	assert.Equal(t, domain.SortHere, b.requests[0].Order)
	assert.True(t, b.requests[0].OrderChanged)

	press(t, e, "l")
	assert.Equal(t, "off", value(t, e, config.FieldOrder), "here steps on like any other order")
}

func TestQuitAsksOnceWhenDirty(t *testing.T) {
	ctx := t.Context()
	assert.True(t, newTestEditor(t, newFakeBackend()).Key(ctx, "q"), "a clean popup closes at once")

	for _, quit := range []string{"q", "\x1b", "\x03"} {
		e := newTestEditor(t, newFakeBackend())
		goTo(t, e, "variant")
		press(t, e, "l")

		assert.False(t, e.Key(ctx, quit))
		assert.Contains(t, e.status, "unsaved changes — q again to discard, s to save")
		assert.True(t, e.Key(ctx, quit), "the second %q discards", quit)
	}

	e := newTestEditor(t, newFakeBackend())
	goTo(t, e, "variant")
	press(t, e, "l")
	assert.False(t, e.Key(ctx, "q"))
	press(t, e, "j")
	assert.Empty(t, e.status, "any key clears the warning")
	assert.False(t, e.Key(ctx, "q"), "any other key disarms the confirm")
}

func TestSaveSendsEveryRowAndTheLiveSwitches(t *testing.T) {
	b := newFakeBackend()
	e := newTestEditor(t, b)
	ctx := t.Context()

	goTo(t, e, config.FieldOrder)
	press(t, e, "l")
	goTo(t, e, "variant")
	press(t, e, "l")
	goTo(t, e, "group_indent")
	press(t, e, "+")
	press(t, e, "s")

	require.Len(t, b.requests, 1)
	req := b.requests[0]
	want := ports.DefaultSettings()
	want.Presentation.Variant = ports.VariantFont
	want.Policy.GroupIndent = 3
	// The colour rows write their shown default back as an explicit value;
	// the file adapter compares by value, so it writes nothing for them.
	want.Presentation.ActiveRowBgLight = ports.ColorOverride{Set: true, Value: "#b9cdf2"}
	want.Presentation.ActiveRowBgDark = ports.ColorOverride{Set: true, Value: "#232636"}
	assert.Equal(t, want, req.Settings)
	assert.True(t, req.PanelOn)
	assert.False(t, req.PanelChanged)
	assert.Equal(t, domain.SortRecent, req.Order)
	assert.True(t, req.OrderChanged)

	assert.Equal(t, "saved to /cfg/config.toml · applied", e.status)
	assert.Equal(t, 2, b.loads, "values are read back after a save")
	assert.False(t, e.dirty())
	assert.Equal(t, "recent", value(t, e, config.FieldOrder))
	assert.True(t, e.Key(ctx, "q"), "nothing left to confirm")
}

func TestSaveSwitchesThePanel(t *testing.T) {
	b := newFakeBackend()
	b.outcome.Applied = false
	e := newTestEditor(t, b)

	press(t, e, "\r", "s")

	require.Len(t, b.requests, 1)
	assert.False(t, b.requests[0].PanelOn)
	assert.True(t, b.requests[0].PanelChanged)
	assert.Equal(t, domain.SortActive, b.requests[0].Order)
	assert.False(t, b.requests[0].OrderChanged)
	assert.Equal(t, "saved to /cfg/config.toml · daemon not running, applies on next start", e.status)
}

func TestSaveReportsNothingOrTheError(t *testing.T) {
	b := newFakeBackend()
	e := newTestEditor(t, b)

	press(t, e, "s")
	assert.Equal(t, "nothing changed", e.status)
	assert.Empty(t, b.requests)

	b.saveErr = errors.New("could not switch the Agents panel order")
	goTo(t, e, "variant")
	press(t, e, "l", "s")
	require.Len(t, b.requests, 1)
	assert.Equal(t, sgrWarn+"could not switch the Agents panel order"+sgrReset, e.status)
	assert.True(t, e.dirty(), "a failed save keeps the edits")
}

func TestRenderTitleRowFitsTheWidth(t *testing.T) {
	e := newTestEditor(t, newFakeBackend())
	lines := strings.Split(e.Render(80, 40), "\n")

	require.True(t, strings.HasPrefix(lines[0], clearScreen))
	assert.Empty(t, strings.TrimPrefix(lines[0], clearScreen), "a blank row above the title")
	title := lines[1]
	assert.True(t, strings.HasPrefix(title, " "+sgrBold+"herdr-scope settings"+sgrReset))
	assert.True(t, strings.HasSuffix(title, "herdr-scope"+sgrReset))
	assert.Equal(t, 78, width(title), "title and plugin id span the popup exactly")
}

func TestRenderMarksCursorChangesAndDefaults(t *testing.T) {
	b := newFakeBackend()
	b.settings.Presentation.Variant = ports.VariantText
	e := newTestEditor(t, b)
	goTo(t, e, "variant")
	press(t, e, "l")
	screen := e.Render(84, 40)

	assert.Contains(t, screen, " "+sgrAccent+"▸"+sgrReset+" "+sgrBold+sgrWarn+"*"+sgrReset+" variant ")
	// The name column is the longest name (colors.active_row_bg_light, 26)
	// plus the gap, so every value starts in one column.
	assert.Contains(t, screen, "variant"+strings.Repeat(" ", 28-len("variant")+1)+"none")
	assert.Contains(t, screen, "\n     agents_panel"+strings.Repeat(" ", 28-len("agents_panel")+1)+"plugin\n",
		"a live switch shows its value plain")
	assert.Contains(t, screen, sgrDim+"off  (default)"+sgrReset, "bool at its default")
	assert.Contains(t, screen, sgrDim+"U+F418  \uF418  (default)"+sgrReset, "glyph at its default")
	assert.Contains(t, screen, sgrDim+"2.5  (default)"+sgrReset)
	assert.Contains(t, screen, "↑↓ select · ←→ change · ↵ edit · r default · s save & apply · q close")

	goTo(t, e, "worktree_mark")
	press(t, e, "\r")
	typeText(t, e, "")
	assert.Contains(t, e.Render(84, 40), sgrInverse+sgrReset+sgrDim+"▏"+sgrReset, "an empty buffer still shows the caret")
}

func TestRenderScrollsWithMarkers(t *testing.T) {
	e := newTestEditor(t, newFakeBackend())
	n := len(e.deps.Fields)
	const rows = 15 // 15 - fixedRows = 6 setting rows
	room := rows - fixedRows

	screen := e.Render(84, rows)
	assert.NotContains(t, screen, sgrDim+"↑ ")
	assert.Contains(t, screen, sgrDim+"↓ "+strconv.Itoa(n-room)+" more"+sgrReset)
	// The trailing newline leaves the cursor on the popup's last row.
	assert.Equal(t, rows-1, strings.Count(screen, "\n"), "the popup fills the height exactly")

	press(t, e, "k") // wrap to the last row
	screen = e.Render(84, rows)
	assert.Contains(t, screen, sgrDim+"↑ "+strconv.Itoa(n-room)+" more"+sgrReset)
	assert.NotContains(t, screen, sgrDim+"↓ ")
	assert.Contains(t, screen, "colors.active_row_bg_dark")
	assert.NotContains(t, screen, "agents_panel")

	press(t, e, "j") // wrap back to the top
	assert.Contains(t, e.Render(84, rows), "agents_panel")
	assert.Equal(t, 0, e.top)
}

func TestRenderStatusTakesAListRow(t *testing.T) {
	e := newTestEditor(t, newFakeBackend())
	const rows = 15
	press(t, e, "s")
	screen := e.Render(84, rows)
	assert.Contains(t, screen, "\n nothing changed\n")
	assert.Equal(t, rows-1, strings.Count(screen, "\n"))
	assert.Contains(t, screen, "↓ "+strconv.Itoa(len(e.deps.Fields)-(rows-fixedRows-statusRows))+" more")
}

func TestRenderListKeepsMinimumRoom(t *testing.T) {
	e := newTestEditor(t, newFakeBackend())
	screen := e.Render(84, 5)
	assert.Contains(t, screen, "↓ "+strconv.Itoa(len(e.deps.Fields)-minimumListRows)+" more")
}

func TestRenderHelpKeepsTwoWrappedRows(t *testing.T) {
	e := newTestEditor(t, newFakeBackend())
	goTo(t, e, config.FieldOrder)
	lines := strings.Split(e.Render(60, 40), "\n")
	hint := len(lines) - 2 // the hint line, then the trailing newline
	require.Contains(t, lines[hint], "↑↓ select")
	assert.Empty(t, lines[hint-1])
	for _, line := range lines[hint-1-helpRows : hint-1] {
		assert.True(t, strings.HasPrefix(line, " "+sgrDim))
		assert.LessOrEqual(t, width(line), 60, "help lines fit the popup")
	}
	assert.Contains(t, lines[hint-3], "Agents panel order: active (grouped, busiest first, stale")
	assert.Contains(t, lines[hint-2], "last), recent (flat, by activity), here (active, only the")

	goTo(t, e, "group_gap")
	lines = strings.Split(e.Render(60, 40), "\n")
	assert.Equal(t, " "+sgrDim+"A blank row between workspace groups."+sgrReset, lines[hint-3])
	assert.Equal(t, " "+sgrDim+sgrReset, lines[hint-2], "a short help still takes two rows")
}

func TestWrapBreaksAtSpaces(t *testing.T) {
	assert.Equal(t, []string{"aaa bbb", "ccc"}, wrap("aaa bbb ccc", 7))
	assert.Equal(t, []string{"a", "toolongword", "b"}, wrap("a toolongword b", 5), "a word longer than the line keeps its own line")
	assert.Equal(t, []string{"漢字", "漢字"}, wrap("漢字 漢字", 8), "wide characters count twice")
	assert.Empty(t, wrap("", 10))
}

func TestWidthCountsWideCharactersAndSkipsColour(t *testing.T) {
	assert.Equal(t, 3, width(sgrBold+"abc"+sgrReset))
	assert.Equal(t, 4, width("漢字"))
	assert.Equal(t, 2, width("한"))
	assert.Equal(t, 2, width("ｱ"), "halfwidth forms count as two cells")
	assert.Equal(t, 1, width("\uF418"), "private-use glyphs are one cell")
}

func TestScrollTopKeepsTheCursorVisible(t *testing.T) {
	assert.Equal(t, 5, scrollTop(20, 12, 8, 0))
	assert.Equal(t, 3, scrollTop(20, 3, 8, 5))
	assert.Equal(t, 0, scrollTop(6, 5, 8, 0))
	assert.Equal(t, 12, scrollTop(20, 19, 8, 15), "never past the end")
}

func TestRunNeedsATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer r.Close()
	defer w.Close()
	err = Run(t.Context(), r, w, newFakeBackend().deps())
	require.ErrorContains(t, err, "needs a terminal")
}
