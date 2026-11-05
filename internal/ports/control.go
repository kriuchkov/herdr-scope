package ports

import (
	"context"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

// ControlCommand names a request to the resident daemon.
type ControlCommand string

// Control commands.
const (
	// ControlPing asks for the daemon's status.
	ControlPing ControlCommand = "ping"
	// ControlView switches the panel order.
	ControlView ControlCommand = "view"
	// ControlReload re-reads the configuration.
	ControlReload ControlCommand = "reload"
	// ControlStop clears the daemon's paint and ends it.
	ControlStop ControlCommand = "stop"
	// ControlWaiting asks which panes wait on the user, in panel order.
	ControlWaiting ControlCommand = "waiting"
	// ControlFold folds or unfolds a workspace's family in the Agents panel.
	ControlFold ControlCommand = "fold"
)

// ControlRequest is one request to the daemon.
type ControlRequest struct {
	Cmd ControlCommand
	// Op and Set describe a view switch: a transition, or a mode outright.
	Op  domain.SortOp
	Set domain.SortMode
	// Purge makes a stop clear every token, not only the marks.
	Purge bool
	// Workspace is the one a fold is for: the focused one, as the caller saw
	// it; empty leaves it to the focus the daemon tracks.
	Workspace domain.WorkspaceID
}

// ControlReply is the daemon's answer.
type ControlReply struct {
	OK      bool
	Error   string
	Status  *DaemonStatus
	Mode    string
	Applied bool
	Stopped bool
	// Panes answers ControlWaiting.
	Panes []domain.PaneID
	// Family and Folded answer ControlFold: the family's name and whether it
	// is folded now.
	Family string
	Folded bool
}

// DaemonStatus is what `herdr-scope status` shows. The control adapter owns
// how it travels.
type DaemonStatus struct {
	PID     int
	Version string
	// Build fingerprints the executable the daemon started from: two builds
	// of one version differ here, so a start action run by another build can
	// tell the daemon is stale. Empty from a daemon older than the field.
	Build string
	// Uptime is how long the daemon has run.
	Uptime time.Duration
	// FireAge is how long since the scheduler last fired at all. Its fixed
	// one-second housekeeping tick fires it whatever heartbeat_interval says
	// (up to a minute), so a long gap is a dead scheduler.
	FireAge time.Duration
	// FrameRunning is how long the frame in flight has run, zero when none.
	FrameRunning   time.Duration
	Connected      bool
	ServerVersion  string
	Protocol       int
	Agents         int
	Mode           string
	LastEvent      time.Time
	LastReconcile  time.Time
	LastSidebar    time.Time
	LastError      string
	StatePath      string
	ConfigPath     string
	EventsReceived uint64
	Frames         uint64
}

// DaemonControl is the thin client's line to the daemon.
type DaemonControl interface {
	// Request sends one request. It fails when no daemon answers.
	Request(ctx context.Context, request ControlRequest) (ControlReply, error)
}

// ControlHandler answers control requests inside the daemon.
type ControlHandler func(ctx context.Context, request ControlRequest) ControlReply

// ControlServer is the daemon's end of the control channel. Binding it is the
// single-instance lock: whoever owns the endpoint is the daemon.
type ControlServer interface {
	// Serve answers requests until ctx ends.
	Serve(ctx context.Context, handler ControlHandler) error
}
