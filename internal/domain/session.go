package domain

import "time"

// AgentSession is what Scope remembers about one agent between frames. The
// exported fields are the durable record; the unexported ones describe what
// this observer instance has seen and start over with every process.
type AgentSession struct {
	Agent     AgentID
	Pane      PaneID
	Terminal  TerminalID
	Workspace WorkspaceID
	Kind      AgentKind
	// LastStatus is the effective status of the last frame. Persisted for
	// diagnostics only: a status remembered from another process is never
	// used to infer that a turn finished.
	LastStatus AgentStatus
	Activity   Activity
	Done       *CompletionMarker
	Blocked    *BlockedMarker
	// LastSeen is when the pane was last focused.
	LastSeen time.Time
	// TurnStart is when the turn in progress began; zero between turns.
	TurnStart time.Time
	UpdatedAt time.Time

	// observed is true once this instance has seen the agent in a frame; the
	// idle grace and the working→idle transition only apply after that.
	observed bool
	// previous is the effective status this instance saw last frame.
	previous AgentStatus
	// recovered is true once activity recovery was attempted for the agent.
	recovered bool
}

// Observation is the outcome of one frame for one agent.
type Observation struct {
	Display DisplayState
	// Since is when the row's state began: the turn's start, the question,
	// the result, the last activity. Zero when unknown.
	Since time.Time
	// Deadlines are future moments at which Display would change without any
	// new report: tier crossings, a timed ✓ expiring and the idle grace
	// running out.
	Deadlines []time.Time
}

// NeedsRecovery reports whether the agent has no activity stamp and nobody
// has tried to recover one from the agent's own session record yet.
func (s *AgentSession) NeedsRecovery() bool {
	return !s.Activity.Known() && !s.recovered
}

// Recovered records the outcome of an activity lookup. A zero time means the
// lookup found nothing; it is not retried by this instance.
func (s *AgentSession) Recovered(at time.Time) {
	s.recovered = true
	if !at.IsZero() && !s.Activity.Known() {
		s.Activity = Activity{At: at}
	}
}

// MarkSeen acknowledges the ✓: looking at the pane is the acknowledgement.
func (s *AgentSession) MarkSeen(now time.Time) {
	s.Done = nil
	s.LastSeen = now
}

// Observe advances the session by one frame and decides what the row shows.
// The order of the steps matters.
func (s *AgentSession) Observe(agent Agent, now time.Time, policy DisplayPolicy) Observation {
	s.place(agent, now)
	s.stamp(agent, now)

	// Recently working and now idle/done? Treat it as still working until the
	// grace window has elapsed: a real turn end survives it, a detector blip
	// does not. Blocked bypasses it — a question must show at once.
	status := agent.Status
	graced := status.settled() && s.observed && s.inGrace(now, policy)
	if graced {
		status = StatusWorking
	}
	switch {
	case status != StatusWorking:
		s.TurnStart = time.Time{}
	case s.TurnStart.IsZero():
		s.TurnStart = now
	}

	// working → idle/done is a finished turn. Only a transition this instance
	// saw counts; a status remembered from another process proves nothing.
	if s.observed && s.previous == StatusWorking && status.settled() {
		s.Done = newCompletionMarker(now, policy.DoneHold)
	}
	s.previous = status
	s.observed = true
	s.LastStatus = status

	s.holdBlocked(agent, status, now, policy)

	// Re-adopt a ✓ this instance never set: it outlived the record.
	if policy.DoneHold.UntilSeen && s.Done == nil && agent.Showing == DisplayDone && agent.Status != StatusWorking {
		s.Done = &CompletionMarker{SetAt: now, UntilSeen: true}
	}

	if agent.Focused {
		s.MarkSeen(now)
	}

	obs := s.display(status, now, policy)
	if graced {
		// Nothing has to be reported for the turn to end: the grace runs out.
		obs.Deadlines = append(obs.Deadlines, s.Activity.At.Add(policy.IdleGrace))
	}
	return obs
}

func (s *AgentSession) place(agent Agent, now time.Time) {
	s.Agent = agent.ID
	s.Pane = agent.Pane
	s.Terminal = agent.Terminal
	s.Workspace = agent.Workspace
	s.Kind = agent.Kind
	s.UpdatedAt = now
}

// stamp is what the clock alone changes in a frame: a working agent's
// activity is now. The idle grace counts from the last frame that saw it.
func (s *AgentSession) stamp(agent Agent, now time.Time) {
	if agent.Status == StatusWorking {
		s.Activity = Activity{At: now}
	}
}

func (s *AgentSession) inGrace(now time.Time, policy DisplayPolicy) bool {
	if !s.Activity.Known() {
		return false
	}
	return now.Sub(s.Activity.At) < policy.IdleGrace
}

// holdBlocked keeps a question's mark until the agent works again. Herdr
// recognises blocked from the shape of the dialog on screen, so focusing or
// redrawing the pane can lose the shape while the question still waits.
func (s *AgentSession) holdBlocked(agent Agent, status AgentStatus, now time.Time, policy DisplayPolicy) {
	if !policy.BlockedHold {
		s.Blocked = nil
		return
	}
	switch {
	case status == StatusBlocked:
		if s.Blocked == nil {
			s.Blocked = &BlockedMarker{Since: now, HadWorked: s.Activity.Known()}
		}
	case status == StatusWorking,
		s.Blocked != nil && !s.Blocked.HadWorked,
		// A lifecycle hook owns the state, so this idle is the hook saying
		// the question was answered, not a misread screen.
		agent.Authoritative && status == StatusIdle:
		s.Blocked = nil
	case s.Blocked == nil && agent.Showing == DisplayBlocked:
		// Outlived the record. Whether it had worked is unknowable, so assume
		// the holding kind: a mark that lingers beats one that vanished.
		s.Blocked = &BlockedMarker{Since: now, HadWorked: true}
	}
}

func (s *AgentSession) display(status AgentStatus, now time.Time, policy DisplayPolicy) Observation {
	switch {
	case status == StatusWorking:
		s.Done = nil
		s.Blocked = nil
		return Observation{Display: DisplayWorking, Since: s.TurnStart}
	case s.Blocked != nil:
		// Answering is what clears it, and answering makes the agent work.
		s.Done = nil
		return Observation{Display: DisplayBlocked, Since: s.Blocked.Since}
	case s.Done != nil && s.Done.Active(now):
		var deadlines []time.Time
		if !s.Done.UntilSeen {
			deadlines = append(deadlines, s.Done.Until)
		}
		return Observation{Display: DisplayDone, Since: s.Done.SetAt, Deadlines: deadlines}
	}
	s.Done = nil
	switch status {
	case StatusIdle:
		return Observation{
			Display: s.Activity.Freshness(now, policy), Since: s.Activity.At,
			Deadlines: s.Activity.Crossings(now, policy),
		}
	case StatusBlocked:
		return Observation{Display: DisplayBlocked}
	case StatusDone:
		return Observation{Display: DisplayDone}
	case StatusWorking, StatusUnknown:
		return Observation{Display: DisplayUnknown}
	}
	return Observation{Display: DisplayUnknown}
}

// release forgets what only matters while the agent is on screen: its
// markers and this instance's view of its transitions. The activity stamp
// stays, so a session that comes back keeps its history.
func (s *AgentSession) release() {
	s.Done = nil
	s.Blocked = nil
	s.observed = false
	s.previous = ""
}
