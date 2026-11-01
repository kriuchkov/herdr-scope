package domain

import (
	"path/filepath"
	"regexp"
	"strings"
)

// TitleSources are the title fields Herdr reports for a pane, plus what the
// location-only test needs. Herdr reports the fields as string|null and an
// empty title as "", so emptiness, not absence, is what matters.
type TitleSources struct {
	// TerminalStripped is the terminal title with Herdr's own spinner removed.
	TerminalStripped string
	// Metadata is the title a reporter set; a mirrored pane has no terminal
	// title of its own and carries the remote's task title here.
	Metadata string
	// Terminal is the raw terminal title, last because it still carries the
	// mark an agent puts in front of its own title.
	Terminal string
	// Cwd is the pane's foreground directory, falling back to its launch one.
	Cwd string
	// Home is the user's home directory, for the "~" spelling of Cwd.
	Home string
}

// vendorPulse matches the attention bracket Codex animates into its terminal
// title while it waits ("[ ! ]" / "[ . ]"). The row pulses its own mark for
// that state, so the bracket would put two things blinking in one line.
var vendorPulse = regexp.MustCompile(`^\[\s*[!.·]\s*]\s*`)

// ResolveTitle picks the text a row shows for a pane: the first title field
// that says anything, without the vendor's attention bracket, and replaced by
// the vendor's name when it only names the pane's directory (the shell's
// default title) or says nothing at all.
func ResolveTitle(kind AgentKind, sources TitleSources) string {
	raw := firstSaid(sources.TerminalStripped, sources.Metadata, sources.Terminal)
	raw = vendorPulse.ReplaceAllString(raw, "")
	clean := strings.TrimSpace(raw)
	if clean == "" || locationOnly(clean, sources.Cwd, sources.Home) {
		if name, ok := kind.DisplayName(); ok {
			return name
		}
	}
	return clean
}

func firstSaid(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// locationOnly reports whether a title names the pane's directory and nothing
// else: the directory itself, its "~" form, its base name, or the shell's
// "<full path>: <job>" form. A title that merely opens with the base name
// ("herdr-scope: rewriting the parser") is a real title.
func locationOnly(title, cwd, home string) bool {
	if title == "" || cwd == "" {
		return false
	}
	if namesDirectory(title, cwd, home, true) {
		return true
	}
	separator := strings.Index(title, ": ")
	return separator > 0 && namesDirectory(title[:separator], cwd, home, false)
}

func namesDirectory(text, cwd, home string, allowBase bool) bool {
	return text == cwd || text == tilde(cwd, home) || (allowBase && text == filepath.Base(cwd))
}

// tilde spells dir the way a shell puts it in a title.
func tilde(dir, home string) string {
	if home == "" {
		return dir
	}
	if dir == home {
		return "~"
	}
	if strings.HasPrefix(dir, home+string(filepath.Separator)) {
		return "~" + dir[len(home):]
	}
	return dir
}
