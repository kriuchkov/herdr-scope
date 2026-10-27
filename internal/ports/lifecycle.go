package ports

import "context"

// DaemonProcess starts and ends the resident daemon as an OS process.
type DaemonProcess interface {
	// Spawn starts a detached daemon; note goes at the top of its log.
	Spawn(ctx context.Context, note string) error
	// Terminate ends a daemon that will not end itself. The pid must come
	// from the daemon's own answer, never from a pid file.
	Terminate(ctx context.Context, pid int) error
}

// FontInstaller installs the bundled icon font for the current user.
type FontInstaller interface {
	Installed() bool
	Install() ([]string, error)
	Uninstall() ([]string, error)
	// ConfigureTerminals maps the font's codepoints in terminal configs that
	// exist (Ghostty, kitty).
	ConfigureTerminals() ([]string, error)
	UnconfigureTerminals() ([]string, error)
}

// OffSwitch records that the user turned Scope off with unconfigure. The
// plugin stays installed and Herdr keeps running its startup and event hooks;
// those must not bring the daemon back. An explicit start or configure does.
type OffSwitch interface {
	Off() bool
	SetOff(off bool) error
}

// SetupStamp records that first-run setup happened, and for which build.
type SetupStamp interface {
	Read() (string, bool)
	Write(value string) error
}
