//go:build windows

package control

import (
	"context"
	"net"
	"os/user"
	"regexp"

	"github.com/Microsoft/go-winio"
)

// ownerOnly lets only the creating user open the pipe.
const ownerOnly = "D:P(A;;GA;;;OW)"

// listen binds a named pipe. On Windows the pipe vanishes with its owner, so
// there is never a stale endpoint to reclaim.
func listen(endpoint string) (net.Listener, error) {
	return winio.ListenPipe(endpoint, &winio.PipeConfig{SecurityDescriptor: ownerOnly}) //nolint:wrapcheck // the caller wraps it
}

func reclaim(string) bool { return false }

func dial(ctx context.Context, endpoint string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, endpoint) //nolint:wrapcheck // wrapped by Request
}

var pipeUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// Endpoint is the per-user, per-session pipe name: two accounts or two named
// sessions never fight over one.
func Endpoint(_ string, sessionSuffix string) string {
	name := "user"
	if u, err := user.Current(); err == nil {
		name = pipeUnsafe.ReplaceAllString(u.Username, "_")
	}
	return `\\.\pipe\herdr-scope.` + name + sessionSuffix + ".ctl"
}
