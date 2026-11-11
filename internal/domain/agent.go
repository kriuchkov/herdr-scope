package domain

// Agent is one agent pane as Herdr reports it, already normalised: the
// title resolved, the declared vendor applied, the published display read
// back. It carries no Herdr wire types.
type Agent struct {
	ID        AgentID
	Pane      PaneID
	Tab       TabID
	Workspace WorkspaceID
	Terminal  TerminalID
	Kind      AgentKind
	// Session is the agent's own session id when it reports one; activity
	// recovery looks the session's record up by it.
	Session string
	Title   string
	Focused bool
	Status  AgentStatus
	// Authoritative is true when a lifecycle hook owns this pane's status
	// and Herdr skipped its screen detector: an idle here is the hook's own
	// report, not a redraw gap misread as idle.
	Authoritative bool
	// Showing is the display state Scope's published tokens currently say,
	// read back from the pane; empty when none. It lets a marker outlive a
	// lost state file.
	Showing DisplayState
}

// ServerInfo identifies the Herdr server a snapshot came from.
type ServerInfo struct {
	Version  string
	Protocol int
}

// Session is the authoritative state of one Herdr session at one moment.
// Workspaces, tabs and agents keep Herdr's order, which is layout order.
type Session struct {
	Server     ServerInfo
	Workspaces []Workspace
	Tabs       []Tab
	Agents     []Agent
	// FocusedWorkspace is the workspace in front, as the snapshot saw it.
	FocusedWorkspace WorkspaceID
	// Home is the user's home directory, which never adopts nested folders.
	Home string
}
