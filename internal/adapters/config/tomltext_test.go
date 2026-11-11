package config

import (
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every form TOML can spell a table in.
func TestClaimsTable(t *testing.T) {
	claims := map[string]string{
		"a header":                                    "[theme.custom]\nname = \"x\"",
		"an indented header":                          "  [theme.custom]",
		"a header with a comment after it":            "[theme.custom] # mine",
		"a header with spaces inside the brackets":    "[ theme.custom ]",
		"a quoted header":                             "[theme.\"custom\"]",
		"an array-of-tables header":                   "[[theme.custom]]",
		"a dotted key under the parent":               "[theme]\ncustom.name = \"x\"",
		"a dotted key at the top level":               "theme.custom.name = \"x\"",
		"an inline table":                             "[theme]\ncustom = { name = \"x\" }",
		"an inline table above it":                    "theme = { custom = { name = \"x\" } }",
		"a scalar where the table would go":           "[theme]\ncustom = 1",
		"a dotted key with a quoted segment":          "[theme]\n\"custom\".name = 1",
		"a header after a multi-line string in array": "[ui]\nx = [ \"\"\"\ntext\n\"\"\" ]\n[theme.custom]",
		"a header after a comment shaped like opener": "[ui]\nx = 1 # = \"\"\"\n[theme.custom]",
		"a header after an even run of backslashes":   "[ui]\nx = \"\"\"abc\\\\\"\"\"\n[theme.custom]",
		"a table after a multi-line array":            "[other]\nx = [\n  1,\n  [2],\n]\n[theme.custom]",
		"a table after a one-line nested array":       "[other]\nx = [ [1], [2] ]\n[theme.custom]",
		"a table after a multi-line string":           "[keys]\nx = \"\"\"\ntext\n\"\"\"\n[theme.custom]",
	}
	for form, text := range claims {
		assert.True(t, claimsTable(text, "theme.custom"), form)
	}
	free := map[string]string{
		"the parent table alone":              "[theme]\nname = \"x\"",
		"a sibling key":                       "[theme]\ncustom_name = \"x\"",
		"a sibling table":                     "[theme.customs]",
		"the same key under another table":    "[ui]\ncustom.name = \"x\"",
		"a commented-out header":              "# [theme.custom]",
		"the name inside a value":             "[theme]\nname = \"[theme.custom]\"",
		"a header inside a multi-line string": "[[keys.command]]\ncommand = \"\"\"\ncat <<EOF\n  [theme.custom]\nEOF\n\"\"\"\n[ui]",
		"a header inside a literal string":    "[keys]\nx = '''\n[theme.custom]\n'''",
		"a closed multi-line string":          "[keys]\nx = \"\"\"abc\"\"\"\n[ui]\ny = 1",
		"a sub-table header":                  "[theme.custom.accent]\nfg = \"x\"",
		"comment-shaped opener then scope":    "[ui]\n# x = \"\"\"\n[other] # \"\"\"\nsidebar.spaces = 1",
		"past an escaped quote":               "[keys]\nx = \"\"\"\necho \\\"\"\"\n  [theme.custom]\n\"\"\"",
		"a multi-line string in an array":     "[ui]\nx = [ \"\"\"\n  [theme.custom]\n\"\"\"]",
		"a second string on the closing line": "[ui]\nx = { a = \"\"\"ok\"\"\", b = \"\"\"\n  [theme.custom]\n\"\"\" }",
		"an opener inside a comment":          "[theme]\nx = 1 # = \"\"\"\n[other] # \"\"\"\ncustom.name = 1",
		"an opener inside a one-line string":  "[theme]\nx = '= \"\"\"'\n[other]\ncustom.name = 1",
		"an opener inside a quoted key":       "[theme]\n'x = \"\"\"' = 1\n[other]\ncustom.name = 1",
		"even backslashes before closing":     "[theme]\nx = \"\"\"abc\\\\\"\"\"\n[other] # \"\"\"\ncustom.name = 1",
		"a bracket in a quoted header name":   "[theme]\n[\"a]b\"]\ncustom.name = 1",
		"a bracket in a quoted segment":       "[theme]\n[other.\"a]b\"]\ncustom.name = 1",
		"CRLF line endings":                   "[theme]\r\nname = \"x\"\r\n[other]\r\ncustom.name = 1\r\n",
		"a quoted name differing in filler":   "[theme.\"cus#tom\"]\nx = 1",
		"a literal name with a backslash":     "[theme.'cus\\tom']\nx = 1",
		"a string ending in four quotes":      "[theme]\nx = \"\"\"abc\"\"\"\" # \"\"\"\"\n[other] # \"\"\"\ncustom.name = 1",
		"a literal ending in five quotes":     "[theme]\nx = '''abc'''''\n[other]\ncustom.name = 1",
		"a nested array element":              "[theme]\n[other]\nx = [\n[\"theme\"]\n]\ncustom.name = 1",
		"a nested array of arrays":            "[theme]\n[other]\nx = [\n[['theme']]\n]\ncustom.name = 1",
		"an array of inline tables":           "[theme]\n[other]\nx = [\n  { custom = 1 },\n]\ncustom.name = 1",
	}
	for form, text := range free {
		assert.False(t, claimsTable(text, "theme.custom"), form)
	}
	table := "ui.sidebar.agents.rows_by_agent"
	assert.False(t, claimsTable("[ui.sidebar.agents.\"rows#by#agent\"]\nx = 1", table))
	assert.False(t, claimsTable("[ui.sidebar.agents.'rows\\by_agent']\nx = 1", table))
	assert.False(t, claimsTable("[ui.sidebar.agents]\n\"rows.by.agent\" = 1", table))
	assert.True(t, claimsTable("[ui]\n# x = \"\"\"\n[ui.sidebar.spaces]\nrow_gap = 1", "ui.sidebar.spaces"))
}

func TestEditsKeepDollarSignsAndComments(t *testing.T) {
	assert.Equal(
		t,
		"row_format = \"cost $$5 $& x\"\n",
		editTopLevel("row_format = \"old\"\n", []edit{{Key: "row_format", Value: `"cost $$5 $& x"`}}),
	)
	edited, ok := editTable("[colors]\nname = \"old\"\n", "colors", []edit{{Key: "name", Value: `"$& $$"`}})
	assert.True(t, ok)
	assert.Equal(t, "[colors]\nname = \"$& $$\"\n", edited)

	text := "# mine\ngroup_gap = true # keep me\n\n[colors]\n# a comment\nactive_row_bg_light = \"#fff\"\n"
	edited = editTopLevel(text, []edit{{Key: "group_gap", Value: "false"}, {Key: "variant", Value: `"text"`}})
	assert.Equal(t, "# mine\ngroup_gap = false\nvariant = \"text\"\n\n[colors]\n# a comment\nactive_row_bg_light = \"#fff\"\n", edited)
	edited, ok = editTable(
		edited,
		"colors",
		[]edit{{Key: "active_row_bg_light", Delete: true}, {Key: "active_row_bg_dark", Value: `"#000"`}},
	)
	assert.True(t, ok)
	assert.Contains(t, edited, "# a comment\nactive_row_bg_dark = \"#000\"\n")
	_, ok = editTable("x = 1", "nope", nil)
	assert.False(t, ok)
}

func TestThemeValueAndRaw(t *testing.T) {
	text := "[theme]\nname = \"catppuccin\" # chosen\nauto_switch = true\n[ui]\nname = \"other\"\n"
	v, ok := themeValue(text, "name")
	assert.True(t, ok)
	assert.Equal(t, "catppuccin", v)
	raw, ok := themeRaw(text, "auto_switch")
	assert.True(t, ok)
	assert.Equal(t, "true", raw)
	raw, _ = themeRaw(text, "name")
	assert.Equal(t, `"catppuccin"`, raw)
	_, ok = themeValue(text, "light_name")
	assert.False(t, ok)
}

func TestBlocks(t *testing.T) {
	start, end := "# >>> b", "# <<< b"
	text := upsertTail("[ui]\nx = 1\n", start, end, start+"\nbody\n"+end)
	assert.Equal(t, "[ui]\nx = 1\n\n"+start+"\nbody\n"+end+"\n", text)
	text = upsertTail(text, start, end, start+"\nnew\n"+end)
	assert.Equal(t, "[ui]\nx = 1\n\n"+start+"\nnew\n"+end+"\n", text, "an existing block is replaced in place")
	assert.Equal(t, "[ui]\nx = 1\n", dropBlock(text, start, end), "an append and a drop are inverses")
	assert.Equal(t, "a\n\nb\n", dropBlock("a\n\n"+start+"\nx\n"+end+"\n\nb\n", start, end))
	assert.Equal(t, "[ui]\nk = 1\n", dropInline("[ui]\n"+start+"\nx\n"+end+"\nk = 1\n", start, end))
	assert.Equal(t, "x\n", dropBlock("x\n", start, end), "dropping an absent block is a no-op")
}

func TestTOMLStringRoundTrips(t *testing.T) {
	for _, s := range []string{"plain", `quote " and \ slash`, "\uF418", "tab\tnew\nline", "$5", "§ ✓"} {
		var parsed struct {
			V string `toml:"V"`
		}
		_, err := toml.Decode("V = "+tomlString(s), &parsed)
		require.NoError(t, err, s)
		assert.Equal(t, s, parsed.V)
	}
	assert.Equal(t, `"\uF418"`, tomlString("\uF418"), "a private-use glyph is written visibly")
}
