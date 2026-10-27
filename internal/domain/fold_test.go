package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// folderScenario is a folder project with two nested repositories and an
// unrelated workspace: acme holds search and billing.
func folderScenario() *scenario {
	s := &scenario{workspaces: []Workspace{
		{ID: "acme", Label: "acme", Root: "/p/acme"},
		{ID: "search", Label: "search", Root: "/p/acme/search"},
		{ID: "billing", Label: "billing", Root: "/p/acme/billing"},
		{ID: "notes", Label: "notes", Root: "/p/notes"},
	}}
	return s.agent("acme", "t1", "p1", 5, DisplayIdle).
		agent("search", "t1", "p1", 1, DisplayBlocked).
		agent("billing", "t1", "p1", 2, DisplayWorking).
		agent("notes", "t1", "p1", 0, DisplayWorking)
}

func (s *scenario) folded(mode SortMode, focused WorkspaceID, roots ...WorkspaceID) SidebarView {
	folded := map[WorkspaceID]bool{}
	for _, root := range roots {
		folded[root] = true
	}
	return BuildSidebar(SidebarInput{
		At: at(0), Mode: mode, Policy: DefaultDisplayPolicy(), Workspaces: s.workspaces, Tabs: s.tabs, Agents: s.agents,
		Folded: folded, Focused: focused,
	})
}

func panesOf(rows []SidebarRow) []PaneID {
	var out []PaneID
	for _, row := range rows {
		out = append(out, row.Pane)
	}
	return out
}

func TestFoldKeepsWhatWaitsOnYou(t *testing.T) {
	v := folderScenario().folded(SortActive, "notes", "acme")
	assert.Equal(t, []string{
		"# notes", "0 notes:p1", "",
		"▸ acme +2 working claude alone", "1 acme:p1",
		"# └ search", "1 search:p1", "",
	}, outline(v), "the line is a row of its own; the question stays under it, the corner closes on what is shown")
	assert.Equal(t, []PaneID{"billing:p1"}, panesOf(v.Folded))
	assert.Equal(t, []PaneID{"search:p1"}, v.Waiting())
}

func TestAFoldThatKeepsNothingIsOneLine(t *testing.T) {
	s := folderScenario()
	s.agents[1].Display = DisplayIdle
	v := s.folded(SortActive, "notes", "acme")
	assert.Equal(t, []string{
		"# notes", "0 notes:p1", "",
		"▸ acme +3 working claude alone", "1 acme:p1", "",
	}, outline(v), "the family's first row carries the line and shows nothing of its own")
	assert.Equal(t, []PaneID{"search:p1", "billing:p1"}, panesOf(v.Folded))
}

func TestFoldNeverHidesTheFocus(t *testing.T) {
	v := folderScenario().folded(SortActive, "acme", "acme")
	assert.Equal(t, []string{
		"# notes", "0 notes:p1", "",
		"▸ acme +1 working claude alone", "2 billing:p1",
		"1 acme:p1", "# └ search", "1 search:p1", "",
	}, outline(v), "a hidden agent carries the line above the family; the line stands for the root's header, its row one level in")
	assert.Empty(t, v.Folded)

	v = folderScenario().folded(SortActive, "billing", "acme")
	assert.Equal(t, []string{
		"# notes", "0 notes:p1", "",
		"▸ acme +1 alone", "1 acme:p1",
		"# ├ search", "1 search:p1", "# └ billing", "1 billing:p1", "",
	}, outline(v), "a nested folder in front stays too")

	s := folderScenario()
	s.agents[2].Agent.Focused = true
	v = s.folded(SortActive, "notes", "acme")
	assert.Contains(t, outline(v), "1 billing:p1", "the focused agent stays wherever the focus seemed to be")
}

func TestALineBorrowsTheKeysOfItsFamilysFirstRow(t *testing.T) {
	v := folderScenario().folded(SortActive, "acme", "acme")
	line, root := v.Groups[1].Rows[0], v.Groups[2].Rows[0]
	require.Equal(t, PaneID("billing:p1"), line.Pane)
	require.Equal(t, PaneID("acme:p1"), root.Pane)
	assert.Equal(t, root.Keys.Workspace, line.Keys.Workspace)
	assert.Equal(t, root.Keys.Tab, line.Keys.Tab)
	assert.Greater(t, line.Keys.Pane, root.Keys.Pane, "the panel sorts descending: the line comes first")
}

func TestFoldsLeaveOnlyScopesGroupedOrders(t *testing.T) {
	for _, mode := range []SortMode{SortRecent, SortOff} {
		v := folderScenario().folded(mode, "notes", "acme")
		assert.Empty(t, v.Folded, mode)
		for _, g := range v.Groups {
			for _, row := range g.Rows {
				assert.Nil(t, row.Fold, mode)
			}
		}
	}
	v := folderScenario().folded(SortHere, "notes", "acme")
	assert.Len(t, v.Folded, 1, "here is active narrowed")
}

func TestFoldedRowsKeepTheirActivityOrder(t *testing.T) {
	s := folderScenario()
	v := s.folded(SortActive, "notes", "acme")
	tree := ResolveTree(s.workspaces, true, "")
	assert.Equal(t, []WorkspaceID{"notes", "acme", "search", "billing"},
		WorkspaceOrder([]WorkspaceID{"acme", "search", "billing", "notes"}, v, tree))
}

func TestFoldKeyIsTheRootsFolder(t *testing.T) {
	assert.Equal(t, "/p/acme", FoldKey(Workspace{ID: "w1", Root: "/p/acme/"}))
	assert.Equal(t, "/src/app", FoldKey(Workspace{ID: "w1", Root: "/src/app/cmd", Repository: repo("app", false, "/src/app")}))
	assert.Equal(t, "w1", FoldKey(Workspace{ID: "w1"}), "no folder: the id")
}

func TestAVisitOpensFromTheLineAndEndsWhenTheFocusLeaves(t *testing.T) {
	s := folderScenario()
	tree := ResolveTree(s.workspaces, true, "")
	last := s.folded(SortActive, "notes", "acme")
	require.Contains(t, outline(last), "1 acme:p1", "acme:p1 carries the line")
	focusOn := func(pane PaneID) []Agent {
		var agents []Agent
		for _, a := range s.agents {
			a.Agent.Focused = a.Agent.Pane == pane
			agents = append(agents, a.Agent)
		}
		return agents
	}

	assert.Nil(t, FollowVisits(nil, last, focusOn("notes:p1"), "notes", tree), "focus on a shown row is no visit")
	opened := FollowVisits(nil, last, focusOn("acme:p1"), "acme", tree)
	assert.Equal(t, map[WorkspaceID]bool{"acme": true}, opened, "focus on the agent carrying the line opens its family")
	assert.Equal(t, opened, FollowVisits(opened, last, focusOn("billing:p1"), "billing", tree), "still open anywhere in the family")
	assert.Equal(t, opened, FollowVisits(opened, last, nil, "search", tree), "no agent in focus: the focused workspace says where it is")
	assert.Nil(t, FollowVisits(opened, last, focusOn("notes:p1"), "notes", tree), "the focus left the family: folded again")
	assert.Nil(t, FollowVisits(opened, last, nil, "", tree), "no focus at all")
}

func TestAFamilyOpenForAVisitShowsWhole(t *testing.T) {
	s := folderScenario()
	v := BuildSidebar(SidebarInput{
		At: at(0), Mode: SortActive, Policy: DefaultDisplayPolicy(), Workspaces: s.workspaces, Tabs: s.tabs, Agents: s.agents,
		Folded: map[WorkspaceID]bool{"acme": true}, Opened: map[WorkspaceID]bool{"acme": true}, Focused: "billing",
	})
	assert.Equal(t, []string{
		"# notes", "0 notes:p1", "",
		"▸ acme +0", "0 acme:p1", "# ├ search", "1 search:p1", "# └ billing", "1 billing:p1", "",
	}, outline(v), "every row, the line on the first in place of the root's header")
	assert.True(t, v.Groups[1].Rows[0].Fold.Open)
	assert.Empty(t, v.Folded)
}
