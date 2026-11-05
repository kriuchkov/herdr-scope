package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// at is a fixed test clock: 2026-10-23T12:00:00Z plus d.
func at(d time.Duration) time.Time { return time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC).Add(d) }

func idleAgent() Agent {
	return Agent{ID: "a1", Pane: "w1:p1", Tab: "w1:t1", Workspace: "w1", Terminal: "term_a", Kind: "claude", Status: StatusIdle}
}

func with(a Agent, status AgentStatus) Agent {
	a.Status = status
	return a
}

func noGrace() DisplayPolicy {
	p := DefaultDisplayPolicy()
	p.IdleGrace = 0
	return p
}

func TestParseAgentStatusTreatsTheUnrecognisedAsUnknown(t *testing.T) {
	assert.Equal(t, StatusBlocked, ParseAgentStatus("blocked"))
	assert.Equal(t, StatusUnknown, ParseAgentStatus("thinking"))
	assert.Equal(t, StatusUnknown, ParseAgentStatus(""))
}

func TestWorkingStampsActivityAndShowsWorking(t *testing.T) {
	var s AgentSession
	obs := s.Observe(with(idleAgent(), StatusWorking), at(0), noGrace())
	assert.Equal(t, DisplayWorking, obs.Display)
	assert.Equal(t, at(0), s.Activity.At)
}

func TestDoneHeldUntilSeen(t *testing.T) {
	var s AgentSession
	p := noGrace()
	s.Observe(with(idleAgent(), StatusWorking), at(0), p)

	obs := s.Observe(idleAgent(), at(time.Second), p)
	require.Equal(t, DisplayDone, obs.Display, "a finished turn shows the tick")
	assert.Empty(t, obs.Deadlines, "an until-seen tick has no expiry")

	obs = s.Observe(idleAgent(), at(time.Hour), p)
	assert.Equal(t, DisplayDone, obs.Display, "the tick outlives idle time")

	focused := idleAgent()
	focused.Focused = true
	obs = s.Observe(focused, at(time.Hour+time.Second), p)
	assert.Equal(t, DisplayIdle, obs.Display, "focusing the pane is the acknowledgement")
	assert.Nil(t, s.Done)
	assert.Equal(t, at(time.Hour+time.Second), s.LastSeen)
}

func TestDoneTimedHoldExpires(t *testing.T) {
	var s AgentSession
	p := noGrace()
	p.DoneHold = DoneHold{For: 30 * time.Second}
	s.Observe(with(idleAgent(), StatusWorking), at(0), p)

	obs := s.Observe(idleAgent(), at(time.Second), p)
	require.Equal(t, DisplayDone, obs.Display)
	assert.Equal(t, []time.Time{at(31 * time.Second)}, obs.Deadlines, "the expiry is a deadline")

	obs = s.Observe(idleAgent(), at(31*time.Second), p)
	assert.Equal(t, DisplayIdleFresh, obs.Display)
	assert.Nil(t, s.Done)
}

func TestDoneNotSetWhileFocused(t *testing.T) {
	var s AgentSession
	p := noGrace()
	working := with(idleAgent(), StatusWorking)
	working.Focused = true
	s.Observe(working, at(0), p)
	idle := idleAgent()
	idle.Focused = true
	obs := s.Observe(idle, at(time.Second), p)
	assert.Equal(t, DisplayIdleFresh, obs.Display, "a turn that ends in front of you needs no tick")
}

func TestHerdrDoneStatusShowsDoneWithoutHold(t *testing.T) {
	var s AgentSession
	obs := s.Observe(with(idleAgent(), StatusDone), at(0), noGrace())
	assert.Equal(t, DisplayDone, obs.Display)
	assert.Nil(t, s.Done, "Herdr's own done is not a held marker")
}

func TestNoDoneInferredFromAStatusRememberedByAnotherProcess(t *testing.T) {
	s := AgentSession{LastStatus: StatusWorking, Activity: Activity{At: at(-time.Hour)}}
	obs := s.Observe(idleAgent(), at(0), noGrace())
	assert.Equal(t, DisplayIdle, obs.Display, "idle after a restart is not proof the turn finished")
	assert.Nil(t, s.Done)
}

func TestGraceAbsorbsDetectorFlap(t *testing.T) {
	var s AgentSession
	p := DefaultDisplayPolicy() // 2.5 s grace
	s.Observe(with(idleAgent(), StatusWorking), at(0), p)

	obs := s.Observe(idleAgent(), at(time.Second), p)
	assert.Equal(t, DisplayWorking, obs.Display, "a blip inside the grace stays working")

	obs = s.Observe(with(idleAgent(), StatusWorking), at(1500*time.Millisecond), p)
	assert.Equal(t, DisplayWorking, obs.Display)
	assert.Nil(t, s.Done, "no tick was synthesised for the blip")

	obs = s.Observe(idleAgent(), at(4100*time.Millisecond), p)
	assert.Equal(t, DisplayDone, obs.Display, "a real turn end survives the grace")
}

func TestGraceEndIsADeadline(t *testing.T) {
	var s AgentSession
	p := DefaultDisplayPolicy() // 2.5 s grace
	s.Observe(with(idleAgent(), StatusWorking), at(0), p)

	obs := s.Observe(idleAgent(), at(time.Second), p)
	require.Equal(t, DisplayWorking, obs.Display)
	assert.Equal(t, []time.Time{at(2500 * time.Millisecond)}, obs.Deadlines, "the tick shows when the grace runs out, with no new report")

	obs = s.Observe(idleAgent(), at(2500*time.Millisecond), p)
	assert.Equal(t, DisplayDone, obs.Display)
}

func TestGraceDoesNotDelayBlocked(t *testing.T) {
	var s AgentSession
	p := DefaultDisplayPolicy()
	s.Observe(with(idleAgent(), StatusWorking), at(0), p)
	obs := s.Observe(with(idleAgent(), StatusBlocked), at(500*time.Millisecond), p)
	assert.Equal(t, DisplayBlocked, obs.Display)
}

func TestGraceNeedsAnObservedAgent(t *testing.T) {
	s := AgentSession{Activity: Activity{At: at(0)}}
	obs := s.Observe(idleAgent(), at(time.Second), DefaultDisplayPolicy())
	assert.Equal(t, DisplayIdleFresh, obs.Display, "a first frame is not inside anybody's grace")
}

func TestBlockedHeldUntilWorkResumes(t *testing.T) {
	var s AgentSession
	p := noGrace()
	s.Observe(with(idleAgent(), StatusWorking), at(0), p)
	s.Observe(with(idleAgent(), StatusBlocked), at(time.Second), p)

	obs := s.Observe(idleAgent(), at(2*time.Second), p)
	assert.Equal(t, DisplayBlocked, obs.Display, "a detector idle is the misread the hold survives")
	assert.Nil(t, s.Done, "answering clears it, so no tick meanwhile")

	obs = s.Observe(with(idleAgent(), StatusWorking), at(3*time.Second), p)
	assert.Equal(t, DisplayWorking, obs.Display)
	assert.Nil(t, s.Blocked)
}

func TestBlockedReleasedByAuthoritativeIdle(t *testing.T) {
	s := AgentSession{Activity: Activity{At: at(0)}, Blocked: &BlockedMarker{Since: at(0), HadWorked: true}}
	idle := idleAgent()
	idle.Authoritative = true
	obs := s.Observe(idle, at(time.Second), noGrace())
	assert.Equal(t, DisplayIdleFresh, obs.Display, "a hook-owned idle means the question was answered")
	assert.Nil(t, s.Blocked)
}

func TestAuthoritativeIdleDoesNotReadoptPublishedBlocked(t *testing.T) {
	var s AgentSession
	idle := idleAgent()
	idle.Authoritative = true
	idle.Showing = DisplayBlocked
	obs := s.Observe(idle, at(0), noGrace())
	assert.Equal(t, DisplayIdle, obs.Display)
	assert.Nil(t, s.Blocked)
}

func TestDetectorIdleReadoptsPublishedBlocked(t *testing.T) {
	var s AgentSession
	idle := idleAgent()
	idle.Showing = DisplayBlocked
	obs := s.Observe(idle, at(0), noGrace())
	assert.Equal(t, DisplayBlocked, obs.Display, "an outlived question was forgotten")
	require.NotNil(t, s.Blocked)
	assert.True(t, s.Blocked.HadWorked, "the holding kind is assumed")
}

func TestBlockedStartupPromptReleasedOnIdle(t *testing.T) {
	var s AgentSession
	p := noGrace()
	s.Observe(with(idleAgent(), StatusBlocked), at(0), p)
	require.NotNil(t, s.Blocked)
	assert.False(t, s.Blocked.HadWorked)

	obs := s.Observe(idleAgent(), at(time.Second), p)
	assert.Equal(t, DisplayIdle, obs.Display, "a trust prompt answered without a turn must not pin the mark")
}

func TestBlockedHoldOffShowsOnlyWhileHerdrSaysBlocked(t *testing.T) {
	var s AgentSession
	p := noGrace()
	p.BlockedHold = false
	s.Observe(with(idleAgent(), StatusWorking), at(0), p)
	obs := s.Observe(with(idleAgent(), StatusBlocked), at(time.Second), p)
	assert.Equal(t, DisplayBlocked, obs.Display)
	assert.Nil(t, s.Blocked)
	obs = s.Observe(idleAgent(), at(2*time.Second), p)
	assert.Equal(t, DisplayIdleFresh, obs.Display)
}

func TestBlockedHoldTurnedOffDropsAHeldMark(t *testing.T) {
	s := AgentSession{Blocked: &BlockedMarker{Since: at(0), HadWorked: true}}
	p := noGrace()
	p.BlockedHold = false
	obs := s.Observe(idleAgent(), at(time.Second), p)
	assert.Equal(t, DisplayIdle, obs.Display)
	assert.Nil(t, s.Blocked)
}

func TestIdleNeverBecomesBlocked(t *testing.T) {
	var s AgentSession
	p := noGrace()
	for i := range 5 {
		obs := s.Observe(idleAgent(), at(time.Duration(i)*time.Minute), p)
		assert.NotEqual(t, DisplayBlocked, obs.Display)
	}
}

func TestDoneReadoptedFromPublishedToken(t *testing.T) {
	var s AgentSession
	idle := idleAgent()
	idle.Showing = DisplayDone
	obs := s.Observe(idle, at(0), noGrace())
	assert.Equal(t, DisplayDone, obs.Display)

	working := with(idleAgent(), StatusWorking)
	working.Showing = DisplayDone
	var t2 AgentSession
	obs = t2.Observe(working, at(0), noGrace())
	assert.Equal(t, DisplayWorking, obs.Display, "a working agent does not re-adopt an old tick")
}

func TestUnknownStaysUnknown(t *testing.T) {
	var s AgentSession
	obs := s.Observe(with(idleAgent(), StatusUnknown), at(0), noGrace())
	assert.Equal(t, DisplayUnknown, obs.Display)
}

func TestIdleTiersCarryCrossingDeadlines(t *testing.T) {
	s := AgentSession{Activity: Activity{At: at(0)}}
	obs := s.Observe(idleAgent(), at(time.Minute), noGrace())
	assert.Equal(t, DisplayIdleFresh, obs.Display)
	assert.Equal(t, []time.Time{at(15*time.Minute + time.Millisecond), at(120 * time.Minute)}, obs.Deadlines)
}

func TestRecoveryIsAttemptedOnce(t *testing.T) {
	var s AgentSession
	assert.True(t, s.NeedsRecovery())
	s.Recovered(time.Time{})
	assert.False(t, s.NeedsRecovery(), "a failed lookup is not retried")
	assert.False(t, s.Activity.Known())

	var u AgentSession
	u.Recovered(at(-3 * time.Hour))
	obs := u.Observe(idleAgent(), at(0), noGrace())
	assert.Equal(t, DisplayIdleStale, obs.Display, "a recovered stamp shades an old pane honestly")
}

func TestEachStateKnowsWhenItBegan(t *testing.T) {
	var s AgentSession
	p := DefaultDisplayPolicy()
	assert.Equal(t, at(0), s.Observe(with(idleAgent(), StatusWorking), at(0), p).Since, "a turn starts when work is first seen")
	assert.Equal(t, at(0), s.Observe(with(idleAgent(), StatusWorking), at(time.Minute), p).Since, "and keeps its start")
	assert.Equal(t, at(0), s.Observe(idleAgent(), at(time.Minute+time.Second), p).Since, "a blip inside the grace is still the turn")
	done := s.Observe(idleAgent(), at(2*time.Minute), p)
	assert.Equal(t, DisplayDone, done.Display)
	assert.Equal(t, at(2*time.Minute), done.Since, "a result is as old as the turn's end")
	assert.True(t, s.TurnStart.IsZero(), "between turns there is no start")

	s.MarkSeen(at(3 * time.Minute))
	idle := s.Observe(idleAgent(), at(4*time.Minute), p)
	assert.Equal(t, at(time.Minute), idle.Since, "idle counts from the last work seen")

	asked := s.Observe(with(idleAgent(), StatusBlocked), at(5*time.Minute), p)
	assert.Equal(t, DisplayBlocked, asked.Display)
	assert.Equal(t, at(5*time.Minute), asked.Since, "a question from when it was asked")
	again := s.Observe(with(idleAgent(), StatusWorking), at(6*time.Minute), p)
	assert.Equal(t, at(6*time.Minute), again.Since, "an answer starts a new turn")
}
