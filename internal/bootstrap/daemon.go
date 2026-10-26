package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/adapters/control"
	"github.com/kriuchkov/herdr-scope/internal/application"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// Binding the endpoint can lose a race with a daemon that was just told to
// stop and still holds it for a moment; a restart retries before deciding a
// live daemon owns it.
const (
	bindAttempts = 20
	bindPause    = 250 * time.Millisecond
)

func levelOf(name string) slog.Level {
	switch name {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	return slog.LevelInfo
}

// RunDaemon is `herdr-scope run`: be the daemon until Herdr goes away, a stop
// request arrives, or a signal ends it.
func (r *Runtime) RunDaemon(ctx context.Context) error {
	log, err := openLog(r.Paths.Log())
	if err != nil {
		return err
	}
	defer log.Close()
	var level slog.LevelVar
	level.Set(levelOf(r.Settings().LogLevel))
	logger := slog.New(slog.NewTextHandler(log, &slog.HandlerOptions{Level: &level}))

	server, err := r.bind(ctx)
	if errors.Is(err, control.ErrAlreadyRunning) {
		logger.InfoContext(ctx, "another daemon owns the control endpoint; leaving it be")
		return nil
	}
	if err != nil {
		return err
	}
	defer server.Close()
	_ = writeAtomic(r.Paths.PIDFile(), []byte(strconv.Itoa(os.Getpid())+"\n"))
	defer func() { _ = os.Remove(r.Paths.PIDFile()) }()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	signals := make(chan os.Signal, 1)
	notifyStop(signals)
	go func() {
		select {
		case <-signals:
			cancel()
		case <-ctx.Done():
		}
	}()

	records, err := r.State.Load(ctx)
	if err != nil {
		logger.WarnContext(ctx, "state unreadable; starting with empty memory", "error", err)
	}
	observer := application.NewObserver(r.observerDeps(logger), r.Settings(), application.CurrentSortMode(r.SortModes), records)
	daemon := application.NewDaemon(application.DaemonDeps{
		Observer: observer, Events: r.Events, Probe: r.Observer, Control: server, Config: r.Plugin,
		SortModes: r.SortModes, Sidebar: r.Writer, Clock: clock{}, Logger: logger,
		Managed: r.Managed, Reloader: r.CLI, TabBar: r.TabBar,
		Appearance: &application.AppearanceFollower{Source: r.Appearance, Managed: r.Managed, Reloader: r.CLI},
		OnSettings: []func(ports.Settings){
			r.applySettings,
			func(s ports.Settings) { level.Set(levelOf(s.LogLevel)) },
		},
		Version: r.Build.Version, Build: r.fingerprint(), PID: os.Getpid(), StatePath: r.State.Path(), ConfigPath: r.Plugin.Path(),
	})
	err = daemon.Run(ctx)
	var tooOld *application.ProtocolTooOldError
	switch {
	case errors.As(err, &tooOld):
		logger.ErrorContext(ctx, "refusing to run", "error", tooOld)
		return fmt.Errorf("ERROR:\n%w", tooOld)
	case errors.Is(err, application.ErrServerGone):
		return nil
	}
	return err
}

func (r *Runtime) bind(ctx context.Context) (*control.Server, error) {
	var err error
	for range bindAttempts {
		var server *control.Server
		server, err = control.Listen(ctx, r.Endpoint)
		if err == nil {
			return server, nil
		}
		if !errors.Is(err, control.ErrAlreadyRunning) {
			return nil, err //nolint:wrapcheck // already names the endpoint
		}
		time.Sleep(bindPause)
	}
	return nil, err
}
