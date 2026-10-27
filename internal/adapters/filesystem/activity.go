package filesystem

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// tailBytes is how much of a transcript is read: only its end, enough to
// find the newest timestamped record. Nothing read is logged or kept.
const tailBytes = 256 * 1024

// Activity implements ports.ActivitySource from the CLIs' own session
// records, for panes older than the observer.
type Activity struct {
	env  Env
	home string
	// sqlite runs a read-only query against Kilo's store; nil disables it.
	sqlite func(ctx context.Context, db, query string) (string, error)
}

// NewActivity reads records under home, honouring CLAUDE_CONFIG_DIR,
// CODEX_HOME and KILO_DATA_DIR from env.
func NewActivity(env Env, home string, sqlite func(ctx context.Context, db, query string) (string, error)) *Activity {
	return &Activity{env: env, home: home, sqlite: sqlite}
}

// LastActivity finds when the session last did something. No record is not
// an error: the pane then reads as plain idle, never stale.
func (a *Activity) LastActivity(ctx context.Context, kind domain.AgentKind, session string) (time.Time, error) {
	if session == "" {
		return time.Time{}, nil
	}
	switch kind {
	case "claude":
		return a.claude(session)
	case "codex":
		return a.codex(session)
	case "kilo":
		return a.kilo(ctx, session)
	}
	return time.Time{}, nil
}

// claude: <CLAUDE_CONFIG_DIR or ~/.claude>/projects/<encoded cwd>/<session>.jsonl
func (a *Activity) claude(session string) (time.Time, error) {
	if strings.ContainsAny(session, `/\`) {
		return time.Time{}, nil
	}
	root := filepath.Join(firstNonEmpty(a.env("CLAUDE_CONFIG_DIR"), filepath.Join(a.home, ".claude")), "projects")
	dirs, err := os.ReadDir(root)
	if err != nil {
		return time.Time{}, nil //nolint:nilerr // no records is an answer, not a failure
	}
	for _, dir := range dirs {
		file := filepath.Join(root, dir.Name(), session+".jsonl")
		if _, statErr := os.Stat(file); statErr == nil {
			return lastActiveAt(file)
		}
	}
	return time.Time{}, nil
}

// codex: <CODEX_HOME or ~/.codex>/sessions/YYYY/MM/DD/rollout-<stamp>-<session>.jsonl
func (a *Activity) codex(session string) (time.Time, error) {
	root := filepath.Join(firstNonEmpty(a.env("CODEX_HOME"), filepath.Join(a.home, ".codex")), "sessions")
	matches, err := filepath.Glob(filepath.Join(root, "*", "*", "*", "*"))
	if err != nil {
		return time.Time{}, nil //nolint:nilerr // a malformed pattern cannot happen; no answer either way
	}
	for _, file := range matches {
		if strings.Contains(filepath.Base(file), session) {
			return lastActiveAt(file)
		}
	}
	return time.Time{}, nil
}

// kiloID is what a Kilo session id looks like; anything else never reaches
// the query.
var kiloID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// kilo reads the session's own time_updated row from Kilo's SQLite store,
// read-only. A store-wide timestamp is never used: it would shade a stale
// pane fresh because a different Kilo pane was busy.
func (a *Activity) kilo(ctx context.Context, session string) (time.Time, error) {
	if a.sqlite == nil || !kiloID.MatchString(session) {
		return time.Time{}, nil
	}
	db := filepath.Join(firstNonEmpty(a.env("KILO_DATA_DIR"), filepath.Join(a.home, ".local", "share", "kilo")), "kilo.db")
	// Never open what is not there: opening is how a store gets created.
	if _, err := os.Stat(db); err != nil {
		return time.Time{}, nil //nolint:nilerr // no store, no record
	}
	out, err := a.sqlite(ctx, db, "SELECT time_updated FROM session WHERE id = '"+session+"' LIMIT 1;")
	if err != nil {
		return time.Time{}, nil //nolint:nilerr // an unreadable store reads as no record
	}
	ms, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	// Kilo writes epoch milliseconds; anything that would be 1970 is not one.
	if err != nil || ms < 1e11 {
		return time.Time{}, nil //nolint:nilerr // not a timestamp is no record
	}
	return time.UnixMilli(ms), nil
}

// SQLiteCLI runs the sqlite3 binary read-only, when it is installed.
func SQLiteCLI(binary string) func(ctx context.Context, db, query string) (string, error) {
	return func(ctx context.Context, db, query string) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, binary, "-readonly", "-batch", "-noheader", db, query).Output()
		return string(out), err
	}
}

// lastActiveAt is the newest line in the file's tail carrying a timestamp.
// The file's mtime lies — reattaching a session appends timestamp-less
// marker lines — so it is only the fallback.
func lastActiveAt(file string) (time.Time, error) {
	f, err := os.Open(file)
	if err != nil {
		return time.Time{}, nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return time.Time{}, nil
	}
	offset := max(0, info.Size()-tailBytes)
	buf := make([]byte, info.Size()-offset)
	if _, err = f.ReadAt(buf, offset); err != nil && !errors.Is(err, io.EOF) {
		return time.Time{}, nil
	}
	lines := bytes.Split(buf, []byte("\n"))
	for _, line := range slices.Backward(lines) {
		var record struct {
			Timestamp string `json:"timestamp"`
		}
		// A line the window cut in half fails to parse and is skipped.
		if json.Unmarshal(line, &record) != nil || record.Timestamp == "" {
			continue
		}
		if at, parseErr := time.Parse(time.RFC3339Nano, record.Timestamp); parseErr == nil {
			return at, nil
		}
	}
	return info.ModTime(), nil
}

var _ ports.ActivitySource = (*Activity)(nil)
