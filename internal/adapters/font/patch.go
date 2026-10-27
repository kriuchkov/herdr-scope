package font

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
)

// patchedFace is a face with the icons grafted in.
type patchedFace struct {
	// File is the file name to install it under: its PostScript name.
	File string
	// Family is what a terminal's font picker lists it as.
	Family     string
	PostScript string
	Icons      int
	Data       []byte
}

// Tables a patch cannot keep: they hold one record per glyph (hdmx, LTSH,
// VDMX, vertical metrics) or sign the bytes it changes (DSIG). Every reader
// gets on without them.
func droppedTables() []string { return []string{"hdmx", "LTSH", "VDMX", "DSIG", "vhea", "vmtx"} }

// patch grafts the icon font's glyphs into a TrueType face, for terminals
// that draw every codepoint from one font: each glyph is scaled from the icon
// font's grid to the face's units, centred on the face's cell and mapped at
// its codepoint, over whatever the face drew there. The face is renamed
// (" Herdr" on the family) so it installs beside the original; a face patched
// before keeps its name.
func patch(base, icons []byte) (patchedFace, error) {
	face, err := parseSFNT(base)
	if err != nil {
		return patchedFace{}, err
	}
	for _, tag := range []string{"fvar", "gvar", "CFF ", "CFF2"} {
		if _, ok := face.tables[tag]; ok {
			return patchedFace{}, badFont("variable and CFF faces are not supported (%q); give a static TrueType face", tag)
		}
	}
	source, err := parseSFNT(icons)
	if err != nil {
		return patchedFace{}, err
	}
	glyphs, err := readGlyphs(face)
	if err != nil {
		return patchedFace{}, err
	}
	iconGlyphs, err := readGlyphs(source)
	if err != nil {
		return patchedFace{}, err
	}
	cmap, sequences, err := parseCmap(face.tables["cmap"])
	if err != nil {
		return patchedFace{}, err
	}
	iconMap, _, err := parseCmap(source.tables["cmap"])
	if err != nil {
		return patchedFace{}, err
	}
	names, err := parseName(face.tables["name"])
	if err != nil {
		return patchedFace{}, err
	}
	renamed, err := patchedNames(names)
	if err != nil {
		return patchedFace{}, err
	}

	codes, err := graft(glyphs, iconGlyphs, cmap, iconMap)
	if err != nil {
		return patchedFace{}, err
	}

	sum := sha256.Sum256(icons)
	if err = glyphs.write(face); err != nil {
		return patchedFace{}, err
	}
	face.tables["cmap"] = buildCmap(cmap, sequences)
	face.tables["name"] = buildName(rename(names, renamed, hex.EncodeToString(sum[:4])))
	patchPost(face)
	patchOS2(face, codes)
	for _, tag := range droppedTables() {
		delete(face.tables, tag)
	}
	return patchedFace{
		File: renamed.newPostScript + ".ttf", Family: renamed.newFamily, PostScript: renamed.newPostScript,
		Icons: len(codes), Data: face.bytes(),
	}, nil
}

// graft adds every icon to glyphs, scaled to its grid and centred on its
// cell, maps it in cmap and returns the codepoints in order.
func graft(glyphs, icons *glyphSet, cmap, iconMap map[rune]uint16) ([]rune, error) {
	cell := glyphs.cell(cmap)
	scale := float64(glyphs.upm) / float64(icons.upm)
	codes := make([]rune, 0, len(iconMap))
	for c := range iconMap {
		codes = append(codes, c)
	}
	slices.Sort(codes)
	for _, c := range codes {
		g := iconMap[c]
		if int(g) >= len(icons.data) {
			return nil, badFont("icon U+%04X names glyph %d of %d", c, g, len(icons.data))
		}
		o, err := decodeGlyph(icons.data[g])
		if err != nil {
			return nil, err
		}
		// A glyph is drawn with its left side bearing point at the origin:
		// where hmtx's bearing and the outline's xMin disagree, the outline
		// moves by their difference. The patched glyph keeps them equal.
		shift := 0.0
		if len(o.points) > 0 {
			shift = float64(icons.lsb[g] - int(int16(be16(icons.data[g][2:]))))
		}
		width := float64(icons.advance[g]) * scale
		cmap[c] = glyphs.add(o.transform(scale, (float64(cell)-width)/2+shift*scale), cell)
	}
	return codes, nil
}

// glyphSet is a face's glyphs and horizontal metrics, as glyph-indexed
// slices, open for appending.
type glyphSet struct {
	upm     int
	data    [][]byte
	advance []int
	lsb     []int
	// bounds and the largest contour and point counts of the added glyphs.
	bounded                bool
	xMin, yMin, xMax, yMax int
	points, contours       int
}

func readGlyphs(f *sfnt) (*glyphSet, error) {
	head, err := f.table(tagHead, 54)
	if err != nil {
		return nil, err
	}
	maxp, err := f.table("maxp", 6)
	if err != nil {
		return nil, err
	}
	hhea, err := f.table("hhea", 36)
	if err != nil {
		return nil, err
	}
	count, metrics := int(be16(maxp[4:])), int(be16(hhea[34:]))
	if metrics == 0 || metrics > count {
		return nil, badFont("hhea.numberOfHMetrics %d out of range", metrics)
	}
	hmtx, err := f.table("hmtx", 4*metrics+2*(count-metrics))
	if err != nil {
		return nil, err
	}
	offsets, err := locaOffsets(f, count, int16(be16(head[50:])) == 1)
	if err != nil {
		return nil, err
	}
	glyf, err := f.table("glyf", 0)
	if err != nil {
		return nil, err
	}
	s := &glyphSet{upm: int(be16(head[18:])), data: make([][]byte, count), advance: make([]int, count), lsb: make([]int, count)}
	if s.upm == 0 {
		return nil, badFont("head.unitsPerEm is zero")
	}
	for i := range count {
		start, end := offsets[i], offsets[i+1]
		if start > end || end > len(glyf) {
			return nil, badFont("glyph %d out of range", i)
		}
		s.data[i] = glyf[start:end]
		if i < metrics {
			s.advance[i], s.lsb[i] = int(be16(hmtx[4*i:])), int(int16(be16(hmtx[4*i+2:])))
		} else {
			s.advance[i], s.lsb[i] = s.advance[metrics-1], int(int16(be16(hmtx[4*metrics+2*(i-metrics):])))
		}
	}
	return s, nil
}

func locaOffsets(f *sfnt, count int, long bool) ([]int, error) {
	size := 2
	if long {
		size = 4
	}
	loca, err := f.table("loca", size*(count+1))
	if err != nil {
		return nil, err
	}
	offsets := make([]int, count+1)
	for i := range offsets {
		if long {
			offsets[i] = int(be32(loca[4*i:]))
		} else {
			offsets[i] = 2 * int(be16(loca[2*i:]))
		}
	}
	return offsets, nil
}

// cell is the face's character width: its digit zero's, else its space's,
// else its widest glyph's.
func (s *glyphSet) cell(cmap map[rune]uint16) int {
	for _, c := range []rune{'0', ' '} {
		if g, ok := cmap[c]; ok && int(g) < len(s.advance) && s.advance[g] > 0 {
			return s.advance[g]
		}
	}
	return slices.Max(s.advance)
}

func (s *glyphSet) add(o outline, advance int) uint16 {
	b := o.bounds()
	switch {
	case len(o.points) == 0:
	case !s.bounded:
		s.bounded = true
		s.xMin, s.yMin, s.xMax, s.yMax = b.xMin, b.yMin, b.xMax, b.yMax
	default:
		s.xMin, s.yMin = min(s.xMin, b.xMin), min(s.yMin, b.yMin)
		s.xMax, s.yMax = max(s.xMax, b.xMax), max(s.yMax, b.yMax)
	}
	s.points, s.contours = max(s.points, len(o.points)), max(s.contours, len(o.ends))
	s.data = append(s.data, o.encode())
	s.advance = append(s.advance, advance)
	s.lsb = append(s.lsb, b.xMin)
	return uint16(len(s.data) - 1)
}

// write puts the glyphs back: glyf with a long loca, one full metric per
// glyph, and the counts and bounds that follow from them.
func (s *glyphSet) write(f *sfnt) error {
	count := len(s.data)
	if count > 0xFFFF {
		return badFont("more than 65535 glyphs once patched")
	}
	var glyf []byte
	loca := make([]byte, 4*(count+1))
	hmtx := make([]byte, 4*count)
	widest := 0
	for i, g := range s.data {
		put32(loca[4*i:], uint32(len(glyf)))
		glyf = append(glyf, g...)
		for len(glyf)%4 != 0 {
			glyf = append(glyf, 0)
		}
		put16(hmtx[4*i:], uint16(s.advance[i]))
		put16(hmtx[4*i+2:], uint16(int16(s.lsb[i])))
		widest = max(widest, s.advance[i])
	}
	put32(loca[4*count:], uint32(len(glyf)))
	f.tables["glyf"], f.tables["loca"], f.tables["hmtx"] = glyf, loca, hmtx

	head := slices.Clone(f.tables[tagHead])
	put16(head[50:], 1)
	if s.bounded {
		widen := func(at int, v int, keep func(a, b int) int) {
			put16(head[at:], uint16(int16(keep(int(int16(be16(head[at:]))), v))))
		}
		widen(36, s.xMin, minInt)
		widen(38, s.yMin, minInt)
		widen(40, s.xMax, maxInt)
		widen(42, s.yMax, maxInt)
	}
	f.tables[tagHead] = head

	hhea := slices.Clone(f.tables["hhea"])
	put16(hhea[10:], uint16(max(int(be16(hhea[10:])), widest)))
	put16(hhea[34:], uint16(count))
	f.tables["hhea"] = hhea

	maxp := slices.Clone(f.tables["maxp"])
	put16(maxp[4:], uint16(count))
	if len(maxp) >= 10 {
		put16(maxp[6:], uint16(max(int(be16(maxp[6:])), s.points)))
		put16(maxp[8:], uint16(max(int(be16(maxp[8:])), s.contours)))
	}
	f.tables["maxp"] = maxp
	return nil
}

// patchPost drops glyph names (post format 2 lists one per glyph, and the
// new glyphs have none); terminals never read them.
func patchPost(f *sfnt) {
	post, ok := f.tables["post"]
	if !ok || len(post) < 32 || be32(post) != 0x00020000 {
		return
	}
	post = slices.Clone(post[:32])
	put32(post, 0x00030000)
	f.tables["post"] = post
}

// patchOS2 widens the first and last character indices to the icons and
// sets the Private Use Area bit of the Unicode ranges.
func patchOS2(f *sfnt, codes []rune) {
	os2, ok := f.tables["OS/2"]
	if !ok || len(os2) < 68 || len(codes) == 0 {
		return
	}
	os2 = slices.Clone(os2)
	const puaBit = 1 << 28 // bit 60 of ulUnicodeRange, in its second word
	put32(os2[46:], be32(os2[46:])|puaBit)
	first, last := rune(be16(os2[64:])), rune(be16(os2[66:]))
	put16(os2[64:], uint16(min(first, codes[0])))
	put16(os2[66:], uint16(min(max(last, codes[len(codes)-1]), 0xFFFF)))
	f.tables["OS/2"] = os2
}

func minInt(a, b int) int { return min(a, b) }
func maxInt(a, b int) int { return max(a, b) }
