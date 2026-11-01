package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

func envOf(m map[string]string) Env { return func(k string) string { return m[k] } }

func TestResolvePathsFromHerdrEnv(t *testing.T) {
	p := ResolvePaths(envOf(map[string]string{
		"HERDR_PLUGIN_ID":         "kriuchkov.herdr-scope",
		"HERDR_PLUGIN_ROOT":       "/plugins/scope",
		"HERDR_PLUGIN_CONFIG_DIR": "/cfg/scope",
		"HERDR_PLUGIN_STATE_DIR":  "/state/scope",
		"HERDR_SOCKET_PATH":       "/home/u/.config/herdr/herdr.sock",
		"HERDR_BIN_PATH":          "/bin/herdr",
	}), "darwin", "/home/u", "/x/bin")
	assert.Equal(t, "/plugins/scope", p.PluginRoot)
	assert.Equal(t, "/cfg/scope/config.toml", p.PluginConfig())
	assert.Equal(t, "/state/scope", p.StateDir)
	assert.Equal(t, "/state/scope", p.RuntimeDir, "the default session keeps the state dir")
	assert.Empty(t, p.SessionSuffix)
	assert.Equal(t, "/bin/herdr", p.Binary)
	assert.Equal(t, "/plugins/scope/dist/HerdrAgentIconsMax-Regular.ttf", p.FontSource())
}

func TestResolvePathsWithoutHerdr(t *testing.T) {
	p := ResolvePaths(envOf(nil), "linux", "/home/u", "/opt/scope/bin")
	assert.Equal(t, defaultPluginID, p.PluginID)
	assert.Equal(t, "/opt/scope", p.PluginRoot, "the binary sits in bin/ under the plugin root")
	assert.Equal(t, "/home/u/.config/herdr/config.toml", p.HerdrConfig)
	assert.Equal(t, "/home/u/.config/herdr/plugins/config/"+defaultPluginID, p.ConfigDir)
	assert.Equal(t, "/home/u/.local/state/herdr/plugins/"+defaultPluginID, p.StateDir)
	assert.Equal(t, "/home/u/.config/herdr/herdr.sock", p.Socket)
	assert.Equal(t, "herdr", p.Binary)

	x := ResolvePaths(envOf(map[string]string{"XDG_CONFIG_HOME": "/xdg/c", "XDG_STATE_HOME": "/xdg/s"}), "windows", `C:\u`, "/b")
	assert.Equal(t, "/xdg/c/herdr/config.toml", x.HerdrConfig, "XDG wins on Windows too")
	assert.Equal(t, "/xdg/s/herdr", x.HerdrStateDir)
}

func TestNamedSessionsGetTheirOwnRuntime(t *testing.T) {
	p := ResolvePaths(envOf(map[string]string{
		"HERDR_PLUGIN_STATE_DIR": "/state",
		"HERDR_SOCKET_PATH":      "/home/u/.config/herdr/sessions/work 2/herdr.sock",
	}), "darwin", "/home/u", "/b")
	assert.Equal(t, "-work_2", p.SessionSuffix, "a session name is made path-safe")
	assert.Equal(t, "/state/runtime-work_2", p.RuntimeDir)
	assert.Empty(t, sessionSuffix("/home/u/.config/herdr/sessions/../herdr.sock"), "a normalised default socket is the default session")
	assert.Equal(t, "/state/tabbar-work.txt", p.TabBarCache("work"))
	assert.Equal(t, "/state/tabbar.txt", p.TabBarCache("default"))
	assert.Equal(t, "/state/tabbar.txt", p.TabBarCache("a b"))
}

func TestClaudeActivityFromTheTranscriptTail(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", "-src-app")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	file := filepath.Join(dir, "s1.jsonl")
	content := strings.Repeat(`{"type":"user","timestamp":"2026-10-20T10:00:00.000Z","message":"old"}`+"\n", 5000) +
		`{"type":"assistant","timestamp":"2026-10-21T09:30:00.123Z"}` + "\n" +
		`{"type":"last-prompt"}` + "\n" + `{"trunc`
	require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
	a := NewActivity(envOf(nil), home, nil)
	at, err := a.LastActivity(context.Background(), "claude", "s1")
	require.NoError(t, err)
	assert.Equal(
		t,
		time.Date(2026, 10, 21, 9, 30, 0, 123e6, time.UTC),
		at,
		"the newest timestamped record wins, markers and torn lines are skipped",
	)

	at, err = a.LastActivity(context.Background(), "claude", "missing")
	require.NoError(t, err)
	assert.True(t, at.IsZero(), "no record is no answer")
	at, _ = a.LastActivity(context.Background(), "claude", "../../etc/passwd")
	assert.True(t, at.IsZero(), "a session id never walks out of the record directory")
}

func TestClaudeActivityHonoursConfigDir(t *testing.T) {
	cfg := t.TempDir()
	dir := filepath.Join(cfg, "projects", "x")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "s2.jsonl"), []byte(`{"timestamp":"2026-10-22T00:00:00Z"}`+"\n"), 0o600))
	at, err := NewActivity(
		envOf(map[string]string{"CLAUDE_CONFIG_DIR": cfg}),
		t.TempDir(),
		nil,
	).LastActivity(context.Background(), "claude", "s2")
	require.NoError(t, err)
	assert.Equal(t, 2026, at.Year())
}

func TestCodexActivity(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "10", "22")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	file := filepath.Join(dir, "rollout-2026-10-22T08-00-00-c-abc.jsonl")
	require.NoError(t, os.WriteFile(file, []byte("not json\n"), 0o600))
	mtime := time.Date(2026, 10, 22, 8, 5, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(file, mtime, mtime))
	at, err := NewActivity(envOf(nil), home, nil).LastActivity(context.Background(), "codex", "c-abc")
	require.NoError(t, err)
	assert.True(t, at.Equal(mtime), "without a timestamped line the mtime is the fallback")
}

func TestKiloActivity(t *testing.T) {
	home := t.TempDir()
	db := filepath.Join(home, ".local", "share", "kilo", "kilo.db")
	var queries []string
	sqlite := func(_ context.Context, path, query string) (string, error) {
		queries = append(queries, query)
		if path != db {
			return "", errors.New("wrong store")
		}
		return "1792750000000\n", nil
	}
	a := NewActivity(envOf(nil), home, sqlite)
	at, err := a.LastActivity(context.Background(), "kilo", "ses_1")
	require.NoError(t, err)
	assert.True(t, at.IsZero(), "a missing store is never opened")
	assert.Empty(t, queries)

	require.NoError(t, os.MkdirAll(filepath.Dir(db), 0o700))
	require.NoError(t, os.WriteFile(db, nil, 0o600))
	at, err = a.LastActivity(context.Background(), "kilo", "ses_1")
	require.NoError(t, err)
	assert.Equal(t, time.UnixMilli(1792750000000), at)
	at, _ = a.LastActivity(context.Background(), "kilo", "x' OR '1'='1")
	assert.True(t, at.IsZero(), "an id that is not an id never reaches the query")
	assert.Len(t, queries, 1)
}

func TestUnknownKindHasNoRecord(t *testing.T) {
	at, err := NewActivity(envOf(nil), t.TempDir(), nil).LastActivity(context.Background(), "gemini", "s")
	require.NoError(t, err)
	assert.True(t, at.IsZero())
}

func TestStamp(t *testing.T) {
	dir := t.TempDir()
	s := NewStamp(dir, func(p string, d []byte) error { return os.WriteFile(p, d, 0o600) })
	_, ok := s.Read()
	assert.False(t, ok)
	require.NoError(t, s.Write("herdr-scope 0.1.0 abcd1234"))
	v, ok := s.Read()
	assert.True(t, ok)
	assert.Equal(t, "herdr-scope 0.1.0 abcd1234", v)
	require.NoError(t, s.Clear())
	require.NoError(t, s.Clear(), "clearing twice is fine")
}

func TestSwitch(t *testing.T) {
	dir := t.TempDir()
	s := NewSwitch(dir, func(p string, d []byte) error { return os.WriteFile(p, d, 0o600) })
	assert.False(t, s.Off())
	require.NoError(t, s.SetOff(true))
	assert.True(t, s.Off())
	assert.FileExists(t, filepath.Join(dir, "off"))
	require.NoError(t, s.SetOff(false))
	assert.False(t, s.Off())
	require.NoError(t, s.SetOff(false), "turning on twice is fine")
}

type fakeTabHost struct {
	pane  domain.PaneID
	cwd   string
	width int
	tabs  int
	fail  error
}

func (h fakeTabHost) FocusedPaneCwd(context.Context) (domain.PaneID, domain.WorkspaceID, string, error) {
	return h.pane, "w1", "/launch", h.fail
}
func (h fakeTabHost) ForegroundCwd(context.Context, domain.PaneID) (string, error) { return h.cwd, nil }
func (h fakeTabHost) TabRowWidth(context.Context, domain.PaneID) (int, error)      { return h.width, nil }
func (h fakeTabHost) TabCount(context.Context, domain.WorkspaceID) (int, error)    { return h.tabs, nil }

// countingTabHost is a host whose focus moves and that counts its reads.
type countingTabHost struct {
	fakeTabHost

	ws                               domain.WorkspaceID
	layoutErr                        error
	focusReads, widthReads, tabReads int
}

func (h *countingTabHost) FocusedPaneCwd(context.Context) (domain.PaneID, domain.WorkspaceID, string, error) {
	h.focusReads++
	return h.pane, h.ws, "/launch", nil
}

func (h *countingTabHost) TabRowWidth(context.Context, domain.PaneID) (int, error) {
	h.widthReads++
	return h.width, h.layoutErr
}

func (h *countingTabHost) TabCount(context.Context, domain.WorkspaceID) (int, error) {
	h.tabReads++
	return h.tabs, nil
}

func fixedNow() func() time.Time {
	return func() time.Time { return time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC) }
}

func TestTabBarRereadsTheRowWhenFocusMovesOrItAges(t *testing.T) {
	file := filepath.Join(t.TempDir(), "tabbar.txt")
	write := func(p string, d []byte) error { return os.WriteFile(p, d, 0o600) }
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	host := &countingTabHost{fakeTabHost: fakeTabHost{pane: "w1:p1", cwd: "/home/u/src/app", width: 120, tabs: 2}, ws: "w1"}
	tb := NewTabBar(host, file, "/home/u", func(string) string { return "" }, write, func() time.Time { return now })
	publish := func() {
		t.Helper()
		require.NoError(t, tb.Publish(context.Background(), ""))
	}

	publish()
	publish()
	assert.Equal(t, 2, host.focusReads, "the focus and its path are read every time")
	assert.Equal(t, []int{1, 1}, []int{host.widthReads, host.tabReads}, "an unmoved focus keeps its row")

	host.pane = "w1:p2"
	publish()
	assert.Equal(t, []int{2, 2}, []int{host.widthReads, host.tabReads}, "another pane: a new or closed tab moves the focus")
	host.ws = "w2"
	publish()
	assert.Equal(t, []int{3, 3}, []int{host.widthReads, host.tabReads}, "another workspace")

	now = now.Add(tabBarRowFor - time.Millisecond)
	publish()
	assert.Equal(t, 3, host.widthReads)
	now = now.Add(time.Millisecond)
	publish()
	assert.Equal(t, []int{4, 4}, []int{host.widthReads, host.tabReads}, "a resize moves no focus, so the row ages out")

	host.pane, host.layoutErr = "w2:p3", errors.New("socket hiccup")
	publish()
	publish()
	assert.Equal(t, 6, host.widthReads, "a failed read is not kept")
}

func TestTabBarPublishesOnlyChanges(t *testing.T) {
	file := filepath.Join(t.TempDir(), "tabbar.txt")
	writes := 0
	write := func(p string, d []byte) error {
		writes++
		return os.WriteFile(p, d, 0o600)
	}
	host := fakeTabHost{pane: "w1:p1", cwd: "/home/u/src/app", width: 120, tabs: 2}
	tb := NewTabBar(host, file, "/home/u", func(string) string { return "main" }, write, fixedNow())
	require.NoError(t, tb.Publish(context.Background(), ""))
	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "~/src/app · main\n", string(raw))
	require.NoError(t, tb.Publish(context.Background(), ""))
	assert.Equal(t, 1, writes, "an unchanged line is not rewritten")

	tb.host = fakeTabHost{fail: errors.New("socket down")}
	require.Error(t, tb.Publish(context.Background(), ""))
	raw, _ = os.ReadFile(file)
	assert.Equal(t, "\n", string(raw), "a failed read hides the segment instead of showing a stale path")
}

func TestTabBarLeadsWithWhoWaits(t *testing.T) {
	file := filepath.Join(t.TempDir(), "tabbar.txt")
	write := func(p string, d []byte) error { return os.WriteFile(p, d, 0o600) }
	read := func() string {
		raw, err := os.ReadFile(file)
		require.NoError(t, err)
		return string(raw)
	}
	tb := NewTabBar(fakeTabHost{pane: "w1:p1", cwd: "/home/u/src/app", width: 120, tabs: 2}, file, "/home/u",
		func(string) string { return "main" }, write, fixedNow())
	require.NoError(t, tb.Publish(context.Background(), "?1 ✓2"))
	assert.Equal(t, "?1 ✓2 · ~/src/app · main\n", read())

	tb.host = fakeTabHost{pane: "w1:p2", cwd: "/home/u/src/app", width: 50, tabs: 4}
	require.NoError(t, tb.Publish(context.Background(), "?1 ✓2"))
	assert.Equal(t, "?1 ✓2\n", read(), "no room for the path: who waits stays")

	tb.host = fakeTabHost{fail: errors.New("socket down")}
	require.Error(t, tb.Publish(context.Background(), "?1"))
	assert.Equal(t, "?1\n", read(), "the badge does not need the focused pane")
}

func TestInvokedByHook(t *testing.T) {
	cases := []struct {
		name string
		vars map[string]string
		want bool
	}{
		{"the watchdog", map[string]string{"HERDR_PLUGIN_EVENT": "pane.agent_detected"}, true},
		{"a startup hook", map[string]string{"HERDR_PLUGIN_CONTEXT_JSON": `{"invocation_source":"startup"}`}, true},
		{"the action from the CLI", map[string]string{
			"HERDR_PLUGIN_ACTION_ID":    "state-start",
			"HERDR_PLUGIN_CONTEXT_JSON": `{"invocation_source":"cli","workspace_id":"w0"}`,
		}, false},
		{"a key binding", map[string]string{"HERDR_PLUGIN_CONTEXT_JSON": `{"invocation_source":"keybinding"}`}, false},
		{"by hand, outside Herdr", nil, false},
		{"a context Herdr garbled", map[string]string{"HERDR_PLUGIN_CONTEXT_JSON": `{`}, false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, InvokedByHook(envOf(c.vars)), c.name)
	}
}

func TestKnownAgentsAreHerdrsDetectorManifests(t *testing.T) {
	state := t.TempDir()
	assert.Nil(t, KnownAgents(state), "no detectors yet: nothing is named")
	dir := filepath.Join(state, "agent-detection", "remote")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	for _, name := range []string{"codex.toml", "claude.toml", "README.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), nil, 0o600))
	}
	assert.Equal(t, []domain.AgentKind{"claude", "codex"}, KnownAgents(state))
}

func TestComposeFitsTheBudget(t *testing.T) {
	cwd := "/home/u/work/clients/acme/services/billing-api"
	assert.Equal(t, "~/work/clients/acme/services/billing-api · fix/rounding", compose(cwd, "fix/rounding", "/home/u", 200, 1, 200))
	line := compose(cwd, "fix/rounding", "/home/u", 80, 3, 48)
	assert.LessOrEqual(t, len([]rune(line)), 80-24-15)
	assert.Contains(t, line, "fix/rounding", "the branch is kept while the path shrinks")
	assert.Equal(t, "billing-api", compose(cwd, "fix/rounding", "/home/u", 20, 9, 48), "bare paths before the bare branch")
	long := "/srv/a-very-long-service-directory-name"
	assert.Equal(t, "fix/rounding", compose(long, "fix/rounding", "", 20, 9, 48), "when no path fits, the branch is what is left")
	assert.Equal(t, "…/acme/services/billing-api", shortenPath(cwd, "/home/u", 28))
	assert.Equal(t, "/", shortenPath("/", "", 10))
}
