package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrackFollowsAPaneMoveByTerminal(t *testing.T) {
	m := NewMemory(nil)
	before := Agent{ID: "term:term_a", Pane: "w12:p1", Terminal: "term_a", Workspace: "w12", Status: StatusIdle}
	m.Track([]Agent{before})
	s := m.Session(before.ID)
	s.Observe(with(before, StatusWorking), at(0), noGrace())
	s.Observe(before, at(time.Second), noGrace())
	require.NotNil(t, s.Done)

	// Same terminal, new pane id and — because the session id arrived — a
	// new agent id.
	after := Agent{ID: "codex:abc", Pane: "w0:p2", Terminal: "term_a", Workspace: "w0", Status: StatusIdle}
	m.Track([]Agent{after})

	moved, ok := m.sessions[after.ID]
	require.True(t, ok, "the memory did not follow the agent")
	assert.Equal(t, at(0), moved.Activity.At, "the last turn was lost")
	assert.NotNil(t, moved.Done, "the held tick was dropped")
	_, stale := m.sessions[before.ID]
	assert.False(t, stale, "the old id is still remembered")

	obs := moved.Observe(after, at(2*time.Second), noGrace())
	assert.Equal(t, DisplayDone, obs.Display)
	assert.Equal(t, PaneID("w0:p2"), moved.Pane)
}

func TestTrackReleasesAgentsThatLeft(t *testing.T) {
	m := NewMemory(nil)
	a := idleAgent()
	m.Track([]Agent{a})
	s := m.Session(a.ID)
	s.Observe(with(a, StatusWorking), at(0), noGrace())
	s.Observe(with(a, StatusBlocked), at(time.Second), noGrace())
	require.NotNil(t, s.Blocked)

	m.Track(nil)
	assert.Nil(t, s.Blocked, "a gone row takes its marker with it")
	assert.Equal(t, at(0), s.Activity.At, "the activity stamp stays for a session that comes back")
}

func TestRekeyMergesIntoAPersistedRecord(t *testing.T) {
	persisted := AgentSession{
		Agent: "claude:s1", Activity: Activity{At: at(-time.Hour)},
		Done: &CompletionMarker{SetAt: at(-time.Hour), UntilSeen: true},
	}
	m := NewMemory([]AgentSession{persisted})
	// After a Herdr restart the agent is detected before its session id.
	fresh := Agent{ID: "term:t9", Pane: "w1:p1", Terminal: "t9", Status: StatusIdle}
	m.Track([]Agent{fresh})
	m.Session(fresh.ID).Recovered(time.Time{})

	resumed := fresh
	resumed.ID = "claude:s1"
	m.Track([]Agent{resumed})

	s, ok := m.sessions["claude:s1"]
	require.True(t, ok)
	assert.Equal(t, at(-time.Hour), s.Activity.At, "the older history survives the merge")
	assert.NotNil(t, s.Done, "an unobserved fresh record does not overwrite persisted markers")
	assert.Equal(t, 1, m.Len())
}

func TestStampMovesOnlyWorkingActivity(t *testing.T) {
	idle, working := idleAgent(), with(idleAgent(), StatusWorking)
	working.ID, working.Pane, working.Terminal = "a2", "w1:p2", "term_b"
	m := NewMemory(nil)
	m.Track([]Agent{idle, working})
	m.Session(idle.ID).Observe(idle, at(0), noGrace())
	m.Session(working.ID).Observe(working, at(0), noGrace())

	m.Stamp([]Agent{idle, working}, at(time.Second))
	assert.False(t, m.Session(idle.ID).Activity.Known(), "an idle agent has nothing to stamp")
	assert.Equal(t, at(time.Second), m.Session(working.ID).Activity.At, "a working one is working now")
	assert.Equal(t, 2, m.Len(), "a stamp creates no session")
}

func TestNewMemorySkipsAnonymousRecords(t *testing.T) {
	m := NewMemory([]AgentSession{{}, {Agent: "a"}})
	assert.Equal(t, 1, m.Len())
}

func TestPruneKeepsLiveAndRecentRecords(t *testing.T) {
	m := NewMemory([]AgentSession{
		{Agent: "old", UpdatedAt: at(-40 * 24 * time.Hour)},
		{Agent: "recent", UpdatedAt: at(-time.Hour)},
		{Agent: "live", UpdatedAt: at(-40 * 24 * time.Hour)},
	})
	m.Track([]Agent{{ID: "live"}})
	assert.Equal(t, 1, m.Prune(at(0), 30*24*time.Hour))
	_, ok := m.sessions["old"]
	assert.False(t, ok)
	ids := []AgentID{}
	for _, r := range m.Records() {
		ids = append(ids, r.Agent)
	}
	assert.Equal(t, []AgentID{"live", "recent"}, ids)
}
