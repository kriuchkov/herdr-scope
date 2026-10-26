package herdr

// Wire DTOs for the parts of the protocol-22 schema Scope reads. Fields are
// pointers where the schema says string|null, because an empty string and a
// null mean different things there.

type wirePong struct {
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
}

type wireSession struct {
	Snapshot wireSnapshot `json:"snapshot"`
}

type wireSnapshot struct {
	Version            string          `json:"version"`
	Protocol           int             `json:"protocol"`
	FocusedWorkspaceID *string         `json:"focused_workspace_id"`
	Workspaces         []wireWorkspace `json:"workspaces"`
	Tabs               []wireTab       `json:"tabs"`
	Panes              []wirePane      `json:"panes"`
	Agents             []wireAgent     `json:"agents"`
}

type wireAgentList struct {
	Agents []wireAgent `json:"agents"`
}

type wirePaneList struct {
	Panes []wirePane `json:"panes"`
}

// wirePaneTokenList is a pane list read for the token names alone; the
// other reads of pane.list leave the tokens undecoded.
type wirePaneTokenList struct {
	Panes []struct {
		PaneID string            `json:"pane_id"`
		Tokens map[string]string `json:"tokens"`
	} `json:"panes"`
}

type wireWorkspaceList struct {
	Workspaces []wireWorkspaceRef `json:"workspaces"`
}

type wireTabList struct {
	Tabs []wireTab `json:"tabs"`
}

type wireWorkspace struct {
	WorkspaceID string        `json:"workspace_id"`
	Number      int           `json:"number"`
	Label       string        `json:"label"`
	Focused     bool          `json:"focused"`
	Worktree    *wireWorktree `json:"worktree"`
}

// wireWorkspaceRef is a workspace of which only the id is read.
type wireWorkspaceRef struct {
	WorkspaceID string `json:"workspace_id"`
}

type wireWorktree struct {
	RepoKey          string `json:"repo_key"`
	RepoName         string `json:"repo_name"`
	RepoRoot         string `json:"repo_root"`
	CheckoutPath     string `json:"checkout_path"`
	IsLinkedWorktree bool   `json:"is_linked_worktree"`
}

type wireTab struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Number      int    `json:"number"`
	Label       string `json:"label"`
	Focused     bool   `json:"focused"`
	PaneCount   int    `json:"pane_count"`
}

type wirePane struct {
	PaneID        string  `json:"pane_id"`
	TerminalID    string  `json:"terminal_id"`
	WorkspaceID   string  `json:"workspace_id"`
	TabID         string  `json:"tab_id"`
	Focused       bool    `json:"focused"`
	Cwd           *string `json:"cwd"`
	ForegroundCwd *string `json:"foreground_cwd"`
}

type wireAgent struct {
	PaneID                 string            `json:"pane_id"`
	TerminalID             string            `json:"terminal_id"`
	WorkspaceID            string            `json:"workspace_id"`
	TabID                  string            `json:"tab_id"`
	Agent                  *string           `json:"agent"`
	DisplayAgent           *string           `json:"display_agent"`
	AgentStatus            string            `json:"agent_status"`
	ScreenDetectionSkipped bool              `json:"screen_detection_skipped"`
	Focused                bool              `json:"focused"`
	TerminalTitle          *string           `json:"terminal_title"`
	TerminalTitleStripped  *string           `json:"terminal_title_stripped"`
	Title                  *string           `json:"title"`
	AgentSession           *wireAgentSession `json:"agent_session"`
	Cwd                    *string           `json:"cwd"`
	ForegroundCwd          *string           `json:"foreground_cwd"`
	Tokens                 map[string]string `json:"tokens"`
}

type wireAgentSession struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

// wireEventData is the union of event payload fields Scope looks at.
type wireEventData struct {
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	AgentStatus string `json:"agent_status"`
	Pane        *struct {
		PaneID      string `json:"pane_id"`
		WorkspaceID string `json:"workspace_id"`
	} `json:"pane"`
	Workspace *struct {
		WorkspaceID string `json:"workspace_id"`
	} `json:"workspace"`
}

type wireProcessInfo struct {
	ProcessInfo struct {
		ForegroundProcesses []struct {
			Cwd *string `json:"cwd"`
		} `json:"foreground_processes"`
	} `json:"process_info"`
}

type wireLayout struct {
	Layout struct {
		Area struct {
			Width int `json:"width"`
		} `json:"area"`
	} `json:"layout"`
}

type wireMoveBlock struct {
	Workspaces []wireWorkspaceRef `json:"workspaces"`
}

// wireViewSort is one key of an agent.view sort: a token, in an order.
type wireViewSort struct {
	Field struct {
		Token string `json:"token"`
	} `json:"field"`
	Order string `json:"order"`
}

// wireViewFilter is one node of an agent.view filter. Field is a built-in
// field's name or a wireTokenField; Value may be a focus context
// ({"context": "current_workspace_id"}), which the server resolves itself.
type wireViewFilter struct {
	Op      string           `json:"op"`
	Field   any              `json:"field,omitempty"`
	Value   any              `json:"value,omitempty"`
	Values  []string         `json:"values,omitempty"`
	Filters []wireViewFilter `json:"filters,omitempty"`
	Filter  *wireViewFilter  `json:"filter,omitempty"`
}

// wireTokenField filters on a token a plugin reported on the pane.
type wireTokenField struct {
	Token string `json:"token"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
