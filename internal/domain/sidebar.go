package domain

import "time"

// TreePosition is where a group sits in the worktree tree.
type TreePosition int

// Tree positions.
const (
	// TreeRoot is a top-level group: a plain workspace or a main checkout.
	TreeRoot TreePosition = iota
	// TreeChild is a worktree under its checkout, with siblings below it.
	TreeChild
	// TreeLastChild is the worktree that closes its family.
	TreeLastChild
	// TreeOrphan is a worktree whose checkout runs no agent: it hangs off a
	// synthesised repository row.
	TreeOrphan
	// TreeDetached is a worktree the panel order put away from its family:
	// top level, marked as a worktree, no corner.
	TreeDetached
)

// Corner is the split-screen corner in front of a row.
type Corner int

// Corners.
const (
	CornerNone Corner = iota
	// CornerTee continues a split below this row.
	CornerTee
	// CornerLast closes a split.
	CornerLast
)

// SidebarView is the whole sidebar as Scope wants it, in display order. It
// is presentation-neutral: no glyphs, colours or token names.
type SidebarView struct {
	At   time.Time
	Mode SortMode
	// Grouped is whether rows sit under workspace headers.
	Grouped bool
	// Groups are contiguous runs of rows of one workspace. Ungrouped, there
	// is one group holding every row and no header.
	Groups []SidebarGroup
	// Spaces is one mark per workspace for the Spaces column.
	Spaces []SpaceView
	// Folded are the rows folded families hide, in display order: still
	// agents of the session, out of the panel.
	Folded []SidebarRow
	// Tabs are the tab bar's marks (mark_tabs).
	Tabs []TabMark
}

// SidebarGroup is one run of rows under one workspace header.
type SidebarGroup struct {
	Workspace WorkspaceID
	Label     string
	Tree      TreePosition
	// OrphanRepo is the repository name shown above a TreeOrphan group.
	OrphanRepo string
	// Folder is true for a nested folder rather than a worktree: its header
	// wears no worktree mark.
	Folder bool
	// Stale is true when every agent of the workspace has gone stale: the
	// header fades with its rows.
	Stale bool
	// Header is true for the run that carries the workspace header.
	Header bool
	// Gap is true when a blank row follows the run.
	Gap  bool
	Rows []SidebarRow
}

// SidebarRow is one agent row.
type SidebarRow struct {
	Agent     AgentID
	Pane      PaneID
	Tab       TabID
	Workspace WorkspaceID
	Kind      AgentKind
	Display   DisplayState
	Title     string
	TabLabel  string
	// Depth is the indent level, 0..3.
	Depth  int
	Corner Corner
	Keys   SortKeys
	// Since is when Display began; zero when unknown.
	Since time.Time
	// Fold is the line of a folded family drawn above this row; with
	// Fold.Alone the row shows nothing else.
	Fold *Fold
}

// SpaceView is the aggregated mark of one workspace.
type SpaceView struct {
	Workspace WorkspaceID
	Label     string
	// Mark is the display state the workspace's mark speaks for; empty when
	// no agent runs there.
	Mark DisplayState
	// MarkKind is the vendor of the agent the mark speaks for.
	MarkKind AgentKind
	// Kinds are the vendors running in the workspace, first seen first.
	Kinds []AgentKind
	// Active are the vendors among Kinds with an agent working or waiting on
	// the user, in the same order.
	Active []AgentKind
	// Corner hangs a nested folder off the workspace above it; CornerNone
	// at the top level, and for a nested folder Herdr's order put elsewhere.
	Corner Corner
}

// ObservedAgent is an agent with its display state for this frame.
type ObservedAgent struct {
	Agent    Agent
	Display  DisplayState
	Activity Activity
	// Since is when Display began (Observation.Since).
	Since time.Time
}

// SidebarInput is everything one projection needs.
type SidebarInput struct {
	At   time.Time
	Mode SortMode
	// HostGrouped is whether Herdr's own panel order is the grouped one; it
	// only matters when Mode is off.
	HostGrouped bool
	Policy      DisplayPolicy
	// Workspaces in Herdr's order.
	Workspaces []Workspace
	Tabs       []Tab
	// Home is the user's home directory, which never adopts nested folders.
	Home string
	// Agents in Herdr's order.
	Agents []ObservedAgent
	// Folded are the family roots folded in the Agents panel; Focused is the
	// workspace in front, which no fold hides. Opened are the folded roots
	// open for a visit: shown whole until the focus leaves them.
	Folded  map[WorkspaceID]bool
	Focused WorkspaceID
	Opened  map[WorkspaceID]bool
	// Tree is ResolveTree(Workspaces, Policy.NestFolders, Home) when the
	// caller keeps it between frames; nil resolves it here.
	Tree *WorktreeTree
}

// spacePriority is which agent a workspace's single mark speaks for: a
// question beats activity beats an unseen result beats parked, and among
// parked ones the freshest. It names every display state.
func spacePriority() []DisplayState {
	return []DisplayState{
		DisplayBlocked, DisplayWorking, DisplayDone,
		DisplayIdleFresh, DisplayIdle, DisplayIdleStale, DisplayUnknown,
	}
}

// BuildSidebar projects one frame.
func BuildSidebar(in SidebarInput) SidebarView {
	labels := workspaceLabels(in.Workspaces)
	tabLabels := map[TabID]string{}
	for _, tab := range in.Tabs {
		tabLabels[tab.ID] = TabName(tab.Label)
	}
	entries := make([]orderEntry, len(in.Agents))
	displays := make(map[PaneID]DisplayState, len(in.Agents))
	since := make(map[PaneID]time.Time, len(in.Agents))
	for i, a := range in.Agents {
		entries[i] = orderEntry{agent: a.Agent, activity: a.Activity}
		displays[a.Agent.Pane] = a.Display
		since[a.Agent.Pane] = a.Since
	}

	var tree WorktreeTree
	if in.Tree != nil {
		tree = *in.Tree
	} else {
		tree = ResolveTree(in.Workspaces, in.Policy.NestFolders, in.Home)
	}
	ranks := rank(entries, tree)
	grouped := in.Mode.Grouped(in.HostGrouped)
	ordered := displayOrder(entries, in.Mode, ranks)
	ranks = detachStrays(ordered, ranks)

	view := SidebarView{
		At: in.At, Mode: in.Mode, Grouped: grouped, Spaces: spaces(in, labels),
		Tabs: TabMarks(in.Tabs, in.Agents, in.Policy.MarkTabs),
	}
	if in.Policy.treeLines() {
		spaceCorners(view.Spaces, in.Workspaces, tree)
	}
	stale := staleWorkspaces(in.Agents)
	builder := rowBuilder{in: in, ranks: ranks, grouped: grouped, labels: labels, tabLabels: tabLabels, displays: displays, since: since}

	if !grouped {
		builder.index(ordered)
		group := SidebarGroup{}
		for _, e := range ordered {
			group.Rows = append(group.Rows, builder.row(e))
		}
		if len(group.Rows) > 0 {
			view.Groups = []SidebarGroup{group}
		}
		return view
	}
	// Furniture is pinned to the rows as the panel shows them, so it is laid
	// out after the folds took theirs.
	folds := fold(ordered, in, tree, displays)
	builder.carriers, builder.borrow = folds.carriers, folds.borrow
	builder.index(folds.shown)
	view.Groups = builder.groups(folds.shown, stale)
	for _, e := range folds.hidden {
		view.Folded = append(view.Folded, builder.row(e))
	}
	return view
}

func workspaceLabels(workspaces []Workspace) map[WorkspaceID]string {
	labels := make(map[WorkspaceID]string, len(workspaces))
	for _, ws := range workspaces {
		labels[ws.ID] = ws.Label
	}
	return labels
}

// labelOf is a workspace's label, or its id when only an agent named it.
func labelOf(labels map[WorkspaceID]string, ws WorkspaceID) string {
	if label, ok := labels[ws]; ok {
		return label
	}
	return string(ws)
}

// staleWorkspaces are the workspaces whose every agent has gone stale.
func staleWorkspaces(agents []ObservedAgent) map[WorkspaceID]bool {
	all := map[WorkspaceID]bool{}
	for _, a := range agents {
		ws := a.Agent.Workspace
		if ws == "" {
			continue
		}
		previous, seen := all[ws]
		all[ws] = (previous || !seen) && a.Display == DisplayIdleStale
	}
	return all
}

type rowBuilder struct {
	in        SidebarInput
	ranks     ranking
	grouped   bool
	labels    map[WorkspaceID]string
	tabLabels map[TabID]string
	displays  map[PaneID]DisplayState
	since     map[PaneID]time.Time
	carriers  map[PaneID]*Fold
	borrow    map[PaneID]Agent

	heads     map[PaneID]bool
	tails     map[PaneID]bool
	splitLast map[TabID]PaneID
}

// index finds the panes group furniture is pinned to, in display order: the
// first and last pane of each workspace, and the last pane of each split. A
// fold line's row is none of its workspace's.
func (b *rowBuilder) index(ordered []orderEntry) {
	b.heads, b.tails, b.splitLast = map[PaneID]bool{}, map[PaneID]bool{}, map[TabID]PaneID{}
	last := map[WorkspaceID]PaneID{}
	for _, e := range ordered {
		ws := e.agent.Workspace
		if ws == "" || b.lone(e.agent.Pane) {
			continue
		}
		if _, seen := last[ws]; !seen {
			b.heads[e.agent.Pane] = true
		}
		last[ws] = e.agent.Pane
		if b.ranks.splitChild[e.agent.Pane] {
			b.splitLast[e.agent.Tab] = e.agent.Pane
		}
	}
	for _, pane := range last {
		b.tails[pane] = true
	}
}

func (b *rowBuilder) row(e orderEntry) SidebarRow {
	a := e.agent
	policy := b.in.Policy
	header := ""
	if b.grouped && policy.TrimGroupPrefix {
		header = b.labels[a.Workspace]
	}
	text := RowTextFor(policy.RowLabel, b.tabLabels[a.Tab], TrimGroupPrefix(a.Title, header))
	keys := b.ranks.keys(a)
	if lead, ok := b.borrow[a.Pane]; ok {
		// Just above the family's first row: its keys, and a pane key that
		// sorts before it ("~" after every digit, and present before missing).
		keys = b.ranks.keys(lead)
		keys.Pane += "~"
	}
	return SidebarRow{
		Agent: a.ID, Pane: a.Pane, Tab: a.Tab, Workspace: a.Workspace, Kind: a.Kind,
		Display: b.displays[a.Pane], Title: text.Title, TabLabel: text.TabLabel,
		Depth: b.depth(a), Corner: b.corner(a), Keys: keys, Since: b.since[a.Pane],
		Fold: b.carriers[a.Pane],
	}
}

// lone reports whether a row is a fold line of its own.
func (b *rowBuilder) lone(pane PaneID) bool {
	f := b.carriers[pane]
	return f != nil && f.Alone
}

// underLine reports whether a row of a folded family's root sits under a fold
// line that is a row of its own.
func (b *rowBuilder) underLine(a Agent) bool {
	for pane, f := range b.carriers {
		if f.Alone && f.Root == a.Workspace && pane != a.Pane {
			return true
		}
	}
	return false
}

// depth: a group's first row sits at the margin (Herdr indents it under the
// header for us), every other row one level in, a worktree's rows one more.
// A root whose header a fold line of its own stands for has no header line
// to hang under: its first row goes one level in too.
func (b *rowBuilder) depth(a Agent) int {
	if !b.grouped {
		return 0
	}
	depth := 0
	if !b.heads[a.Pane] || b.underLine(a) {
		depth = 1
	}
	if b.ranks.nested(a.Workspace) {
		depth++
	}
	return depth
}

// corner hangs the other panes of a split off the first. A split adds no
// level: its corner is the offset.
func (b *rowBuilder) corner(a Agent) Corner {
	policy := b.in.Policy
	if !b.grouped || !policy.SplitCorner || !policy.treeLines() || !b.ranks.splitChild[a.Pane] {
		return CornerNone
	}
	if b.splitLast[a.Tab] == a.Pane {
		return CornerLast
	}
	return CornerTee
}

// groups splits the ordered rows into contiguous workspace runs and decides
// each run's header, tree position and gap.
func (b *rowBuilder) groups(ordered []orderEntry, stale map[WorkspaceID]bool) []SidebarGroup {
	var order []WorkspaceID
	seen := map[WorkspaceID]bool{}
	lastChild := map[WorkspaceID]WorkspaceID{}
	folded := map[WorkspaceID]bool{}
	for _, f := range b.carriers {
		folded[f.Root] = true
	}
	for _, e := range ordered {
		ws := e.agent.Workspace
		if ws == "" || seen[ws] || b.lone(e.agent.Pane) {
			continue
		}
		seen[ws] = true
		order = append(order, ws)
		if parent, ok := b.ranks.parentOf[ws]; ok {
			lastChild[parent] = ws
		}
	}
	// A family reads as one block: no gap above any member, after the parent
	// or after a sibling: a gap between siblings would leave a corner
	// pointing at a blank line.
	family := func(ws WorkspaceID) WorkspaceID {
		if parent, ok := b.ranks.parentOf[ws]; ok {
			return parent
		}
		return ws
	}
	noGap := map[WorkspaceID]bool{}
	for i := 0; i+1 < len(order); i++ {
		if _, child := b.ranks.parentOf[order[i+1]]; child && family(order[i+1]) == family(order[i]) {
			noGap[order[i]] = true
		}
	}

	var groups []SidebarGroup
	headerShown := map[WorkspaceID]bool{}
	for _, e := range ordered {
		ws := e.agent.Workspace
		if lone := b.carriers[e.agent.Pane]; lone != nil && lone.Alone {
			// A fold line is a group of its own: no header, and a gap only
			// when nothing of its family stays under it.
			g := b.newGroup(ws, false, lastChild, stale)
			g.Rows = []SidebarRow{b.row(e)}
			g.Gap = b.in.Policy.GroupGap && !lone.Kept
			groups = append(groups, g)
			continue
		}
		if n := len(groups); n == 0 || groups[n-1].Workspace != ws || b.lone(groups[n-1].Rows[0].Pane) {
			// A fold line names its root in place of the root's header.
			groups = append(groups, b.newGroup(ws, !headerShown[ws] && ws != "" && !folded[ws], lastChild, stale))
			headerShown[ws] = true
		}
		g := &groups[len(groups)-1]
		g.Rows = append(g.Rows, b.row(e))
		g.Gap = b.in.Policy.GroupGap && b.tails[e.agent.Pane] && !noGap[ws] && ws != ""
	}
	return groups
}

func (b *rowBuilder) newGroup(ws WorkspaceID, header bool, lastChild map[WorkspaceID]WorkspaceID, stale map[WorkspaceID]bool) SidebarGroup {
	g := SidebarGroup{Workspace: ws, Label: labelOf(b.labels, ws), Header: header, Stale: stale[ws], Folder: b.ranks.folder[ws]}
	switch parent, child := b.ranks.parentOf[ws]; {
	case b.ranks.detached[ws]:
		g.Tree = TreeDetached
	case child && lastChild[parent] == ws:
		g.Tree = TreeLastChild
	case child:
		g.Tree = TreeChild
	default:
		if repo, orphan := b.ranks.orphanRepo[ws]; orphan {
			g.Tree, g.OrphanRepo = TreeOrphan, repo
		}
	}
	return g
}

// spaces aggregates each workspace's agents into one mark, in Herdr's
// workspace order, followed by any workspace only an agent named.
func spaces(in SidebarInput, labels map[WorkspaceID]string) []SpaceView {
	byWorkspace := map[WorkspaceID][]ObservedAgent{}
	var ids []WorkspaceID
	known := map[WorkspaceID]bool{}
	for _, ws := range in.Workspaces {
		if !known[ws.ID] {
			known[ws.ID] = true
			ids = append(ids, ws.ID)
		}
	}
	for _, a := range in.Agents {
		ws := a.Agent.Workspace
		if ws == "" {
			continue
		}
		byWorkspace[ws] = append(byWorkspace[ws], a)
		if !known[ws] {
			known[ws] = true
			ids = append(ids, ws)
		}
	}
	out := make([]SpaceView, 0, len(ids))
	for _, ws := range ids {
		space := SpaceView{Workspace: ws, Label: labelOf(labels, ws)}
		agents := byWorkspace[ws]
	priority:
		for _, state := range spacePriority() {
			for _, a := range agents {
				if a.Display == state {
					space.Mark, space.MarkKind = state, a.Agent.Kind
					break priority
				}
			}
		}
		seen, active := map[AgentKind]bool{}, map[AgentKind]bool{}
		for _, a := range agents {
			if a.Agent.Kind != "" && !seen[a.Agent.Kind] {
				seen[a.Agent.Kind] = true
				space.Kinds = append(space.Kinds, a.Agent.Kind)
			}
			if a.Display.Active() {
				active[a.Agent.Kind] = true
			}
		}
		for _, kind := range space.Kinds {
			if active[kind] {
				space.Active = append(space.Active, kind)
			}
		}
		out = append(out, space)
	}
	return out
}

// spaceCorners hangs each nested folder off the workspace above it in the
// Spaces column, where Herdr only draws worktree families. A member is
// attached when the row above, as Herdr shows them, is its parent or an
// attached member of the same family; only attached folders get a corner, so
// a nested folder Herdr's order put elsewhere stays plain rather than hang off
// a stranger, and a stray cannot vouch for the next.
func spaceCorners(spaces []SpaceView, workspaces []Workspace, tree WorktreeTree) {
	order := hostSpaceOrder(workspaces)
	attached := map[WorkspaceID]bool{}
	for i := 1; i < len(order); i++ {
		parent, nested := tree.Parents[order[i]]
		above := order[i-1]
		attached[order[i]] = nested && (above == parent || attached[above] && tree.Parents[above] == parent)
	}
	corners := map[WorkspaceID]Corner{}
	for i, ws := range order {
		parent, isFolder := tree.Folders[ws]
		if !isFolder || !attached[ws] {
			continue
		}
		corners[ws] = CornerLast
		for _, next := range order[i+1:] {
			if !attached[next] || tree.Parents[next] != parent {
				break
			}
			if tree.Folders[next] == parent {
				corners[ws] = CornerTee
				break
			}
		}
	}
	for i := range spaces {
		spaces[i].Corner = corners[spaces[i].Workspace]
	}
}

// hostSpaceOrder is the order Herdr's Spaces column lists workspaces in: its
// own order, except that a repository with a linked worktree and its main
// checkout open is drawn as one block where its first member stands — main
// checkouts, then worktrees (client/shell/sidebar.rs workspace_entries).
func hostSpaceOrder(workspaces []Workspace) []WorkspaceID {
	hasLinked, hasMain := map[RepositoryID]bool{}, map[RepositoryID]bool{}
	for _, ws := range workspaces {
		if repo := ws.Repository; repo != nil && repo.ID != "" {
			hasLinked[repo.ID] = hasLinked[repo.ID] || repo.Linked
			hasMain[repo.ID] = hasMain[repo.ID] || !repo.Linked
		}
	}
	family := func(ws Workspace) RepositoryID {
		if repo := ws.Repository; repo != nil && hasLinked[repo.ID] && hasMain[repo.ID] {
			return repo.ID
		}
		return ""
	}
	out := make([]WorkspaceID, 0, len(workspaces))
	emitted := map[RepositoryID]bool{}
	for _, ws := range workspaces {
		id := family(ws)
		if id == "" {
			out = append(out, ws.ID)
			continue
		}
		if emitted[id] {
			continue
		}
		emitted[id] = true
		for _, wantLinked := range []bool{false, true} {
			for _, member := range workspaces {
				if family(member) == id && member.Repository.Linked == wantLinked {
					out = append(out, member.ID)
				}
			}
		}
	}
	return out
}

// Waiting are the panes that wait on the user, in the order the panel shows
// them: every question first, then every result not yet seen.
func (v SidebarView) Waiting() []PaneID {
	var asking, finished []PaneID
	for _, g := range v.Groups {
		for _, row := range g.Rows {
			switch row.Display {
			case DisplayBlocked:
				asking = append(asking, row.Pane)
			case DisplayDone:
				finished = append(finished, row.Pane)
			case DisplayWorking, DisplayIdleFresh, DisplayIdle, DisplayIdleStale, DisplayUnknown:
			}
		}
	}
	return append(asking, finished...)
}

// NextWaiting is where the jump action goes: the pane after focused in
// waiting (Waiting's order), round the list, or the first when the focus is
// on none of them. Empty when nobody waits.
func NextWaiting(waiting []PaneID, focused PaneID) PaneID {
	if len(waiting) == 0 {
		return ""
	}
	for i, pane := range waiting {
		if pane == focused {
			return waiting[(i+1)%len(waiting)]
		}
	}
	return waiting[0]
}

// WaitingCounts are how many rows ask a question, then how many hold a
// result nobody has seen yet.
func (v SidebarView) WaitingCounts() (int, int) {
	asking, finished := 0, 0
	for _, g := range v.Groups {
		for _, row := range g.Rows {
			switch row.Display {
			case DisplayBlocked:
				asking++
			case DisplayDone:
				finished++
			case DisplayWorking, DisplayIdleFresh, DisplayIdle, DisplayIdleStale, DisplayUnknown:
			}
		}
	}
	return asking, finished
}
