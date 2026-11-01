package sidebar

import (
	"strings"
	"unicode"
)

// The golden renderer draws the panel the way Herdr would, from the managed
// layout and the tokens on each target: every row of a layout is one line,
// visible cells joined with " · ", a row with no visible cell dropped, an
// entry's first line indented one column and its continuation lines three.
// It exists so a change to the look shows up as a diff of plain text.

const (
	separator   = " · "
	firstIndent = " "
	contIndent  = "   "
	maxValueLen = 80
)

// Normalize is Herdr's own treatment of a reported token value: trimmed,
// control characters dropped, cut at 80 characters; empty means absent.
func Normalize(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	var b strings.Builder
	count := 0
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			continue
		}
		if count == maxValueLen {
			break
		}
		b.WriteRune(r)
		count++
	}
	out := strings.TrimSpace(b.String())
	return out, out != ""
}

// Entry is one target as the renderer sees it.
type Entry struct {
	// Rows is the layout the target is drawn with.
	Rows [][]Cell
	// Tokens are the target's token values as Herdr stores them.
	Tokens map[string]string
}

// RenderOptions tune the golden text.
type RenderOptions struct {
	// Annotate appends each visible cell's token name, so the golden text
	// also pins which state colour a title is drawn in.
	Annotate bool
}

// Render draws entries top to bottom.
func Render(entries []Entry, opts RenderOptions) string {
	var lines []string
	for _, e := range entries {
		first := true
		for _, row := range e.Rows {
			var cells []string
			for _, c := range row {
				value, ok := e.Tokens[strings.TrimPrefix(c.Token, "$")]
				if !ok {
					continue
				}
				text := strings.ReplaceAll(value, zwsp, "")
				if opts.Annotate {
					text += "{" + strings.TrimPrefix(c.Token, "$") + "}"
				}
				cells = append(cells, text)
			}
			if len(cells) == 0 {
				continue
			}
			indent := contIndent
			if first {
				indent = firstIndent
			}
			first = false
			lines = append(lines, strings.TrimRight(indent+strings.Join(cells, separator), " "))
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// Store is a token store per target that applies patches the way Herdr
// does, for tests and the reference comparison.
type Store map[string]map[string]string

// Apply merges a patch into one target.
func (s Store) Apply(target string, patch map[string]*string) {
	tokens, ok := s[target]
	if !ok {
		tokens = map[string]string{}
		s[target] = tokens
	}
	for name, value := range patch {
		if value == nil {
			delete(tokens, name)
			continue
		}
		if normalized, present := Normalize(*value); present {
			tokens[name] = normalized
		} else {
			delete(tokens, name)
		}
	}
}
