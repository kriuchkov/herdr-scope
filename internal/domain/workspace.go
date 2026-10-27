package domain

import "strings"

// Workspace is one Herdr workspace (a Space).
type Workspace struct {
	ID    WorkspaceID
	Label string
	// Root is the directory the workspace's first pane was opened in; empty
	// when unknown.
	Root string
	// Repository is the git checkout the workspace was opened on, nil when it
	// is not one (or nobody could tell).
	Repository *RepositoryInfo
}

// RepositoryInfo is the git identity of one checkout.
type RepositoryInfo struct {
	// ID is the repository's common git directory, shared by the main
	// checkout and every linked worktree.
	ID RepositoryID
	// Name is the repository's directory name.
	Name string
	// Root is the main checkout's path.
	Root string
	// Checkout is this checkout's path.
	Checkout WorktreeID
	// Linked is true for a worktree created with `git worktree add`, false
	// for the main checkout.
	Linked bool
}

// Tab is one Herdr tab. Its panes form one split screen.
type Tab struct {
	ID        TabID
	Workspace WorkspaceID
	// Label is the tab's name, or its number when it was never named.
	Label string
}

// WorktreeTree is how open workspaces nest: a git worktree under the checkout
// it was cut from and, with nest_folders, a workspace opened inside another
// workspace's folder under that workspace. It is one level deep.
type WorktreeTree struct {
	// Parents maps a nested workspace to the top-level workspace it hangs
	// under.
	Parents map[WorkspaceID]WorkspaceID
	// ParentLabels names the parent row synthesised when the parent runs no
	// agent, the case Parents cannot cover: a linked worktree's repository, a
	// nested folder's parent workspace. Every linked worktree has one, its
	// main checkout open or not.
	ParentLabels map[WorkspaceID]string
	// Folders are the nested folders among Parents. Herdr draws a worktree
	// family in its Spaces column itself; these it does not know about.
	Folders map[WorkspaceID]WorkspaceID
}

// ResolveWorktrees groups workspaces by repository. The member that is not a
// linked worktree is the checkout the others were cut from; a second
// workspace on the same main checkout is a peer, not a branch of the first.
func ResolveWorktrees(workspaces []Workspace) WorktreeTree {
	tree := WorktreeTree{
		Parents: map[WorkspaceID]WorkspaceID{}, ParentLabels: map[WorkspaceID]string{},
		Folders: map[WorkspaceID]WorkspaceID{},
	}
	byRepo := map[RepositoryID][]Workspace{}
	var order []RepositoryID
	for _, ws := range workspaces {
		repo := ws.Repository
		if repo == nil || repo.ID == "" {
			continue
		}
		if repo.Linked {
			tree.ParentLabels[ws.ID] = repo.Name
		}
		if _, seen := byRepo[repo.ID]; !seen {
			order = append(order, repo.ID)
		}
		byRepo[repo.ID] = append(byRepo[repo.ID], ws)
	}
	for _, id := range order {
		members := byRepo[id]
		if len(members) < 2 {
			continue
		}
		var parent *Workspace
		for i := range members {
			if !members[i].Repository.Linked {
				parent = &members[i]
				break
			}
		}
		if parent == nil {
			continue
		}
		for _, ws := range members {
			if ws.Repository.Linked {
				tree.Parents[ws.ID] = parent.ID
			}
		}
	}
	return tree
}

// ResolveTree is the workspace tree the sidebar draws: the worktree families
// and, when nestFolders is set, folder nesting. A workspace whose folder lies
// inside another open workspace's folder hangs under the nearest such
// workspace — or that one's own parent: the tree stays one level deep, and a
// worktree of a nested checkout rides with the family too. Linked worktrees
// never adopt, and neither do home or a filesystem root, which would swallow
// every other workspace.
func ResolveTree(workspaces []Workspace, nestFolders bool, home string) WorktreeTree {
	tree := ResolveWorktrees(workspaces)
	if !nestFolders {
		return tree
	}
	nearest := map[WorkspaceID]WorkspaceID{}
	for _, ws := range workspaces {
		if _, child := tree.Parents[ws.ID]; child || linked(ws) {
			continue
		}
		if parent, ok := enclosing(ws, workspaces, home); ok {
			nearest[ws.ID] = parent
		}
	}
	top := func(ws WorkspaceID) WorkspaceID {
		seen := map[WorkspaceID]bool{ws: true}
		for {
			parent, ok := nearest[ws]
			if !ok {
				parent, ok = tree.Parents[ws]
			}
			if !ok || seen[parent] {
				return ws
			}
			seen[parent] = true
			ws = parent
		}
	}
	labels := workspaceLabels(workspaces)
	for child := range nearest {
		root := top(child)
		tree.Parents[child], tree.Folders[child], tree.ParentLabels[child] = root, root, labels[root]
	}
	for child := range tree.Parents {
		tree.Parents[child] = top(child)
	}
	return tree
}

func linked(ws Workspace) bool {
	return ws.Repository != nil && ws.Repository.Linked
}

// enclosing is the open workspace whose folder most closely contains ws's.
func enclosing(ws Workspace, workspaces []Workspace, home string) (WorkspaceID, bool) {
	dir := folderOf(ws)
	var parent WorkspaceID
	best := -1
	for _, other := range workspaces {
		candidate := folderOf(other)
		if other.ID == ws.ID || linked(other) || !adopts(candidate, home) || !within(dir, candidate) {
			continue
		}
		if len(candidate) > best {
			parent, best = other.ID, len(candidate)
		}
	}
	return parent, best >= 0
}

// folderOf is the folder a workspace stands for: its checkout when it is
// one (a pane opened deeper in the repository still means the repository),
// else the folder its first pane was opened in.
func folderOf(ws Workspace) string {
	if ws.Repository != nil && ws.Repository.Checkout != "" {
		return cleanDir(string(ws.Repository.Checkout))
	}
	return cleanDir(ws.Root)
}

func cleanDir(dir string) string {
	return strings.TrimRight(dir, `/\`)
}

// adopts is whether a folder may hold others: not unknown, not home, not a
// filesystem root ("/" cleans to "", a Windows drive to "C:").
func adopts(dir, home string) bool {
	if dir == "" || (home != "" && dir == cleanDir(home)) {
		return false
	}
	return len(dir) != 2 || dir[1] != ':'
}

// within is whether dir lies strictly inside parent.
func within(dir, parent string) bool {
	if len(dir) <= len(parent) || !strings.HasPrefix(dir, parent) {
		return false
	}
	next := dir[len(parent)]
	return next == '/' || next == '\\'
}

// Root is the root of ws's family: the workspace it hangs under, or ws
// itself. The tree is one level deep, so one step is the whole way.
func (t WorktreeTree) Root(ws WorkspaceID) WorkspaceID {
	if parent, ok := t.Parents[ws]; ok {
		return parent
	}
	return ws
}

// Family is the workspaces the here view shows with ws: its family's root
// and every member hanging under it, in the order given. A workspace with no
// family is alone in its own.
func (t WorktreeTree) Family(ws WorkspaceID, order []WorkspaceID) []WorkspaceID {
	root := t.Root(ws)
	family := []WorkspaceID{root}
	for _, id := range order {
		if t.Parents[id] == root && id != root {
			family = append(family, id)
		}
	}
	return family
}
