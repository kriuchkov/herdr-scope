package sidebar

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// textPresentation is the classic layout with state marks in Spaces, logos
// always in their vendor's colour and no ages: most tests here pin its
// tokens.
func textPresentation() Presentation {
	s := ports.DefaultSettings()
	s.Presentation.Variant = ports.VariantText
	s.Presentation.Layout = ports.LayoutClassic
	s.Presentation.SpaceMark = ports.SpaceMarkState
	s.Presentation.LogoColor = ports.LogoColorAlways
	s.Presentation.ShowAge = ports.AgeOff
	return NewPresentation(s, nil)
}

// scopePresentation is Scope's own defaults in the text variant.
func scopePresentation() Presentation {
	s := ports.DefaultSettings()
	s.Presentation.Variant = ports.VariantText
	return NewPresentation(s, nil)
}

func compactPresentation() Presentation {
	s := ports.DefaultSettings()
	s.Presentation.Variant = ports.VariantText
	s.Presentation.Layout = ports.LayoutCompact
	return NewPresentation(s, nil)
}

func val(t *testing.T, tokens Tokens, name string) string {
	t.Helper()
	v, ok := tokens[name]
	require.True(t, ok, "token %s is not managed", name)
	if v == nil {
		return ""
	}
	return *v
}

// at0 is a moment on a spinner-step boundary.
func at0() time.Time { return time.UnixMilli(1_800_000_000_100 - 1_800_000_000_100%150) }

func rowView(row domain.SidebarRow, grouped bool) (domain.SidebarView, domain.SidebarGroup) {
	g := domain.SidebarGroup{Workspace: row.Workspace, Label: "dashboard", Header: true, Gap: true, Rows: []domain.SidebarRow{row}}
	return domain.SidebarView{At: at0(), Grouped: grouped, Groups: []domain.SidebarGroup{g}}, g
}

func TestGlyphVariants(t *testing.T) {
	text := NewGlyphs(ports.PresentationSettings{Variant: ports.VariantText}, nil)
	logo, ok := text.Logo("claude")
	assert.True(t, ok)
	assert.Equal(t, "§", logo)
	assert.Equal(t, "✓", text.State(domain.DisplayDone))
	assert.Equal(t, "○", text.State(domain.DisplayWorking))

	font := NewGlyphs(ports.PresentationSettings{Variant: ports.VariantAuto}, func() bool { return true })
	assert.Equal(t, ports.VariantFont, font.Variant(), "auto picks the installed font")
	logo, _ = font.Logo("codex")
	assert.Equal(t, "\uE1A1", logo)
	assert.Equal(t, "\uE1C2", font.State(domain.DisplayWorking), "working shares the idle ring")
	assert.Equal(t, "\uE1C0", font.State(domain.DisplayDone))

	auto := NewGlyphs(ports.PresentationSettings{Variant: ports.VariantAuto}, func() bool { return false })
	assert.Equal(t, ports.VariantText, auto.Variant())

	none := NewGlyphs(ports.PresentationSettings{Variant: ports.VariantNone}, nil)
	_, ok = none.Logo("claude")
	assert.False(t, ok)
	assert.Equal(t, "?", none.State(domain.DisplayBlocked), "a state line keeps its mark without logos")
	assert.Empty(t, none.Resolve(domain.Agent{Kind: "claude"}).Glyph)
}

func TestUnknownAgentGetsTheGenericMark(t *testing.T) {
	g := NewGlyphs(ports.PresentationSettings{Variant: ports.VariantText}, nil)
	icon := g.Resolve(domain.Agent{Kind: "droid"})
	assert.Equal(t, Icon{Glyph: GenericMark}, icon)
	assert.Equal(t, Icon{Glyph: GenericMark}, g.Resolve(domain.Agent{}), "no kind at all still gets a mark")
	assert.True(t, g.Resolve(domain.Agent{Kind: "gemini"}).Known)
}

func TestGlyphOverrides(t *testing.T) {
	overrides := make(map[domain.DisplayState]string)
	overrides[domain.DisplayDone] = "✔"
	g := NewGlyphs(ports.PresentationSettings{Variant: ports.VariantFont, Glyphs: overrides}, nil)
	assert.Equal(t, "✔", g.State(domain.DisplayDone), "an explicit choice wins over the variant")
	assert.Equal(t, "\uE1C1", g.State(domain.DisplayBlocked))
}

func TestWorkingRowSpinner(t *testing.T) {
	p := textPresentation()
	row := domain.SidebarRow{Pane: "w1:p1", Workspace: "w1", Kind: "claude", Display: domain.DisplayWorking, Title: "Implement OAuth"}
	view, g := rowView(row, true)
	tokens := p.PaneTokens(view, g, 0)
	step := view.At.UnixMilli() / 150
	frame := spinnerFrames()[step%8]
	assert.Equal(t, frame+" Implement OAuth", val(t, tokens, "title_working"))
	assert.Equal(t, "§", val(t, tokens, "logo_working"), "a working logo is published under its own name")
	assert.Empty(t, val(t, tokens, "logo"))
	assert.Equal(t, "○", val(t, tokens, "state_working"))
	for _, other := range []string{"title_idle", "title_done", "state_done"} {
		assert.Empty(t, val(t, tokens, other), "only the current state's names are set")
	}

	view.At = view.At.Add(150 * time.Millisecond)
	next := p.PaneTokens(view, g, 0)
	assert.NotEqual(t, val(t, tokens, "title_working"), val(t, next, "title_working"), "the spinner turns each step")
	assert.Equal(t, view.At.Add(150*time.Millisecond), NextAnimation(view))
}

func TestBlockedPulse(t *testing.T) {
	p := textPresentation()
	row := domain.SidebarRow{Pane: "w1:p1", Workspace: "w1", Kind: "codex", Display: domain.DisplayBlocked, Title: "Which port?"}
	view, g := rowView(row, true)
	loud := val(t, p.PaneTokens(view, g, 0), "title_blocked")
	view.At = view.At.Add(5 * 150 * time.Millisecond)
	quiet := val(t, p.PaneTokens(view, g, 0), "title_blocked")
	assert.ElementsMatch(
		t,
		[]string{"? Which port?", "· Which port?"},
		[]string{loud, quiet},
		"the mark breathes between ? and the quiet ring",
	)
	assert.Equal(t, "Λ", val(t, p.PaneTokens(view, g, 0), "logo"))
}

func TestDoneAndUnknownLeadWithTheirMark(t *testing.T) {
	p := textPresentation()
	cases := []struct {
		state domain.DisplayState
		want  string
	}{
		{domain.DisplayDone, "✓ x"}, {domain.DisplayUnknown, "◌ x"}, {domain.DisplayIdle, "x"}, {domain.DisplayIdleFresh, "x"},
	}
	for _, c := range cases {
		view, g := rowView(domain.SidebarRow{Pane: "w1:p1", Kind: "claude", Display: c.state, Title: "x"}, true)
		assert.Equal(t, c.want, val(t, p.PaneTokens(view, g, 0), "title_"+string(c.state)), c.state)
	}
	assert.True(t, NextAnimation(domain.SidebarView{At: at0()}).IsZero(), "a static panel needs no repaint")
}

func TestStaleRowFadesItsLogoAndHeader(t *testing.T) {
	p := textPresentation()
	row := domain.SidebarRow{Pane: "w1:p1", Workspace: "w1", Kind: "claude", Display: domain.DisplayIdleStale, Title: "old"}
	view, g := rowView(row, true)
	g.Stale = true
	view.Groups[0] = g
	tokens := p.PaneTokens(view, g, 0)
	assert.Equal(t, "§", val(t, tokens, "logo_stale"))
	assert.Equal(t, "dashboard", val(t, tokens, "group_stale"))
	assert.Empty(t, val(t, tokens, "group"))
}

func TestDepthIndentsTheFirstCell(t *testing.T) {
	p := textPresentation()
	row := domain.SidebarRow{Pane: "w1:p2", Workspace: "w1", Kind: "claude", Display: domain.DisplayIdle, Title: "t", Depth: 1}
	view, g := rowView(row, true)
	g.Rows = []domain.SidebarRow{{Pane: "w1:p1"}, row}
	assert.Equal(t, zwsp+"  §", val(t, p.PaneTokens(view, g, 1), "logo"), "the logo carries the indent")

	row.Kind = "droid"
	g.Rows[1] = row
	assert.Equal(t, zwsp+"  "+GenericMark, val(t, p.PaneTokens(view, g, 1), "logo"))

	none := p
	none.Glyphs = NewGlyphs(ports.PresentationSettings{Variant: ports.VariantNone}, nil)
	assert.Equal(t, zwsp+"  t", val(t, none.PaneTokens(view, g, 1), "title_idle"), "without a logo the title carries it")
}

func TestSplitCornerCarriesTheIndent(t *testing.T) {
	p := textPresentation()
	row := domain.SidebarRow{
		Pane:      "w1:p2",
		Workspace: "w1",
		Kind:      "claude",
		Display:   domain.DisplayIdle,
		Title:     "t",
		Depth:     1,
		Corner:    domain.CornerLast,
	}
	view, g := rowView(row, true)
	g.Rows = []domain.SidebarRow{{Pane: "w1:p1"}, row}
	tokens := p.PaneTokens(view, g, 1)
	assert.Equal(t, zwsp+"  └─ ", val(t, tokens, "split_mark"))
	assert.Equal(t, "§", val(t, tokens, "logo"), "behind a corner the logo has no indent of its own")
}

func TestIndentZeroKeepsHeadersGapsAndMarks(t *testing.T) {
	s := ports.DefaultSettings()
	s.Presentation.Variant = ports.VariantText
	s.Presentation.WorktreeMark = "*"
	s.Presentation.Layout = ports.LayoutClassic
	s.Policy.GroupIndent = 0
	p := NewPresentation(s, nil)
	row := domain.SidebarRow{Pane: "wt:p1", Workspace: "wt", Kind: "claude", Display: domain.DisplayIdle, Title: "t", Depth: 1}
	view, g := rowView(row, true)
	g.Label, g.Tree = "worktree", domain.TreeLastChild
	tokens := p.PaneTokens(view, g, 0)
	assert.Equal(t, "* worktree", val(t, tokens, "group"), "no corner, the mark stays")
	assert.Equal(t, zwsp, val(t, tokens, "gap"))
	assert.Equal(t, "§", val(t, tokens, "logo_idle"), "zero indent draws no indent")
}

func TestWorktreeBranches(t *testing.T) {
	p := textPresentation()
	p.WorktreeMark = "\uF418"
	for tree, want := range map[domain.TreePosition]string{
		domain.TreeChild:     zwsp + "  ├─ \uF418 feature",
		domain.TreeLastChild: zwsp + "  └─ \uF418 feature",
		domain.TreeOrphan:    "└─ \uF418 feature",
		domain.TreeDetached:  "\uF418 feature",
		domain.TreeRoot:      "feature",
	} {
		g := domain.SidebarGroup{
			Label:      "feature",
			Tree:       tree,
			Header:     true,
			OrphanRepo: "app",
			Rows:       []domain.SidebarRow{{Pane: "w:p1", Display: domain.DisplayIdle}},
		}
		tokens := p.PaneTokens(domain.SidebarView{At: at0(), Grouped: true, Groups: []domain.SidebarGroup{g}}, g, 0)
		assert.Equal(t, want, val(t, tokens, "group"), "tree %d", tree)
		if tree == domain.TreeOrphan {
			assert.Equal(t, "app", val(t, tokens, "group_parent"))
		} else {
			assert.Empty(t, val(t, tokens, "group_parent"))
		}
	}
	p.WorktreeMark = ""
	g := domain.SidebarGroup{Label: "feature", Tree: domain.TreeDetached, Header: true, Rows: []domain.SidebarRow{{Pane: "w:p1"}}}
	assert.Equal(t, "feature", val(t, p.PaneTokens(domain.SidebarView{Grouped: true}, g, 0), "group"), "an empty mark is no mark")
}

func TestFlatViewClearsFurniture(t *testing.T) {
	p := textPresentation()
	view, g := rowView(domain.SidebarRow{Pane: "w1:p1", Display: domain.DisplayIdle, Title: "t"}, false)
	tokens := p.PaneTokens(view, g, 0)
	for _, name := range []string{"group", "group_stale", "group_parent", "gap"} {
		assert.Empty(t, val(t, tokens, name))
	}
}

func TestTabLabelRidesInFrontOfTheTitle(t *testing.T) {
	p := textPresentation()
	view, g := rowView(
		domain.SidebarRow{Pane: "w1:p1", Kind: "claude", Display: domain.DisplayDone, Title: "fix", TabLabel: "review"},
		true,
	)
	tokens := p.PaneTokens(view, g, 0)
	assert.Equal(t, "review ✓ fix", val(t, tokens, "title_done"))
	assert.Equal(t, "review ✓", val(t, tokens, "state_done"))
}

func TestSortKeyTokens(t *testing.T) {
	p := textPresentation()
	view, g := rowView(
		domain.SidebarRow{Pane: "w1:p1", Display: domain.DisplayIdle, Keys: domain.SortKeys{Pane: "1", Workspace: "2", Tab: "3"}},
		false,
	)
	tokens := p.PaneTokens(view, g, 0)
	assert.Equal(t, "1", val(t, tokens, "sort_key"))
	assert.Equal(t, "2", val(t, tokens, "ws_key"))
	assert.Equal(t, "3", val(t, tokens, "tab_key"))
	g.Rows[0].Keys.Pane = ""
	assert.Empty(t, val(t, p.PaneTokens(view, g, 0), "sort_key"), "no stamp publishes no key, which sorts last")
}

func TestSpaceTokens(t *testing.T) {
	p := textPresentation()
	tokens := p.SpaceTokens(domain.SpaceView{
		Workspace: "w1", Label: "dashboard", Mark: domain.DisplayWorking, MarkKind: "claude",
		Kinds: []domain.AgentKind{"claude", "codex", "droid"},
	}, at0())
	assert.Equal(t, "○", val(t, tokens, "space_working_claude"))
	assert.Equal(t, "§ claude", val(t, tokens, "space_logo_claude"))
	assert.Equal(t, "Λ codex droid", val(t, tokens, "space_logo_other"), "hueless vendors share one cell")
	assert.Equal(t, "dashboard", val(t, tokens, "space_label"))
	assert.Empty(t, val(t, tokens, "space_none"))

	tokens = p.SpaceTokens(domain.SpaceView{Workspace: "w2", Label: "empty"}, at0())
	assert.Equal(t, "◦", val(t, tokens, "space_none"), "an empty space gets a hollow bullet")

	tokens = p.SpaceTokens(domain.SpaceView{Mark: domain.DisplayWorking, MarkKind: "crush"}, at0())
	assert.Equal(t, "○", val(t, tokens, "space_working_other"), "a vendor past the row budget works in other's colour")
	tokens = p.SpaceTokens(domain.SpaceView{Mark: domain.DisplayIdleStale}, at0())
	assert.Equal(t, "·", val(t, tokens, "space_idle"), "idle tiers share one cell")
	assert.LessOrEqual(t, len(spaceTokenNames()), 32, "a workspace holds at most 32 tokens")
	assert.LessOrEqual(t, len(paneTokenNames()), 32, "a pane holds at most 32 tokens")
}

func TestCompactLineIsOneCell(t *testing.T) {
	p := compactPresentation()
	row := domain.SidebarRow{Pane: "w1:p1", Workspace: "w1", Kind: "claude", Display: domain.DisplayWorking, Title: "Implement OAuth"}
	view, g := rowView(row, true)
	tokens := p.PaneTokens(view, g, 0)
	frame := spinnerFrames()[(view.At.UnixMilli()/150)%8]
	assert.Equal(t, "§ "+frame+" Implement OAuth", val(t, tokens, "title_working"))
	for _, name := range []string{"logo", "logo_working", "logo_stale", "split_mark"} {
		assert.Empty(t, val(t, tokens, name), "no cell of its own: Herdr would put a · beside it")
	}
	assert.Equal(t, "○", val(t, tokens, "state_working"), "the state still reads back")

	row = domain.SidebarRow{Pane: "w1:p1", Kind: "claude", Display: domain.DisplayDone, Title: "fix", TabLabel: "review"}
	view, g = rowView(row, true)
	assert.Equal(t, "§ review ✓ fix", val(t, p.PaneTokens(view, g, 0), "title_done"))

	row = domain.SidebarRow{Pane: "w1:p1", Kind: "droid", Display: domain.DisplayIdle}
	view, g = rowView(row, true)
	assert.Equal(t, GenericMark, val(t, p.PaneTokens(view, g, 0), "title_idle"), "an untitled row still shows whose it is")
	none := p
	none.Glyphs = NewGlyphs(ports.PresentationSettings{Variant: ports.VariantNone}, nil)
	assert.Empty(t, val(t, none.PaneTokens(view, g, 0), "title_idle"), "nothing to show is no row, not a blank one")
}

func TestCompactBlockedPulseBlinks(t *testing.T) {
	p := compactPresentation()
	row := domain.SidebarRow{Pane: "w1:p1", Workspace: "w1", Kind: "codex", Display: domain.DisplayBlocked, Title: "Which port?"}
	view, g := rowView(row, true)
	loud := val(t, p.PaneTokens(view, g, 0), "title_blocked")
	view.At = view.At.Add(5 * 150 * time.Millisecond)
	quiet := val(t, p.PaneTokens(view, g, 0), "title_blocked")
	assert.ElementsMatch(t, []string{"Λ ? Which port?", "Λ   Which port?"}, []string{loud, quiet},
		"a dot beside the logo would read as Herdr's separator, so the mark blinks; the title never moves")

	s := ports.DefaultSettings()
	s.Presentation.Variant = ports.VariantText
	s.Presentation.Glyphs = make(map[domain.DisplayState]string)
	s.Presentation.Glyphs[domain.DisplayIdle] = "•"
	s.Presentation.Layout = ports.LayoutCompact
	chosen := NewPresentation(s, nil)
	view.At = view.At.Add(5 * 150 * time.Millisecond)
	both := []string{val(t, chosen.PaneTokens(view, g, 0), "title_blocked")}
	view.At = view.At.Add(5 * 150 * time.Millisecond)
	both = append(both, val(t, chosen.PaneTokens(view, g, 0), "title_blocked"))
	assert.Contains(t, both, "Λ • Which port?", "a mark the user chose for idle is kept")
}

func TestCompactLineCarriesIndentAndCorner(t *testing.T) {
	p := compactPresentation()
	row := domain.SidebarRow{Pane: "w1:p2", Workspace: "w1", Kind: "claude", Display: domain.DisplayIdle, Title: "t", Depth: 1}
	view, g := rowView(row, true)
	g.Rows = []domain.SidebarRow{{Pane: "w1:p1"}, row}
	assert.Equal(t, zwsp+"  § t", val(t, p.PaneTokens(view, g, 1), "title_idle"))

	row.Corner = domain.CornerLast
	g.Rows[1] = row
	assert.Equal(t, zwsp+"  └─ § t", val(t, p.PaneTokens(view, g, 1), "title_idle"))
}

func TestCompactSpaceIsOneCell(t *testing.T) {
	p := compactPresentation()
	tokens := p.SpaceTokens(domain.SpaceView{
		Workspace: "w1", Label: "dashboard", Mark: domain.DisplayWorking, MarkKind: "claude",
		Kinds: []domain.AgentKind{"claude", "codex"},
	}, at0())
	assert.Equal(t, "§ dashboard", val(t, tokens, "space_working_claude"), "the mark is the agent's logo")
	for _, name := range []string{"space_label", "space_logo_claude", "space_logo_other"} {
		assert.Empty(t, val(t, tokens, name))
	}
	tokens = p.SpaceTokens(domain.SpaceView{Workspace: "w2", Label: "empty"}, at0())
	assert.Equal(t, "◦ empty", val(t, tokens, "space_none"))
}

func TestNestedFolderHeaderWearsNoWorktreeMark(t *testing.T) {
	p := textPresentation()
	row := domain.SidebarRow{Pane: "w2:p1", Workspace: "w2", Kind: "codex", Display: domain.DisplayIdle, Title: "t", Depth: 1}
	view, g := rowView(row, true)
	g.Label, g.Tree = "billing", domain.TreeLastChild
	assert.Equal(t, "\u200B  └─ \uF418 billing", val(t, p.PaneTokens(view, g, 0), "group"), "a worktree keeps its mark")
	g.Folder = true
	assert.Equal(t, "\u200B  └─ billing", val(t, p.PaneTokens(view, g, 0), "group"))
}

func TestNestedSpaceHangsOffItsParent(t *testing.T) {
	compact := compactPresentation().SpaceTokens(domain.SpaceView{
		Workspace: "w2", Label: "billing", Mark: domain.DisplayIdle, MarkKind: "codex", Corner: domain.CornerTee,
	}, at0())
	assert.Equal(t, "\u200B  ├─ Λ billing", val(t, compact, "space_idle"), "the corner sits where Herdr draws a worktree's")

	classic := textPresentation().SpaceTokens(domain.SpaceView{
		Workspace: "w3", Label: "search", Mark: domain.DisplayWorking, MarkKind: "codex",
		Kinds: []domain.AgentKind{"codex", "claude"}, Corner: domain.CornerLast,
	}, at0())
	assert.Equal(t, "\u200B  └─ ○", val(t, classic, "space_working_other"))
	assert.Equal(t, "search", val(t, classic, "space_label"))
	assert.Equal(t, "\u200B     § claude", val(t, classic, "space_logo_claude"), "the first logo cell drawn carries the indent")
	assert.Equal(t, "Λ codex", val(t, classic, "space_logo_other"))

	plain := textPresentation().SpaceTokens(domain.SpaceView{Workspace: "w4", Label: "solo", Mark: domain.DisplayIdle, Kinds: []domain.AgentKind{"codex"}}, at0())
	assert.Equal(t, "·", val(t, plain, "space_idle"))
	assert.Equal(t, "Λ codex", val(t, plain, "space_logo_other"))
}

func TestReadBack(t *testing.T) {
	assert.Equal(t, domain.DisplayBlocked, ReadBack(map[string]string{"title_blocked": "x", "state_blocked": "?"}))
	assert.Equal(t, domain.DisplayState(""), ReadBack(map[string]string{"title_done": "x"}))
}

func TestAgeIsShortAndCoarse(t *testing.T) {
	now := at0()
	for _, tc := range []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, ""}, {time.Minute, "1m"}, {59 * time.Minute, "59m"}, {time.Hour, "1h"},
		{47 * time.Hour, "47h"}, {48 * time.Hour, "2d"}, {200 * time.Hour, "8d"},
	} {
		assert.Equal(t, tc.want, Age(now, now.Add(-tc.ago)), tc.ago)
	}
	assert.Empty(t, Age(now, time.Time{}), "unknown")
	since := now.Add(-90 * time.Second)
	assert.Equal(t, since.Add(2*time.Minute), ageChange(now, since), "1m turns into 2m")
	since = now.Add(-150 * time.Minute)
	assert.Equal(t, since.Add(3*time.Hour), ageChange(now, since), "2h turns into 3h")
}

func TestShowAgePutsTheAgeBeforeTheTitle(t *testing.T) {
	row := domain.SidebarRow{
		Pane: "w1:p1", Workspace: "w1", Kind: "claude", Display: domain.DisplayWorking, Title: "Ship it",
		Since: at0().Add(-12 * time.Minute),
	}
	view, g := rowView(row, true)
	compact := compactPresentation()
	assert.Equal(t, "§ ⣷ Ship it", val(t, compact.PaneTokens(view, g, 0), "title_working"), "by default nobody waits on a turn")
	compact.ShowAge = ports.AgeAll
	assert.Equal(t, "§ ⣷ 12m Ship it", val(t, compact.PaneTokens(view, g, 0), "title_working"))
	classic := textPresentation()
	classic.ShowAge = ports.AgeAll
	assert.Equal(t, "⣷ 12m Ship it", val(t, classic.PaneTokens(view, g, 0), "title_working"))

	idle := row
	idle.Display, idle.Since = domain.DisplayIdleStale, at0().Add(-3*time.Hour)
	view, g = rowView(idle, true)
	assert.Equal(t, "3h Ship it", val(t, classic.PaneTokens(view, g, 0), "title_idle_stale"))

	assert.Equal(t, view.At.Add(time.Hour), classic.NextRepaint(view), "the next repaint is when 3h turns 4h")
	classic.ShowAge = ports.AgeOff
	assert.True(t, classic.NextRepaint(view).IsZero(), "without ages a stale row never moves")
}

func TestShowAgeWaitingIsForRowsThatWaitOnYou(t *testing.T) {
	p := scopePresentation()
	asking := domain.SidebarRow{
		Pane: "w1:p1", Workspace: "w1", Kind: "codex", Display: domain.DisplayBlocked, Title: "Which port?",
		Since: at0().Add(-4 * time.Minute),
	}
	view, g := rowView(asking, true)
	assert.Equal(t, p.Glyphs.BlockedFrame(stepAt(view.At))+" 4m Which port?", val(t, p.PaneTokens(view, g, 0), "title_blocked"))

	done := asking
	done.Display, done.Since = domain.DisplayDone, at0().Add(-12*time.Minute)
	view, g = rowView(done, true)
	assert.Equal(t, "✓ 12m Which port?", val(t, p.PaneTokens(view, g, 0), "title_done"))
	assert.Equal(t, done.Since.Add(13*time.Minute), p.NextRepaint(view))

	parked := asking
	parked.Display, parked.Since = domain.DisplayIdleStale, at0().Add(-3*time.Hour)
	view, g = rowView(parked, true)
	assert.Equal(t, "Which port?", val(t, p.PaneTokens(view, g, 0), "title_idle_stale"))
	assert.True(t, p.NextRepaint(view).IsZero(), "an age nobody sees schedules nothing")
}

func TestSpaceMarkIsTheAgentsLogo(t *testing.T) {
	p := scopePresentation()
	tokens := p.SpaceTokens(domain.SpaceView{
		Workspace: "w1", Label: "dashboard", Mark: domain.DisplayWorking, MarkKind: "claude",
		Kinds: []domain.AgentKind{"claude", "codex"}, Active: []domain.AgentKind{"claude"},
	}, at0())
	assert.Equal(t, "§", val(t, tokens, "space_working_claude"))
	assert.Equal(t, "dashboard", val(t, tokens, "space_label"))
	for _, name := range []string{"space_logo_claude", "space_logo_other", "space_logo_idle"} {
		assert.Empty(t, val(t, tokens, name), "the mark says whose: no line naming the agents")
	}
	assert.Equal(t, "Λ", val(t, p.SpaceTokens(domain.SpaceView{Mark: domain.DisplayIdleStale, MarkKind: "codex"}, at0()), "space_idle"))
	assert.Equal(t, "✓", val(t, p.SpaceTokens(domain.SpaceView{Mark: domain.DisplayDone, MarkKind: "claude"}, at0()), "space_done"),
		"a result keeps its tick")
	assert.Equal(t, "◌", val(t, p.SpaceTokens(domain.SpaceView{Mark: domain.DisplayUnknown, MarkKind: "claude"}, at0()), "space_unknown"))
	assert.Equal(t, "◦", val(t, p.SpaceTokens(domain.SpaceView{Label: "empty"}, at0()), "space_none"))

	asking := domain.SpaceView{Workspace: "w2", Mark: domain.DisplayBlocked, MarkKind: "gemini"}
	frames := []string{
		val(t, p.SpaceTokens(asking, at0()), "space_blocked"),
		val(t, p.SpaceTokens(asking, at0().Add(pulseHalf)), "space_blocked"),
	}
	assert.ElementsMatch(t, []string{"?", "✦"}, frames, "a question blinks ? and the logo")
	view := domain.SidebarView{At: at0(), Spaces: []domain.SpaceView{asking}}
	next := NextAnimation(view)
	assert.True(t, next.After(view.At) && !next.After(view.At.Add(pulseHalf)), "the blink repaints with no row on screen")

	none := p
	none.Glyphs = NewGlyphs(ports.PresentationSettings{Variant: ports.VariantNone}, nil)
	working := domain.SpaceView{Mark: domain.DisplayWorking, MarkKind: "claude"}
	assert.Equal(t, "○", val(t, none.SpaceTokens(working, at0()), "space_working_claude"), "no logos: the state mark")
}

func TestIdleLogosTakeTheirTitlesColour(t *testing.T) {
	p := scopePresentation()
	for state, want := range map[domain.DisplayState]string{
		domain.DisplayWorking: "logo_working", domain.DisplayBlocked: "logo", domain.DisplayDone: "logo",
		domain.DisplayIdleFresh: "logo_fresh", domain.DisplayIdle: "logo_idle", domain.DisplayIdleStale: "logo_stale",
		domain.DisplayUnknown: "logo_unknown",
	} {
		view, g := rowView(domain.SidebarRow{Pane: "w1:p1", Kind: "claude", Display: state, Title: "x"}, true)
		assert.Equal(t, "§", val(t, p.PaneTokens(view, g, 0), want), state)
	}
	view, g := rowView(domain.SidebarRow{Pane: "w1:p1", Kind: "claude", Display: domain.DisplayIdle, Title: "x"}, true)
	assert.Equal(t, "§", val(t, textPresentation().PaneTokens(view, g, 0), "logo"), "logo_color = always: an idle logo keeps it")

	named := scopePresentation()
	named.SpaceMark = ports.SpaceMarkState
	tokens := named.SpaceTokens(domain.SpaceView{
		Workspace: "w1", Label: "dashboard", Mark: domain.DisplayWorking, MarkKind: "claude",
		Kinds: []domain.AgentKind{"claude", "gemini", "codex"}, Active: []domain.AgentKind{"claude"},
	}, at0())
	assert.Equal(t, "§ claude", val(t, tokens, "space_logo_claude"))
	assert.Empty(t, val(t, tokens, "space_logo_gemini"))
	assert.Empty(t, val(t, tokens, "space_logo_other"))
	assert.Equal(t, "✦ gemini Λ codex", val(t, tokens, "space_logo_idle"), "vendors with nothing going on share a grey cell")
}
