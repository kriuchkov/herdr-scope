package font

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

// iTerm2's preferences domain, read with `defaults export` (read-only: it
// prints the domain as an XML property list).
const (
	itermDomain    = "com.googlecode.iterm2"
	helperDefaults = "defaults"
)

// TerminalFont is the face a terminal draws private-use codepoints with.
type TerminalFont struct {
	Terminal string
	// Name is the face's PostScript name, Size its point size as written.
	Name, Size string
	// Path is the font file with that PostScript name among the user's and
	// the machine's fonts; empty when none was found.
	Path string
	// Icons is whether that face carries the bundled icons.
	Icons bool
}

// TerminalFont reports the face iTerm2's default profile draws private-use
// codepoints with — its non-ASCII font when the profile turns that on, its
// normal font otherwise — and whether that face carries the icons. It is the
// one terminal whose font Scope can read: false on other systems, or when
// iTerm2 has no preferences here.
func (i *Installer) TerminalFont() (TerminalFont, bool) {
	if i.opts.GOOS != goosDarwin {
		return TerminalFont{}, false
	}
	out, err := i.opts.Run(helperDefaults, "export", itermDomain, "-")
	if err != nil || strings.TrimSpace(out) == "" {
		return TerminalFont{}, false
	}
	spec, ok := itermProfileFont([]byte(out))
	if !ok {
		return TerminalFont{}, false
	}
	tf := TerminalFont{Terminal: "iTerm2", Name: spec}
	if at := strings.LastIndexByte(spec, ' '); at > 0 {
		tf.Name, tf.Size = spec[:at], spec[at+1:]
	}
	tf.Path, tf.Icons = i.findFace(tf.Name)
	return tf, true
}

// itermProfileFont is the font spec ("PostScriptName size") the default
// profile draws non-ASCII text with.
func itermProfileFont(plist []byte) (string, bool) {
	root, err := parsePlist(plist)
	if err != nil {
		return "", false
	}
	prefs, _ := root.(map[string]any)
	profiles, _ := prefs["New Bookmarks"].([]any)
	guid, _ := prefs["Default Bookmark Guid"].(string)
	var profile map[string]any
	for _, p := range profiles {
		if m, ok := p.(map[string]any); ok && (profile == nil || m["Guid"] == guid) {
			profile = m
			if m["Guid"] == guid {
				break
			}
		}
	}
	key := "Normal Font"
	if on, _ := profile["Use Non-ASCII Font"].(bool); on {
		key = "Non Ascii Font"
	}
	spec, ok := profile[key].(string)
	return spec, ok && spec != ""
}

// findFace looks for the font file whose PostScript name is name, in the
// user's font directory and the machine's, and says whether it carries the
// bundled icons (every codepoint of the bundled font).
func (i *Installer) findFace(name string) (string, bool) {
	icons, err := os.ReadFile(i.opts.Source)
	if err != nil {
		return "", false
	}
	source, err := parseSFNT(icons)
	if err != nil {
		return "", false
	}
	want, _, err := parseCmap(source.tables["cmap"])
	if err != nil {
		return "", false
	}
	for _, dir := range []string{i.UserFontDir(), "/Library/Fonts"} {
		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			continue
		}
		for _, entry := range entries {
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if ext != ".ttf" && ext != ".otf" {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			tables, faceErr := readFaceTables(path, "name", "cmap")
			if faceErr != nil {
				continue
			}
			records, nameErr := parseName(tables["name"])
			if nameErr != nil || lookupName(records, namePostScript) != name {
				continue
			}
			have, _, cmapErr := parseCmap(tables["cmap"])
			if cmapErr != nil {
				return path, false
			}
			for c := range want {
				if _, ok := have[c]; !ok {
					return path, false
				}
			}
			return path, true
		}
	}
	return "", false
}

// readFaceTables reads only the named tables of a font file: scanning a
// font directory by name must not read every face whole.
func readFaceTables(path string, tags ...string) (map[string][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrPersistence, err)
	}
	defer f.Close()
	header := make([]byte, 12)
	if _, err = io.ReadFull(f, header); err != nil {
		return nil, badFont("too short for a font file")
	}
	if v := be32(header); v != sfntTrueType && v != sfntApple && v != sfntCFF {
		return nil, badFont("not a single-face font")
	}
	directory := make([]byte, 16*int(be16(header[4:])))
	if _, err = io.ReadFull(f, directory); err != nil {
		return nil, badFont("truncated table directory")
	}
	out := map[string][]byte{}
	for at := 0; at < len(directory); at += 16 {
		tag := string(directory[at : at+4])
		for _, want := range tags {
			if tag != want {
				continue
			}
			t := make([]byte, be32(directory[at+12:]))
			if _, err = f.ReadAt(t, int64(be32(directory[at+8:]))); err != nil {
				return nil, badFont("table %q unreadable", tag)
			}
			out[tag] = t
		}
	}
	for _, tag := range tags {
		if out[tag] == nil {
			return nil, badFont("no %q table", tag)
		}
	}
	return out, nil
}

// parsePlist reads an XML property list: dict → map[string]any, array →
// []any, true/false → bool, every other value (string, integer, real, date,
// data) → its text.
func parsePlist(data []byte) (any, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: plist: %w", domain.ErrDecode, err)
		}
		if start, ok := tok.(xml.StartElement); ok && start.Name.Local != "plist" {
			return plistValue(dec, start)
		}
	}
}

var errPlistEnd = errors.New("end of container")

func plistValue(dec *xml.Decoder, start xml.StartElement) (any, error) {
	switch start.Name.Local {
	case "dict":
		m := map[string]any{}
		for {
			key, err := plistNext(dec)
			if errors.Is(err, errPlistEnd) {
				return m, nil
			}
			if err != nil {
				return nil, err
			}
			k, _ := key.(string)
			value, err := plistNext(dec)
			if err != nil {
				return nil, fmt.Errorf("%w: plist: dict key %q has no value", domain.ErrDecode, k)
			}
			m[k] = value
		}
	case "array":
		var list []any
		for {
			v, err := plistNext(dec)
			if errors.Is(err, errPlistEnd) {
				return list, nil
			}
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
	case "true", "false":
		if err := dec.Skip(); err != nil {
			return nil, fmt.Errorf("%w: plist: %w", domain.ErrDecode, err)
		}
		return start.Name.Local == "true", nil
	}
	var text string
	if err := dec.DecodeElement(&text, &start); err != nil {
		return nil, fmt.Errorf("%w: plist: %w", domain.ErrDecode, err)
	}
	return text, nil
}

// plistNext is the next value inside a container, or errPlistEnd at its end.
func plistNext(dec *xml.Decoder) (any, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: plist: %w", domain.ErrDecode, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			return plistValue(dec, t)
		case xml.EndElement:
			return nil, errPlistEnd
		}
	}
}
