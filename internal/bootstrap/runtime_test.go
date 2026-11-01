package bootstrap

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kriuchkov/herdr-scope/internal/adapters/sidebar"
	"github.com/kriuchkov/herdr-scope/internal/domain"
)

// The writer's presentation probes the terminal font; a command that never
// paints must not pay for it.
func TestTheSidebarWriterIsBuiltOnFirstUse(t *testing.T) {
	builds := 0
	w := &lazyWriter{build: func() *sidebar.Writer {
		builds++
		return sidebar.NewWriter(nil, sidebar.Presentation{}, time.Now)
	}}
	assert.Zero(t, builds, "wiring it builds nothing")
	assert.Empty(t, w.Badge(domain.SidebarView{}))
	w.Resync()
	assert.Equal(t, 1, builds, "built once, then reused")
}
