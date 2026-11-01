package control

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

func endpoint(t *testing.T) string {
	t.Helper()
	// Not t.TempDir: a Unix socket path must stay under 104 bytes.
	dir, err := os.MkdirTemp("", "hc") //nolint:usetesting // socket path length
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "control.sock")
}

func serve(t *testing.T, at string, handler ports.ControlHandler) context.CancelFunc {
	t.Helper()
	server, err := Listen(context.Background(), at)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = server.Serve(ctx, handler) }()
	t.Cleanup(cancel)
	return cancel
}

func TestRequestReply(t *testing.T) {
	at := endpoint(t)
	serve(t, at, func(_ context.Context, r ports.ControlRequest) ports.ControlReply {
		if r.Cmd == ports.ControlView {
			return ports.ControlReply{OK: true, Applied: true, Mode: string(r.Set)}
		}
		return ports.ControlReply{OK: true, Status: &ports.DaemonStatus{PID: 42}}
	})
	c := Client{Endpoint: at}
	reply, err := c.Request(context.Background(), ports.ControlRequest{Cmd: ports.ControlPing})
	require.NoError(t, err)
	assert.Equal(t, 42, reply.Status.PID)
	reply, err = c.Request(context.Background(), ports.ControlRequest{Cmd: ports.ControlView, Set: domain.SortRecent})
	require.NoError(t, err)
	assert.Equal(t, "recent", reply.Mode)

	info, err := os.Stat(at)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "only the user can reach the endpoint")
}

// The channel's bytes are a contract between builds: during a rebuild a
// daemon the old binary started answers the new binary's requests, and the
// other way round. These are the lines it carried when the wire shape still
// lived in the ports.
const (
	requestLine = `{"cmd":"view","op":"flip","set":"recent","purge":true,"workspace":"w1"}`
	replyLine   = `{"ok":true,"error":"busy","status":{"pid":42,"version":"0.4.0","build":"abcd",` +
		`"uptime_ns":90000000000,"fire_age_ns":1000000000,"frame_running_ns":2000000,"connected":true,` +
		`"server_version":"0.9.1","protocol":22,"agents":3,"mode":"active","last_event":"2026-10-26T09:30:00Z",` +
		`"last_reconcile":"2026-10-26T09:30:01Z","last_sidebar":"2026-10-26T09:30:02Z","last_error":"timeout",` +
		`"state_path":"/s/state.json","config_path":"/c/config.toml","events_received":7,"frames":9},` +
		`"mode":"recent","applied":true,"stopped":true,"panes":["p1","p2"],"family":"app","folded":true}`
)

func fullRequest() ports.ControlRequest {
	return ports.ControlRequest{Cmd: ports.ControlView, Op: domain.SortFlip, Set: domain.SortRecent, Purge: true, Workspace: "w1"}
}

func fullReply() ports.ControlReply {
	at := time.Date(2026, 10, 26, 9, 30, 0, 0, time.UTC)
	return ports.ControlReply{
		OK: true, Error: "busy", Mode: "recent", Applied: true, Stopped: true,
		Panes: []domain.PaneID{"p1", "p2"}, Family: "app", Folded: true,
		Status: &ports.DaemonStatus{
			PID: 42, Version: "0.4.0", Build: "abcd",
			Uptime: 90 * time.Second, FireAge: time.Second, FrameRunning: 2 * time.Millisecond,
			Connected: true, ServerVersion: "0.9.1", Protocol: 22, Agents: 3, Mode: "active",
			LastEvent: at, LastReconcile: at.Add(time.Second), LastSidebar: at.Add(2 * time.Second),
			LastError: "timeout", StatePath: "/s/state.json", ConfigPath: "/c/config.toml",
			EventsReceived: 7, Frames: 9,
		},
	}
}

func TestTheWireFormatIsStable(t *testing.T) {
	// The client: the line it writes, and an old daemon's line read back.
	at := endpoint(t)
	listener, err := net.Listen("unix", at)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	sent := make(chan string, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		line, _ := bufio.NewReader(conn).ReadString('\n')
		sent <- line
		_, _ = conn.Write([]byte(replyLine + "\n"))
	}()
	reply, err := Client{Endpoint: at}.Request(t.Context(), fullRequest())
	require.NoError(t, err)
	assert.Equal(t, requestLine+"\n", <-sent) //nolint:testifylint // the bytes themselves, not JSON equivalence
	assert.Equal(t, fullReply(), reply)

	// The daemon: an old client's line read, and the line written back.
	at = endpoint(t)
	var got ports.ControlRequest
	serve(t, at, func(_ context.Context, r ports.ControlRequest) ports.ControlReply {
		got = r
		return fullReply()
	})
	conn, err := net.Dial("unix", at)
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.Write([]byte(requestLine + "\n"))
	require.NoError(t, err)
	line, err := bufio.NewReader(conn).ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, replyLine+"\n", line) //nolint:testifylint // the bytes themselves, not JSON equivalence
	assert.Equal(t, fullRequest(), got)
}

func TestStatusJSONIsTheWireShape(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"status": StatusJSON(fullReply().Status), "none": StatusJSON(nil)})
	require.NoError(t, err)
	start := strings.Index(replyLine, `"status":`)
	end := strings.Index(replyLine, `,"mode":"recent"`)
	assert.JSONEq(t, `{"none":null,`+replyLine[start:end]+`}`, string(raw), "status --json prints what the channel carries")
}

func TestASecondDaemonIsRefused(t *testing.T) {
	at := endpoint(t)
	serve(t, at, func(context.Context, ports.ControlRequest) ports.ControlReply { return ports.ControlReply{OK: true} })
	_, err := Listen(context.Background(), at)
	require.ErrorIs(t, err, ErrAlreadyRunning)
}

func TestAStaleSocketIsReclaimed(t *testing.T) {
	at := endpoint(t)
	// A crashed daemon leaves its socket file behind with nobody listening.
	l, err := net.Listen("unix", at)
	require.NoError(t, err)
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	require.NoError(t, l.Close())
	_, statErr := os.Stat(at)
	require.NoError(t, statErr, "the corpse is still on disk")

	serve(t, at, func(context.Context, ports.ControlRequest) ports.ControlReply { return ports.ControlReply{OK: true} })
	reply, err := Client{Endpoint: at}.Request(context.Background(), ports.ControlRequest{Cmd: ports.ControlPing})
	require.NoError(t, err)
	assert.True(t, reply.OK)
}

func TestNoDaemonIsAnError(t *testing.T) {
	_, err := Client{
		Endpoint: endpoint(t),
		Timeout:  200 * time.Millisecond,
	}.Request(
		context.Background(),
		ports.ControlRequest{Cmd: ports.ControlPing},
	)
	require.Error(t, err)
}

func TestBadRequestsGetAnAnswer(t *testing.T) {
	at := endpoint(t)
	serve(t, at, func(context.Context, ports.ControlRequest) ports.ControlReply { return ports.ControlReply{OK: true} })
	conn, err := net.Dial("unix", at)
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.Write([]byte("not json\n"))
	require.NoError(t, err)
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	assert.Contains(t, string(buf[:n]), "bad_request")
}

func TestServeEndsWithItsContext(t *testing.T) {
	at := endpoint(t)
	cancel := serve(t, at, func(context.Context, ports.ControlRequest) ports.ControlReply { return ports.ControlReply{OK: true} })
	cancel()
	require.Eventually(t, func() bool {
		_, err := Client{
			Endpoint: at,
			Timeout:  100 * time.Millisecond,
		}.Request(
			context.Background(),
			ports.ControlRequest{Cmd: ports.ControlPing},
		)
		return err != nil
	}, 2*time.Second, 20*time.Millisecond)
	assert.Equal(t, filepath.Join("/run", "control.sock"), Endpoint("/run", "-x"))
}
