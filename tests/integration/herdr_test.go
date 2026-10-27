// Package integration runs Scope against a real Herdr: it starts a headless
// server of its own — its own HOME, XDG directories and socket, never the
// user's session — links the plugin, drives agent states through Herdr's
// hook API and checks what the sidebar tokens and config file say.
//
//	HERDR_SCOPE_INTEGRATION=1 go test ./tests/integration -v
//
// It needs the herdr binary (HERDR_SCOPE_HERDR, else herdr on PATH) and a Go
// toolchain to build the plugin.
package integration

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const pluginID = "kriuchkov.herdr-scope"

type env struct {
	t      *testing.T
	herdr  string
	root   string
	plugin string
	server *exec.Cmd
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// environ isolates every process from the user's Herdr: no inherited HERDR_*
// variables, a private HOME and private XDG directories.
func (e *env) environ() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + filepath.Join(e.root, "home"),
		"XDG_CONFIG_HOME=" + filepath.Join(e.root, "config"),
		"XDG_STATE_HOME=" + filepath.Join(e.root, "state"),
		"TERM=xterm-256color",
	}
}

func (e *env) run(args ...string) string {
	e.t.Helper()
	cmd := exec.Command(e.herdr, args...)
	cmd.Env = e.environ()
	out, err := cmd.CombinedOutput()
	require.NoError(e.t, err, "herdr %v: %s", args, out)
	return string(out)
}

func (e *env) json(args ...string) map[string]any {
	e.t.Helper()
	var reply map[string]any
	require.NoError(e.t, json.Unmarshal([]byte(e.run(args...)), &reply), "herdr %v", args)
	result, _ := reply["result"].(map[string]any)
	return result
}

// dump puts the plugin's command log and the daemon's log in the test
// output, so a failure says why.
func (e *env) dump() {
	cmd := exec.Command(e.herdr, "plugin", "log", "list", "--plugin", pluginID, "--limit", "5")
	cmd.Env = e.environ()
	out, _ := cmd.CombinedOutput()
	e.t.Logf("plugin log:\n%s", out)
	logs, _ := filepath.Glob(filepath.Join(e.stateDir(), "*.log"))
	for _, l := range logs {
		raw, _ := os.ReadFile(l)
		e.t.Logf("%s:\n%s", l, raw)
	}
}

func (e *env) socket() string { return filepath.Join(e.root, "config", "herdr", "herdr.sock") }

func (e *env) stateDir() string { return filepath.Join(e.root, "state", "herdr", "plugins", pluginID) }

func (e *env) configFile() string { return filepath.Join(e.root, "config", "herdr", "config.toml") }

func (e *env) startServer() {
	e.t.Helper()
	cmd := exec.Command(e.herdr, "server")
	cmd.Env = e.environ()
	var log bytes.Buffer
	cmd.Stdout, cmd.Stderr = &log, &log
	require.NoError(e.t, cmd.Start())
	e.server = cmd
	require.Eventually(e.t, func() bool {
		_, err := os.Stat(e.socket())
		return err == nil
	}, 10*time.Second, 50*time.Millisecond, "the isolated server never opened its socket: %s", log.String())
}

// stopServer ends only the server this test started, by its own process.
func (e *env) stopServer() {
	if e.server == nil || e.server.Process == nil {
		return
	}
	_ = e.server.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() { _ = e.server.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = e.server.Process.Kill()
	}
	e.server = nil
}

func setup(t *testing.T) *env {
	t.Helper()
	if os.Getenv("HERDR_SCOPE_INTEGRATION") != "1" {
		t.Skip("set HERDR_SCOPE_INTEGRATION=1 to run against a real, isolated Herdr")
	}
	herdrBin := os.Getenv("HERDR_SCOPE_HERDR")
	if herdrBin == "" {
		var err error
		herdrBin, err = exec.LookPath("herdr")
		if err != nil {
			t.Skip("herdr not installed")
		}
	}
	// Short: a Unix socket path must stay under 104 bytes.
	root, err := os.MkdirTemp("", "hsi") //nolint:usetesting // socket path length
	require.NoError(t, err)
	e := &env{t: t, herdr: herdrBin, root: root, plugin: filepath.Join(root, "plugin")}
	t.Cleanup(func() {
		if t.Failed() {
			e.dump()
		}
		e.stopServer()
		_ = os.RemoveAll(root)
	})
	for _, dir := range []string{"home", "config/herdr", "state", "plugin/bin", "plugin/dist"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o700))
	}
	repo := repoRoot(t)
	build := exec.Command("go", "build", "-o", filepath.Join(e.plugin, "bin", "herdr-scope"), "./cmd/herdr-scope")
	build.Dir = repo
	out, err := build.CombinedOutput()
	require.NoError(t, err, string(out))
	for _, f := range []string{"herdr-plugin.toml", "dist/HerdrAgentIconsMax-Regular.ttf", "dist/OFL.txt"} {
		raw, readErr := os.ReadFile(filepath.Join(repo, f))
		require.NoError(t, readErr)
		require.NoError(t, os.WriteFile(filepath.Join(e.plugin, f), raw, 0o600))
	}
	require.NoError(t, os.WriteFile(e.configFile(), []byte(userConfig), 0o600))
	e.startServer()
	return e
}

const userConfig = "# integration test config\n[ui]\nsidebar_width = 34\n\n[theme]\nname = \"catppuccin\"\n"

type agentView struct {
	Pane   string            `json:"pane_id"`
	Status string            `json:"agent_status"`
	Tokens map[string]string `json:"tokens"`
}

func (e *env) agents() map[string]agentView {
	e.t.Helper()
	var reply struct {
		Result struct {
			Agents []agentView `json:"agents"`
		} `json:"result"`
	}
	require.NoError(e.t, json.Unmarshal([]byte(e.run("agent", "list")), &reply))
	out := map[string]agentView{}
	for _, a := range reply.Result.Agents {
		out[a.Pane] = a
	}
	return out
}

func (e *env) title(pane string) string {
	for name := range e.agents()[pane].Tokens {
		if strings.HasPrefix(name, "title_") {
			return name
		}
	}
	return ""
}

func (e *env) eventually(cond func() bool, msg string, args ...any) {
	e.t.Helper()
	require.Eventually(e.t, cond, 15*time.Second, 100*time.Millisecond, append([]any{msg}, args...)...)
}

func (e *env) report(pane, agent, state string) {
	e.run("pane", "report-agent", pane, "--source", "test:hook", "--agent", agent, "--state", state)
}

func (e *env) invoke(action string) {
	e.run("plugin", "action", "invoke", pluginID+"."+action)
}

func TestScopeAgainstARealHerdr(t *testing.T) {
	e := setup(t)

	e.run("plugin", "link", e.plugin)
	ws := e.json("workspace", "create", "--cwd", filepath.Join(e.root, "home"), "--label", "project", "--focus")
	pane := ws["root_pane"].(map[string]any)["pane_id"].(string)
	tab := e.json("tab", "create", "--workspace", ws["workspace"].(map[string]any)["workspace_id"].(string), "--label", "second")
	other := tab["root_pane"].(map[string]any)["pane_id"].(string)
	e.report(pane, "claude", "working")
	e.report(other, "codex", "blocked")
	// The claude pane must not be the focused one: a turn that ends in front
	// of you needs no tick.
	e.run("tab", "focus", tab["tab"].(map[string]any)["tab_id"].(string))

	t.Log("install: state-start sets up, then the daemon paints")
	e.invoke("state-start")
	e.eventually(func() bool { return e.title(pane) == "title_working" }, "the working agent was never painted")
	// One frame writes each pane with its own request: the other may land a
	// moment later.
	e.eventually(func() bool { return e.title(other) == "title_blocked" }, "the blocked agent was never painted")
	assert.Contains(t, e.agents()[pane].Tokens, "sort_key")
	config, err := os.ReadFile(e.configFile())
	require.NoError(t, err)
	assert.Contains(t, string(config), "# >>> HERDR-SCOPE-BEGIN sidebar")
	assert.Contains(t, e.run("config", "check"), "config: ok", "the managed blocks parse")

	t.Log("a finished turn holds its tick past the idle grace")
	e.report(pane, "claude", "idle")
	e.eventually(func() bool { return e.title(pane) == "title_done" }, "no tick after the turn ended")

	t.Log("the tick and the question survive a daemon restart")
	e.invoke("state-stop")
	e.eventually(func() bool {
		_, statErr := os.Stat(filepath.Join(e.stateDir(), "control.sock"))
		return os.IsNotExist(statErr)
	}, "the daemon did not stop")
	e.invoke("state-start")
	e.eventually(func() bool { return e.title(pane) == "title_done" && e.title(other) == "title_blocked" },
		"markers lost across a restart: %s / %s", e.title(pane), e.title(other))

	t.Log("work resumes: the question clears")
	e.report(other, "codex", "working")
	e.eventually(func() bool { return e.title(other) == "title_working" }, "the question did not clear")

	t.Log("unconfigure gives the user's config back byte for byte and clears every token")
	e.invoke("unconfigure")
	e.eventually(func() bool {
		raw, readErr := os.ReadFile(e.configFile())
		return readErr == nil && string(raw) == userConfig
	}, "config.toml was not restored")
	e.eventually(func() bool {
		for _, a := range e.agents() {
			if len(a.Tokens) > 0 {
				return false
			}
		}
		return true
	}, "tokens left behind after unconfigure")

	t.Log("unconfigured, Herdr's hooks leave Scope off; the start action brings it back")
	third := e.json("tab", "create", "--workspace", ws["workspace"].(map[string]any)["workspace_id"].(string), "--label", "third")
	thirdPane := third["root_pane"].(map[string]any)["pane_id"].(string)
	e.report(thirdPane, "gemini", "working")
	e.eventually(func() bool {
		return strings.Contains(e.run("plugin", "log", "list", "--plugin", pluginID, "--limit", "3"), "Scope is unconfigured")
	}, "the watchdog never ran for the new agent")
	_, statErr := os.Stat(filepath.Join(e.stateDir(), "control.sock"))
	assert.True(t, os.IsNotExist(statErr), "the watchdog started a daemon for an unconfigured Scope")
	raw, err := os.ReadFile(e.configFile())
	require.NoError(t, err)
	assert.Equal(t, userConfig, string(raw), "nor did it put the blocks back")

	e.invoke("state-start")
	e.eventually(func() bool { return e.title(thirdPane) == "title_working" }, "the start action did not bring Scope back")
	raw, err = os.ReadFile(e.configFile())
	require.NoError(t, err)
	assert.Contains(t, string(raw), "# >>> HERDR-SCOPE-BEGIN sidebar", "with its blocks")

	t.Log("a rebuilt bin/ takes over at the next start, without a stop first")
	rebuild := exec.Command("go", "build", "-ldflags", "-X main.version=0.0.0-rebuilt",
		"-o", filepath.Join(e.plugin, "bin", "herdr-scope"), "./cmd/herdr-scope")
	rebuild.Dir = repoRoot(t)
	out, err := rebuild.CombinedOutput()
	require.NoError(t, err, string(out))
	e.invoke("state-start")
	e.eventually(func() bool {
		return strings.Contains(e.run("plugin", "log", "list", "--plugin", pluginID, "--limit", "3"), "restarted on a new build")
	}, "the start action kept the old build's daemon")
	e.eventually(func() bool { return e.title(thirdPane) == "title_working" }, "the new build did not repaint")

	t.Log("jump focuses the agent that asks")
	e.report(other, "codex", "blocked")
	e.eventually(func() bool { return e.title(other) == "title_blocked" }, "the question did not show")
	e.run("tab", "focus", third["tab"].(map[string]any)["tab_id"].(string))
	e.eventually(func() bool { return e.focusedPane() == thirdPane }, "the third tab did not take the focus")
	e.invoke("jump")
	e.eventually(func() bool { return e.focusedPane() == other }, "jump did not focus the asking pane: %s", e.focusedPane())
}

func (e *env) focusedPane() string {
	var list struct {
		Result struct {
			Panes []struct {
				PaneID  string `json:"pane_id"`
				Focused bool   `json:"focused"`
			} `json:"panes"`
		} `json:"result"`
	}
	if json.Unmarshal([]byte(e.run("pane", "list")), &list) != nil {
		return ""
	}
	for _, p := range list.Result.Panes {
		if p.Focused {
			return p.PaneID
		}
	}
	return ""
}
