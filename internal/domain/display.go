package domain

// DisplayState is what a sidebar row shows. It extends AgentStatus with the
// held markers and splits idle into three tiers by time since the last turn.
type DisplayState string

// Display states, in the order they are published.
const (
	DisplayWorking   DisplayState = "working"
	DisplayDone      DisplayState = "done"
	DisplayBlocked   DisplayState = "blocked"
	DisplayIdleFresh DisplayState = "idle_fresh"
	DisplayIdle      DisplayState = "idle"
	DisplayIdleStale DisplayState = "idle_stale"
	DisplayUnknown   DisplayState = "unknown"
)

// DisplayStates lists every display state in publishing order.
func DisplayStates() []DisplayState {
	return []DisplayState{
		DisplayWorking, DisplayDone, DisplayBlocked,
		DisplayIdleFresh, DisplayIdle, DisplayIdleStale, DisplayUnknown,
	}
}

// ParseDisplayState maps a published state name back onto DisplayState.
// The boolean is false for names Scope never publishes.
func ParseDisplayState(raw string) (DisplayState, bool) {
	for _, state := range DisplayStates() {
		if string(state) == raw {
			return state, true
		}
	}
	return "", false
}

// Animated reports whether rows in this state move: working spins, blocked
// pulses. Everything else is static.
func (d DisplayState) Animated() bool {
	return d == DisplayWorking || d == DisplayBlocked
}

// Waiting reports whether a row in this state waits on the user: a question,
// or a result nobody has looked at yet.
func (d DisplayState) Waiting() bool {
	return d == DisplayBlocked || d == DisplayDone
}

// Active reports whether the agent is doing something or holding something
// for the user: working, or waiting on them.
func (d DisplayState) Active() bool {
	return d == DisplayWorking || d.Waiting()
}
