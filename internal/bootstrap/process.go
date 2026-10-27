package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/adapters/filesystem"
	"github.com/kriuchkov/herdr-scope/internal/application"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// maxLog bounds the daemon log; one older generation is kept.
const maxLog = 1 << 20

// rotatingLog is an append-only log file that rolls over at maxLog, so a
// crash loop cannot fill a disk.
type rotatingLog struct {
	mu   sync.Mutex
	path string
	file *os.File
	size int64
}

func openLog(path string) (*rotatingLog, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("log dir: %w", err)
	}
	l := &rotatingLog{path: path}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *rotatingLog) open() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("stat log: %w", err)
	}
	l.file, l.size = f, info.Size()
	return nil
}

func (l *rotatingLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.size+int64(len(p)) > maxLog {
		_ = l.file.Close()
		_ = os.Rename(l.path, l.path+".1")
		if err := l.open(); err != nil {
			return 0, err
		}
	}
	n, err := l.file.Write(p)
	l.size += int64(n)
	return n, err //nolint:wrapcheck // io.Writer contract
}

func (l *rotatingLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close() //nolint:wrapcheck // io.Closer contract
}

// process implements ports.DaemonProcess.
type process struct {
	runtime *Runtime
}

// Spawn starts `herdr-scope run` detached: it outlives the action or hook
// that started it, in its own session so a closing pane does not take it
// down. Its stderr goes to the log, opened with the reason it was needed.
func (p process) Spawn(ctx context.Context, note string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate binary: %w", err)
	}
	log, err := openLog(p.runtime.Paths.Log())
	if err != nil {
		return err
	}
	defer log.Close()
	if note != "" {
		_, _ = io.WriteString(log, time.Now().UTC().Format(time.RFC3339)+" "+note+"\n")
	}
	// Never cancelled: the daemon must outlive this command.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), exe, "run")
	cmd.Dir = p.runtime.Paths.PluginRoot
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = log.file
	cmd.SysProcAttr = detached()
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	return cmd.Process.Release() //nolint:wrapcheck // os.Process contract
}

// Terminate ends a daemon that will not end itself: a polite signal first,
// then a forced one if the endpoint still answers.
func (p process) Terminate(ctx context.Context, pid int) error {
	if pid <= 0 {
		return errors.New("no pid to terminate")
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find %d: %w", pid, err)
	}
	_ = terminateSignal(proc)
	if p.gone(ctx) {
		return nil
	}
	_ = proc.Kill()
	if p.gone(ctx) {
		return nil
	}
	return application.ErrDaemonStillRunning
}

func (p process) gone(ctx context.Context) bool {
	for range 20 {
		if health, _, _ := application.DaemonHealth(ctx, p.runtime.Control); health == application.HealthNone {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// sqliteRunner reads Kilo's store through the sqlite3 binary when one is
// installed; without it Kilo panes read as plain idle.
func sqliteRunner() func(ctx context.Context, db, query string) (string, error) {
	if path, err := exec.LookPath("sqlite3"); err == nil {
		return filesystem.SQLiteCLI(path)
	}
	return nil
}

var _ ports.DaemonProcess = process{}
