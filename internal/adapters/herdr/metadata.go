package herdr

import (
	"context"
	"fmt"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

// maxTokensPerReport is the server's ceiling on one metadata report; a patch
// over it is rejected whole.
const maxTokensPerReport = 16

// Request parameters several methods share.
const (
	paramPaneID      = "pane_id"
	paramTabID       = "tab_id"
	paramWorkspaceID = "workspace_id"
	paramLabel       = "label"
	paramSource      = "source"
)

// Not-found answers are final: the target is gone and its tokens (or its
// name) with it.
const (
	codePaneNotFound      = "pane_not_found"
	codeWorkspaceNotFound = "workspace_not_found"
	codeTabNotFound       = "tab_not_found"
)

// ReportPane sets (non-nil) and clears (nil) pane tokens under Scope's
// source. A pane that no longer exists counts as written.
func (c *Client) ReportPane(ctx context.Context, pane domain.PaneID, tokens map[string]*string) error {
	if len(tokens) > maxTokensPerReport {
		return fmt.Errorf("%w: %d tokens in one report, at most %d", domain.ErrProjection, len(tokens), maxTokensPerReport)
	}
	err := c.call(ctx, "pane.report_metadata", map[string]any{
		paramPaneID: string(pane), paramSource: c.source, "tokens": tokens,
	}, nil)
	if IsCode(err, codePaneNotFound) {
		return nil
	}
	return err
}

// ReportWorkspace is ReportPane for workspace tokens (the Spaces column).
func (c *Client) ReportWorkspace(ctx context.Context, ws domain.WorkspaceID, tokens map[string]*string) error {
	if len(tokens) > maxTokensPerReport {
		return fmt.Errorf("%w: %d tokens in one report, at most %d", domain.ErrProjection, len(tokens), maxTokensPerReport)
	}
	err := c.call(ctx, "workspace.report_metadata", map[string]any{
		paramWorkspaceID: string(ws), paramSource: c.source, "tokens": tokens,
	}, nil)
	if IsCode(err, codeWorkspaceNotFound) {
		return nil
	}
	return err
}

// SetAgentView installs the Agents panel sort: each token descending, in
// order, missing values last. A row carrying the hide token is left out (an
// empty one hides nothing); here keeps the agents of the workspace in front
// and of the rest of its family. Setting the same view again is a plain
// assignment on the server.
func (c *Client) SetAgentView(
	ctx context.Context, label string, tokens []string, hide string, here bool, family []domain.WorkspaceID,
) error {
	sorts := make([]wireViewSort, len(tokens))
	for i, token := range tokens {
		sorts[i].Field.Token = token
		sorts[i].Order = "desc"
	}
	params := map[string]any{paramSource: c.source, paramLabel: label, "sort": sorts}
	if filter := viewFilter(hide, here, family); filter != nil {
		params["filter"] = filter
	}
	return c.call(ctx, "agent.view.set", params, nil)
}

// viewFilter is the agent.view filter for a view; nil keeps every agent.
func viewFilter(hide string, here bool, family []domain.WorkspaceID) *wireViewFilter {
	var filters []wireViewFilter
	if here {
		filters = append(filters, hereFilter(family))
	}
	if hide != "" {
		filters = append(filters, wireViewFilter{Op: "not", Filter: &wireViewFilter{Op: "exists", Field: wireTokenField{Token: hide}}})
	}
	switch len(filters) {
	case 0:
		return nil
	case 1:
		return &filters[0]
	}
	return &wireViewFilter{Op: "all", Filters: filters}
}

// hereFilter keeps the agents of the workspace in front. Herdr follows the
// focus itself; a family's other members it cannot know, so Scope names them
// and renames them when the focus moves (Herdr compares its focus context with
// workspace_id alone, never with a token).
func hereFilter(family []domain.WorkspaceID) wireViewFilter {
	const workspace = "workspace_id"
	current := wireViewFilter{Op: "eq", Field: workspace, Value: map[string]string{"context": "current_workspace_id"}}
	if len(family) < 2 {
		return current
	}
	members := make([]string, len(family))
	for i, ws := range family {
		members[i] = string(ws)
	}
	return wireViewFilter{Op: "any", Filters: []wireViewFilter{current, {Op: "in", Field: workspace, Values: members}}}
}

// ClearAgentView hands the panel back to Herdr's own order.
func (c *Client) ClearAgentView(ctx context.Context) error {
	return c.call(ctx, "agent.view.clear", map[string]any{paramSource: c.source}, nil)
}

// PaneTokens lists the token names currently on every pane (all sources).
func (c *Client) PaneTokens(ctx context.Context) (map[domain.PaneID][]string, error) {
	var list wirePaneTokenList
	if err := c.call(ctx, "pane.list", struct{}{}, &list); err != nil {
		return nil, err
	}
	out := make(map[domain.PaneID][]string, len(list.Panes))
	for _, p := range list.Panes {
		names := make([]string, 0, len(p.Tokens))
		for name := range p.Tokens {
			names = append(names, name)
		}
		out[domain.PaneID(p.PaneID)] = names
	}
	return out, nil
}

// WorkspaceIDs lists every workspace.
func (c *Client) WorkspaceIDs(ctx context.Context) ([]domain.WorkspaceID, error) {
	var list wireWorkspaceList
	if err := c.call(ctx, "workspace.list", struct{}{}, &list); err != nil {
		return nil, err
	}
	out := make([]domain.WorkspaceID, 0, len(list.Workspaces))
	for _, ws := range list.Workspaces {
		out = append(out, domain.WorkspaceID(ws.WorkspaceID))
	}
	return out, nil
}

// MoveWorkspaces reorders the workspace list (reorder_workspaces) and returns
// the order the server reports back.
func (c *Client) MoveWorkspaces(ctx context.Context, order []domain.WorkspaceID) ([]domain.WorkspaceID, error) {
	ids := make([]string, len(order))
	for i, id := range order {
		ids[i] = string(id)
	}
	var reply wireMoveBlock
	if err := c.call(ctx, "workspace.move_block", map[string]any{"workspace_ids": ids}, &reply); err != nil {
		return nil, err
	}
	if len(reply.Workspaces) != len(order) {
		return order, nil
	}
	out := make([]domain.WorkspaceID, len(reply.Workspaces))
	for i, ws := range reply.Workspaces {
		out[i] = domain.WorkspaceID(ws.WorkspaceID)
	}
	return out, nil
}

// FocusPane focuses a pane; Herdr brings its tab and workspace forward.
func (c *Client) FocusPane(ctx context.Context, pane domain.PaneID) error {
	return c.call(ctx, "pane.focus", map[string]any{paramPaneID: string(pane)}, nil)
}

// FocusedPane is the focused pane and its workspace.
func (c *Client) FocusedPane(ctx context.Context) (domain.PaneID, domain.WorkspaceID, error) {
	pane, ws, _, err := c.FocusedPaneCwd(ctx)
	return pane, ws, err
}

// FocusedPaneCwd is the focused pane, its workspace and the directory it was
// launched in, from the pane list.
func (c *Client) FocusedPaneCwd(ctx context.Context) (domain.PaneID, domain.WorkspaceID, string, error) {
	var list wirePaneList
	if err := c.call(ctx, "pane.list", struct{}{}, &list); err != nil {
		return "", "", "", err
	}
	for _, p := range list.Panes {
		if p.Focused {
			return domain.PaneID(p.PaneID), domain.WorkspaceID(p.WorkspaceID), deref(p.Cwd), nil
		}
	}
	return "", "", "", nil
}

// ForegroundCwd is the real working directory of a pane's foreground
// process: Herdr's own pane cwd only knows where the pane was launched.
func (c *Client) ForegroundCwd(ctx context.Context, pane domain.PaneID) (string, error) {
	var info wireProcessInfo
	if err := c.call(ctx, "pane.process_info", map[string]any{paramPaneID: string(pane)}, &info); err != nil {
		return "", err
	}
	if procs := info.ProcessInfo.ForegroundProcesses; len(procs) > 0 {
		return deref(procs[0].Cwd), nil
	}
	return "", nil
}

// TabRowWidth is the width of the pane area, which is what the tab row has.
func (c *Client) TabRowWidth(ctx context.Context, pane domain.PaneID) (int, error) {
	var layout wireLayout
	if err := c.call(ctx, "pane.layout", map[string]any{paramPaneID: string(pane)}, &layout); err != nil {
		return 0, err
	}
	return layout.Layout.Area.Width, nil
}

// Tabs lists every tab, in Herdr's order.
func (c *Client) Tabs(ctx context.Context) ([]domain.Tab, error) {
	var list wireTabList
	if err := c.call(ctx, "tab.list", struct{}{}, &list); err != nil {
		return nil, err
	}
	return tabs(list.Tabs), nil
}

// RenameTab gives a tab a name; Herdr counts it as named from then on. A tab
// that no longer exists counts as renamed.
func (c *Client) RenameTab(ctx context.Context, tab domain.TabID, label string) error {
	err := c.call(ctx, "tab.rename", map[string]any{paramTabID: string(tab), paramLabel: label}, nil)
	if IsCode(err, codeTabNotFound) {
		return nil
	}
	return err
}

// TabCount is how many tabs a workspace has.
func (c *Client) TabCount(ctx context.Context, ws domain.WorkspaceID) (int, error) {
	all, err := c.Tabs(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, tab := range all {
		if tab.Workspace == ws {
			count++
		}
	}
	return count, nil
}
