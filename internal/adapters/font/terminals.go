package font

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

// The markers fence the block in a terminal config. Nothing outside them is
// ever edited.
const (
	markerStart = "# >>> HERDR-SCOPE-BEGIN font"
	markerEnd   = "# <<< HERDR-SCOPE-END font"
)

// fenced is the block with the blank lines in front of it, which upsert put
// there and drop takes away again.
var fenced = regexp.MustCompile(`\n*` + regexp.QuoteMeta(markerStart) + `[\s\S]*?` + regexp.QuoteMeta(markerEnd))

// ranges are the two codepoint ranges the font owns: vendor logos and state
// marks. The logo table lives in the sidebar adapter, which this one does
// not import, so a vendor added past U+E1BA
// must widen this range too — or its terminal keeps looking in its own font
// and draws nothing while install reports success. The state range is wider
// than the marks in use: E1C4 and E1C5 are in the font, unused.
func ranges() [][2]string {
	return [][2]string{{"E1A0", "E1BA"}, {"E1C0", "E1C5"}}
}

// terminal is one terminal whose config is a plain-text file at a known
// path, and the lines that map our codepoints to the font.
type terminal struct {
	name   string
	files  []string
	lines  []string
	reload string
}

func (i *Installer) terminals() []terminal {
	config := cmp.Or(i.opts.XDGConfigHome, filepath.Join(i.opts.Home, ".config"))
	appSupport := filepath.Join(i.opts.Home, "Library", "Application Support", "com.mitchellh.ghostty")
	// Ghostty reads `config` and, on macOS, also `config.ghostty` from the
	// same directories; a user who created the file from Finder has the
	// latter and nothing else.
	var ghosttyFiles []string
	for _, name := range []string{"config", "config.ghostty"} {
		ghosttyFiles = append(ghosttyFiles, filepath.Join(appSupport, name), filepath.Join(config, "ghostty", name))
	}
	// Codepoint maps only. Ghostty's `font-family` is not "also load this
	// font", it is the terminal's primary font — and ours holds nothing but
	// icons, so setting it sends every ordinary character through a face
	// that cannot draw it. The maps redirect our two ranges
	// whatever the primary font is. The family stays unquoted: Ghostty reads
	// quotes as part of the name, and then nothing matches.
	var ghosttyLines, kittyLines []string
	for _, r := range ranges() {
		ghosttyLines = append(ghosttyLines, fmt.Sprintf("font-codepoint-map = U+%s-U+%s=%s", r[0], r[1], iconFamily))
		kittyLines = append(kittyLines, fmt.Sprintf("symbol_map U+%s-U+%s %s", r[0], r[1], iconFamily))
	}
	return []terminal{
		{
			name: "ghostty", files: ghosttyFiles, lines: ghosttyLines,
			reload: "reload the config (cmd+shift+, on macOS) or reopen the terminal",
		},
		{
			name: "kitty", files: []string{filepath.Join(config, "kitty", "kitty.conf")}, lines: kittyLines,
			reload: "reload the config (ctrl+shift+f5) or reopen the terminal",
		},
	}
}

// ConfigureTerminals writes the codepoint map into every terminal config that
// exists. A missing config is never created: the user may not run that
// terminal, and a file they did not write is one more thing to leave behind.
func (i *Installer) ConfigureTerminals() ([]string, error) {
	var notes []string
	found := false
	for _, t := range i.terminals() {
		body := markerStart + "\n" + strings.Join(t.lines, "\n") + "\n" + markerEnd
		for _, file := range t.files {
			text, ok, err := readConfig(file)
			if err != nil {
				return notes, err
			}
			if !ok {
				continue
			}
			found = true
			next := upsert(text, body)
			if next == text {
				notes = append(notes, t.name+": codepoint map already in "+file)
				continue
			}
			if err = writeAtomic(file, []byte(next), 0o600); err != nil {
				return notes, fmt.Errorf("%w: %s: %w", domain.ErrConfiguration, t.name, err)
			}
			notes = append(notes, t.name+": codepoint map written to "+file+" — "+t.reload)
		}
	}
	if !found {
		notes = append(notes, "terminal: no ghostty/kitty config found. Point your terminal at the font by codepoint:")
		for _, r := range ranges() {
			notes = append(notes, fmt.Sprintf("  U+%s-U+%s -> %q", r[0], r[1], iconFamily))
		}
		notes = append(notes,
			"  (tty7: add the family to font_fallbacks; Windows Terminal / iTerm: no codepoint map, use the merged font instead)")
	}
	return notes, nil
}

// mapped reports whether any terminal config carries Scope's codepoint map.
func (i *Installer) mapped() bool {
	for _, t := range i.terminals() {
		for _, file := range t.files {
			if text, ok, err := readConfig(file); err == nil && ok && strings.Contains(text, markerStart) {
				return true
			}
		}
	}
	return false
}

// UnconfigureTerminals takes the block out of every terminal config that
// carries it, and leaves the rest of each file as it was.
func (i *Installer) UnconfigureTerminals() ([]string, error) {
	var notes []string
	for _, t := range i.terminals() {
		for _, file := range t.files {
			text, ok, err := readConfig(file)
			if err != nil {
				return notes, err
			}
			next := drop(text)
			if !ok || next == text {
				continue
			}
			if err = writeAtomic(file, []byte(next), 0o600); err != nil {
				return notes, fmt.Errorf("%w: %s: %w", domain.ErrConfiguration, t.name, err)
			}
			notes = append(notes, t.name+": codepoint map removed from "+file)
		}
	}
	return notes, nil
}

// readConfig reads a terminal config; false when there is none.
func readConfig(file string) (string, bool, error) {
	raw, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("%w: read %s: %w", domain.ErrConfiguration, file, err)
	}
	return string(raw), true, nil
}

// upsert replaces the block in place, or appends it after one blank line at
// the end of the file. The text around it is spliced, not run through a
// regexp replacement, so a `$` in the user's config stays a `$`.
func upsert(text, body string) string {
	if loc := fenced.FindStringIndex(text); loc != nil {
		head := text[:loc[0]]
		if head != "" {
			head += "\n\n"
		}
		return head + body + text[loc[1]:]
	}
	if kept := strings.TrimRight(text, "\n"); kept != "" {
		return kept + "\n\n" + body + "\n"
	}
	return body + "\n"
}

// drop removes the block and the blank lines around it, leaving one blank
// line where it sat between two parts of the file. A file that only gained
// the block at its end comes back exactly as it was.
func drop(text string) string {
	loc := fenced.FindStringIndex(text)
	if loc == nil {
		return text
	}
	head, tail := text[:loc[0]], strings.TrimLeft(text[loc[1]:], "\n")
	switch {
	case head == "":
		return tail
	case tail == "":
		return head + "\n"
	}
	return head + "\n\n" + tail
}

// writeAtomic replaces path with data so a terminal reading its config, or a
// font system scanning its directory, never sees half a file: a sibling
// temporary file, fsync, rename, fsync the directory. A symlinked path (a
// config kept in a dotfiles repo) is written at its target, so the link stays
// a link. An existing file keeps its mode; perm is for a new one.
func writeAtomic(path string, data []byte, perm os.FileMode) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}
	if info, err := os.Stat(target); err == nil {
		perm = info.Mode().Perm()
	}
	dir := filepath.Dir(target)
	// Dot-named, so neither a font scan nor a glance at the directory takes
	// it for the real file in the moment it exists.
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	name := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(name)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err = tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", name, err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsync %s: %w", name, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	if err = os.Rename(name, target); err != nil {
		return fmt.Errorf("rename into %s: %w", target, err)
	}
	keep = true
	syncDir(dir)
	return nil
}

// syncDir makes the rename durable. Best effort: not every platform can open
// a directory for syncing, and the rename has already happened.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	defer d.Close()
	_ = d.Sync()
}
