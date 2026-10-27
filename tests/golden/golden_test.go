// Package golden pins the sidebar's look: every scenario under
// tests/fixtures/scenarios is replayed through Scope's pipeline and drawn as
// Herdr would draw it, once in the classic layout, the default
// (scenario-NN.classic.txt), and once in the compact one (scenario-NN.txt).
// A change to the look must change these files explicitly:
// go test ./tests/golden -update.
package golden

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/adapters/scenario"
	"github.com/kriuchkov/herdr-scope/internal/adapters/sidebar"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

//nolint:gochecknoglobals // a test flag is package state by design
var update = flag.Bool("update", false, "rewrite the golden files")

func root(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(file), "..")
}

func scenarios(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(root(t), "fixtures", "scenarios", "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	return files
}

func TestGoldenSidebar(t *testing.T) {
	for _, file := range scenarios(t) {
		for layout, infix := range map[ports.Layout]string{ports.LayoutCompact: "", ports.LayoutClassic: ".classic"} {
			name := strings.TrimSuffix(filepath.Base(file), ".json") + infix
			t.Run(name, func(t *testing.T) {
				s, err := scenario.Load(file)
				require.NoError(t, err)
				testGolden(t, s, layout, name)
			})
		}
	}
}

// TestGoldenAges pins the age in front of a title, which no scenario shows by
// itself: only a waiting row wears one by default, and every wait there is
// seconds old. Scenario 01 runs on with every age shown, five minutes later.
func TestGoldenAges(t *testing.T) {
	file := filepath.Join(root(t), "fixtures", "scenarios", "scenario-01.json")
	for layout, infix := range map[ports.Layout]string{ports.LayoutCompact: "", ports.LayoutClassic: ".classic"} {
		name := "scenario-01" + infix + ".ages"
		t.Run(name, func(t *testing.T) {
			s, err := scenario.Load(file)
			require.NoError(t, err)
			s.Description += " Five minutes on, with show_age = all."
			s.Config = "show_age = \"all\"\n" + s.Config
			later := s.Steps[len(s.Steps)-1]
			later.At = "5m"
			s.Steps = append(s.Steps, later)
			testGolden(t, s, layout, name)
		})
	}
}

func testGolden(t *testing.T, s scenario.Scenario, layout ports.Layout, name string) {
	t.Helper()
	s.Layout = layout
	result, err := scenario.Run(s)
	require.NoError(t, err)
	for suffix, opts := range map[string]sidebar.RenderOptions{".txt": {}, ".tokens.txt": {Annotate: true}} {
		got := "# " + s.Description + "\n" + result.Render(opts)
		path := filepath.Join(root(t), "golden", name+suffix)
		if *update {
			require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
			continue
		}
		want, readErr := os.ReadFile(path)
		require.NoError(t, readErr, "missing golden file; run go test ./tests/golden -update")
		assert.Equal(t, string(want), got, "the sidebar changed; if that is intended, run go test ./tests/golden -update")
	}
}
