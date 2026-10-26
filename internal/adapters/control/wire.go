package control

import (
	"time"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// The channel's wire shape: one JSON object a line. Two builds talk across a
// rebuild — a daemon the old binary started answers the new one — so a name
// here never changes. Request and status share the ports types' fields in
// their order, so a conversion maps them and a field added on one side only
// does not compile.
type wireRequest struct {
	Cmd       ports.ControlCommand `json:"cmd"`
	Op        domain.SortOp        `json:"op,omitempty"`
	Set       domain.SortMode      `json:"set,omitempty"`
	Purge     bool                 `json:"purge,omitempty"`
	Workspace domain.WorkspaceID   `json:"workspace,omitempty"`
}

type wireReply struct {
	OK      bool            `json:"ok"`
	Error   string          `json:"error,omitempty"`
	Status  *wireStatus     `json:"status,omitempty"`
	Mode    string          `json:"mode,omitempty"`
	Applied bool            `json:"applied,omitempty"`
	Stopped bool            `json:"stopped,omitempty"`
	Panes   []domain.PaneID `json:"panes,omitempty"`
	Family  string          `json:"family,omitempty"`
	Folded  bool            `json:"folded,omitempty"`
}

type wireStatus struct {
	PID            int           `json:"pid"`
	Version        string        `json:"version"`
	Build          string        `json:"build,omitempty"`
	Uptime         time.Duration `json:"uptime_ns"`
	FireAge        time.Duration `json:"fire_age_ns"`
	FrameRunning   time.Duration `json:"frame_running_ns"`
	Connected      bool          `json:"connected"`
	ServerVersion  string        `json:"server_version"`
	Protocol       int           `json:"protocol"`
	Agents         int           `json:"agents"`
	Mode           string        `json:"mode"`
	LastEvent      time.Time     `json:"last_event"`
	LastReconcile  time.Time     `json:"last_reconcile"`
	LastSidebar    time.Time     `json:"last_sidebar"`
	LastError      string        `json:"last_error,omitempty"`
	StatePath      string        `json:"state_path"`
	ConfigPath     string        `json:"config_path"`
	EventsReceived uint64        `json:"events_received"`
	Frames         uint64        `json:"frames"`
}

func replyToWire(reply ports.ControlReply) wireReply {
	w := wireReply{
		OK: reply.OK, Error: reply.Error, Mode: reply.Mode, Applied: reply.Applied, Stopped: reply.Stopped,
		Panes: reply.Panes, Family: reply.Family, Folded: reply.Folded,
	}
	if reply.Status != nil {
		status := wireStatus(*reply.Status)
		w.Status = &status
	}
	return w
}

func replyFromWire(w wireReply) ports.ControlReply {
	reply := ports.ControlReply{
		OK: w.OK, Error: w.Error, Mode: w.Mode, Applied: w.Applied, Stopped: w.Stopped,
		Panes: w.Panes, Family: w.Family, Folded: w.Folded,
	}
	if w.Status != nil {
		status := ports.DaemonStatus(*w.Status)
		reply.Status = &status
	}
	return reply
}

// StatusJSON is a daemon status in the channel's own shape, for `status
// --json` to print the fields it carries; nil prints as null.
func StatusJSON(status *ports.DaemonStatus) any {
	if status == nil {
		return nil
	}
	return wireStatus(*status)
}
