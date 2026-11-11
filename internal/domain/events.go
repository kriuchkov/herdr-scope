package domain

import "time"

// EventKind is a normalised Herdr event. The wire uses two spellings
// (pane_created for lifecycle subscriptions, pane.agent_status_changed for
// per-pane ones); both map here.
type EventKind string

// Event kinds Scope reacts to.
const (
	EventPaneCreated        EventKind = "pane_created"
	EventPaneClosed         EventKind = "pane_closed"
	EventPaneExited         EventKind = "pane_exited"
	EventPaneMoved          EventKind = "pane_moved"
	EventPaneFocused        EventKind = "pane_focused"
	EventAgentDetected      EventKind = "pane_agent_detected"
	EventAgentStatusChanged EventKind = "pane_agent_status_changed"
	EventWorkspaceCreated   EventKind = "workspace_created"
	EventWorkspaceClosed    EventKind = "workspace_closed"
	EventWorkspaceRenamed   EventKind = "workspace_renamed"
	EventWorkspaceFocused   EventKind = "workspace_focused"
	EventWorkspaceMoved     EventKind = "workspace_moved"
	EventWorkspaceReordered EventKind = "workspace_reordered"
	EventTabCreated         EventKind = "tab_created"
	EventTabClosed          EventKind = "tab_closed"
	EventTabRenamed         EventKind = "tab_renamed"
	EventTabMoved           EventKind = "tab_moved"
	// EventStreamStarted is synthesised when a subscription is acknowledged:
	// whatever happened while it was down has to be read back.
	EventStreamStarted EventKind = "stream_started"
	EventOther         EventKind = "other"
)

// AgentEvent is a Herdr event in Scope's own terms. It is a wake hint: it
// says something may have changed, never what the state now is. AgentID is
// empty when the event does not identify an agent.
type AgentEvent struct {
	AgentID   AgentID
	PaneID    PaneID
	Workspace WorkspaceID
	Kind      EventKind
	// Status is the status the event reported, when it carries one.
	Status AgentStatus
	// Timestamp is when Scope received the event; Herdr stamps none.
	Timestamp time.Time
}

// Refresh is how much has to be re-read after an event.
type Refresh int

// Refresh scopes, cheapest first.
const (
	RefreshNone Refresh = iota
	// RefreshAgents re-reads the agent list.
	RefreshAgents
	// RefreshAll re-reads the whole session: workspaces, tabs, worktrees.
	RefreshAll
)

// Refresh says what an event invalidates. Structure (workspaces, tabs, pane
// membership) needs the full snapshot; status and focus need the agent list.
func (e AgentEvent) Refresh() Refresh {
	switch e.Kind {
	case EventPaneCreated, EventPaneClosed, EventPaneExited, EventPaneMoved,
		EventWorkspaceCreated, EventWorkspaceClosed, EventWorkspaceRenamed,
		EventWorkspaceMoved, EventWorkspaceReordered,
		EventTabCreated, EventTabClosed, EventTabRenamed, EventTabMoved, EventStreamStarted:
		return RefreshAll
	case EventPaneFocused, EventAgentDetected, EventAgentStatusChanged, EventWorkspaceFocused:
		return RefreshAgents
	case EventOther:
		return RefreshNone
	}
	return RefreshNone
}
