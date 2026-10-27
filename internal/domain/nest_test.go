package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// folder is a workspace opened in a plain folder.
func folder(id WorkspaceID, root string) Workspace {
	return Workspace{ID: id, Label: string(id), Root: root}
}

// checkout is a workspace on a git checkout; its first pane may sit deeper.
func checkout(id WorkspaceID, path string, linked bool, repo string) Workspace {
	return Workspace{ID: id, Label: string(id), Root: path + "/cmd", Repository: &RepositoryInfo{
		ID: RepositoryID("/git/" + repo), Name: repo, Root: "/src/" + repo, Checkout: WorktreeID(path), Linked: linked,
	}}
}

// projectSession is a plain folder holding three repositories, opened in an
// order that scatters them.
func projectSession() []Workspace {
	return []Workspace{
		folder("acme", "/home/user/acme"),
		folder("notes", "/home/user/src/notes"),
		folder("blog", "/home/user/src/blog"),
		checkout("gateway", "/home/user/acme/externals/gateway", false, "gateway"),
		checkout("billing", "/home/user/acme/services/billing", false, "billing"),
		checkout("search", "/home/user/acme/services/search", false, "search"),
		folder("herdr-scope", "/home/user/src/herdr-scope"),
	}
}

func order(workspaces []Workspace) []WorkspaceID {
	out := make([]WorkspaceID, len(workspaces))
	for i, ws := range workspaces {
		out[i] = ws.ID
	}
	return out
}

func TestResolveTreeNestsFoldersUnderTheEnclosingWorkspace(t *testing.T) {
	tree := ResolveTree(projectSession(), true, "/home/user")
	want := map[WorkspaceID]WorkspaceID{"gateway": "acme", "billing": "acme", "search": "acme"}
	assert.Equal(t, want, tree.Folders)
	assert.Equal(t, want, tree.Parents)
	assert.Equal(t, "acme", tree.ParentLabels["billing"], "the parent names a synthesised row")

	assert.Empty(t, ResolveTree(projectSession(), false, "/home/user").Folders, "nest_folders off")
}

func TestResolveTreeStaysOneLevelDeep(t *testing.T) {
	workspaces := []Workspace{
		folder("acme", "/home/user/acme"),
		folder("services", "/home/user/acme/services"),
		checkout("billing", "/home/user/acme/services/billing", false, "billing"),
	}
	tree := ResolveTree(workspaces, true, "")
	assert.Equal(t, map[WorkspaceID]WorkspaceID{"services": "acme", "billing": "acme"}, tree.Parents,
		"a folder nested twice hangs under the outermost workspace")
}

func TestResolveTreeLeavesPeersHomeAndRootsAlone(t *testing.T) {
	workspaces := []Workspace{
		folder("root", "/"),
		folder("home", "/home/user/"),
		folder("acme", "/home/user/acme"),
		folder("acme-copy", "/home/user/acme"),
		folder("acme-old", "/home/user/acme-old"),
		folder("unknown", ""),
	}
	assert.Empty(t, ResolveTree(workspaces, true, "/home/user").Parents,
		"neither home nor / adopts, the same folder is a peer, a shared prefix is not a parent")
}

func TestResolveTreeKeepsWorktreesWithTheirFamily(t *testing.T) {
	workspaces := []Workspace{
		folder("acme", "/home/user/acme"),
		checkout("billing", "/home/user/acme/services/billing", false, "billing"),
		checkout("billing-fix", "/home/user/wt/billing-fix", true, "billing"),
		checkout("inside", "/home/user/acme/wt/billing-other", true, "billing"),
	}
	tree := ResolveTree(workspaces, true, "")
	assert.Equal(t, map[WorkspaceID]WorkspaceID{"billing": "acme"}, tree.Folders,
		"a linked worktree is its repository's, wherever it lives")
	assert.Equal(t, map[WorkspaceID]WorkspaceID{"billing": "acme", "billing-fix": "acme", "inside": "acme"}, tree.Parents,
		"a worktree of a nested checkout rides with the outermost family")
	assert.Equal(t, "billing", tree.ParentLabels["billing-fix"])
}

func TestResolveTreeReadsWindowsPaths(t *testing.T) {
	workspaces := []Workspace{
		folder("drive", `C:\`),
		folder("acme", `C:\Users\u\acme`),
		folder("billing", `C:\Users\u\acme\services\billing`),
	}
	assert.Equal(t, map[WorkspaceID]WorkspaceID{"billing": "acme"}, ResolveTree(workspaces, true, `C:\Users\u`).Folders)
}

func TestNestOrderMovesOnlyFamiliesThatTookInAFolder(t *testing.T) {
	workspaces := append(projectSession(),
		checkout("app", "/home/user/src/app", false, "app"),
		checkout("feature", "/home/user/wt/feature", true, "app"))
	current := order(workspaces)
	wanted := NestOrder(current, ResolveTree(workspaces, true, "/home/user"))
	assert.Equal(t, []WorkspaceID{"acme", "gateway", "billing", "search", "notes", "blog", "herdr-scope", "app", "feature"}, wanted)
	assert.Equal(t, wanted, NestOrder(wanted, ResolveTree(workspaces, true, "/home/user")), "applying it twice moves nothing")

	worktreesOnly := []WorkspaceID{"app", "notes", "feature"}
	assert.Equal(t, worktreesOnly, NestOrder(worktreesOnly, ResolveTree(workspaces, true, "/home/user")),
		"Herdr draws a worktree family as a block already")
}

func TestSpaceCornersHangNestedFoldersOffTheirParent(t *testing.T) {
	s := &scenario{workspaces: projectSession()}
	nested := func(workspaces []Workspace, policy DisplayPolicy) map[WorkspaceID]Corner {
		s.workspaces = workspaces
		out := map[WorkspaceID]Corner{}
		for _, space := range BuildSidebar(SidebarInput{At: at(0), Mode: SortActive, Policy: policy, Workspaces: workspaces, Home: "/home/user"}).Spaces {
			if space.Corner != CornerNone {
				out[space.Workspace] = space.Corner
			}
		}
		return out
	}
	policy := DefaultDisplayPolicy()
	assert.Empty(t, nested(projectSession(), policy), "Herdr's order put them away from acme: no corner hangs off a stranger")

	byID := map[WorkspaceID]Workspace{}
	for _, ws := range projectSession() {
		byID[ws.ID] = ws
	}
	var moved []Workspace
	for _, id := range NestOrder(order(projectSession()), ResolveTree(projectSession(), true, "/home/user")) {
		moved = append(moved, byID[id])
	}
	assert.Equal(t, map[WorkspaceID]Corner{"gateway": CornerTee, "billing": CornerTee, "search": CornerLast}, nested(moved, policy))

	policy.GroupIndent = 0
	assert.Empty(t, nested(moved, policy), "group_indent = 0 draws no tree lines")
	policy = DefaultDisplayPolicy()
	policy.NestFolders = false
	assert.Empty(t, nested(moved, policy))
}

func TestSpaceCornersSeeHerdrsWorktreeBlocks(t *testing.T) {
	workspaces := []Workspace{
		folder("acme", "/home/user/acme"),
		checkout("billing", "/home/user/acme/services/billing", false, "billing"),
		checkout("search", "/home/user/acme/services/search", false, "search"),
		checkout("billing-fix", "/home/user/wt/billing-fix", true, "billing"),
	}
	assert.Equal(t, []WorkspaceID{"acme", "billing", "billing-fix", "search"}, hostSpaceOrder(workspaces),
		"Herdr draws the worktree under its checkout, wherever its index is")
	corners := map[WorkspaceID]Corner{}
	for _, space := range BuildSidebar(SidebarInput{At: at(0), Mode: SortActive, Policy: DefaultDisplayPolicy(), Workspaces: workspaces}).Spaces {
		corners[space.Workspace] = space.Corner
	}
	assert.Equal(t, CornerTee, corners["billing"])
	assert.Equal(t, CornerNone, corners["billing-fix"], "Herdr draws a worktree's corner itself")
	assert.Equal(t, CornerLast, corners["search"], "the row above is billing's worktree, family still")
}

func TestNestedFolderHangsUnderItsParentInAgents(t *testing.T) {
	s := &scenario{workspaces: projectSession()}
	s.agent("acme", "t1", "p1", 300, DisplayIdleStale).
		agent("billing", "t1", "p1", 1, DisplayWorking).
		agent("notes", "t1", "p1", 5, DisplayIdle)
	assert.Equal(t, []string{
		"# acme (stale)", "0 acme:p1",
		"# └ billing", "1 billing:p1", "",
		"# notes", "0 notes:p1", "",
	}, outline(nestedInput(s)), "the busy folder pulls its parent up and hangs under it")

	s = &scenario{workspaces: projectSession()}
	s.agent("billing", "t1", "p1", 1, DisplayWorking)
	assert.Equal(t, []string{"# @acme └ billing", "1 billing:p1", ""}, outline(nestedInput(s)),
		"a parent with no agent is synthesised, as for a worktree")
}

func nestedInput(s *scenario) SidebarView {
	return BuildSidebar(SidebarInput{
		At: at(0), Mode: SortActive, Policy: DefaultDisplayPolicy(),
		Workspaces: s.workspaces, Tabs: s.tabs, Agents: s.agents,
	})
}
