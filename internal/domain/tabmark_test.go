package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTabMarksCarryTheMostUrgentWait(t *testing.T) {
	s := (&scenario{}).ws("w1", "billing", nil).ws("w2", "search", nil).
		agent("w1", "api", "p1", 1, DisplayDone).
		agent("w1", "api", "p2", 1, DisplayBlocked).
		agent("w1", "docs", "p3", 1, DisplayDone).
		agent("w2", "ui", "p4", 1, DisplayWorking)
	marks := TabMarks(s.tabs, s.agents, true)
	assert.Equal(t, []TabMark{
		{Tab: "w1:api", Label: "api", Name: "api", Waiting: DisplayBlocked},
		{Tab: "w1:docs", Label: "docs", Name: "docs", Waiting: DisplayDone},
		{Tab: "w2:ui", Label: "ui", Name: "ui"},
	}, marks, "a question outranks a result; a working agent waits on nobody")
}

func TestTabMarksTakeScopesMarkOff(t *testing.T) {
	assert.Equal(t, "api", TabName(TabLabel("api", "?")))
	assert.Equal(t, "api ?", TabName("api ?"), "a name of the user's own that ends in a mark stays whole")
	assert.Equal(t, "api", TabLabel("api", ""))

	tabs := []Tab{{ID: "w1:t1", Workspace: "w1", Label: TabLabel("api", "✓")}}
	assert.Equal(t, "api", TabMarks(tabs, nil, true)[0].Name)
}

func TestABareNumberFollowsTheTabsPlace(t *testing.T) {
	// Tab 1 closed: Herdr renumbers an unnamed tab, never one Scope renamed.
	tabs := []Tab{
		{ID: "w1:t2", Workspace: "w1", Label: TabLabel("2", "?")},
		{ID: "w1:t3", Workspace: "w1", Label: "3"},
		{ID: "w2:t1", Workspace: "w2", Label: "notes"},
		{ID: "w2:t2", Workspace: "w2", Label: "2"},
	}
	var names []string
	for _, m := range TabMarks(tabs, nil, true) {
		names = append(names, m.Name)
	}
	assert.Equal(t, []string{"1", "2", "notes", "2"}, names, "numbers count within a workspace")
}

func TestMarksOffTouchOnlyMarkedTabs(t *testing.T) {
	tabs := []Tab{
		{ID: "w1:t1", Workspace: "w1", Label: "1"},
		{ID: "w1:t2", Workspace: "w1", Label: "7"},
		{ID: "w1:t3", Workspace: "w1", Label: TabLabel("api", "?")},
	}
	s := (&scenario{}).ws("w1", "billing", nil).agent("w1", "t1", "p1", 1, DisplayBlocked)
	assert.Equal(t, []TabMark{{Tab: "w1:t3", Label: TabLabel("api", "?"), Name: "api"}}, TabMarks(tabs, s.agents, false),
		"off, the mark comes off and no other tab is renamed")
}

func TestSidebarRowsReadTheTabsNameWithoutTheMark(t *testing.T) {
	s := (&scenario{}).ws("w1", "billing", nil).agent("w1", "api", "p1", 1, DisplayBlocked)
	s.tabs[0].Label = TabLabel("api", "?")
	p := DefaultDisplayPolicy()
	p.RowLabel = RowLabelTab
	v := s.build(SortActive, p)
	assert.Equal(t, "api", v.Groups[0].Rows[0].Title)
	assert.Equal(t, []TabMark{{Tab: "w1:api", Label: TabLabel("api", "?"), Name: "api", Waiting: DisplayBlocked}}, v.Tabs)
}
