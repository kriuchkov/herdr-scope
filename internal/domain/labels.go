package domain

import (
	"regexp"
	"strings"
)

// prefixSeparator is what may follow a workspace name at the start of a
// title for the name to count as a prefix: a middot-like mark, a colon, or a
// spaced dash. `api-gateway` under a header of `api` has no boundary.
var prefixSeparator = regexp.MustCompile(`^(?:\s*[·・‧|»]\s*|\s*:\s+|\s+[-—–]\s+)`)

// TrimGroupPrefix drops the group's name from the front of a title when the
// header above already says it ("billing · fix rounding" under "billing").
// Only an exact match followed by a separator is dropped, and only when
// something is left.
func TrimGroupPrefix(title, header string) string {
	if header == "" || !strings.HasPrefix(title, header) {
		return title
	}
	rest := title[len(header):]
	separator := prefixSeparator.FindString(rest)
	if separator == "" {
		return title
	}
	if left := strings.TrimSpace(rest[len(separator):]); left != "" {
		return left
	}
	return title
}

// RowText is the two pieces of text that name a row.
type RowText struct {
	// TabLabel is written in front of the title (row_label = both).
	TabLabel string
	Title    string
}

// NamedTab reports whether a tab carries a name someone gave it. Herdr labels
// an unnamed tab with its number.
func NamedTab(label string) bool {
	if label == "" {
		return false
	}
	for _, r := range label {
		if r < '0' || r > '9' {
			return true
		}
	}
	return false
}

// RowTextFor names a row under the row-label setting. A tab-only row keeps
// its title when the tab was never named, so no row reads as a bare number.
func RowTextFor(label RowLabel, tabName, title string) RowText {
	tabName = strings.TrimSpace(tabName)
	switch label {
	case RowLabelTab:
		if NamedTab(tabName) {
			return RowText{Title: tabName}
		}
		return RowText{Title: title}
	case RowLabelBoth:
		return RowText{TabLabel: tabName, Title: title}
	case RowLabelTitle:
		return RowText{Title: title}
	}
	return RowText{Title: title}
}
