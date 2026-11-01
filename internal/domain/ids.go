// Package domain holds Herdr Scope's model and rules: agent statuses, the
// display state machine, activity tiers, grouping, ordering and the sidebar
// projection. It is pure: no I/O, no clock, no Herdr types.
package domain

// AgentID identifies one agent session across pane moves and, when the agent
// reports a session id, across Herdr restarts. Opaque to the domain.
type AgentID string

// PaneID is Herdr's pane identifier ("w4:p2"). It changes when a pane moves
// to another workspace and restarts with each Herdr server.
type PaneID string

// TabID is Herdr's tab identifier ("w4:t5"); unique per session.
type TabID string

// WorkspaceID is Herdr's workspace identifier ("w4").
type WorkspaceID string

// TerminalID identifies the terminal running in a pane; it survives a pane
// move, unlike PaneID.
type TerminalID string

// RepositoryID identifies a git repository: its absolute common directory,
// the same value Herdr reports as repo_key.
type RepositoryID string

// WorktreeID identifies one checkout of a repository: its absolute path.
type WorktreeID string

// AgentKind is the vendor key Herdr detects ("claude", "codex", …). Empty
// when Herdr detected no known agent.
type AgentKind string
