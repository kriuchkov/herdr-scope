package font

import (
	"strings"
	"unicode/utf16"
)

// Name IDs the patch reads or renames.
const (
	nameFamily       = 1
	nameUnique       = 3
	nameFull         = 4
	namePostScript   = 6
	nameTypoFamily   = 16
	nameWWSFamily    = 21
	platformUnicode  = 0
	platformMac      = 1
	platformWindows  = 3
	langTagThreshold = 0x8000
)

// familySuffix is what a patched face adds to its family's name; the
// PostScript name gets it without the space.
const familySuffix = " Herdr"

type nameRecord struct {
	platform, encoding, language, id uint16
	raw                              []byte
}

func (r nameRecord) text() string {
	switch r.platform {
	case platformUnicode, platformWindows:
		units := make([]uint16, len(r.raw)/2)
		for i := range units {
			units[i] = be16(r.raw[2*i:])
		}
		return string(utf16.Decode(units))
	case platformMac:
		// Mac Roman agrees with ASCII, which is all a family name uses.
		return string(r.raw)
	}
	return string(r.raw)
}

func (r nameRecord) with(text string) nameRecord {
	switch r.platform {
	case platformUnicode, platformWindows:
		units := utf16.Encode([]rune(text))
		r.raw = make([]byte, 2*len(units))
		for i, u := range units {
			put16(r.raw[2*i:], u)
		}
	default:
		r.raw = []byte(text)
	}
	return r
}

// parseName reads the name records. Records tied to a format 1 language tag
// are dropped: the table goes back out as format 0.
func parseName(t []byte) ([]nameRecord, error) {
	if len(t) < 6 {
		return nil, badFont("name table too short")
	}
	if format := be16(t); format > 1 {
		return nil, badFont("name table format %d", format)
	}
	count, storage := int(be16(t[2:])), int(be16(t[4:]))
	records := make([]nameRecord, 0, count)
	for i := range count {
		at := 6 + 12*i
		if at+12 > len(t) {
			return nil, badFont("name records truncated")
		}
		length, offset := int(be16(t[at+8:])), int(be16(t[at+10:]))
		if storage+offset+length > len(t) {
			return nil, badFont("name string out of range")
		}
		r := nameRecord{
			platform: be16(t[at:]), encoding: be16(t[at+2:]), language: be16(t[at+4:]), id: be16(t[at+6:]),
			raw: t[storage+offset : storage+offset+length],
		}
		if r.language < langTagThreshold {
			records = append(records, r)
		}
	}
	return records, nil
}

func buildName(records []nameRecord) []byte {
	storage := 6 + 12*len(records)
	out := append(make([]byte, 0, storage), make([]byte, storage)...)
	put16(out[2:], uint16(len(records)))
	put16(out[4:], uint16(storage))
	for i, r := range records {
		at := 6 + 12*i
		put16(out[at:], r.platform)
		put16(out[at+2:], r.encoding)
		put16(out[at+4:], r.language)
		put16(out[at+6:], r.id)
		put16(out[at+8:], uint16(len(r.raw)))
		put16(out[at+10:], uint16(len(out)-storage))
		out = append(out, r.raw...)
	}
	return out
}

// faceNames is what a face is called before and after the patch.
type faceNames struct {
	family, postScript       string
	newFamily, newPostScript string
}

func lookupName(records []nameRecord, id uint16) string {
	found := ""
	for _, r := range records {
		if r.id != id {
			continue
		}
		if r.platform == platformWindows {
			return r.text()
		}
		if found == "" {
			found = r.text()
		}
	}
	return found
}

// patchedNames derives the patched face's names. A face patched before
// keeps them: patching it again only refreshes the icons.
func patchedNames(records []nameRecord) (faceNames, error) {
	n := faceNames{family: lookupName(records, nameTypoFamily), postScript: lookupName(records, namePostScript)}
	if n.family == "" {
		n.family = lookupName(records, nameFamily)
	}
	if n.family == "" || n.postScript == "" {
		return faceNames{}, badFont("the face has no family or PostScript name")
	}
	n.newFamily, n.newPostScript = n.family, n.postScript
	if !strings.HasSuffix(n.family, familySuffix) {
		n.newFamily = n.family + familySuffix
	}
	base, style, styled := strings.Cut(n.postScript, "-")
	if suffix := strings.TrimPrefix(familySuffix, " "); !strings.HasSuffix(base, suffix) {
		n.newPostScript = base + suffix
		if styled {
			n.newPostScript += "-" + style
		}
	}
	return n, nil
}

// rename gives every family-bearing record the patched family, and the
// unique and PostScript names theirs.
func rename(records []nameRecord, n faceNames, stamp string) []nameRecord {
	out := make([]nameRecord, len(records))
	for i, r := range records {
		text := r.text()
		switch r.id {
		case nameFamily, nameFull, nameTypoFamily, nameWWSFamily:
			if !strings.Contains(text, n.newFamily) {
				text = strings.Replace(text, n.family, n.newFamily, 1)
			}
		case nameUnique:
			if before, _, cut := strings.Cut(text, "; herdr icons"); cut {
				text = before
			}
			if !strings.Contains(text, n.newFamily) {
				text = strings.Replace(text, n.family, n.newFamily, 1)
			}
			text += "; herdr icons " + stamp
		case namePostScript:
			text = n.newPostScript
		}
		out[i] = r.with(text)
	}
	return out
}
