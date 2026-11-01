// Package control is the daemon's local control channel and its instance
// lock. Binding the endpoint is atomic, so whoever owns it is the daemon; a
// pid file cannot say that (a recycled pid looks alive). One JSON line per
// connection in, one JSON line back. Local only: a Unix socket owned by the
// user, or a named pipe on Windows — never a network listener.
package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// ErrAlreadyRunning means a live daemon answers the endpoint.
var ErrAlreadyRunning = errors.New("another daemon owns the control endpoint")

const (
	// defaultTimeout bounds a request; Stop waits longer, the daemon clears
	// every token before it answers.
	defaultTimeout = 1500 * time.Millisecond
	// ioTimeout bounds one connection inside the server.
	ioTimeout = 30 * time.Second
	maxLine   = 1 << 20
)

// Client implements ports.DaemonControl.
type Client struct {
	Endpoint string
	Timeout  time.Duration
}

// Request sends one request and reads one reply; an error means no daemon
// answered.
func (c Client) Request(ctx context.Context, request ports.ControlRequest) (ports.ControlReply, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
		if request.Cmd == ports.ControlStop {
			timeout = 10 * time.Second
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := dial(ctx, c.Endpoint)
	if err != nil {
		return ports.ControlReply{}, fmt.Errorf("control: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	line, err := json.Marshal(wireRequest(request))
	if err != nil {
		return ports.ControlReply{}, fmt.Errorf("control: %w", err)
	}
	if _, err = conn.Write(append(line, '\n')); err != nil {
		return ports.ControlReply{}, fmt.Errorf("control: %w", err)
	}
	reader := bufio.NewReaderSize(conn, 4096)
	raw, err := reader.ReadBytes('\n')
	if err != nil {
		// A peer hanging up without a word — a daemon caught mid-exit — is
		// no answer, not a hang.
		return ports.ControlReply{}, fmt.Errorf("control: %w", err)
	}
	var reply wireReply
	if err = json.Unmarshal(raw, &reply); err != nil {
		return ports.ControlReply{}, fmt.Errorf("control: %w", err)
	}
	return replyFromWire(reply), nil
}

// Server implements ports.ControlServer on a bound endpoint.
type Server struct {
	listener net.Listener
	once     sync.Once
}

// Listen binds the endpoint. A bind that fails because the endpoint exists is
// settled by asking it: a live owner answers (ErrAlreadyRunning), a corpse —
// a stale Unix socket after a crash — does not and is reclaimed.
func Listen(ctx context.Context, endpoint string) (*Server, error) {
	listener, err := listen(endpoint)
	if err == nil {
		return &Server{listener: listener}, nil
	}
	if _, pingErr := (Client{Endpoint: endpoint}).Request(ctx, ports.ControlRequest{Cmd: ports.ControlPing}); pingErr == nil {
		return nil, ErrAlreadyRunning
	}
	if !reclaim(endpoint) {
		return nil, fmt.Errorf("control: listen %s: %w", endpoint, err)
	}
	listener, err = listen(endpoint)
	if err != nil {
		return nil, fmt.Errorf("control: listen %s: %w", endpoint, err)
	}
	return &Server{listener: listener}, nil
}

// Serve answers requests until ctx ends.
func (s *Server) Serve(ctx context.Context, handler ports.ControlHandler) error {
	stop := context.AfterFunc(ctx, s.Close)
	defer stop()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("control: accept: %w", err)
		}
		go answer(ctx, conn, handler)
	}
}

func answer(ctx context.Context, conn net.Conn, handler ports.ControlHandler) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(ioTimeout))
	reader := bufio.NewReaderSize(conn, 4096)
	raw, err := reader.ReadSlice('\n')
	reply := ports.ControlReply{Error: "bad_request"}
	if err == nil && len(raw) < maxLine {
		var request wireRequest
		if json.Unmarshal(raw, &request) == nil {
			reply = handler(ctx, ports.ControlRequest(request))
		}
	}
	line, err := json.Marshal(replyToWire(reply))
	if err != nil {
		return
	}
	_, _ = conn.Write(append(line, '\n'))
}

// Close releases the endpoint.
func (s *Server) Close() {
	s.once.Do(func() { _ = s.listener.Close() })
}

var (
	_ ports.DaemonControl = Client{}
	_ ports.ControlServer = (*Server)(nil)
)
