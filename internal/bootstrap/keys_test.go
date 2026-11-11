package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The docs carry the snippet for readers without a built binary; it must stay
// what `configure --keys` prints.
func TestTheDocsCarryTheKeySnippet(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "configuration.md"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "```toml\n"+keybindings("kriuchkov.herdr-scope")+"\n```")
}
