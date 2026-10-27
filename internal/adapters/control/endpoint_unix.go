//go:build !windows

package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// maxSocketPath is the longest Unix socket path every platform accepts
// (macOS's sun_path holds 104 bytes, the terminating NUL included).
const maxSocketPath = 103

// listen binds a Unix socket only the user can reach.
func listen(endpoint string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(endpoint), 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", filepath.Dir(endpoint), err)
	}
	var lc net.ListenConfig
	listener, err := lc.Listen(context.Background(), "unix", endpoint)
	if err != nil {
		return nil, err //nolint:wrapcheck // the caller wraps it with the endpoint
	}
	if err = os.Chmod(endpoint, 0o600); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("chmod %s: %w", endpoint, err)
	}
	return listener, nil
}

// reclaim removes a socket file nobody answers.
func reclaim(endpoint string) bool {
	return os.Remove(endpoint) == nil
}

func dial(ctx context.Context, endpoint string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", endpoint) //nolint:wrapcheck // wrapped by Request
}

// Endpoint is the control socket for a runtime directory. A path too long
// for a Unix socket — a long home or state directory, a named session — moves
// to a short per-user directory instead, named after the runtime directory so
// each session keeps its own.
func Endpoint(runtimeDir, _ string) string {
	path := filepath.Join(runtimeDir, "control.sock")
	if len(path) <= maxSocketPath {
		return path
	}
	sum := sha256.Sum256([]byte(runtimeDir))
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "herdr-scope-"+hex.EncodeToString(sum[:6])+".sock")
}
