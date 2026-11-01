// Package config owns Scope's two configuration files: the plugin's own
// config.toml (settings) and the managed blocks inside Herdr's config.toml.
// Herdr's file is the user's: it is edited as text, line by line, never
// parsed and re-serialised, so comments and ordering survive.
package config

import (
	"regexp"
	"strings"
)

// tableSpan slices `[table]` and its bare keys out of text: from the header
// to the next header or the end.
type tableSpan struct {
	head, body, tail string
}

func findTable(text, table string) (tableSpan, bool) {
	header := regexp.MustCompile(`(?m)^\[` + regexp.QuoteMeta(table) + `\]\s*$`)
	loc := header.FindStringIndex(text)
	if loc == nil {
		return tableSpan{}, false
	}
	start := loc[0]
	rest := text[start+1:]
	next := regexp.MustCompile(`(?m)^\[`).FindStringIndex(rest)
	if next == nil {
		return tableSpan{head: text[:start], body: text[start:]}, true
	}
	end := start + 1 + next[0]
	return tableSpan{head: text[:start], body: text[start:end], tail: text[end:]}, true
}

// themeValue is the double-quoted value of a bare key in Herdr's `[theme]`.
func themeValue(text, key string) (string, bool) {
	span, ok := findTable(text, tableTheme)
	if !ok {
		return "", false
	}
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `\s*=\s*"([^"]+)"`).FindStringSubmatch(span.body)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// themeRaw is the raw right-hand side of a bare key in `[theme]` — quotes
// and all, minus a trailing comment — so it can be written back verbatim.
func themeRaw(text, key string) (string, bool) {
	span, ok := findTable(text, tableTheme)
	if !ok {
		return "", false
	}
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `\s*=\s*(.*)$`).FindStringSubmatch(span.body)
	if m == nil {
		return "", false
	}
	return strings.TrimSpace(regexp.MustCompile(`\s+#.*$`).ReplaceAllString(m[1], "")), true
}

// edit is one key edit: a verbatim TOML value, or Delete.
type edit struct {
	Key    string
	Value  string
	Delete bool
}

// editTable rewrites bare keys in `[table]`; false when the table is absent.
// Only uncommented assignments at the start of a line count: Herdr's file
// ships with a commented template of every key.
func editTable(text, table string, edits []edit) (string, bool) {
	span, ok := findTable(text, table)
	if !ok {
		return text, false
	}
	return span.head + applyEdits(span.body, edits) + span.tail, true
}

func applyEdits(body string, edits []edit) string {
	for _, e := range edits {
		key := regexp.QuoteMeta(e.Key)
		if e.Delete {
			body = replaceFirst(regexp.MustCompile(`(?m)^`+key+`\s*=.*\n?`), body, "")
			continue
		}
		line := e.Key + " = " + e.Value
		pattern := regexp.MustCompile(`(?m)^` + key + `\s*=.*$`)
		if pattern.MatchString(body) {
			body = replaceFirst(pattern, body, line)
		} else {
			body = strings.TrimRight(body, "\n") + "\n" + line + "\n"
		}
	}
	return body
}

// replaceFirst replaces the first match literally — a value may carry `$`.
func replaceFirst(re *regexp.Regexp, text, replacement string) string {
	loc := re.FindStringIndex(text)
	if loc == nil {
		return text
	}
	return text[:loc[0]] + replacement + text[loc[1]:]
}

// editTopLevel rewrites bare keys before the first table header; new keys are
// appended to that region so they never land inside a table by accident.
func editTopLevel(text string, edits []edit) string {
	first := regexp.MustCompile(`(?m)^\[`).FindStringIndex(text)
	head, tail := text, ""
	if first != nil {
		head, tail = text[:first[0]], text[first[0]:]
	}
	head = applyEdits(head, edits)
	if tail != "" && !strings.HasSuffix(head, "\n\n") {
		head = strings.TrimRight(head, "\n") + "\n\n"
	}
	return strings.TrimLeft(head, "\n") + tail
}

// upsertTail replaces the block between the start and end marker lines, or
// appends it at the end of the file.
func upsertTail(text, start, end, body string) string {
	pattern := regexp.MustCompile(`\n*` + regexp.QuoteMeta(start) + `[\s\S]*?` + regexp.QuoteMeta(end))
	if pattern.MatchString(text) {
		return replaceFirst(pattern, text, "\n\n"+body)
	}
	return strings.TrimRight(text, "\n") + "\n\n" + body + "\n"
}

// dropBlock removes the block between the markers and the blank lines
// upsertTail put around it, so an append followed by a drop gives back the
// text it started from.
func dropBlock(text, start, end string) string {
	pattern := regexp.MustCompile(`\n*` + regexp.QuoteMeta(start) + `[\s\S]*?` + regexp.QuoteMeta(end) + `\n*`)
	loc := pattern.FindStringIndex(text)
	if loc == nil {
		return text
	}
	replacement := "\n\n"
	switch {
	case loc[0] == 0:
		replacement = ""
	case loc[1] == len(text):
		replacement = "\n"
	}
	return text[:loc[0]] + replacement + text[loc[1]:]
}

// dropInline removes a block written inside a table (the tab-bar block, put
// right under [ui]'s header): the block and the one newline in front of it,
// nothing else.
func dropInline(text, start, end string) string {
	pattern := regexp.MustCompile(`\n?` + regexp.QuoteMeta(start) + `[\s\S]*?` + regexp.QuoteMeta(end))
	return replaceFirst(pattern, text, "")
}

// filler replaces what a string or comment holds that the scanner below
// could mistake for structure. Not an underscore: that would let a quoted
// "rows#by#agent" read as the bare key rows_by_agent.
const filler = '·'

// neutralize makes every string and comment inert: a comment is dropped, and
// inside a string each bracket, '#', '=', '.', backslash or newline becomes
// the filler. Quotes stay, so a quoted key is still a quoted key. One pass
// over characters, because TOML strings do not respect lines.
func neutralize(text string) string {
	runes := []rune(text)
	var out strings.Builder
	i := 0
	opens := func(delimiter string) bool {
		return strings.HasPrefix(string(runes[i:min(i+len(delimiter), len(runes))]), delimiter)
	}
	str := func(delimiter string, escapes bool) {
		out.WriteString(delimiter)
		i += len([]rune(delimiter))
		single := len(delimiter) == 1
		for i < len(runes) && !opens(delimiter) {
			c := runes[i]
			if single && c == '\n' {
				break
			}
			if escapes && c == '\\' {
				out.WriteRune(filler)
				out.WriteRune(filler)
				i += 2
				continue
			}
			if strings.ContainsRune("[]#=.\\\r\n", c) {
				out.WriteRune(filler)
			} else {
				out.WriteRune(c)
			}
			i++
		}
		if i < len(runes) && opens(delimiter) {
			out.WriteString(delimiter)
			i += len([]rune(delimiter))
			// Up to two more quotes belong to the string, not what follows.
			for extra := 0; !single && extra < 2 && i < len(runes) && runes[i] == rune(delimiter[0]); extra++ {
				out.WriteRune(filler)
				i++
			}
		}
	}
	for i < len(runes) {
		c := runes[i]
		switch {
		case c == '#':
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
		case opens(`"""`):
			str(`"""`, true)
		case c == '"':
			str(`"`, true)
		case opens(`'''`):
			str(`'''`, false)
		case c == '\'':
			str(`'`, false)
		default:
			out.WriteRune(c)
			i++
		}
	}
	return out.String()
}

// keyPath normalises a dotted key: whitespace and quotes dropped.
func keyPath(raw string) string {
	parts := strings.Split(raw, ".")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if len(part) >= 2 && (part[0] == '"' && part[len(part)-1] == '"' || part[0] == '\'' && part[len(part)-1] == '\'') {
			part = part[1 : len(part)-1]
		}
		parts[i] = part
	}
	return strings.Join(parts, ".")
}

var (
	headerLine = regexp.MustCompile(`^\[\[?\s*([^\]]+?)\s*\]\]?\s*$`)
	keyLine    = regexp.MustCompile(`^((?:[A-Za-z0-9_-]+|"[^"]*"|'[^']*')(?:\s*\.\s*(?:[A-Za-z0-9_-]+|"[^"]*"|'[^']*'))*)\s*=\s*(\{?)`)
)

// claimsTable reports whether text declares table in any form TOML allows —
// a header, a dotted key under a parent header or at the top level, an
// inline table on the way down. Each makes a later `[table]` header a parse
// error. A sub-table header is not a claim: TOML lets the parent be declared
// after it.
func claimsTable(text, table string) bool {
	under := table + "."
	current := ""
	open := 0
	brackets := func(s string) int { return strings.Count(s, "[") - strings.Count(s, "]") }
	for raw := range strings.SplitSeq(neutralize(text), "\n") {
		line := strings.TrimSpace(raw)
		if open > 0 {
			open += brackets(line)
			continue
		}
		if m := headerLine.FindStringSubmatch(line); m != nil {
			current = keyPath(m[1])
			if current == table {
				return true
			}
			continue
		}
		m := keyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		open = max(0, brackets(line[len(m[0])-len(m[2]):]))
		// Only the key's own segments define tables; the header's parents
		// stay open to a declaration later on.
		if current != "" && !strings.HasPrefix(table, current+".") {
			continue
		}
		full := keyPath(m[1])
		if current != "" {
			full = current + "." + full
		}
		if full == table || strings.HasPrefix(full, under) {
			return true
		}
		// An inline table is closed when its line ends: nothing can be added
		// to it or beneath it later.
		if m[2] != "" && strings.HasPrefix(table, full+".") {
			return true
		}
	}
	return false
}

// tomlString quotes s as a TOML basic string, escaping what TOML requires and
// anything that would be invisible in the file (private-use glyphs included).
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f || (r >= 0xE000 && r <= 0xF8FF) || r == 0x200b:
			b.WriteString(`\u` + strings.ToUpper(hex4(r)))
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func hex4(r rune) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[r>>12&0xf], digits[r>>8&0xf], digits[r>>4&0xf], digits[r&0xf]})
}
