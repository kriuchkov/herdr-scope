// Package filesystem holds the adapters that are plain file access: where
// Scope's and Herdr's files live and what Herdr's plugin environment says,
// activity recovery from an agent's own session record, the agents Herdr's
// detectors know, the setup stamp and the tab-bar cache.
package filesystem

import (
	"path/filepath"
	"regexp"
	"strings"
)

// defaultPluginID is the plugin id when Herdr did not inject one (a bare
// shell run). Herdr's convention is <github owner>.<repo>.
const defaultPluginID = "kriuchkov.herdr-scope"

// Env reads one environment variable.
type Env func(key string) string

// Paths is where everything lives, resolved the way Herdr lays it out so a
// process Herdr did not start (a key binding, a shell) finds the same files.
type Paths struct {
	PluginID string
	// PluginRoot is the installed or linked plugin directory.
	PluginRoot string
	// ConfigDir holds the plugin's config.toml.
	ConfigDir string
	// StateDir holds state, caches, backups and logs.
	StateDir string
	// RuntimeDir holds the per-session lock, control endpoint and log.
	RuntimeDir string
	// SessionSuffix is "" for the default Herdr session, "-<name>" otherwise.
	SessionSuffix string
	// Session is the named session ("" for the default one).
	Session string
	// HerdrConfig is Herdr's config.toml; HerdrStateDir its state directory.
	HerdrConfig   string
	HerdrStateDir string
	Socket        string
	Binary        string
	Home          string
}

// ResolvePaths derives every path from the environment. executableDir is
// the directory of the running binary (bin/ inside the plugin root).
func ResolvePaths(env Env, goos, home, executableDir string) Paths {
	p := Paths{Home: home}
	p.PluginID = firstNonEmpty(env("HERDR_PLUGIN_ID"), defaultPluginID)
	p.PluginRoot = firstNonEmpty(env("HERDR_PLUGIN_ROOT"), filepath.Dir(executableDir))
	p.HerdrConfig = herdrConfigPath(env, goos, home)
	p.HerdrStateDir = herdrStateDir(env, goos, home)
	p.ConfigDir = firstNonEmpty(env("HERDR_PLUGIN_CONFIG_DIR"),
		filepath.Join(filepath.Dir(p.HerdrConfig), "plugins", "config", p.PluginID))
	p.StateDir = firstNonEmpty(env("HERDR_SCOPE_STATE"), env("HERDR_PLUGIN_STATE_DIR"),
		filepath.Join(p.HerdrStateDir, "plugins", p.PluginID))
	p.Socket = firstNonEmpty(env("HERDR_SOCKET_PATH"), filepath.Join(filepath.Dir(p.HerdrConfig), "herdr.sock"))
	p.Binary = firstNonEmpty(env("HERDR_BIN_PATH"), "herdr")
	p.SessionSuffix = sessionSuffix(env("HERDR_SOCKET_PATH"))
	p.Session = strings.TrimPrefix(p.SessionSuffix, "-")
	p.RuntimeDir = p.StateDir
	if p.SessionSuffix != "" {
		p.RuntimeDir = filepath.Join(p.StateDir, "runtime"+p.SessionSuffix)
	}
	return p
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// herdrConfigPath: XDG_CONFIG_HOME wins everywhere, Windows included — Herdr
// honours it there too — and %APPDATA% is only the fallback.
func herdrConfigPath(env Env, goos, home string) string {
	if xdg := env("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "herdr", "config.toml")
	}
	base := filepath.Join(home, ".config")
	if goos == "windows" {
		base = firstNonEmpty(env("APPDATA"), filepath.Join(home, "AppData", "Roaming"))
	}
	return filepath.Join(base, "herdr", "config.toml")
}

// herdrStateDir is Herdr's state directory, not its config directory — a
// different XDG variable governs it.
func herdrStateDir(env Env, goos, home string) string {
	if xdg := env("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "herdr")
	}
	if goos == "windows" {
		return filepath.Join(firstNonEmpty(env("LOCALAPPDATA"), filepath.Join(home, "AppData", "Local")), "herdr")
	}
	return filepath.Join(home, ".local", "state", "herdr")
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// sessionSuffix names the Herdr session a socket belongs to. Named sessions
// listen at <config>/herdr/sessions/<name>/herdr.sock and each needs its own
// daemon; the default session's socket sits one level up and keeps the
// suffix empty.
func sessionSuffix(socket string) string {
	if socket == "" {
		return ""
	}
	dir := filepath.Dir(filepath.Clean(socket))
	if filepath.Base(filepath.Dir(dir)) != "sessions" {
		return ""
	}
	name := filepath.Base(dir)
	if name == "." || name == ".." {
		return ""
	}
	if safe := unsafeName.ReplaceAllString(name, "_"); safe != "" {
		return "-" + safe
	}
	return ""
}

// TabBarCache is the per-session tab-bar line. The reader is one command in
// config.toml serving every session, so the name comes from HERDR_SESSION,
// which only a named session's server has.
func (p Paths) TabBarCache(session string) string {
	if session != "" && session != "default" && !unsafeName.MatchString(session) {
		return filepath.Join(p.StateDir, "tabbar-"+session+".txt")
	}
	return filepath.Join(p.StateDir, "tabbar.txt")
}

// PluginConfig is the plugin's config.toml.
func (p Paths) PluginConfig() string { return filepath.Join(p.ConfigDir, "config.toml") }

// FontSource is the bundled icon font.
func (p Paths) FontSource() string {
	return filepath.Join(p.PluginRoot, "dist", "HerdrAgentIconsMax-Regular.ttf")
}

// Log is the daemon's log file.
func (p Paths) Log() string { return filepath.Join(p.RuntimeDir, "scope.log") }

// PIDFile records the daemon's pid, for diagnostics only.
func (p Paths) PIDFile() string { return filepath.Join(p.RuntimeDir, "scope.pid") }
