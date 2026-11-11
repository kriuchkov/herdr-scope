package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveTitle(t *testing.T) {
	home := "/Users/me"
	cwd := "/Users/me/src/notes"
	cases := []struct {
		name    string
		kind    AgentKind
		sources TitleSources
		want    string
	}{
		{"agent title kept", "claude", TitleSources{TerminalStripped: "Add rate limits to the API"}, "Add rate limits to the API"},
		{"outer whitespace trimmed", "claude", TitleSources{TerminalStripped: "  Fix it  "}, "Fix it"},
		{"empty falls through to metadata", "claude", TitleSources{TerminalStripped: " ", Metadata: "Remote task"}, "Remote task"},
		{"raw title last", "claude", TitleSources{Terminal: "✳ Raw"}, "✳ Raw"},
		{"base name only becomes vendor", "codex", TitleSources{TerminalStripped: "notes", Cwd: cwd, Home: home}, "Codex"},
		{"tilde path becomes vendor", "agy", TitleSources{TerminalStripped: "~/src/notes", Cwd: cwd, Home: home}, "Antigravity"},
		{"shell form becomes vendor", "agy", TitleSources{TerminalStripped: "~/src/notes: agy - agy", Cwd: cwd, Home: home}, "Antigravity"},
		{
			"base-name prefix is a real title",
			"claude",
			TitleSources{TerminalStripped: "notes: rewriting the parser", Cwd: cwd, Home: home},
			"notes: rewriting the parser",
		},
		{"nothing at all becomes vendor", "codex", TitleSources{}, "Codex"},
		{
			"codex attention bracket stripped",
			"codex",
			TitleSources{TerminalStripped: "[ ! ] Action Required | scope"},
			"Action Required | scope",
		},
		{"bracket only becomes vendor", "codex", TitleSources{TerminalStripped: "[ . ]"}, "Codex"},
		{"unknown vendor keeps location", "droid", TitleSources{TerminalStripped: "notes", Cwd: cwd, Home: home}, "notes"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, ResolveTitle(c.kind, c.sources), c.name)
	}
}

func TestTrimGroupPrefix(t *testing.T) {
	cases := []struct{ title, header, want string }{
		{"billing · fix rounding", "billing", "fix rounding"},
		{"billing: fix rounding", "billing", "fix rounding"},
		{"billing - fix rounding", "billing", "fix rounding"},
		{"billing — fix rounding", "billing", "fix rounding"},
		{"billing | fix", "billing", "fix"},
		{"billing · billing", "billing", "billing"},
		{"billing · ", "billing", "billing · "},
		{"api-gateway", "api", "api-gateway"},
		{"billing-fix", "billing", "billing-fix"},
		{"anything", "", "anything"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, TrimGroupPrefix(c.title, c.header), "%q under %q", c.title, c.header)
	}
}

func TestRowTextFor(t *testing.T) {
	assert.Equal(t, RowText{Title: "Architect::QA"}, RowTextFor(RowLabelTitle, "architect", "Architect::QA"))
	assert.Equal(t, RowText{Title: "architect"}, RowTextFor(RowLabelTab, "architect", "Architect::QA"))
	assert.Equal(t, RowText{Title: "Architect::QA"}, RowTextFor(RowLabelTab, "1", "Architect::QA"), "an unnamed tab keeps the title")
	assert.Equal(t, RowText{Title: "Architect::QA"}, RowTextFor(RowLabelTab, "  ", "Architect::QA"))
	assert.Equal(t, RowText{TabLabel: "architect", Title: "Architect::QA"}, RowTextFor(RowLabelBoth, " architect ", "Architect::QA"))
	assert.Equal(t, RowText{Title: "x"}, RowTextFor("weird", "tab", "x"), "an unknown mode names a row by its title")
}

func TestKnownKinds(t *testing.T) {
	kinds := knownAgents()
	assert.Len(t, kinds, 27)
	assert.Equal(t, AgentKind("claude"), kinds[0].Kind)
	name, ok := AgentKind("agy").DisplayName()
	assert.True(t, ok)
	assert.Equal(t, "Antigravity", name)
	assert.False(t, AgentKind("droid").IsKnown())
}

func TestSortTransitions(t *testing.T) {
	assert.Equal(t, SortRecent, SortActive.Apply(SortFlip))
	assert.Equal(t, SortActive, SortRecent.Apply(SortFlip))
	assert.Equal(t, SortActive, SortOff.Apply(SortFlip), "flip never lands on off")

	assert.Equal(t, SortActive, SortOff.Apply(SortCycle))
	assert.Equal(t, SortRecent, SortActive.Apply(SortCycle))
	assert.Equal(t, SortOff, SortRecent.Apply(SortCycle))

	assert.Equal(t, SortOff, SortRecent.Apply(SortNative))
	assert.Equal(t, SortActive, SortOff.Apply(SortNative), "coming back in always lands on active")

	mode, ok := ParseSortMode("grouped")
	assert.True(t, ok)
	assert.Equal(t, SortActive, mode)
	_, ok = ParseSortMode("priority")
	assert.False(t, ok)

	assert.True(t, SortActive.Grouped(false))
	assert.False(t, SortRecent.Grouped(true))
	assert.True(t, SortOff.Grouped(true))
	assert.False(t, SortOff.Grouped(false))
}

func TestEventRefreshScopes(t *testing.T) {
	assert.Equal(t, RefreshAll, AgentEvent{Kind: EventWorkspaceRenamed}.Refresh())
	assert.Equal(t, RefreshAll, AgentEvent{Kind: EventStreamStarted}.Refresh())
	assert.Equal(t, RefreshAgents, AgentEvent{Kind: EventPaneFocused}.Refresh())
	assert.Equal(t, RefreshNone, AgentEvent{Kind: EventOther}.Refresh())
}
