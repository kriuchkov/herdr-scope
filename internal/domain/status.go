package domain

// AgentStatus is Herdr's verdict on what an agent is doing.
type AgentStatus string

// The statuses Herdr reports. Unknown is a state of its own: an agent Herdr
// could not classify is not evidence of anything, least of all completion.
const (
	StatusWorking AgentStatus = "working"
	StatusBlocked AgentStatus = "blocked"
	StatusDone    AgentStatus = "done"
	StatusIdle    AgentStatus = "idle"
	StatusUnknown AgentStatus = "unknown"
)

// ParseAgentStatus maps a reported status onto AgentStatus. Anything Scope
// does not recognise is unknown, never idle or done.
func ParseAgentStatus(raw string) AgentStatus {
	switch AgentStatus(raw) {
	case StatusWorking, StatusBlocked, StatusDone, StatusIdle, StatusUnknown:
		return AgentStatus(raw)
	default:
		return StatusUnknown
	}
}

// settled reports whether the status ends a turn: idle or done. Blocked is
// not a turn end — the agent is waiting in the middle of one.
func (s AgentStatus) settled() bool {
	return s == StatusIdle || s == StatusDone
}
