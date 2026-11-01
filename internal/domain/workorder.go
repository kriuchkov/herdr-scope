package domain

import (
	"slices"
	"sort"
)

// WorkspaceOrder computes the workspace order that makes Herdr's indexed
// workspace shortcuts follow Scope's activity order (reorder_workspaces).
// Whole families of the tree move, parent first; workspaces with nothing
// running keep their relative order at the end. The result equals current
// when nothing would move, so applying it is idempotent.
func WorkspaceOrder(current []WorkspaceID, view SidebarView, tree WorktreeTree) []WorkspaceID {
	keys := map[WorkspaceID]string{}
	// A folded row is out of the panel, not out of the activity order.
	runs := [][]SidebarRow{view.Folded}
	for _, g := range view.Groups {
		runs = append(runs, g.Rows)
	}
	for _, row := range slices.Concat(runs...) {
		if row.Workspace != "" && row.Keys.Workspace != "" {
			keys[row.Workspace] = row.Keys.Workspace
		}
	}
	if len(keys) == 0 {
		return current
	}
	type family struct {
		ids   []WorkspaceID
		first int
		key   string
	}
	families := map[WorkspaceID]*family{}
	var inOrder []*family
	for index, ws := range current {
		r := tree.Root(ws)
		f, ok := families[r]
		if !ok {
			f = &family{first: index}
			families[r] = f
			inOrder = append(inOrder, f)
		}
		f.ids = append(f.ids, ws)
		if key, has := keys[ws]; has && key > f.key {
			f.key = key
		}
	}
	var active, inactive []*family
	for _, f := range inOrder {
		if f.key != "" {
			active = append(active, f)
		} else {
			inactive = append(inactive, f)
		}
	}
	sort.SliceStable(active, func(i, j int) bool {
		if active[i].key != active[j].key {
			return active[i].key > active[j].key
		}
		return active[i].first < active[j].first
	})
	out := make([]WorkspaceID, 0, len(current))
	for _, f := range append(active, inactive...) {
		out = append(out, f.ids...)
	}
	return out
}

// NestOrder keeps Herdr's workspace order but moves each family that took in
// a nested folder right under its parent, members in their current order
// (nest_folders without reorder_workspaces). Families of worktrees alone stay
// where they are: Herdr already draws them as a block. The result equals
// current when nothing would move.
func NestOrder(current []WorkspaceID, tree WorktreeTree) []WorkspaceID {
	present := make(map[WorkspaceID]bool, len(current))
	for _, ws := range current {
		present[ws] = true
	}
	adopting := map[WorkspaceID]bool{}
	for child, parent := range tree.Folders {
		if present[child] && present[parent] {
			adopting[parent] = true
		}
	}
	members := map[WorkspaceID][]WorkspaceID{}
	moves := func(ws WorkspaceID) (WorkspaceID, bool) {
		parent, ok := tree.Parents[ws]
		return parent, ok && adopting[parent]
	}
	for _, ws := range current {
		if parent, ok := moves(ws); ok {
			members[parent] = append(members[parent], ws)
		}
	}
	out := make([]WorkspaceID, 0, len(current))
	for _, ws := range current {
		if _, ok := moves(ws); ok {
			continue
		}
		out = append(out, ws)
		out = append(out, members[ws]...)
	}
	return out
}
