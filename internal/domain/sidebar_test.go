package domain

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scenario builds sidebar inputs compactly: one agent per (workspace, tab,
// pane) triple, activity in minutes before t0 (negative = none).
type scenario struct {
	workspaces []Workspace
	tabs       []Tab
	agents     []ObservedAgent
}

func (s *scenario) ws(id WorkspaceID, label string, repo *RepositoryInfo) *scenario {
	s.workspaces = append(s.workspaces, Workspace{ID: id, Label: label, Repository: repo})
	return s
}

func (s *scenario) agent(ws WorkspaceID, tab string, pane string, minutesAgo int, display DisplayState) *scenario {
	tabID := TabID(fmt.Sprintf("%s:%s", ws, tab))
	known := false
	for _, t := range s.tabs {
		known = known || t.ID == tabID
	}
	if !known {
		s.tabs = append(s.tabs, Tab{ID: tabID, Workspace: ws, Label: tab})
	}
	activity := Activity{}
	if minutesAgo >= 0 {
		activity = Activity{At: at(0).Add(-time.Duration(minutesAgo) * time.Minute)}
	}
	paneID := PaneID(fmt.Sprintf("%s:%s", ws, pane))
	s.agents = append(s.agents, ObservedAgent{
		Agent:    Agent{ID: AgentID(paneID), Pane: paneID, Tab: tabID, Workspace: ws, Kind: "claude", Title: "task " + string(paneID)},
		Display:  display,
		Activity: activity,
	})
	return s
}

func (s *scenario) build(mode SortMode, policy DisplayPolicy) SidebarView {
	return BuildSidebar(SidebarInput{At: at(0), Mode: mode, Policy: policy, Workspaces: s.workspaces, Tabs: s.tabs, Agents: s.agents})
}

func repo(name string, linked bool, checkout string) *RepositoryInfo {
	return &RepositoryInfo{
		ID:       RepositoryID("/src/" + name + "/.git"),
		Name:     name,
		Root:     "/src/" + name,
		Checkout: WorktreeID(checkout),
		Linked:   linked,
	}
}

// outline renders a view as text for compact assertions.
func outline(v SidebarView) []string {
	var out []string
	for _, g := range v.Groups {
		if f := g.Rows[0].Fold; f != nil {
			line := fmt.Sprintf("▸ %s +%d", f.Label, f.Hidden)
			if f.Working {
				line += " working " + string(f.WorkingKind)
			}
			if f.Alone {
				line += " alone"
			}
			out = append(out, line)
		}
		if g.Header {
			tree := map[TreePosition]string{TreeRoot: "", TreeChild: "├ ", TreeLastChild: "└ ", TreeOrphan: "@" + g.OrphanRepo + " └ ", TreeDetached: "~ "}[g.Tree]
			stale := ""
			if g.Stale {
				stale = " (stale)"
			}
			out = append(out, "# "+tree+g.Label+stale)
		}
		for _, r := range g.Rows {
			corner := map[Corner]string{CornerNone: "", CornerTee: "├─", CornerLast: "└─"}[r.Corner]
			out = append(out, fmt.Sprintf("%d%s %s", r.Depth, corner, r.Pane))
		}
		if g.Gap {
			out = append(out, "")
		}
	}
	return out
}

func TestActiveOrderPutsTheBusiestGroupFirst(t *testing.T) {
	s := (&scenario{}).ws("w1", "billing", nil).ws("w2", "dashboard", nil).
		agent("w1", "t1", "p1", 90, DisplayIdle).
		agent("w2", "t1", "p1", 1, DisplayIdleFresh).
		agent("w2", "t2", "p2", 30, DisplayIdle)
	v := s.build(SortActive, DefaultDisplayPolicy())
	require.True(t, v.Grouped)
	assert.Equal(t, []string{
		"# dashboard", "0 w2:p1", "1 w2:p2", "",
		"# billing", "0 w1:p1", "",
	}, outline(v))
}

func TestRecentOrderIsFlatAndUnstampedLast(t *testing.T) {
	s := (&scenario{}).ws("w1", "billing", nil).ws("w2", "dashboard", nil).
		agent("w1", "t1", "p1", -1, DisplayIdle).
		agent("w1", "t2", "p2", 5, DisplayIdle).
		agent("w2", "t1", "p1", 1, DisplayWorking)
	v := s.build(SortRecent, DefaultDisplayPolicy())
	assert.False(t, v.Grouped)
	assert.Equal(t, []string{"0 w2:p1", "0 w1:p2", "0 w1:p1"}, outline(v))
	assert.Empty(t, v.Groups[0].Rows[2].Keys.Pane, "no stamp publishes no pane key")
}

func TestOffOrderKeepsHerdrOrderAndGroupsOnlyWhenHerdrDoes(t *testing.T) {
	s := (&scenario{}).ws("w1", "billing", nil).ws("w2", "dashboard", nil).
		agent("w1", "t1", "p1", 90, DisplayIdle).
		agent("w2", "t1", "p1", 1, DisplayIdle)
	in := SidebarInput{
		At:          at(0),
		Mode:        SortOff,
		HostGrouped: true,
		Policy:      DefaultDisplayPolicy(),
		Workspaces:  s.workspaces,
		Tabs:        s.tabs,
		Agents:      s.agents,
	}
	assert.Equal(t, []string{"# billing", "0 w1:p1", "", "# dashboard", "0 w2:p1", ""}, outline(BuildSidebar(in)))
	in.HostGrouped = false
	assert.Equal(t, []string{"0 w1:p1", "0 w2:p1"}, outline(BuildSidebar(in)))
}

func TestGapsFollowTheSetting(t *testing.T) {
	s := (&scenario{}).ws("w1", "a", nil).ws("w2", "b", nil).
		agent("w1", "t1", "p1", 1, DisplayIdle).agent("w2", "t1", "p1", 2, DisplayIdle)
	p := DefaultDisplayPolicy()
	p.GroupGap = false
	assert.Equal(t, []string{"# a", "0 w1:p1", "# b", "0 w2:p1"}, outline(s.build(SortActive, p)))
}

func TestWorktreeFamilyStaysTogetherParentFirst(t *testing.T) {
	s := (&scenario{}).
		ws("app", "app", repo("app", false, "/src/app")).
		ws("other", "other", nil).
		ws("feature", "feature/auth", repo("app", true, "/wt/feature")).
		ws("hotfix", "hotfix", repo("app", true, "/wt/hotfix")).
		agent("app", "t1", "p1", 300, DisplayIdleStale).
		agent("other", "t1", "p1", 10, DisplayIdle).
		agent("feature", "t1", "p1", 1, DisplayWorking).
		agent("hotfix", "t1", "p1", 20, DisplayIdle)
	v := s.build(SortActive, DefaultDisplayPolicy())
	// The busy worktree pulls its dormant parent up with it; the parent is
	// drawn first and the family has no gap inside.
	assert.Equal(t, []string{
		"# app (stale)", "0 app:p1",
		"# ├ feature/auth", "1 feature:p1",
		"# └ hotfix", "1 hotfix:p1", "",
		"# other", "0 other:p1", "",
	}, outline(v))
}

func TestOrphanWorktreeGetsARepositoryRow(t *testing.T) {
	s := (&scenario{}).
		ws("app", "app", repo("app", false, "/src/app")).
		ws("feature", "feature/auth", repo("app", true, "/wt/feature")).
		agent("feature", "t1", "p1", 1, DisplayIdle)
	v := s.build(SortActive, DefaultDisplayPolicy())
	assert.Equal(t, []string{"# @app └ feature/auth", "1 feature:p1", ""}, outline(v))
}

func TestStrayWorktreeIsDetachedInHerdrOrder(t *testing.T) {
	// Herdr's order: app, its first worktree, an unrelated project, a worktree
	// of app opened later, another project.
	s := (&scenario{}).
		ws("app", "app", repo("app", false, "/src/app")).
		ws("feature", "feature", repo("app", true, "/wt/feature")).
		ws("other", "other", nil).
		ws("hotfix", "hotfix", repo("app", true, "/wt/hotfix")).
		ws("scratch", "scratch", nil)
	for _, ws := range []WorkspaceID{"app", "feature", "other", "hotfix", "scratch"} {
		s.agent(ws, "t1", "p1", 1, DisplayIdle)
	}
	in := SidebarInput{
		At:          at(0),
		Mode:        SortOff,
		HostGrouped: true,
		Policy:      DefaultDisplayPolicy(),
		Workspaces:  s.workspaces,
		Tabs:        s.tabs,
		Agents:      s.agents,
	}
	assert.Equal(t, []string{
		"# app", "0 app:p1",
		"# └ feature", "1 feature:p1", "",
		"# other", "0 other:p1", "",
		"# ~ hotfix", "0 hotfix:p1", "",
		"# scratch", "0 scratch:p1", "",
	}, outline(BuildSidebar(in)), "the stray gets no corner into a stranger and no second repo row")

	v := s.build(SortActive, DefaultDisplayPolicy())
	for _, g := range v.Groups {
		assert.NotEqual(t, TreeDetached, g.Tree, "in Scope's own order a family is one block")
	}
}

func TestSecondPaneOfAWorkspaceIndents(t *testing.T) {
	s := (&scenario{}).ws("w1", "a", repo("a", true, "/wt/a")).ws("w0", "root", repo("a", false, "/src/a")).
		agent("w0", "t1", "p1", 5, DisplayIdle).
		agent("w1", "t1", "p1", 1, DisplayIdle).
		agent("w1", "t2", "p2", 2, DisplayIdle)
	assert.Equal(t, []string{
		"# root", "0 w0:p1",
		"# └ a", "1 w1:p1", "2 w1:p2", "",
	}, outline(s.build(SortActive, DefaultDisplayPolicy())))
}

func TestSplitCorners(t *testing.T) {
	s := (&scenario{}).ws("w1", "proj", nil).
		agent("w1", "t1", "p1", 3, DisplayIdle).
		agent("w1", "t1", "p2", 1, DisplayIdle).
		agent("w1", "t1", "p3", 2, DisplayIdle).
		agent("w1", "t2", "p4", 10, DisplayIdle)
	p := DefaultDisplayPolicy()
	assert.Equal(t, []string{"# proj", "0 w1:p1", "1 w1:p2", "1 w1:p3", "1 w1:p4", ""}, outline(s.build(SortActive, p)),
		"by default a split draws plain rows, but its panes stay together, the split-from pane first")

	p.SplitCorner = true
	assert.Equal(t, []string{"# proj", "0 w1:p1", "1├─ w1:p2", "1└─ w1:p3", "1 w1:p4", ""}, outline(s.build(SortActive, p)))

	p.GroupIndent = 0
	assert.Equal(t, []string{"# proj", "0 w1:p1", "1 w1:p2", "1 w1:p3", "1 w1:p4", ""}, outline(s.build(SortActive, p)),
		"zero indent draws no corners")
}

func TestRowLabelsAndPrefixTrim(t *testing.T) {
	s := (&scenario{}).ws("w1", "billing", nil).agent("w1", "review", "p1", 1, DisplayIdle)
	s.agents[0].Agent.Title = "billing · fix rounding"
	p := DefaultDisplayPolicy()
	row := s.build(SortActive, p).Groups[0].Rows[0]
	assert.Equal(t, "fix rounding", row.Title)

	p.TrimGroupPrefix = false
	assert.Equal(t, "billing · fix rounding", s.build(SortActive, p).Groups[0].Rows[0].Title)

	p = DefaultDisplayPolicy()
	assert.Equal(t, "billing · fix rounding", s.build(SortRecent, p).Groups[0].Rows[0].Title, "no header in the flat view, nothing to trim")

	p.RowLabel = RowLabelBoth
	row = s.build(SortActive, p).Groups[0].Rows[0]
	assert.Equal(t, "review", row.TabLabel)
	assert.Equal(t, "fix rounding", row.Title)
}

func TestSortKeysEncodeFamilyAndSplit(t *testing.T) {
	s := (&scenario{}).ws("w1", "a", nil).agent("w1", "t1", "p1", 1, DisplayIdle).agent("w1", "t1", "p2", -1, DisplayIdle)
	v := s.build(SortActive, DefaultDisplayPolicy())
	head, second := v.Groups[0].Rows[0], v.Groups[0].Rows[1]
	minute := fmt.Sprintf("%012d", at(0).Add(-time.Minute).UnixMilli()/60000)
	assert.Equal(t, minute, head.Keys.Pane)
	assert.Equal(t, minute+"-w1-1-"+minute+"-w1", head.Keys.Workspace)
	assert.Equal(t, minute+"-w1:t1-1-"+minute, head.Keys.Tab)
	assert.Equal(t, minute+"-w1:t1-0-"+noStamp, second.Keys.Tab)
	assert.Empty(t, second.Keys.Pane)
}

func TestSpacesAggregateByUrgency(t *testing.T) {
	s := (&scenario{}).ws("w1", "a", nil).ws("w2", "b", nil).ws("w3", "empty", nil).
		agent("w1", "t1", "p1", 1, DisplayWorking).
		agent("w1", "t2", "p2", 1, DisplayBlocked).
		agent("w2", "t1", "p1", 30, DisplayIdle).
		agent("w2", "t2", "p2", 1, DisplayIdleFresh)
	s.agents[1].Agent.Kind = "codex"
	v := s.build(SortActive, DefaultDisplayPolicy())
	require.Len(t, v.Spaces, 3)
	assert.Equal(
		t,
		SpaceView{
			Workspace: "w1", Label: "a", Mark: DisplayBlocked, MarkKind: "codex",
			Kinds: []AgentKind{"claude", "codex"}, Active: []AgentKind{"claude", "codex"},
		},
		v.Spaces[0],
	)
	assert.Equal(t, DisplayIdleFresh, v.Spaces[1].Mark, "among parked agents the freshest speaks")
	assert.Empty(t, v.Spaces[1].Active, "nothing going on: no vendor is active")
	assert.Equal(t, SpaceView{Workspace: "w3", Label: "empty"}, v.Spaces[2], "a workspace with no agent has no mark")
}

func TestStaleHeaderOnlyWhenEveryAgentIsStale(t *testing.T) {
	s := (&scenario{}).ws("w1", "a", nil).
		agent("w1", "t1", "p1", 300, DisplayIdleStale).
		agent("w1", "t2", "p2", 1, DisplayIdle)
	assert.False(t, s.build(SortActive, DefaultDisplayPolicy()).Groups[0].Stale)
	s.agents[1].Display = DisplayIdleStale
	assert.True(t, s.build(SortActive, DefaultDisplayPolicy()).Groups[0].Stale)
}

func TestEmptySidebar(t *testing.T) {
	v := (&scenario{}).ws("w1", "a", nil).build(SortActive, DefaultDisplayPolicy())
	assert.Empty(t, v.Groups)
	assert.Len(t, v.Spaces, 1)
	assert.Empty(t, (&scenario{}).build(SortRecent, DefaultDisplayPolicy()).Groups)
}

func TestWorkspaceOrderMovesFamiliesAndIsIdempotent(t *testing.T) {
	s := (&scenario{}).
		ws("app", "app", repo("app", false, "/src/app")).
		ws("idle", "idle", nil).
		ws("other", "other", nil).
		ws("feature", "feature", repo("app", true, "/wt/feature")).
		agent("app", "t1", "p1", 300, DisplayIdleStale).
		agent("other", "t1", "p1", 10, DisplayIdle).
		agent("feature", "t1", "p1", 1, DisplayWorking)
	v := s.build(SortActive, DefaultDisplayPolicy())
	current := []WorkspaceID{"app", "idle", "other", "feature"}
	tree := ResolveWorktrees(s.workspaces)
	wanted := WorkspaceOrder(current, v, tree)
	assert.Equal(t, []WorkspaceID{"app", "feature", "other", "idle"}, wanted)
	assert.Equal(t, wanted, WorkspaceOrder(wanted, v, tree), "applying it twice moves nothing")
	assert.Equal(t, current, WorkspaceOrder(current, SidebarView{}, tree), "no activity, no move")
}

func TestWaitingAsksBeforeFinished(t *testing.T) {
	view := SidebarView{Groups: []SidebarGroup{
		{Rows: []SidebarRow{{Pane: "w1:p1", Display: DisplayDone}, {Pane: "w1:p2", Display: DisplayWorking}}},
		{Rows: []SidebarRow{{Pane: "w2:p1", Display: DisplayBlocked}, {Pane: "w2:p2", Display: DisplayIdleFresh}}},
		{Rows: []SidebarRow{{Pane: "w3:p1", Display: DisplayDone}, {Pane: "w3:p2", Display: DisplayBlocked}}},
	}}
	assert.Equal(t, []PaneID{"w2:p1", "w3:p2", "w1:p1", "w3:p1"}, view.Waiting(),
		"every question in panel order, then every unseen result")
	assert.Empty(t, SidebarView{}.Waiting())
}

func TestNextWaitingGoesRoundTheList(t *testing.T) {
	waiting := []PaneID{"w2:p1", "w1:p1", "w3:p1"}
	assert.Equal(t, PaneID("w2:p1"), NextWaiting(waiting, "w9:p9"), "the focus is on none of them: the first")
	assert.Equal(t, PaneID("w1:p1"), NextWaiting(waiting, "w2:p1"), "the one after the focused one")
	assert.Equal(t, PaneID("w2:p1"), NextWaiting(waiting, "w3:p1"), "round the list")
	assert.Equal(t, PaneID("w2:p1"), NextWaiting([]PaneID{"w2:p1"}, "w2:p1"), "alone: itself again")
	assert.Empty(t, NextWaiting(nil, "w2:p1"), "nobody waits")
}

func TestBuildSidebarDrawsTheTreeItIsGiven(t *testing.T) {
	s := folderScenario()
	in := SidebarInput{
		At: at(0), Mode: SortActive, Policy: DefaultDisplayPolicy(), Workspaces: s.workspaces, Tabs: s.tabs, Agents: s.agents,
	}
	given := in
	resolved := ResolveTree(s.workspaces, true, "")
	given.Tree = &resolved
	assert.Equal(t, BuildSidebar(in), BuildSidebar(given), "the tree it would resolve itself")
	flat := ResolveTree(s.workspaces, false, "")
	given.Tree = &flat
	assert.NotEqual(t, outline(BuildSidebar(in)), outline(BuildSidebar(given)), "a given tree is the one drawn")
}

func TestHereNarrowsActiveAndComesBack(t *testing.T) {
	assert.Equal(t, SortHere, SortActive.Apply(SortHereToggle))
	assert.Equal(t, SortHere, SortRecent.Apply(SortHereToggle))
	assert.Equal(t, SortActive, SortHere.Apply(SortHereToggle), "again: all of them")
	assert.Equal(t, SortRecent, SortHere.Apply(SortCycle), "the cycle carries on as from active")
	assert.True(t, SortHere.Grouped(false), "grouped like active")
	mode, ok := ParseSortMode("here")
	assert.True(t, ok)
	assert.Equal(t, SortHere, mode)
}

func TestFamilyIsTheRootAndItsMembers(t *testing.T) {
	workspaces := projectSession()
	tree := ResolveTree(workspaces, true, "/home/user")
	ids := order(workspaces)
	assert.Equal(t, []WorkspaceID{"acme", "gateway", "billing", "search"}, tree.Family("acme", ids))
	assert.Equal(t, []WorkspaceID{"acme", "gateway", "billing", "search"}, tree.Family("billing", ids), "from a member too")
	assert.Equal(t, []WorkspaceID{"notes"}, tree.Family("notes", ids), "alone in its own")
	assert.Equal(t, WorkspaceID("acme"), tree.Root("billing"))
	assert.Equal(t, WorkspaceID("acme"), tree.Root("acme"))
	assert.Equal(t, WorkspaceID("w9"), tree.Root("w9"), "a workspace the tree does not know is its own root")
}

func TestWaitingCounts(t *testing.T) {
	view := SidebarView{Groups: []SidebarGroup{{Rows: []SidebarRow{
		{Display: DisplayBlocked}, {Display: DisplayDone}, {Display: DisplayDone}, {Display: DisplayWorking},
	}}}}
	asking, finished := view.WaitingCounts()
	assert.Equal(t, 1, asking)
	assert.Equal(t, 2, finished)
}
