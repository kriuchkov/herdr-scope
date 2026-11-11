package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithLockSerialises(t *testing.T) {
	dir := t.TempDir()
	var inside, overlaps, runs atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			err := withLock(context.Background(), dir, "x.lock", func() error {
				if inside.Add(1) > 1 {
					overlaps.Add(1)
				}
				time.Sleep(10 * time.Millisecond)
				inside.Add(-1)
				runs.Add(1)
				return nil
			})
			assert.NoError(t, err)
		})
	}
	wg.Wait()
	assert.Equal(t, int32(8), runs.Load())
	assert.Zero(t, overlaps.Load(), "two holders at once")
	_, err := os.Stat(filepath.Join(dir, "x.lock"))
	assert.True(t, os.IsNotExist(err), "the lock is released")
}

func TestWithLockBreaksAStaleLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.lock")
	require.NoError(t, os.WriteFile(path, []byte("1\n"), 0o600))
	old := time.Now().Add(-2 * lockStale)
	require.NoError(t, os.Chtimes(path, old, old))
	ran := false
	require.NoError(t, withLock(context.Background(), dir, "x.lock", func() error { ran = true; return nil }))
	assert.True(t, ran, "a lock left by a dead process does not block forever")
}
