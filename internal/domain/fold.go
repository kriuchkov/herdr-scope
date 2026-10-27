package domain

import "slices"

// Fold is the line a folded family keeps in the Agents panel in place of the
// rows it hides: the root's name, how many agents it hides and, while one of
// them works, whose.
type Fold struct {
	Root  WorkspaceID
	Label string
	// Hidden counts the family's agents the panel does not show.
	Hidden int
	// Working is true while a hidden agent works; WorkingKind is its vendor.
	Working     bool
	WorkingKind AgentKind
	// Alone is true when the line is a row of its own, carried by an agent
	// the fold hides: that row shows the line and nothing else. Going to it
	// is going to the line.
	Alone bool
	// Kept is true when rows of the family stay under the line.
	Kept bool
	// Open is true while the family is open for a visit: every row shows,
	// and the line says it folds again once the focus leaves.
	Open bool
}

// FoldKey names a family in the fold store: its root's folder, which outlives
// a server restart that renumbers workspaces, or the id when the folder is
// unknown.
func FoldKey(ws Workspace) string {
	if dir := folderOf(ws); dir != "" {
		return dir
	}
	return string(ws.ID)
}

// Folds reports whether folded families leave the Agents panel in this
// order: only Scope's grouped orders install the filter that hides them.
func (m SortMode) Folds() bool { return m == SortActive || m == SortHere }

// FollowVisits is the folded roots open for a visit (SidebarInput.Opened)
// once the focus is where agents and focused say. A click on a fold line
// focuses the hidden agent that carries it; no fold gives its line to the
// agent in focus, so focus on an agent the last view showed as a line is a
// visit, however it came. A visit lasts until the focus leaves the family,
// so only the focused family can be open; the fold itself stays.
func FollowVisits(
	opened map[WorkspaceID]bool,
	last SidebarView,
	agents []Agent,
	focused WorkspaceID,
	tree WorktreeTree,
) map[WorkspaceID]bool {
	focus, line := focused, WorkspaceID("")
	if i := slices.IndexFunc(agents, func(a Agent) bool { return a.Focused }); i >= 0 {
		focus = agents[i].Workspace
		line = last.lineRoot(agents[i].Pane)
	}
	root := tree.Root(focus)
	if root == "" || !opened[root] && line != root {
		return nil
	}
	return map[WorkspaceID]bool{root: true}
}

// lineRoot is the root of the folded family whose line pane's row is, or
// empty when that row is no line of its own.
func (v SidebarView) lineRoot(pane PaneID) WorkspaceID {
	for _, g := range v.Groups {
		for _, row := range g.Rows {
			if row.Pane == pane && row.Fold != nil && row.Fold.Alone {
				return row.Fold.Root
			}
		}
	}
	return ""
}

// folding is what the folds make of the display order.
type folding struct {
	shown, hidden []orderEntry
	// carriers are the rows that carry a fold line.
	carriers map[PaneID]*Fold
	// borrow names, for a line's row, the row whose sort keys it takes so the
	// panel puts it above the rest of its family.
	borrow map[PaneID]Agent
}

// fold splits the display order into the rows the panel shows and the rows
// the folds hide. A folded family keeps every agent that waits on the user,
// the focused agent and those of the focused workspace. Its line is a row of
// its own at the head of the family, carried by the first agent it hides, so
// that going to the line means going to a hidden agent; a fold that hides
// nothing hangs its line on the family's first row.
func fold(ordered []orderEntry, in SidebarInput, tree WorktreeTree, displays map[PaneID]DisplayState) folding {
	if len(in.Folded) == 0 || !in.Mode.Folds() {
		return folding{shown: ordered}
	}
	f := foldPass{
		in: in, tree: tree, displays: displays,
		folds: map[WorkspaceID]*Fold{}, first: map[WorkspaceID]Agent{}, carrier: map[WorkspaceID]orderEntry{},
	}
	f.survey(ordered)
	return f.place(ordered)
}

// foldPass is one fold pass over a display order.
type foldPass struct {
	in       SidebarInput
	tree     WorktreeTree
	displays map[PaneID]DisplayState
	folds    map[WorkspaceID]*Fold
	// first is each folded family's first row, carrier the first it hides.
	first   map[WorkspaceID]Agent
	carrier map[WorkspaceID]orderEntry
}

func (f *foldPass) keeps(e orderEntry) bool {
	return f.in.Opened[f.tree.Root(e.agent.Workspace)] || f.displays[e.agent.Pane].Waiting() ||
		e.agent.Focused || e.agent.Workspace == f.in.Focused
}

// survey counts what each folded family hides and picks its line's row.
func (f *foldPass) survey(ordered []orderEntry) {
	labels := workspaceLabels(f.in.Workspaces)
	for _, e := range ordered {
		root := f.tree.Root(e.agent.Workspace)
		if e.agent.Workspace == "" || !f.in.Folded[root] {
			continue
		}
		fd, seen := f.folds[root]
		if !seen {
			fd = &Fold{Root: root, Label: labelOf(labels, root), Open: f.in.Opened[root]}
			f.folds[root], f.first[root] = fd, e.agent
		}
		if f.keeps(e) {
			fd.Kept = true
			continue
		}
		fd.Hidden++
		if f.displays[e.agent.Pane] == DisplayWorking && !fd.Working {
			fd.Working, fd.WorkingKind = true, e.agent.Kind
		}
		if _, has := f.carrier[root]; !has {
			f.carrier[root] = e
		}
	}
}

// place lays the rows out: each family's line at its head, what it keeps
// under it, the rest out of the panel.
func (f *foldPass) place(ordered []orderEntry) folding {
	out := folding{carriers: map[PaneID]*Fold{}, borrow: map[PaneID]Agent{}}
	placed := map[WorkspaceID]bool{}
	for _, e := range ordered {
		root := f.tree.Root(e.agent.Workspace)
		fd, folded := f.folds[root]
		if !folded {
			out.shown = append(out.shown, e)
			continue
		}
		c, alone := f.carrier[root]
		if !placed[root] {
			placed[root] = true
			if alone {
				fd.Alone = true
				out.carriers[c.agent.Pane] = fd
				out.shown = append(out.shown, c)
				if c.agent.Pane != f.first[root].Pane {
					out.borrow[c.agent.Pane] = f.first[root]
				}
			} else {
				out.carriers[e.agent.Pane] = fd
			}
		}
		switch {
		case f.keeps(e):
			out.shown = append(out.shown, e)
		case !alone || c.agent.Pane != e.agent.Pane:
			out.hidden = append(out.hidden, e)
		}
	}
	return out
}
