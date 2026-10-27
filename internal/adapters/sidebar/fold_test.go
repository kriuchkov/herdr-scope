package sidebar

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// foldedView is acme folded: its own working row carries the line, and
// search's question stays under it.
func foldedView(fold domain.Fold) (domain.SidebarView, domain.SidebarGroup, domain.SidebarGroup) {
	fold.Alone, fold.Kept = true, true
	line := domain.SidebarRow{
		Pane: "w1:p1", Workspace: "w1", Kind: "claude", Display: domain.DisplayWorking, Title: "Draft release notes", Depth: 1,
		Fold: &fold,
	}
	asking := domain.SidebarRow{
		Pane:      "w2:p1",
		Workspace: "w2",
		Kind:      "gemini",
		Display:   domain.DisplayBlocked,
		Title:     "Rotate keys",
		Depth:     1,
	}
	lineGroup := domain.SidebarGroup{Workspace: "w1", Label: "acme", Rows: []domain.SidebarRow{line}}
	search := domain.SidebarGroup{
		Workspace: "w2", Label: "search", Tree: domain.TreeLastChild, Folder: true, Header: true, Gap: true,
		Rows: []domain.SidebarRow{asking},
	}
	hidden := domain.SidebarRow{Pane: "w3:p1", Workspace: "w3", Kind: "codex", Display: domain.DisplayWorking, Title: "Ledger rows"}
	view := domain.SidebarView{
		At:      at0(),
		Grouped: true,
		Groups:  []domain.SidebarGroup{lineGroup, search},
		Folded:  []domain.SidebarRow{hidden},
	}
	return view, lineGroup, search
}

func TestFoldLineIsARowOfItsOwn(t *testing.T) {
	p := textPresentation()
	view, line, search := foldedView(domain.Fold{Root: "w1", Label: "acme", Hidden: 2})
	tokens := p.PaneTokens(view, line, 0)
	assert.Equal(t, "▸ acme", val(t, tokens, "fold"))
	assert.Equal(t, "+2", val(t, tokens, "fold_count"))
	for _, name := range []string{"title_working", "logo_working", "group", "gap"} {
		assert.Empty(t, val(t, tokens, name), "%s: the line is all this row shows", name)
	}
	asking := p.PaneTokens(view, search, 0)
	assert.Equal(t, zwsp+"  └─ search", val(t, asking, "group"), "under the line a member's header is its own first line")

	view, line, _ = foldedView(domain.Fold{Root: "w1", Label: "acme", Hidden: 2, Working: true, WorkingKind: "claude"})
	frame := spinnerFrames()[stepAt(view.At)%8]
	tokens = p.PaneTokens(view, line, 0)
	assert.Equal(t, "§ "+frame+" +2", val(t, tokens, "fold_count"), "the logo of the one that works colours the count")
	assert.Equal(t, view.At.Add(spinStep), NextAnimation(view), "and its spinner turns")

	layout, err := NewLayout(p, ports.AppearanceDark, nil)
	require.NoError(t, err)
	out := Render([]Entry{
		{Rows: layout.Agents, Tokens: rendered(tokens)},
		{Rows: layout.Agents, Tokens: rendered(p.PaneTokens(view, search, 0))},
	}, RenderOptions{})
	assert.Equal(t, " ▸ acme · § "+frame+" +2\n   └─ search\n     ✦ · ? Rotate keys\n\n", out)
}

func TestALineThatHidesNothingRidesOnTheFirstRow(t *testing.T) {
	p := textPresentation()
	_, _, search := foldedView(domain.Fold{})
	search.Rows[0].Fold = &domain.Fold{Root: "w1", Label: "acme"}
	view := domain.SidebarView{At: at0(), Grouped: true, Groups: []domain.SidebarGroup{search}}
	tokens := p.PaneTokens(view, search, 0)
	assert.Equal(t, "▸ acme", val(t, tokens, "fold"))
	assert.Empty(t, val(t, tokens, "fold_count"))
	assert.Equal(t, "└─ search", val(t, tokens, "group"), "under the fold line the header is a continuation line")
	assert.Equal(t, "? Rotate keys", val(t, tokens, "title_blocked"))

	search.Rows[0].Fold.Open = true
	assert.Equal(t, "▾ acme", val(t, p.PaneTokens(view, search, 0), "fold"), "open for a visit: it folds again on leaving")
	search.Rows[0].Fold.Open = false

	search.Tree, search.OrphanRepo = domain.TreeOrphan, "acme"
	tokens = p.PaneTokens(view, search, 0)
	assert.Empty(t, val(t, tokens, "group_parent"), "the fold line names the family already")
	assert.Equal(t, "└─ search", val(t, tokens, "group"))
}

func TestAloneFoldShowsTheLineAlone(t *testing.T) {
	p := textPresentation()
	row := domain.SidebarRow{
		Pane: "w1:p1", Workspace: "w1", Kind: "claude", Display: domain.DisplayIdle, Title: "Draft release notes",
		Fold: &domain.Fold{Root: "w1", Label: "acme", Hidden: 3, Alone: true},
	}
	g := domain.SidebarGroup{Workspace: "w1", Label: "acme", Gap: true, Rows: []domain.SidebarRow{row}}
	view := domain.SidebarView{At: at0(), Grouped: true, Groups: []domain.SidebarGroup{g}}
	tokens := p.PaneTokens(view, g, 0)
	assert.Equal(t, "▸ acme", val(t, tokens, "fold"))
	assert.Equal(t, "+3", val(t, tokens, "fold_count"))
	for _, name := range []string{"title_idle", "logo", "split_mark", "group"} {
		assert.Empty(t, val(t, tokens, name), name)
	}
	assert.Equal(t, "·", val(t, tokens, "state_idle"), "the unrendered mark stays for read-back")
	assert.Equal(t, zwsp, val(t, tokens, "gap"))
}

func TestFoldedRowIsMarkedAndKeepsStill(t *testing.T) {
	p := textPresentation()
	view, _, _ := foldedView(domain.Fold{Root: "w1", Label: "acme", Hidden: 1})
	tokens := p.FoldedTokens(view, view.Folded[0])
	assert.Equal(t, "1", val(t, tokens, "folded"), "the mark the panel's filter drops")
	assert.Equal(t, spinnerFrames()[0]+" Ledger rows", val(t, tokens, "title_working"),
		"readable after a stop, and still: nobody sees it turn")
	view.At = view.At.Add(time.Second)
	assert.Equal(t, tokens, p.FoldedTokens(view, view.Folded[0]))
	assert.Empty(t, val(t, tokens, "group"))
}

func TestCompactFoldIsOneCell(t *testing.T) {
	p := compactPresentation()
	view, g, _ := foldedView(domain.Fold{Root: "w1", Label: "acme", Hidden: 2, Working: true, WorkingKind: "claude"})
	frame := spinnerFrames()[stepAt(view.At)%8]
	tokens := p.PaneTokens(view, g, 0)
	assert.Equal(t, "▸ acme § "+frame+" +2", val(t, tokens, "fold"))
	assert.Empty(t, val(t, tokens, "fold_count"))
}

// rendered is a patch as Herdr stores it: set values only.
func rendered(tokens Tokens) map[string]string {
	out := map[string]string{}
	for name, value := range tokens {
		if value != nil {
			out[name] = *value
		}
	}
	return out
}

func TestFoldCountColours(t *testing.T) {
	p := textPresentation()
	layout, err := NewLayout(p, ports.AppearanceDark, nil)
	require.NoError(t, err)
	count := layout.Agents[0][2]
	require.Equal(t, "$fold_count", count.Token)
	pick := func(value string) string {
		for _, r := range count.Rules {
			if (r.Contains != "" && strings.Contains(value, r.Contains)) || (r.StartsWith != "" && strings.HasPrefix(value, r.StartsWith)) {
				return r.Fg
			}
		}
		return count.Fg
	}
	claude, _ := BrandColor("claude")
	assert.Equal(t, claude, pick("§ ⣷ +2"), "a branded vendor's own colour")
	assert.Equal(t, otherBrand, pick("Λ ⣷ +2"), "any other vendor at work: the working colour")
	assert.Equal(t, colorsFor(ports.AppearanceDark).IdleStale, pick("+2"), "a bare count fades")
	assert.LessOrEqual(t, len(count.Rules), ruleLimit)
	assert.Contains(t, layout.TOML(), `{ starts_with = "+", fg = "`)
}
