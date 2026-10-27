// Package font installs the bundled icon font for the current user and maps
// its codepoints in the terminal configs that already exist.
//
// Per-user font directories need no elevation on any platform: macOS and
// Linux just take the file; Windows takes the file plus one value under HKCU
// (the "install for me" Windows 10 added). Nothing here ever asks for
// administrator rights.
package font

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// iconFamily is the icon font's family name. It never changes between builds: a
// font system asked for a family it already resolved opens whatever file it
// points at now, so a rebuilt font under the same name reaches a running
// terminal, and every codepoint map already written keeps matching.
const iconFamily = "Herdr Agent Icons Max"

// baseName starts every installed copy's file name; a copy is ours by it.
const baseName = "HerdrAgentIconsMax"

// The per-user registration on Windows: one value naming the file.
const (
	regKey   = `HKCU\Software\Microsoft\Windows NT\CurrentVersion\Fonts`
	regValue = iconFamily + " (TrueType)"
)

// The helpers the installer runs, and how long each may take.
const (
	helperReg     = "reg"
	helperFcCache = "fc-cache"
	helperFcMatch = "fc-match"
	helperTimeout = 30 * time.Second
	regTimeout    = 10 * time.Second
	probeTimeout  = 3 * time.Second
)

// The platforms whose font handling differs.
const (
	goosDarwin  = "darwin"
	goosLinux   = "linux"
	goosWindows = "windows"
)

// A font is not a secret: the file is world-readable like any copied font,
// and only the user writes the directory.
const (
	fontFileMode = 0o644
	fontDirMode  = 0o750
)

// Options locate the font and the per-user directories. They all come from
// the caller, so tests point them at temporary directories.
type Options struct {
	// Source is the bundled TTF (dist/HerdrAgentIconsMax-Regular.ttf in the
	// plugin root).
	Source string
	// GOOS is runtime.GOOS outside tests.
	GOOS string
	Home string
	// XDGDataHome, XDGConfigHome and LocalAppData may be empty; each then
	// takes its usual default under Home.
	XDGDataHome   string
	XDGConfigHome string
	LocalAppData  string
	// Run executes a helper (fc-cache, fc-match, reg) and returns its stdout;
	// nil runs the real binary.
	Run func(name string, args ...string) (string, error)
}

// Installer implements ports.FontInstaller.
type Installer struct {
	opts Options
}

// New binds an installer to opts.
func New(opts Options) *Installer {
	if opts.Run == nil {
		opts.Run = runHelper
	}
	return &Installer{opts: opts}
}

// UserFontDir is the per-user font directory.
func (i *Installer) UserFontDir() string {
	switch i.opts.GOOS {
	case goosDarwin:
		return filepath.Join(i.opts.Home, "Library", "Fonts")
	case goosWindows:
		local := cmp.Or(i.opts.LocalAppData, filepath.Join(i.opts.Home, "AppData", "Local"))
		return filepath.Join(local, "Microsoft", "Windows", "Fonts")
	}
	return filepath.Join(cmp.Or(i.opts.XDGDataHome, filepath.Join(i.opts.Home, ".local", "share")), "fonts")
}

// InstalledPath is the installed copy of the font shipped here, if any. A
// copy of an older build does not count: it lacks whatever the rebuild added.
func (i *Installer) InstalledPath() (string, bool) {
	target, _, err := i.target()
	if err != nil {
		return "", false
	}
	if _, err = os.Stat(target); err != nil {
		return "", false
	}
	return target, true
}

// Installed reports whether the font shipped here is installed.
func (i *Installer) Installed() bool {
	_, ok := i.InstalledPath()
	return ok
}

// available reports whether a terminal can reach the font, for the auto
// glyph variant. The copy installed here counts everywhere; besides it only
// fontconfig can be asked, so on macOS and Windows a font installed by hand
// still reads as absent and auto stays with text.
func (i *Installer) available() bool {
	if i.Installed() {
		return true
	}
	if i.opts.GOOS != goosLinux {
		return false
	}
	// fc-match always answers with some face — its best fallback when the
	// family is missing — so only our family named back means it is there.
	out, err := i.opts.Run(helperFcMatch, "--format", "%{family}\n", iconFamily)
	if err != nil {
		return false
	}
	for name := range strings.FieldsFuncSeq(out, func(r rune) bool { return r == '\n' || r == ',' }) {
		if strings.TrimSpace(name) == iconFamily {
			return true
		}
	}
	return false
}

// Renderable is whether the icons will draw rather than show as boxes. On
// Linux fontconfig's fallback reaches any face it knows. Elsewhere a copy in
// the font directory is not enough: iTerm2, Terminal.app and Windows
// Terminal never look in it for a private-use codepoint, so only a terminal
// whose config maps the codepoints to the face (Ghostty, kitty) counts.
func (i *Installer) Renderable() bool {
	if i.opts.GOOS == goosLinux {
		return i.available()
	}
	if i.Installed() && i.mapped() {
		return true
	}
	// A terminal whose own font carries the icons (font patch) needs no map.
	tf, ok := i.TerminalFont()
	return ok && tf.Icons
}

// PatchFonts writes a copy of each face with the bundled icons grafted in
// (patch), for terminals that draw every codepoint from one font. The copies
// go into dir, or are installed in the user font directory and registered
// when dir is empty; the original faces are not touched.
func (i *Installer) PatchFonts(paths []string, dir string) ([]string, error) {
	icons, err := os.ReadFile(i.opts.Source)
	if err != nil {
		return nil, fmt.Errorf("%w: read bundled font: %w", domain.ErrPersistence, err)
	}
	install := dir == ""
	if install {
		dir = i.UserFontDir()
	}
	if err = os.MkdirAll(dir, fontDirMode); err != nil {
		return nil, fmt.Errorf("%w: create %s: %w", domain.ErrPersistence, dir, err)
	}
	var notes []string
	for _, path := range paths {
		base, readErr := os.ReadFile(path)
		if readErr != nil {
			return notes, fmt.Errorf("%w: %w", domain.ErrPersistence, readErr)
		}
		patched, patchErr := patch(base, icons)
		if patchErr != nil {
			return notes, fmt.Errorf("%s: %w", filepath.Base(path), patchErr)
		}
		target := filepath.Join(dir, patched.File)
		if err = writeAtomic(target, patched.Data, fontFileMode); err != nil {
			return notes, fmt.Errorf("%w: write %s: %w", domain.ErrPersistence, target, err)
		}
		notes = append(notes, fmt.Sprintf("font: %s -> %s (%q, %d icons)", filepath.Base(path), target, patched.Family, patched.Icons))
		if install {
			notes = append(notes, i.register(target, patched.Family)...)
		}
	}
	return notes, nil
}

// Install copies the font into the user font directory, registers it where
// the platform asks for that, and removes the copies older builds left.
func (i *Installer) Install() ([]string, error) {
	target, data, err := i.target()
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(target)
	if err = os.MkdirAll(dir, fontDirMode); err != nil {
		return nil, fmt.Errorf("%w: create %s: %w", domain.ErrPersistence, dir, err)
	}
	var notes []string
	if _, err = os.Stat(target); err == nil {
		notes = append(notes, "font: already installed ("+filepath.Base(target)+")")
	} else {
		// Atomic, because a half-written copy would carry the right name and
		// every later install would skip it as already there.
		if err = writeAtomic(target, data, fontFileMode); err != nil {
			return nil, fmt.Errorf("%w: install font: %w", domain.ErrPersistence, err)
		}
		notes = append(notes, "font: installed "+target)
	}
	notes = append(notes, i.register(target, iconFamily)...)
	// Older copies from previous builds: remove the ones nothing holds open.
	for _, file := range i.copies() {
		if file == target {
			continue
		}
		if err = os.Remove(file); err != nil {
			notes = append(notes, "font: "+filepath.Base(file)+" is in use, left for now (restart the terminal and run again)")
			continue
		}
		notes = append(notes, "font: removed old "+filepath.Base(file))
	}
	return notes, nil
}

// Uninstall removes the registration and every copy of the font.
func (i *Installer) Uninstall() ([]string, error) {
	var notes []string
	if i.opts.GOOS == goosWindows {
		// A refusal here means nothing was registered: no news to report.
		if _, err := i.opts.Run(helperReg, "delete", regKey, "/v", regValue, "/f"); err == nil {
			notes = append(notes, "font: registry value removed")
		}
	}
	for _, file := range i.copies() {
		if err := os.Remove(file); err != nil {
			notes = append(notes, "font: "+filepath.Base(file)+" is in use, remove it after restarting the terminal")
			continue
		}
		notes = append(notes, "font: removed "+filepath.Base(file))
	}
	if i.opts.GOOS == goosLinux {
		_, _ = i.opts.Run(helperFcCache, "-f", i.UserFontDir())
	}
	if len(notes) == 0 {
		notes = append(notes, "font: nothing installed")
	}
	return notes, nil
}

// target is where the font shipped here installs, named after its content.
// That is load-bearing rather than tidy: a rebuilt font reaches a terminal
// that is already running only if it arrives as a NEW path. Font systems
// cache a file by path and timestamp, and a terminal holding the old file
// open may keep serving it with no way to tell — so an installed path is
// never written twice, and older copies are deleted afterwards instead.
func (i *Installer) target() (string, []byte, error) {
	data, err := os.ReadFile(i.opts.Source)
	if err != nil {
		return "", nil, fmt.Errorf("%w: read bundled font: %w", domain.ErrPersistence, err)
	}
	sum := sha256.Sum256(data)
	return filepath.Join(i.UserFontDir(), baseName+"-"+hex.EncodeToString(sum[:4])+".ttf"), data, nil
}

// register tells the platform about target, a face of family. A helper that
// fails is a note, not a failed install: the file is in place and a terminal
// restart or a second run may be all it takes.
func (i *Installer) register(target, family string) []string {
	switch i.opts.GOOS {
	case goosWindows:
		// Re-pointing the value is how a rebuilt font takes over while a
		// terminal still locks the old file.
		value := family + " (TrueType)"
		if _, err := i.opts.Run(helperReg, "add", regKey, "/v", value, "/t", "REG_SZ", "/d", target, "/f"); err != nil {
			return []string{"font: registry write failed: " + err.Error()}
		}
		return []string{"font: registered for this user (HKCU)"}
	case goosLinux:
		if _, err := i.opts.Run(helperFcCache, "-f", filepath.Dir(target)); err != nil {
			return []string{"font: fc-cache not run (fontconfig missing?)"}
		}
		return []string{"font: fontconfig cache refreshed"}
	}
	return nil
}

// copies lists every copy of the font in the user font directory, ours by
// name. An unreadable directory holds no copies we could remove anyway.
func (i *Installer) copies() []string {
	dir := i.UserFontDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, entry := range entries {
		if name := entry.Name(); strings.HasPrefix(name, baseName) && strings.HasSuffix(name, ".ttf") {
			out = append(out, filepath.Join(dir, name))
		}
	}
	return out
}

// runHelper runs one of the helpers above within its timeout.
func runHelper(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), helperTimeoutOf(name))
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return stdout.String(), fmt.Errorf("%s: %w: %s", name, err, msg)
		}
		return stdout.String(), fmt.Errorf("%s: %w", name, err)
	}
	return stdout.String(), nil
}

// helperTimeoutOf is how long a helper may run. fc-match and defaults are
// asked on the way to choosing a glyph variant — by any command that paints
// — so they get seconds, not the half minute a cold fc-cache can take.
func helperTimeoutOf(name string) time.Duration {
	switch name {
	case helperFcMatch, helperDefaults:
		return probeTimeout
	case helperReg:
		return regTimeout
	}
	return helperTimeout
}

var _ ports.FontInstaller = (*Installer)(nil)
