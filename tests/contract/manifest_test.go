package contract

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/bootstrap"
)

type command struct {
	ID        string   `toml:"id"`
	On        string   `toml:"on"`
	Command   []string `toml:"command"`
	Platforms []string `toml:"platforms"`
	Placement string   `toml:"placement"`
}

type manifest struct {
	ID              string    `toml:"id"`
	Name            string    `toml:"name"`
	Version         string    `toml:"version"`
	MinHerdrVersion string    `toml:"min_herdr_version"`
	Platforms       []string  `toml:"platforms"`
	Build           []command `toml:"build"`
	Startup         []command `toml:"startup"`
	Events          []command `toml:"events"`
	Actions         []command `toml:"actions"`
	Panes           []command `toml:"panes"`
}

func loadManifest(t *testing.T) manifest {
	t.Helper()
	var m manifest
	_, err := toml.DecodeFile(filepath.Join(filepath.Dir(callerFile(t)), "..", "..", "herdr-plugin.toml"), &m)
	require.NoError(t, err)
	return m
}

func TestManifestIdentity(t *testing.T) {
	m := loadManifest(t)
	assert.Equal(t, "kriuchkov.herdr-scope", m.ID)
	assert.Equal(t, bootstrap.Version, m.Version, "the manifest and the binary declare one version")
	assert.Equal(t, "0.9.0", m.MinHerdrVersion, "protocol 22 shipped with Herdr 0.9.0")
	assert.ElementsMatch(t, []string{"linux", "macos", "windows"}, m.Platforms)
}

func TestManifestActionsAreHerdrConventionsAndSpecComplete(t *testing.T) {
	m := loadManifest(t)
	ids := map[string]bool{}
	valid := regexp.MustCompile(`^[A-Za-z0-9:_-]+$`)
	for _, a := range m.Actions {
		assert.Regexp(t, valid, a.ID, "action ids may not contain dots")
		assert.False(t, ids[a.ID], "duplicate action id %s (Herdr refuses one even across platforms)", a.ID)
		ids[a.ID] = true
	}
	for _, want := range []string{"state-start", "state-stop", "view-flip", "jump", "settings", "configure", "unconfigure", "uninstall-font", "install-font"} {
		assert.True(t, ids[want], "missing action %s", want)
	}
	require.Len(t, m.Panes, 1)
	assert.Equal(t, "settings", m.Panes[0].ID)
	assert.Equal(t, "popup", m.Panes[0].Placement)
	require.Len(t, m.Events, 1)
	assert.Equal(t, "pane.agent_detected", m.Events[0].On)
}

// TestManifestCommandsExist runs every command the manifest declares against
// the real command tree with --help, so a renamed subcommand or flag fails
// here instead of in a user's plugin log.
func TestManifestCommandsExist(t *testing.T) {
	m := loadManifest(t)
	var all []command
	for _, group := range [][]command{m.Startup, m.Events, m.Actions, m.Panes, m.Build[2:]} {
		all = append(all, group...)
	}
	for _, c := range all {
		require.NotEmpty(t, c.Command)
		assert.Equal(t, "bin/herdr-scope", c.Command[0], "commands run the plugin binary by its relative path")
		var out, errOut bytes.Buffer
		root := bootstrap.NewRootCommand(context.Background(), bootstrap.Build{Version: "test"}, &out, &errOut)
		root.SetArgs(append(append([]string{}, c.Command[1:]...), "--help"))
		require.NoError(t, root.Execute(), "%v: %s", c.Command, errOut.String())
		assert.NotContains(t, out.String(), "Available Commands", "%v resolves to a command group, not a command", c.Command)
	}
	require.Len(t, m.Build, 3)
	assert.Equal(t, []string{"sh", "scripts/build.sh"}, m.Build[0].Command)
	assert.Equal(t, []string{"linux", "macos"}, m.Build[0].Platforms)
	assert.Equal(t, []string{"cmd", "/d", "/c", `scripts\build.cmd`}, m.Build[1].Command)
	assert.Equal(t, []string{"windows"}, m.Build[1].Platforms)
	root := filepath.Join(filepath.Dir(callerFile(t)), "..", "..")
	for _, script := range []string{"scripts/build.sh", "scripts/build.cmd"} {
		raw, err := os.ReadFile(filepath.Join(root, script))
		require.NoError(t, err)
		assert.Contains(t, string(raw), "go build -trimpath", "%s: without a prebuilt binary the step builds from source", script)
		assert.Contains(t, string(raw), "releases/download/v", "%s: without Go the step downloads the release binary", script)
		assert.Contains(t, string(raw), "SHA256SUMS", "%s: and checks it against the release's checksums", script)
	}
	_, err := os.Stat(filepath.Join(root, "dist", "HerdrAgentIconsMax-Regular.ttf"))
	require.NoError(t, err, "the bundled font ships with the plugin")
}
