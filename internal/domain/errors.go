package domain

import "errors"

// Error kinds. Adapters wrap their failures in one of these with %w so the
// observer can react by kind without knowing the adapter.
var (
	// ErrTransport: the host could not be reached (socket, pipe, timeout).
	ErrTransport = errors.New("transport error")
	// ErrProtocol: the host speaks a protocol Scope does not support.
	ErrProtocol = errors.New("protocol error")
	// ErrRefused: the host refused one request (bad parameters, a disabled
	// plugin); the protocol itself is fine.
	ErrRefused = errors.New("request refused")
	// ErrDecode: the host answered something Scope could not read.
	ErrDecode = errors.New("decode error")
	// ErrPersistence: local state could not be read or written.
	ErrPersistence = errors.New("persistence error")
	// ErrProjection: the sidebar could not be written.
	ErrProjection = errors.New("projection error")
	// ErrConfiguration: a configuration is invalid or could not be applied.
	ErrConfiguration = errors.New("configuration error")
	// ErrNotRepository: a path is not inside a git checkout.
	ErrNotRepository = errors.New("not a git repository")
)
