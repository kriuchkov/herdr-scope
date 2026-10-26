// Package herdr is the adapter to Herdr's socket API (hot path) and CLI (cold
// path). It owns every Herdr wire type; nothing it decodes leaves the package
// except as domain values.
package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	herdrapi "github.com/cyperx84/herdr-api"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

const (
	// callTimeout bounds one request; the server answers in milliseconds.
	callTimeout = 4 * time.Second
	// maxInFlight keeps a full repaint from opening dozens of connections at
	// once: the server answers one request per connection.
	maxInFlight = 16
)

// Client talks to one Herdr server over its socket.
type Client struct {
	api    *herdrapi.Client
	path   string
	source string
	slots  chan struct{}
}

// NewClient returns a client for the socket at path; source tags every
// metadata write ("plugin:<plugin id>").
func NewClient(path, source string) *Client {
	return &Client{
		api:    herdrapi.New(path),
		path:   path,
		source: source,
		slots:  make(chan struct{}, maxInFlight),
	}
}

// APIError is a refusal from the server, with its code.
type APIError struct {
	Method  string
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("herdr %s: %s: %s", e.Method, e.Code, e.Message)
}

// Unwrap classifies a refusal: the request was refused, the protocol stands.
func (e *APIError) Unwrap() error { return domain.ErrRefused }

// call issues one request and decodes the result into out (which may be nil).
func (c *Client) call(ctx context.Context, method string, params, out any) error {
	select {
	case c.slots <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("%w: %s: %w", domain.ErrTransport, method, ctx.Err())
	}
	defer func() { <-c.slots }()

	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err := c.api.Call(ctx, method, params, out); err != nil {
		return classify(method, err)
	}
	return nil
}

// classify sorts a failed call by kind. The transport decodes the result
// itself and wraps a decode failure with %w, so a result of the wrong shape
// still reads as a decode error.
func classify(method string, err error) error {
	var apiErr *herdrapi.APIError
	if errors.As(err, &apiErr) {
		return &APIError{Method: method, Code: apiErr.Code, Message: apiErr.Message}
	}
	var syntax *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &typeErr) {
		return fmt.Errorf("%w: %s: %w", domain.ErrDecode, method, err)
	}
	return fmt.Errorf("%w: %s: %w", domain.ErrTransport, method, err)
}

// IsCode reports whether err is a server refusal with one of codes.
func IsCode(err error, codes ...string) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && slices.Contains(codes, apiErr.Code)
}

// Ping identifies the server.
func (c *Client) Ping(ctx context.Context) (domain.ServerInfo, error) {
	var pong wirePong
	if err := c.call(ctx, "ping", struct{}{}, &pong); err != nil {
		return domain.ServerInfo{}, err
	}
	return domain.ServerInfo{Version: pong.Version, Protocol: pong.Protocol}, nil
}

// Gone reports whether the server's endpoint has disappeared — the socket
// marker vanished, so this server is not coming back on it.
func (c *Client) Gone() bool {
	_, err := os.Stat(c.path)
	return errors.Is(err, os.ErrNotExist)
}
