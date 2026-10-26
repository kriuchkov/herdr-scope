package sidebar

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// Herdr's ceilings: rules per cell, tokens per row. A row over the token
// limit is a parse error for the whole config file, taking every plugin and
// keybinding down with it — so a generated layout that would cross it is
// refused here.
const (
	ruleLimit = 16
	// VariantTag starts the comment line that records which glyph table the
	// logo rules match; the daemon rewrites the block when it disagrees.
	VariantTag = "# logo glyphs: "
	// LayoutTag records the layout the rows were written for.
	LayoutTag = "# layout: "
)

// Rule colours a cell whose value contains a string, or starts with one.
// Herdr applies the first rule that matches.
type Rule struct {
	Contains   string
	StartsWith string
	Fg         string
}

// Cell is one token in a row layout. Bare cells carry no style.
type Cell struct {
	Token string
	Fg    string
	Bold  bool
	Dim   bool
	Bare  bool
	Rules []Rule
}

// Layout is the managed sidebar block as data: the Agents rows (one copy per
// vendor with a hue, differing only in the working title's colour) and the
// Spaces rows.
type Layout struct {
	Variant ports.Variant
	Kind    ports.Layout
	Agents  [][]Cell
	ByAgent []AgentRows
	Spaces  [][]Cell
}

// AgentRows is the layout for one canonical agent id.
type AgentRows struct {
	Kind domain.AgentKind
	Rows [][]Cell
}

func cell(token, fg string, bold bool) Cell {
	return Cell{Token: token, Fg: fg, Bold: bold}
}

// logoCell colours the logo by what it says: the value is the vendor's glyph
// (behind an indent, hence contains), so one cell carries every vendor's hue.
// The cell's own colour is the ink, so a hueless or generic mark is drawn in
// plain ink rather than a colour made up for it.
func logoCell(token string, glyphs Glyphs, ink string) Cell {
	c := Cell{Token: token, Fg: ink}
	for _, b := range brandColors() {
		if len(c.Rules) == ruleLimit {
			break
		}
		if glyph, ok := glyphs.Logo(b.Kind); ok {
			c.Rules = append(c.Rules, Rule{Contains: glyph, Fg: b.Color})
		}
	}
	return c
}

func agentRows(kind domain.AgentKind, p Presentation, appearance ports.Appearance) [][]Cell {
	c := colorsFor(appearance)
	working, ok := BrandColor(kind)
	if !ok {
		working = otherBrand
	}
	// A working title wears its vendor's colour and is the one title that
	// bolds: a long panel is scanned for what is still going.
	titles := []Cell{
		cell("$title_working", working, true),
		cell("$title_done", c.Done, false),
		cell("$title_blocked", c.Blocked, false),
		cell("$title_idle_fresh", c.IdleFresh, false),
		cell("$title_idle", c.IdleNormal, false),
		cell("$title_idle_stale", c.IdleStale, false),
		cell("$title_unknown", c.Unknown, false),
	}
	line := titles
	if !p.compact() {
		inkColor := ink(appearance)
		// With logo_color active an idle logo goes out under its tier's
		// cell and takes its title's colour.
		line = append([]Cell{
			cell("$split_mark", c.IdleStale, false),
			logoCell("$logo", p.Glyphs, inkColor),
			logoCell("$logo_working", p.Glyphs, inkColor),
			cell("$logo_stale", c.IdleStale, false),
			cell("$logo_fresh", c.IdleFresh, false),
			cell("$logo_idle", c.IdleNormal, false),
			cell("$logo_unknown", c.Unknown, false),
		}, titles...)
	}
	return [][]Cell{
		// A synthesised parent for a worktree whose checkout runs no agent, or
		// a folded family's line, which stands in for it.
		foldLine(c, p),
		// The header fades with its workspace.
		{cell("$group", c.Subtle, true), cell("$group_stale", c.IdleStale, true)},
		line,
		{{Token: "$gap", Bare: true}},
	}
}

// foldLine is an entry's first line: the repository row, or a fold's mark
// and name, then its count. A bare count fades; one behind a spinner is in
// the working colour, a branded vendor's own when its logo leads it. The
// compact layout sends the fold as one cell.
func foldLine(c stateColors, p Presentation) []Cell {
	line := []Cell{cell("$group_parent", c.IdleStale, true), cell("$fold", c.Subtle, true)}
	if p.compact() {
		return line
	}
	count := logoCell("$fold_count", p.Glyphs, otherBrand)
	count.Rules = append(count.Rules[:min(len(count.Rules), ruleLimit-1)], Rule{StartsWith: "+", Fg: c.IdleStale})
	return append(line, count)
}

// spaceRows are a workspace's lines: its mark, then (the classic layout, state
// marks) its agents' logos. In the compact layout the mark's cell carries the name too,
// so working, blocked and done workspaces read in their state's colour.
func spaceRows(p Presentation, appearance ports.Appearance) [][]Cell {
	c := colorsFor(appearance)
	marks := []Cell{cell("$space_blocked", c.Blocked, true)}
	for _, vendor := range SpaceWorkingVendors() {
		color, _ := BrandColor(vendor)
		marks = append(marks, cell("$space_working_"+string(vendor), color, true))
	}
	marks = append(marks,
		cell("$space_working_other", otherBrand, true),
		cell("$space_done", c.Done, true),
		cell("$space_idle", c.Idle, false),
		cell("$space_unknown", c.Unknown, false),
		cell("$space_none", c.None, false),
	)
	if p.compact() {
		return [][]Cell{marks}
	}
	// The name is a token of ours, so the whole row follows the block.
	marks = append(marks, cell("$space_label", c.None, false))
	logos := make([]Cell, 0, len(BrandVendors())+2)
	for _, vendor := range BrandVendors() {
		color, _ := BrandColor(vendor)
		logos = append(logos, cell("$space_logo_"+string(vendor), color, false))
	}
	logos = append(logos, cell("$space_logo_other", ink(appearance), false), cell("$space_logo_idle", c.Idle, false))
	return [][]Cell{marks, logos}
}

// NewLayout builds the managed block for one appearance. byAgent names the
// vendors Herdr accepts as rows_by_agent keys — an id it does not know is a
// parse error, so only ids from its downloaded detectors may appear.
func NewLayout(p Presentation, appearance ports.Appearance, byAgent []domain.AgentKind) (Layout, error) {
	layout := Layout{
		Variant: p.Glyphs.Variant(),
		Kind:    cmp.Or(p.Layout, ports.LayoutClassic),
		Agents:  agentRows("", p, appearance),
		Spaces:  spaceRows(p, appearance),
	}
	for _, kind := range byAgent {
		if _, branded := BrandColor(kind); branded {
			layout.ByAgent = append(layout.ByAgent, AgentRows{Kind: kind, Rows: agentRows(kind, p, appearance)})
		}
	}
	for _, rows := range [][][]Cell{layout.Agents, layout.Spaces} {
		for _, row := range rows {
			if len(row) > rowTokenLimit {
				return Layout{}, fmt.Errorf("%w: a sidebar row would carry %d tokens; Herdr allows %d",
					domain.ErrConfiguration, len(row), rowTokenLimit)
			}
		}
	}
	return layout, nil
}

// RowsFor is the layout an agent's entry is drawn with.
func (l Layout) RowsFor(kind domain.AgentKind) [][]Cell {
	for _, a := range l.ByAgent {
		if a.Kind == kind {
			return a.Rows
		}
	}
	return l.Agents
}

func (c Cell) toml() string {
	if c.Bare {
		return fmt.Sprintf("%q", c.Token)
	}
	base := fmt.Sprintf("token = %q, fg = %q, bold = %t, dim = %t", c.Token, c.Fg, c.Bold, c.Dim)
	if len(c.Rules) == 0 {
		return "{ " + base + " }"
	}
	rules := make([]string, len(c.Rules))
	for i, r := range c.Rules {
		if r.StartsWith != "" {
			rules[i] = fmt.Sprintf("{ starts_with = %q, fg = %q }", r.StartsWith, r.Fg)
			continue
		}
		rules[i] = fmt.Sprintf("{ contains = %q, fg = %q }", r.Contains, r.Fg)
	}
	return "{ " + base + ", rules = [" + strings.Join(rules, ", ") + "] }"
}

func inlineRows(rows [][]Cell) string {
	out := make([]string, len(rows))
	for i, row := range rows {
		cells := make([]string, len(row))
		for j, c := range row {
			cells[j] = c.toml()
		}
		out[i] = "[" + strings.Join(cells, ", ") + "]"
	}
	return "[" + strings.Join(out, ", ") + "]"
}

// TOML is the block body.
func (l Layout) TOML() string {
	lines := []string{
		"[ui.sidebar.agents]",
		VariantTag + string(l.Variant),
		LayoutTag + string(l.Kind),
		"rows = " + inlineRows(l.Agents),
		"",
		"[ui.sidebar.agents.rows_by_agent]",
	}
	for _, a := range l.ByAgent {
		lines = append(lines, string(a.Kind)+" = "+inlineRows(a.Rows))
	}
	lines = append(lines, "", "[ui.sidebar.spaces]", "row_gap = 0")
	spaces := make([]string, len(l.Spaces))
	for i, row := range l.Spaces {
		cells := make([]string, len(row))
		for j, c := range row {
			cells[j] = c.toml()
		}
		spaces[i] = "  [\n    " + strings.Join(cells, ",\n    ") + "\n  ]"
	}
	lines = append(lines, "rows = [\n"+strings.Join(spaces, ",\n")+"\n]")
	return strings.Join(lines, "\n")
}

// BlockVariant reads the variant tag back out of a block's text.
func BlockVariant(text string) (ports.Variant, bool) {
	for line := range strings.SplitSeq(text, "\n") {
		if rest, ok := strings.CutPrefix(line, VariantTag); ok {
			return ports.Variant(strings.TrimSpace(rest)), true
		}
	}
	return "", false
}
