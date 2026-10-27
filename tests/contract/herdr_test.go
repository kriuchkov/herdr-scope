// Package contract checks Scope's Herdr adapter against payloads recorded
// from a real Herdr 0.9.5 (protocol 22) server.
package contract

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/adapters/herdr"
	"github.com/kriuchkov/herdr-scope/internal/domain"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "fixtures", "herdr", name))
	require.NoError(t, err)
	return raw
}

func mapper() herdr.Mapper {
	return herdr.Mapper{Home: "/home/user", Showing: func(tokens map[string]string) domain.DisplayState {
		for _, state := range domain.DisplayStates() {
			if _, ok := tokens["state_"+string(state)]; ok {
				return state
			}
		}
		return ""
	}}
}

func TestRecordedSnapshot(t *testing.T) {
	session, err := herdr.DecodeSnapshot(mapper(), fixture(t, "session.snapshot.json"))
	require.NoError(t, err)
	assert.Equal(t, domain.ServerInfo{Version: "0.9.5", Protocol: 22}, session.Server)

	require.Len(t, session.Workspaces, 3)
	app, feature, billing := session.Workspaces[0], session.Workspaces[1], session.Workspaces[2]
	require.NotNil(t, app.Repository)
	require.NotNil(t, feature.Repository)
	assert.Nil(t, billing.Repository, "a plain directory has no worktree info")
	assert.Equal(t, domain.RepositoryID("/home/user/src/app/.git"), app.Repository.ID)
	assert.Equal(t, app.Repository.ID, feature.Repository.ID, "both checkouts share the repository key")
	assert.False(t, app.Repository.Linked)
	assert.True(t, feature.Repository.Linked)
	assert.Equal(t, "app", feature.Repository.Name)
	assert.Equal(t, "/home/user/src/app", app.Root, "the root is the first pane's launch directory")

	tree := domain.ResolveWorktrees(session.Workspaces)
	assert.Equal(t, map[domain.WorkspaceID]domain.WorkspaceID{feature.ID: app.ID}, tree.Parents)

	require.Len(t, session.Agents, 5)
	statuses := map[domain.PaneID]domain.AgentStatus{}
	for _, a := range session.Agents {
		statuses[a.Pane] = a.Status
		assert.NotEmpty(t, a.Terminal)
		assert.NotEmpty(t, a.Tab)
	}
	assert.Equal(t, domain.StatusWorking, statuses["w2:p1"])
	assert.Equal(t, domain.StatusBlocked, statuses["w3:p1"])
	assert.Equal(t, domain.AgentKind("droid"), session.Agents[4].Kind, "an unknown vendor keeps its kind")

	tabs := map[domain.TabID]string{}
	for _, tab := range session.Tabs {
		tabs[tab.ID] = tab.Label
	}
	assert.Equal(t, "review", tabs["w3:t2"])
}

func TestRecordedAgentListMatchesSnapshotAgents(t *testing.T) {
	agents, err := herdr.DecodeAgents(mapper(), fixture(t, "agent.list.json"))
	require.NoError(t, err)
	session, err := herdr.DecodeSnapshot(mapper(), fixture(t, "session.snapshot.json"))
	require.NoError(t, err)
	require.Len(t, agents, len(session.Agents))
	for i := range agents {
		assert.Equal(t, session.Agents[i].ID, agents[i].ID, "agent.list and session.snapshot name agents alike")
	}
	assert.Equal(t, "billing · fix rounding", agents[3].Title, "the metadata title fills in for a pane with no terminal title")
	assert.Equal(t, "Codex", agents[1].Title, "a pane with no title shows its vendor's name")
}

func TestLivePaneShapes(t *testing.T) {
	agents, err := herdr.DecodeAgents(mapper(), fixture(t, "agent.list.live.json"))
	require.NoError(t, err)
	require.Len(t, agents, 5, "an entry without a pane id is skipped")

	claude := agents[0]
	assert.Equal(t, domain.AgentID("claude:5f0c2a7e-91d4-4b3e-8a6f-0c1d2e3f4a5b"), claude.ID, "the session id is the identity")
	assert.Equal(t, "5f0c2a7e-91d4-4b3e-8a6f-0c1d2e3f4a5b", claude.Session)
	assert.Equal(t, "Add rate limits to the API", claude.Title)
	assert.Equal(t, domain.DisplayDone, claude.Showing, "the published tick is read back")

	codex := agents[1]
	assert.Equal(t, domain.AgentID("term:term_5c0ffee04d5e6f7"), codex.ID)
	assert.Equal(t, "Codex", codex.Title, "a title that only names the directory becomes the vendor")
	assert.True(t, codex.Focused)

	mirrored := agents[2]
	assert.Equal(t, "Remote: rename ledger columns", mirrored.Title)
	assert.True(t, mirrored.Authoritative)
	assert.Equal(t, domain.DisplayBlocked, mirrored.Showing)

	glm := agents[3]
	assert.Equal(t, domain.AgentKind("glm"), glm.Kind, "a declared vendor Scope can draw outranks detection")
	assert.Equal(t, "Action Required | billing", glm.Title, "the vendor's attention bracket is stripped")

	droid := agents[4]
	assert.Equal(t, domain.AgentID("pane:w8:p3"), droid.ID, "no session, no terminal: the pane id")
	assert.Equal(t, domain.StatusUnknown, droid.Status)
}

func TestRecordedEventsNormalise(t *testing.T) {
	f, err := os.Open(filepath.Join(filepath.Dir(callerFile(t)), "..", "fixtures", "herdr", "events.jsonl"))
	require.NoError(t, err)
	defer f.Close()
	seen := map[domain.EventKind]domain.AgentEvent{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		event, decodeErr := herdr.DecodeEvent(scanner.Bytes())
		require.NoError(t, decodeErr)
		assert.NotEqual(t, domain.EventOther, event.Kind, "unrecognised event: %s", scanner.Text())
		seen[event.Kind] = event
	}
	require.NoError(t, scanner.Err())

	status := seen[domain.EventAgentStatusChanged]
	assert.Equal(t, domain.PaneID("w1:p1"), status.PaneID, "the dotted per-pane spelling maps to the same kind")
	assert.Equal(t, domain.StatusBlocked, status.Status)
	assert.Equal(t, domain.PaneID("w2:p2"), seen[domain.EventPaneClosed].PaneID)
	assert.NotEmpty(t, seen[domain.EventPaneCreated].PaneID, "the pane id is read from the nested pane object")
	assert.Equal(t, domain.WorkspaceID("w3"), seen[domain.EventWorkspaceRenamed].Workspace)
	for _, kind := range []domain.EventKind{domain.EventWorkspaceCreated, domain.EventTabCreated, domain.EventTabRenamed, domain.EventPaneFocused} {
		assert.Contains(t, seen, kind)
	}
}

func TestSubscriptionTypeSpelling(t *testing.T) {
	assert.Equal(t, "pane.agent_detected", herdr.SubscriptionType(domain.EventAgentDetected))
	assert.Equal(t, "workspace.created", herdr.SubscriptionType(domain.EventWorkspaceCreated))
	assert.Equal(t, "tab.renamed", herdr.SubscriptionType(domain.EventTabRenamed))
}

func callerFile(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return file
}
