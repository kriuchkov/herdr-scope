package domain

import (
	"strconv"
	"strings"
)

// TabMark is what the tab bar says about a tab: its own name and, while an
// agent in it waits on the user, the most urgent wait.
type TabMark struct {
	Tab TabID
	// Label is the tab's name as Herdr has it now, a mark included.
	Label string
	// Name is the tab's own name: Scope's mark taken off, and a bare number
	// kept in step with the tab's place.
	Name string
	// Waiting is the most urgent wait among the tab's agents — a question
	// before an unseen result — and empty when none waits.
	Waiting DisplayState
}

// tabMarkSeparator joins a tab's name to Scope's mark. A no-break space draws
// as a space but nobody types one, so it tells a mark from a name whichever
// glyph the mark is.
const tabMarkSeparator = "\u00a0"

// TabName is a tab's label without Scope's mark.
func TabName(label string) string {
	if i := strings.LastIndex(label, tabMarkSeparator); i >= 0 {
		return label[:i]
	}
	return label
}

// TabLabel is the label a tab carries: its name, then the mark, if any.
func TabLabel(name, mark string) string {
	if mark == "" {
		return name
	}
	return name + tabMarkSeparator + mark
}

// TabMarks lays out the tab bar's marks. Herdr counts a renamed tab as named
// for good and stops numbering it, so a tab whose name is a bare number gets
// its place back as its name. With marks off, only the tabs still carrying a
// mark are listed, so the mark comes off and nothing else is touched.
func TabMarks(tabs []Tab, agents []ObservedAgent, on bool) []TabMark {
	waits := map[TabID]DisplayState{}
	if on {
		for _, a := range agents {
			if a.Display.Waiting() && waits[a.Agent.Tab] != DisplayBlocked {
				waits[a.Agent.Tab] = a.Display
			}
		}
	}
	place := map[WorkspaceID]int{}
	var out []TabMark
	for _, tab := range tabs {
		place[tab.Workspace]++
		if !on && !strings.Contains(tab.Label, tabMarkSeparator) {
			continue
		}
		name := TabName(tab.Label)
		if !NamedTab(name) {
			name = strconv.Itoa(place[tab.Workspace])
		}
		out = append(out, TabMark{Tab: tab.ID, Label: tab.Label, Name: name, Waiting: waits[tab.ID]})
	}
	return out
}
