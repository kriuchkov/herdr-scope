//go:build !windows

package control

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/ports"
)

func TestALongRuntimeDirGetsAShortEndpoint(t *testing.T) {
	long := "/Users/someone-with-a-long-name/.local/state/herdr/plugins/kriuchkov.herdr-scope/runtime-a-named-session"
	at := Endpoint(long, "")
	assert.LessOrEqual(t, len(at), maxSocketPath)
	assert.Equal(t, at, Endpoint(long, ""), "the same session always gets the same endpoint")
	assert.NotEqual(t, at, Endpoint(long+"-2", ""), "another session gets another")
	serve(t, at, func(context.Context, ports.ControlRequest) ports.ControlReply { return ports.ControlReply{OK: true} })
	reply, err := Client{Endpoint: at}.Request(context.Background(), ports.ControlRequest{Cmd: ports.ControlPing})
	require.NoError(t, err)
	assert.True(t, reply.OK)
}
