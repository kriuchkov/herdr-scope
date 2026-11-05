package sidebar

import (
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

func presentationFor(variant ports.Variant, layout ports.Layout) Presentation {
	return Presentation{Glyphs: NewGlyphs(ports.PresentationSettings{Variant: variant}, nil), Layout: layout}
}

func TestLayoutParsesAsTOMLAndRespectsHerdrLimits(t *testing.T) {
	for _, variant := range []ports.Variant{ports.VariantText, ports.VariantFont, ports.VariantNone} {
		for _, appearance := range []ports.Appearance{ports.AppearanceLight, ports.AppearanceDark} {
			for _, kind := range []ports.Layout{ports.LayoutCompact, ports.LayoutClassic} {
				testLayoutParses(t, presentationFor(variant, kind), appearance)
			}
		}
	}
}

func testLayoutParses(t *testing.T, p Presentation, appearance ports.Appearance) {
	t.Helper()
	layout, err := NewLayout(p, appearance, []domain.AgentKind{"claude", "gemini", "codex"})
	require.NoError(t, err)
	text := layout.TOML()
	var parsed map[string]any
	_, err = toml.Decode(text, &parsed)
	require.NoError(t, err, "%s %s %s:\n%s", p.Glyphs.Variant(), p.Layout, appearance, text)

	ui := parsed["ui"].(map[string]any)["sidebar"].(map[string]any)
	agents := ui["agents"].(map[string]any)
	rows := agents["rows"].([]any)
	assert.LessOrEqual(t, len(rows), 16)
	for _, row := range rows {
		assert.LessOrEqual(t, len(row.([]any)), 16)
	}
	byAgent := agents["rows_by_agent"].(map[string]any)
	assert.Contains(t, byAgent, "claude")
	assert.NotContains(t, byAgent, "codex", "a vendor without a hue needs no override row")
	for _, row := range ui["spaces"].(map[string]any)["rows"].([]any) {
		assert.LessOrEqual(t, len(row.([]any)), 16, "the Spaces row is at its limit by design")
	}
	got, ok := BlockVariant(text)
	assert.True(t, ok)
	assert.Equal(t, p.Glyphs.Variant(), got)
	assert.Contains(t, text, LayoutTag+string(p.Layout))
}

func TestLayoutColoursFollowTheAppearance(t *testing.T) {
	p := presentationFor(ports.VariantText, ports.LayoutClassic)
	light, err := NewLayout(p, ports.AppearanceLight, nil)
	require.NoError(t, err)
	dark, err := NewLayout(p, ports.AppearanceDark, nil)
	require.NoError(t, err)
	assert.Contains(t, light.TOML(), `token = "$title_idle_stale", fg = "#9a9da8"`)
	assert.Contains(t, dark.TOML(), `token = "$title_idle_stale", fg = "#6b6f82"`)
	assert.Contains(t, dark.TOML(), `token = "$title_idle_fresh", fg = "#c8cde0"`, "fresh idle is bright, not green")
	assert.Contains(t, light.TOML(), `token = "$space_label", fg = "#5c5f77"`, "a name reads on a light panel")
	assert.Contains(t, dark.TOML(), `token = "$space_label", fg = "#9a9eb3"`)
	assert.Contains(t, light.TOML(), `fg = "#16161c", bold = false, dim = false, rules = [{ contains = "§", fg = "#d97757" }`)
	assert.Contains(t, light.TOML(), `token = "$title_working", fg = "#c78a1f", bold = true`, "the fallback row works in other's colour")
	for token, fg := range map[string]string{"$logo_fresh": "#c8cde0", "$logo_idle": "#9499ad", "$logo_unknown": "#907aa9"} {
		assert.Contains(t, dark.TOML(), `token = "`+token+`", fg = "`+fg+`"`, "an idle logo takes its title's colour")
	}
	assert.Contains(t, dark.TOML(), `token = "$space_logo_idle", fg = "#8f95ab"`)
}

func TestWorkingTitleWearsItsVendorsColour(t *testing.T) {
	layout, err := NewLayout(presentationFor(ports.VariantText, ports.LayoutClassic), ports.AppearanceLight, []domain.AgentKind{"gemini"})
	require.NoError(t, err)
	row := layout.RowsFor("gemini")[2]
	i := slices.IndexFunc(row, func(c Cell) bool { return c.Token == "$title_working" })
	require.GreaterOrEqual(t, i, 0)
	assert.Equal(t, "#4285f4", row[i].Fg)
	assert.Equal(t, layout.Agents, layout.RowsFor("droid"), "anything else uses the fallback rows")

	compact, err := NewLayout(presentationFor(ports.VariantText, ports.LayoutCompact), ports.AppearanceLight, []domain.AgentKind{"gemini"})
	require.NoError(t, err)
	row = compact.RowsFor("gemini")[2]
	assert.Equal(t, "$title_working", row[0].Token)
	assert.Equal(t, "#4285f4", row[0].Fg, "the whole working line wears the vendor's colour")
}

func TestCompactLayoutKeepsEveryLineInOneCell(t *testing.T) {
	layout, err := NewLayout(presentationFor(ports.VariantText, ports.LayoutCompact), ports.AppearanceDark, nil)
	require.NoError(t, err)
	for _, c := range layout.Agents[2] {
		assert.True(t, strings.HasPrefix(c.Token, "$title_"), "only state cells, one of which is set: %s", c.Token)
	}
	require.Len(t, layout.Spaces, 1, "a workspace is one line")
	for _, c := range layout.Spaces[0] {
		assert.NotEqual(t, "$space_label", c.Token, "the name rides in the mark's cell")
	}
	assert.NotContains(t, layout.TOML(), "rules = [", "no logo cell to colour by rule")
}

func TestNoneVariantHasNoLogoRules(t *testing.T) {
	layout, err := NewLayout(presentationFor(ports.VariantNone, ports.LayoutClassic), ports.AppearanceLight, nil)
	require.NoError(t, err)
	assert.NotContains(t, layout.TOML(), "contains = ", "only the fold count's own rule is left")
	assert.Contains(t, layout.TOML(), VariantTag+"none")
}

func TestRender(t *testing.T) {
	layout, err := NewLayout(presentationFor(ports.VariantText, ports.LayoutClassic), ports.AppearanceLight, nil)
	require.NoError(t, err)
	store := Store{}
	store.Apply("p1", map[string]*string{"group": new("dashboard"), "logo": new("§"), "title_done": new("✓ Cache search results")})
	store.Apply(
		"p2",
		map[string]*string{"logo": new(zwsp + "  Λ"), "title_idle": new("Fix"), "gap": new(zwsp), "empty": new("  ")},
	)
	out := Render([]Entry{{Rows: layout.Agents, Tokens: store["p1"]}, {Rows: layout.Agents, Tokens: store["p2"]}}, RenderOptions{})
	assert.Equal(t, " dashboard\n   § · ✓ Cache search results\n   Λ · Fix\n\n", out, "the gap row draws as a blank line")
	annotated := Render([]Entry{{Rows: layout.Agents, Tokens: store["p1"]}}, RenderOptions{Annotate: true})
	assert.Contains(t, annotated, "✓ Cache search results{title_done}")
	assert.NotContains(t, store["p2"], "empty", "an empty value is a clear")
}

func TestNormalizeMatchesHerdr(t *testing.T) {
	v, ok := Normalize("  review\nready  ")
	assert.True(t, ok)
	assert.Equal(t, "reviewready", v)
	_, ok = Normalize(" \n ")
	assert.False(t, ok)
	v, _ = Normalize(zwsp + "  x")
	assert.Equal(t, zwsp+"  x", v, "a zero-width space survives the trim")
	v, _ = Normalize(strings.Repeat("a", 100))
	assert.Len(t, v, 80)
}
