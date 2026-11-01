package domain

import (
	"sort"
	"time"
)

// Memory holds every AgentSession, keyed by AgentID, and keeps the keys
// right as agents move, gain a session id, or leave.
type Memory struct {
	sessions map[AgentID]*AgentSession
	// live is the set of agents in the last tracked snapshot.
	live map[AgentID]bool
}

// NewMemory starts from persisted records.
func NewMemory(records []AgentSession) *Memory {
	m := &Memory{sessions: map[AgentID]*AgentSession{}, live: map[AgentID]bool{}}
	for i := range records {
		record := records[i]
		if record.Agent == "" {
			continue
		}
		m.sessions[record.Agent] = &record
	}
	return m
}

// Session returns the session for id, creating an empty one if needed.
func (m *Memory) Session(id AgentID) *AgentSession {
	if s, ok := m.sessions[id]; ok {
		return s
	}
	s := &AgentSession{Agent: id}
	m.sessions[id] = s
	return s
}

// Track aligns the memory with a fresh snapshot before any frame logic runs.
//
// An agent whose id is new but whose terminal is known under an id that left
// the snapshot is the same agent: it moved to another workspace (a new pane
// id, the same terminal) or its session id was reported after detection.
// Its memory follows it to the new id. Agents that left the snapshot are
// released: their markers go with their rows, their activity stays.
func (m *Memory) Track(agents []Agent) {
	present := make(map[AgentID]bool, len(agents))
	for _, agent := range agents {
		present[agent.ID] = true
	}
	byTerminal := map[TerminalID]AgentID{}
	for id, s := range m.sessions {
		if s.Terminal != "" && !present[id] {
			byTerminal[s.Terminal] = id
		}
	}
	for _, agent := range agents {
		if agent.Terminal == "" {
			continue
		}
		from, ok := byTerminal[agent.Terminal]
		if !ok || from == agent.ID {
			continue
		}
		m.Rekey(from, agent.ID)
		delete(byTerminal, agent.Terminal)
	}
	for _, agent := range agents {
		s := m.Session(agent.ID)
		s.Pane, s.Terminal, s.Workspace, s.Kind = agent.Pane, agent.Terminal, agent.Workspace, agent.Kind
	}
	for id := range m.live {
		if !present[id] {
			if s, ok := m.sessions[id]; ok {
				s.release()
			}
		}
	}
	m.live = present
}

// Stamp carries the memory through a frame that brought nothing new: agents
// are the snapshot last tracked and observed, only the clock moved, and of
// what AgentSession.Observe does only the working agents' activity follows it.
func (m *Memory) Stamp(agents []Agent, now time.Time) {
	for _, agent := range agents {
		if s, ok := m.sessions[agent.ID]; ok {
			s.stamp(agent, now)
		}
	}
}

// Rekey moves the memory of from to to. When to already has a record (one
// persisted before a Herdr restart, say), the two are merged: the newest
// activity wins, and the markers of whichever one this instance has actually
// been watching.
func (m *Memory) Rekey(from, to AgentID) {
	source, ok := m.sessions[from]
	if !ok || from == to {
		return
	}
	delete(m.sessions, from)
	delete(m.live, from)
	target, exists := m.sessions[to]
	if !exists {
		source.Agent = to
		m.sessions[to] = source
		return
	}
	if source.Activity.At.After(target.Activity.At) {
		target.Activity = source.Activity
	}
	if source.observed {
		target.Done, target.Blocked = source.Done, source.Blocked
		target.observed, target.previous = true, source.previous
	}
	target.recovered = target.recovered || source.recovered
	if source.LastSeen.After(target.LastSeen) {
		target.LastSeen = source.LastSeen
	}
}

// Records returns a copy of every session, sorted by id, for persistence.
func (m *Memory) Records() []AgentSession {
	out := make([]AgentSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Agent < out[j].Agent })
	return out
}

// Prune drops records that were not updated for maxAge and are not live.
func (m *Memory) Prune(now time.Time, maxAge time.Duration) int {
	dropped := 0
	for id, s := range m.sessions {
		if m.live[id] {
			continue
		}
		if now.Sub(s.UpdatedAt) > maxAge {
			delete(m.sessions, id)
			dropped++
		}
	}
	return dropped
}

// Len is the number of remembered sessions.
func (m *Memory) Len() int {
	return len(m.sessions)
}
