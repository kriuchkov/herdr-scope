package domain

import (
	"maps"
	"sort"
	"strconv"
	"time"
)

// SortKeys are the ordering keys of one row. Each is a string that sorts
// lexicographically in the intended order (descending = most active first),
// because the host sorts the panel by them as opaque values.
type SortKeys struct {
	// Pane ranks the row by its own last activity, minute-grained; empty
	// when the agent has no activity stamp (such rows sort last).
	Pane string
	// Workspace ranks the row's whole workspace: its worktree family's most
	// recent member first, the family's checkout before its worktrees, then
	// the workspace's own activity, with the id as tie-break.
	Workspace string
	// Tab ranks the panes of one split screen as a unit, the pane the others
	// were split from first.
	Tab string
}

const noStamp = "000000000000"

// minuteKey is an activity stamp at minute grain, zero-padded so that string
// order is numeric order: a working pane changes its key once a minute.
func minuteKey(activity Activity) string {
	if !activity.Known() {
		return ""
	}
	key := strconv.FormatInt(activity.At.UnixMilli()/int64(time.Minute/time.Millisecond), 10)
	if len(key) < len(noStamp) {
		key = noStamp[len(key):] + key
	}
	return key
}

// orNone is a minute key, or noStamp for a pane without activity.
func orNone(key string) string {
	if key == "" {
		return noStamp
	}
	return key
}

// orderEntry is one agent row as ordering sees it.
type orderEntry struct {
	agent    Agent
	activity Activity
}

// ranking is everything ordering knows about one snapshot.
type ranking struct {
	pane      map[PaneID]string
	workspace map[WorkspaceID]string
	tab       map[PaneID]string
	// parentOf hangs a member under a parent the panel lists; orphanOf names
	// the family of a member whose parent it does not, so that the family
	// still ranks and sits as one block, under a synthesised parent row.
	parentOf   map[WorkspaceID]WorkspaceID
	orphanOf   map[WorkspaceID]WorkspaceID
	orphanRepo map[WorkspaceID]string
	detached   map[WorkspaceID]bool
	splitChild map[PaneID]bool
}

func (r ranking) keys(agent Agent) SortKeys {
	return SortKeys{Pane: r.pane[agent.Pane], Workspace: r.workspace[agent.Workspace], Tab: r.tab[agent.Pane]}
}

// nested reports whether a workspace sits one level in: a worktree under a
// real parent group or under a synthesised parent row.
func (r ranking) nested(ws WorkspaceID) bool {
	_, child := r.parentOf[ws]
	_, orphan := r.orphanRepo[ws]
	return child || orphan
}

// family is the block a workspace ranks with: its parent, listed or
// synthesised, or itself.
func (r ranking) family(ws WorkspaceID) WorkspaceID {
	if parent, ok := r.parentOf[ws]; ok {
		return parent
	}
	if parent, ok := r.orphanOf[ws]; ok {
		return parent
	}
	return ws
}

// rank computes the ordering keys of a snapshot.
//
// Git worktrees ride with the checkout they were cut from, and nested folders
// with the folder: a family ranks by its most recent member and stays
// contiguous, parent first. When the parent runs no agent the members still
// stay together, under a synthesised parent row (orphanRepo: the folder's
// name, or the repository's).
func rank(entries []orderEntry, tree WorktreeTree) ranking {
	r := ranking{
		pane: make(map[PaneID]string, len(entries)), workspace: map[WorkspaceID]string{},
		tab: make(map[PaneID]string, len(entries)), parentOf: map[WorkspaceID]WorkspaceID{},
		orphanOf: map[WorkspaceID]WorkspaceID{}, orphanRepo: map[WorkspaceID]string{}, detached: map[WorkspaceID]bool{},
		splitChild: make(map[PaneID]bool, len(entries)),
	}
	minutes := make([]string, len(entries))
	own := map[WorkspaceID]string{}
	present := map[WorkspaceID]bool{}
	var wsOrder []WorkspaceID
	for i, e := range entries {
		minutes[i] = minuteKey(e.activity)
		r.pane[e.agent.Pane] = minutes[i]
		ws := e.agent.Workspace
		if ws == "" {
			continue
		}
		if !present[ws] {
			present[ws] = true
			wsOrder = append(wsOrder, ws)
		}
		if key := orNone(minutes[i]); key > own[ws] {
			own[ws] = key
		}
	}
	for child, parent := range tree.Parents {
		switch {
		case !present[child]:
		case present[parent]:
			r.parentOf[child] = parent
		default:
			r.orphanOf[child] = parent
		}
	}
	for _, ws := range wsOrder {
		if repo := tree.ParentLabels[ws]; repo != "" {
			if _, child := r.parentOf[ws]; !child {
				r.orphanRepo[ws] = repo
			}
		}
	}
	familyKey := map[WorkspaceID]string{}
	for _, ws := range wsOrder {
		if key := own[ws]; key > familyKey[r.family(ws)] {
			familyKey[r.family(ws)] = key
		}
	}
	for _, ws := range wsOrder {
		f := r.family(ws)
		depth := "0"
		if f == ws {
			depth = "1"
		}
		r.workspace[ws] = familyKey[f] + "-" + string(f) + "-" + depth + "-" + own[ws] + "-" + string(ws)
	}
	rankTabs(entries, minutes, &r)
	return r
}

// rankTabs ranks the panes sharing a tab — one split screen — as a unit, by
// their most recent member; inside it the pane the others were split from
// (Herdr lists panes in layout order, so the first seen) comes first.
// minutes are the entries' minute keys.
func rankTabs(entries []orderEntry, minutes []string, r *ranking) {
	panes := map[TabID][]PaneID{}
	best := map[TabID]string{}
	for i, e := range entries {
		tab := e.agent.Tab
		if tab == "" {
			continue
		}
		panes[tab] = append(panes[tab], e.agent.Pane)
		if key := orNone(minutes[i]); key > best[tab] {
			best[tab] = key
		}
	}
	for i, e := range entries {
		tab := e.agent.Tab
		if tab == "" {
			continue
		}
		head := "0"
		if panes[tab][0] == e.agent.Pane {
			head = "1"
		}
		r.tab[e.agent.Pane] = best[tab] + "-" + string(tab) + "-" + head + "-" + orNone(minutes[i])
		r.splitChild[e.agent.Pane] = len(panes[tab]) > 1 && panes[tab][0] != e.agent.Pane
	}
}

// displayOrder predicts the order the panel shows, because group furniture
// (headers, gaps, corners) is pinned to the first and last rows as displayed.
// It mirrors the host's stable sort on the same keys, missing values last.
func displayOrder(entries []orderEntry, mode SortMode, r ranking) []orderEntry {
	out := append([]orderEntry(nil), entries...)
	switch mode {
	case SortActive, SortHere:
		sort.SliceStable(out, func(i, j int) bool { return compareActive(out[i], out[j], r) < 0 })
	case SortRecent:
		sort.SliceStable(out, func(i, j int) bool {
			return compareDescMissingLast(r.pane[out[i].agent.Pane], r.pane[out[j].agent.Pane]) < 0
		})
	case SortOff:
	}
	return out
}

func compareActive(a, b orderEntry, r ranking) int {
	if c := compareDesc(r.workspace[a.agent.Workspace], r.workspace[b.agent.Workspace]); c != 0 {
		return c
	}
	if c := compareDesc(r.tab[a.agent.Pane], r.tab[b.agent.Pane]); c != 0 {
		return c
	}
	return compareDescMissingLast(r.pane[a.agent.Pane], r.pane[b.agent.Pane])
}

func compareDesc(a, b string) int {
	switch {
	case a == b:
		return 0
	case a > b:
		return -1
	default:
		return 1
	}
}

func compareDescMissingLast(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	default:
		return compareDesc(a, b)
	}
}

// detachStrays draws a worktree's corner only where the panel puts it right
// under its parent, or under a sibling that is. In the plugin's own order a
// family is one block; in Herdr's order a worktree opened later is appended
// to the end and a corner would hang it off whichever project sits above. Such
// a worktree is drawn detached: top level, its mark the only sign of what it
// is, and no repository row (the repository is already on the panel).
func detachStrays(ordered []orderEntry, r ranking) ranking {
	parentOf := maps.Clone(r.parentOf)
	detached := map[WorkspaceID]bool{}
	seen := map[WorkspaceID]bool{}
	var previous WorkspaceID
	for _, e := range ordered {
		ws := e.agent.Workspace
		if ws == "" || seen[ws] {
			continue
		}
		seen[ws] = true
		parent, isChild := parentOf[ws]
		// A detached sibling left parentOf, so it cannot vouch for the next:
		// a run of strays stays a run of strays.
		if isChild && previous != parent && parentOf[previous] != parent {
			delete(parentOf, ws)
			detached[ws] = true
		}
		previous = ws
	}
	r.parentOf = parentOf
	r.detached = detached
	return r
}
