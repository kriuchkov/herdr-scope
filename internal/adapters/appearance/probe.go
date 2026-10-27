// Package appearance reads the desktop's light/dark appearance.
package appearance

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/ports"
)

const (
	// probeTimeout bounds one probe; a loaded machine that cannot answer in
	// time gives no answer, which is not the same as light.
	probeTimeout = 3 * time.Second
	// cacheFor bounds how often the OS is asked: the appearance flips twice
	// a day at most.
	cacheFor = time.Minute
)

// Runner runs a probe command and returns its stdout and exit code; err is
// set only when the command could not run to completion.
type Runner func(ctx context.Context, name string, args ...string) (stdout string, exitCode int, err error)

// Probe implements ports.AppearanceSource.
type Probe struct {
	goos string
	run  Runner
	now  func() time.Time

	mu    sync.Mutex
	value ports.Appearance
	at    time.Time
}

// NewProbe probes for goos with run.
func NewProbe(goos string, run Runner, now func() time.Time) *Probe {
	return &Probe{goos: goos, run: run, now: now}
}

// Current is the appearance, throttled; unknown where the platform has no
// answer — callers must then leave things alone.
func (p *Probe) Current(ctx context.Context) (ports.Appearance, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if p.value != ports.AppearanceUnknown && now.Sub(p.at) < cacheFor {
		return p.value, nil
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	value := p.probe(ctx)
	if value != ports.AppearanceUnknown {
		p.value, p.at = value, now
	}
	return value, nil
}

var lightTheme = regexp.MustCompile(`(?i)AppsUseLightTheme\s+REG_DWORD\s+0x([0-9a-f]+)`)

func (p *Probe) probe(ctx context.Context) ports.Appearance {
	switch p.goos {
	case "darwin":
		// The key exists only in dark mode. A non-zero exit is the light
		// signal, so only a command that never completed means "no answer".
		out, code, err := p.run(ctx, "defaults", "read", "-g", "AppleInterfaceStyle")
		if err != nil {
			return ports.AppearanceUnknown
		}
		if code == 0 && strings.Contains(strings.ToLower(out), "dark") {
			return ports.AppearanceDark
		}
		return ports.AppearanceLight
	case "linux":
		out, code, err := p.run(ctx, "gsettings", "get", "org.gnome.desktop.interface", "color-scheme")
		if err != nil || code != 0 {
			return ports.AppearanceUnknown
		}
		if strings.Contains(strings.ToLower(out), "dark") {
			return ports.AppearanceDark
		}
		return ports.AppearanceLight
	case "windows":
		out, _, err := p.run(ctx, "reg", "query",
			`HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, "/v", "AppsUseLightTheme")
		m := lightTheme.FindStringSubmatch(out)
		if err != nil || m == nil {
			return ports.AppearanceUnknown
		}
		if strings.TrimLeft(m[1], "0") == "" {
			return ports.AppearanceDark
		}
		return ports.AppearanceLight
	}
	return ports.AppearanceUnknown
}

// ExecRunner runs probes as real processes.
func ExecRunner(ctx context.Context, name string, args ...string) (string, int, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return string(out), exit.ExitCode(), nil
	}
	if err != nil {
		return "", -1, err
	}
	return string(out), 0, nil
}

var _ ports.AppearanceSource = (*Probe)(nil)
