package tui

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/kriuchkov/herdr-scope/internal/adapters/config"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// SGR sequences, the popup's palette.
const (
	sgrReset   = "\x1b[0m"
	sgrBold    = "\x1b[1m"
	sgrDim     = "\x1b[2m"
	sgrInverse = "\x1b[7m"
	sgrAccent  = "\x1b[38;5;110m"
	sgrWarn    = "\x1b[38;5;179m"
	// clearScreen starts every frame: the popup redraws whole, never in place.
	clearScreen = "\x1b[2J\x1b[H"
)

// The help sits in a fixed number of rows so the hint line below it does not
// jump as the cursor moves between short and long descriptions.
const helpRows = 2

const (
	blankAboveTitleRows = 1
	titleRows           = 1
	listBoundaryRows    = 2
	gapBelowHelpRows    = 1
	keyHintRows         = 1
	trailingNewlineRows = 1
)

// fixedRows is every row of the popup that is not a setting.
const fixedRows = blankAboveTitleRows + titleRows + listBoundaryRows + helpRows + gapBelowHelpRows + keyHintRows + trailingNewlineRows

const (
	nameValueGap     = 2
	defaultPopupRows = 26
	minimumListRows  = 3
	statusRows       = 1
	// A popup narrower than minPopupCols draws as if it were that wide;
	// popupColsMargin keeps the last column free so a full line never wraps.
	defaultPopupCols = 84
	minPopupCols     = 60
	popupColsMargin  = 2
)

const (
	browseHint = "↑↓ select · ←→ change · ↵ edit · r default · s save & apply · q close"
	editHint   = "type a value · ↵ confirm · esc cancel"
)

var sgrPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// Render draws the whole popup for a terminal of cols by rows (0 when the
// size is unknown), screen clear included.
func (e *editor) Render(cols, rows int) string {
	if cols <= 0 {
		cols = defaultPopupCols
	}
	if rows <= 0 {
		rows = defaultPopupRows
	}
	cols = max(minPopupCols, cols-popupColsMargin)
	fields := e.deps.Fields
	room := listRoom(rows, e.status != "")
	e.top = scrollTop(len(fields), e.cursor, room, e.top)
	end := min(len(fields), e.top+room)
	keyW := e.nameColumnWidth()
	current := e.settings()

	out := []string{""}
	// Title left, plugin id right, the gap between them measured — not
	// guessed — so the pair fits the popup's width exactly and never wraps.
	title, id := e.deps.Title, e.deps.PluginID
	gap := max(1, cols-1-width(title)-width(id))
	out = append(out, " "+sgrBold+title+sgrReset+sgrDim+strings.Repeat(" ", gap)+id+sgrReset)
	out = append(out, moreMarker("↑", e.top))
	for i := e.top; i < end; i++ {
		out = append(out, e.row(i, keyW, current))
	}
	out = append(out, moreMarker("↓", len(fields)-end))
	help := wrap(e.field().Help, cols-1)
	for i := range helpRows {
		line := ""
		if i < len(help) {
			line = help[i]
		}
		out = append(out, " "+sgrDim+line+sgrReset)
	}
	out = append(out, "")
	hint := browseHint
	if e.editing {
		hint = editHint
	}
	out = append(out, " "+sgrDim+hint+sgrReset)
	if e.status != "" {
		out = append(out, " "+e.status)
	}
	return clearScreen + strings.Join(out, "\n") + "\n"
}

func (e *editor) row(i, keyW int, current ports.Settings) string {
	f := e.deps.Fields[i]
	selected := i == e.cursor
	value := show(f, e.values[i], current)
	if selected && e.editing {
		value = sgrInverse + e.buffer + sgrReset + sgrDim + "▏" + sgrReset
	}
	marker := " "
	if e.changed(i) {
		marker = sgrWarn + "*" + sgrReset
	}
	row := marker + " " + pad(f.Name(), keyW) + " " + value
	if selected {
		return " " + sgrAccent + "▸" + sgrReset + " " + sgrBold + row + sgrReset
	}
	return "   " + row
}

// nameColumnWidth fits the longest name, so every value starts in one column.
func (e *editor) nameColumnWidth() int {
	w := 0
	for _, f := range e.deps.Fields {
		w = max(w, width(f.Name()))
	}
	return w + nameValueGap
}

// listRoom is whatever the popup's height leaves the list.
func listRoom(rows int, hasStatus bool) int {
	room := rows - fixedRows
	if hasStatus {
		room -= statusRows
	}
	return max(minimumListRows, room)
}

// scrollTop is the first row to show of a list taller than its room, keeping
// the cursor on screen: the list scrolls only when the cursor would leave it,
// and never past its own end.
func scrollTop(count, cursor, room, top int) int {
	if count <= room {
		return 0
	}
	next := min(top, cursor)
	if cursor >= next+room {
		next = cursor - room + 1
	}
	return min(max(next, 0), count-room)
}

func moreMarker(arrow string, hidden int) string {
	if hidden <= 0 {
		return ""
	}
	return "   " + sgrDim + arrow + " " + strconv.Itoa(hidden) + " more" + sgrReset
}

// show is a row's value as the popup prints it. A config row at its default
// says so, dimmed, so a value someone chose stands out from one nobody did; a
// live switch has no default to fall back to, so it always reads plain. A
// default that follows another row (group_gap, the layout) is read off current.
func show(f config.Field, value string, current ports.Settings) string {
	if !f.Virtual && value == f.Default(current) {
		return sgrDim + sgrPattern.ReplaceAllString(showValue(f, value), "") + "  (default)" + sgrReset
	}
	return showValue(f, value)
}

func showValue(f config.Field, value string) string {
	switch f.Kind {
	case config.KindBool:
		if value == boolTrue {
			return "on"
		}
		return "off"
	case config.KindGlyph:
		if value == "" {
			return sgrDim + "none" + sgrReset
		}
		return config.Codepoint(value) + "  " + value
	case config.KindColor:
		if value == "" {
			return sgrDim + "theme's own" + sgrReset
		}
	case config.KindEnum, config.KindNumber:
	}
	return value
}

// wide reports whether r takes two cells: Hangul Jamo, CJK and Yi, Hangul
// syllables, CJK compatibility ideographs and forms, and the fullwidth and
// halfwidth forms (halfwidth included). Everything else,
// the icon font's private-use marks among it, is one cell.
func wide(r rune) bool {
	return r >= 0x1100 && r <= 0x11FF ||
		r >= 0x2E80 && r <= 0xA4CF ||
		r >= 0xAC00 && r <= 0xD7A3 ||
		r >= 0xF900 && r <= 0xFAFF ||
		r >= 0xFE30 && r <= 0xFE4F ||
		r >= 0xFF00 && r <= 0xFFEF
}

// width is the cells s takes on screen, colour codes excluded.
func width(s string) int {
	w := 0
	for _, r := range sgrPattern.ReplaceAllString(s, "") {
		w++
		if wide(r) {
			w++
		}
	}
	return w
}

func pad(s string, n int) string { return s + strings.Repeat(" ", max(0, n-width(s))) }

// wrap breaks a sentence at spaces so no line exceeds cols. The terminal
// would wrap it anyway, but mid-word and without the row's leading indent; a
// popup this narrow needs the break chosen, not suffered.
func wrap(text string, cols int) []string {
	var lines []string
	line := ""
	for word := range strings.SplitSeq(text, " ") {
		switch {
		case line == "":
			line = word
		case width(line)+1+width(word) > cols:
			lines = append(lines, line)
			line = word
		default:
			line += " " + word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
