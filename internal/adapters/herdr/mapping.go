package herdr

import (
	"fmt"
	"strings"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

// ShowingReader reads which display state Scope's published tokens say a
// pane is in. The token names belong to the sidebar projection, so the
// composition root injects it.
type ShowingReader func(tokens map[string]string) domain.DisplayState

// Mapper turns wire payloads into domain values.
type Mapper struct {
	// Home is the user's home directory, for the "~" form of a cwd.
	Home    string
	Showing ShowingReader
}

// Agents maps an agent list in Herdr's order. Entries without a pane or a
// status are skipped; duplicate identities fall back to a less stable but
// unique source.
func (m Mapper) Agents(list []wireAgent) []domain.Agent {
	out := make([]domain.Agent, 0, len(list))
	used := map[domain.AgentID]bool{}
	for _, a := range list {
		if a.PaneID == "" || a.AgentStatus == "" {
			continue
		}
		agent := m.agent(a)
		for _, candidate := range identities(a) {
			if !used[candidate] {
				agent.ID = candidate
				break
			}
		}
		used[agent.ID] = true
		out = append(out, agent)
	}
	return out
}

func (m Mapper) agent(a wireAgent) domain.Agent {
	detected := domain.AgentKind(strings.TrimSpace(deref(a.Agent)))
	cwd := deref(a.ForegroundCwd)
	if cwd == "" {
		cwd = deref(a.Cwd)
	}
	session := ""
	if a.AgentSession != nil {
		session = a.AgentSession.Value
	}
	agent := domain.Agent{
		Pane:      domain.PaneID(a.PaneID),
		Tab:       domain.TabID(a.TabID),
		Workspace: domain.WorkspaceID(a.WorkspaceID),
		Terminal:  domain.TerminalID(a.TerminalID),
		Kind:      domain.ResolveKind(detected, deref(a.DisplayAgent)),
		Session:   session,
		// The title fallback names the detected binary: a GLM pane running
		// the stock claude binary is still "Claude Code" there.
		Title: domain.ResolveTitle(detected, domain.TitleSources{
			TerminalStripped: deref(a.TerminalTitleStripped),
			Metadata:         deref(a.Title),
			Terminal:         deref(a.TerminalTitle),
			Cwd:              cwd,
			Home:             m.Home,
		}),
		Focused:       a.Focused,
		Status:        domain.ParseAgentStatus(a.AgentStatus),
		Authoritative: a.ScreenDetectionSkipped,
	}
	if m.Showing != nil {
		agent.Showing = m.Showing(a.Tokens)
	}
	return agent
}

// identities lists an agent's possible ids, most stable first.
func identities(a wireAgent) []domain.AgentID {
	var ids []domain.AgentID
	if s := a.AgentSession; s != nil && s.Value != "" {
		agent := s.Agent
		if agent == "" {
			agent = deref(a.Agent)
		}
		ids = append(ids, domain.AgentID(fmt.Sprintf("%s:%s", agent, s.Value)))
	}
	if a.TerminalID != "" {
		ids = append(ids, domain.AgentID("term:"+a.TerminalID))
	}
	return append(ids, domain.AgentID("pane:"+a.PaneID))
}

// Session maps a whole snapshot.
func (m Mapper) Session(s wireSnapshot) domain.Session {
	roots := map[string]string{}
	for _, p := range s.Panes {
		if _, seen := roots[p.WorkspaceID]; !seen && p.WorkspaceID != "" {
			roots[p.WorkspaceID] = deref(p.Cwd)
		}
	}
	session := domain.Session{
		Server: domain.ServerInfo{Version: s.Version, Protocol: s.Protocol},
		Agents: m.Agents(s.Agents),
		Home:   m.Home,

		FocusedWorkspace: domain.WorkspaceID(deref(s.FocusedWorkspaceID)),
	}
	for _, ws := range s.Workspaces {
		if ws.WorkspaceID == "" {
			continue
		}
		session.Workspaces = append(session.Workspaces, domain.Workspace{
			ID:         domain.WorkspaceID(ws.WorkspaceID),
			Label:      ws.Label,
			Root:       roots[ws.WorkspaceID],
			Repository: repository(ws.Worktree),
		})
	}
	session.Tabs = tabs(s.Tabs)
	return session
}

// tabs maps a tab list in Herdr's order.
func tabs(list []wireTab) []domain.Tab {
	var out []domain.Tab
	for _, tab := range list {
		if tab.TabID == "" {
			continue
		}
		out = append(out, domain.Tab{
			ID: domain.TabID(tab.TabID), Workspace: domain.WorkspaceID(tab.WorkspaceID), Label: tab.Label,
		})
	}
	return out
}

func repository(w *wireWorktree) *domain.RepositoryInfo {
	if w == nil || w.RepoKey == "" {
		return nil
	}
	return &domain.RepositoryInfo{
		ID:       domain.RepositoryID(w.RepoKey),
		Name:     w.RepoName,
		Root:     w.RepoRoot,
		Checkout: domain.WorktreeID(w.CheckoutPath),
		Linked:   w.IsLinkedWorktree,
	}
}
