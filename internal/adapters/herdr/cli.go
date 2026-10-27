package herdr

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

const cliTimeout = 10 * time.Second

// CLI is the cold path: one-shot commands through $HERDR_BIN_PATH, for what
// the socket does not cover or where the CLI is the documented interface.
type CLI struct {
	Binary string
}

func (c CLI) run(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()
	binary := c.Binary
	if binary == "" {
		binary = "herdr"
	}
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	text := strings.TrimSpace(out.String())
	if err != nil {
		return text, fmt.Errorf("%w: herdr %s: %w", domain.ErrTransport, strings.Join(args, " "), err)
	}
	return text, nil
}

// ReloadConfig makes the running server re-read config.toml.
func (c CLI) ReloadConfig(ctx context.Context) error {
	_, err := c.run(ctx, "server", "reload-config")
	return err
}

// ConfigCheck is Herdr's own verdict on its config file. parses is what
// matters: on a parse error Herdr falls back to defaults, which takes every
// plugin down; an unknown key is only ignored, and the exit code is 1 for
// both. A Herdr without the command counts as parsing — the check is a
// safety net, not a gate.
func (c CLI) ConfigCheck(ctx context.Context) (bool, string) {
	output, err := c.run(ctx, "config", "check")
	if err != nil && strings.Contains(output, "unrecognized subcommand") {
		return true, output
	}
	return !strings.Contains(output, "config parse error"), output
}

// Notify shows a toast; best effort.
func (c CLI) Notify(ctx context.Context, title, body string) {
	_, _ = c.run(ctx, "notification", "show", title, "--body", body)
}

// OpenPluginPane opens a plugin pane entrypoint (the settings popup).
func (c CLI) OpenPluginPane(ctx context.Context, plugin, entrypoint, cwd string) error {
	args := []string{"plugin", "pane", "open", "--plugin", plugin, "--entrypoint", entrypoint}
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

// Version is `herdr --version`.
func (c CLI) Version(ctx context.Context) (string, error) {
	return c.run(ctx, "--version")
}
