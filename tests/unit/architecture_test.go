// Package unit holds repository-wide unit checks that belong to no single
// package: the dependency rule of the hexagonal layout.
package unit

import (
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const module = "github.com/kriuchkov/herdr-scope"

// layerRules: for each inner layer, the module packages it may import and the
// standard-library packages it must not.
func layerRules() map[string]struct {
	allowedModule []string
	forbiddenStd  []string
} {
	io := []string{"os", "os/exec", "os/signal", "net", "net/http", "io/fs", "io/ioutil", "log", "log/slog", "syscall", "unsafe"}
	return map[string]struct {
		allowedModule []string
		forbiddenStd  []string
	}{
		"internal/domain":      {allowedModule: nil, forbiddenStd: append(io, "context")},
		"internal/ports":       {allowedModule: []string{"internal/domain"}, forbiddenStd: io},
		"internal/application": {allowedModule: []string{"internal/domain", "internal/ports"}, forbiddenStd: io},
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func imports(t *testing.T, dir string) map[string][]string {
	t.Helper()
	found := map[string][]string{}
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
		require.NoError(t, parseErr)
		for _, spec := range file.Imports {
			path, unquoteErr := strconv.Unquote(spec.Path.Value)
			require.NoError(t, unquoteErr)
			found[path] = append(found[path], name)
		}
	}
	return found
}

func TestInnerLayersKeepTheDependencyRule(t *testing.T) {
	root := repoRoot(t)
	for layer, rule := range layerRules() {
		for path, files := range imports(t, filepath.Join(root, layer)) {
			switch {
			case strings.HasPrefix(path, module+"/"):
				inner := strings.TrimPrefix(path, module+"/")
				allowed := false
				for _, a := range rule.allowedModule {
					allowed = allowed || inner == a
				}
				assert.True(t, allowed, "%s imports %s (%v)", layer, path, files)
			case strings.Contains(strings.SplitN(path, "/", 2)[0], "."):
				assert.Fail(t, "third-party import in an inner layer", "%s imports %s (%v)", layer, path, files)
			default:
				for _, forbidden := range rule.forbiddenStd {
					assert.NotEqual(t, forbidden, path, "%s imports %s (%v)", layer, path, files)
				}
			}
		}
	}
}

// adapterAllowList: the module packages beyond domain and ports an adapter
// may import. Adapters never import one another, the application or the
// composition root, except the inbound UI and the replay tool.
func adapterAllowList() map[string][]string {
	return map[string][]string{
		// The settings popup is an inbound UI: it drives the use cases.
		"tui": {"internal/adapters/config", "internal/application"},
		// Test and tool replay of Herdr payloads through the real pipeline.
		"scenario": {"internal/adapters/config", "internal/adapters/herdr", "internal/adapters/sidebar"},
	}
}

func TestAdaptersStayApart(t *testing.T) {
	root := repoRoot(t)
	allowed := adapterAllowList()
	var listed []string
	for _, adapter := range slices.Sorted(maps.Keys(allowed)) {
		listed = append(listed, adapter+" → "+strings.Join(allowed[adapter], ", "))
	}
	entries, err := os.ReadDir(filepath.Join(root, "internal", "adapters"))
	require.NoError(t, err)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		adapter := entry.Name()
		for path, files := range imports(t, filepath.Join(root, "internal", "adapters", adapter)) {
			inner, ok := strings.CutPrefix(path, module+"/")
			if !ok || slices.Contains(allowed[adapter], inner) {
				continue
			}
			barred := strings.HasPrefix(inner, "internal/adapters/") || inner == "internal/application" ||
				strings.HasPrefix(inner, "internal/bootstrap")
			assert.False(t, barred, "internal/adapters/%s imports %s (%v): no other adapter, application or bootstrap "+
				"outside the allow-list (%s)", adapter, path, files, strings.Join(listed, "; "))
		}
	}
}

func TestAdaptersDoNotLeakIntoInnerLayers(t *testing.T) {
	root := repoRoot(t)
	for _, layer := range []string{"internal/domain", "internal/ports", "internal/application"} {
		for path := range imports(t, filepath.Join(root, layer)) {
			assert.NotContains(t, path, "/adapters/", "%s depends on an adapter", layer)
			assert.NotContains(t, path, "herdr-api", "%s depends on the Herdr client", layer)
			assert.NotContains(t, path, "toml", "%s depends on a TOML parser", layer)
		}
	}
}
