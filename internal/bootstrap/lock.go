package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Several state-starts can run at once — the startup hook, the watchdog on
// every agent detection, an action — and each may run first-run setup, which
// edits Herdr's config.toml. They take turns through a lock file.
const (
	lockWait  = 15 * time.Second
	lockStale = time.Minute
	lockPoll  = 50 * time.Millisecond
)

// withLock runs fn while holding <dir>/<name>. A lock older than lockStale
// belonged to a process that died holding it and is broken.
func withLock(ctx context.Context, dir, name string, fn func() error) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("lock dir: %w", err)
	}
	path := filepath.Join(dir, name)
	deadline := time.Now().Add(lockWait)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = f.WriteString(strconv.Itoa(os.Getpid()) + "\n")
			_ = f.Close()
			defer func() { _ = os.Remove(path) }()
			return fn()
		}
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("lock %s: %w", path, err)
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > lockStale {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("lock %s: still held after %s", path, lockWait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck // cancellation
		case <-time.After(lockPoll):
		}
	}
}
