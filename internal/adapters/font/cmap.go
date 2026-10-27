package font

import (
	"slices"
)

// cmapEncoding is one (platform, encoding) pair of a cmap subtable.
type cmapEncoding struct{ platform, encoding uint16 }

// cmapReadOrder is the Unicode subtables to read the mapping from, fullest
// first: the whole repertoire (format 12 usually), then the BMP ones.
func cmapReadOrder() []cmapEncoding {
	return []cmapEncoding{{3, 10}, {0, 4}, {0, 6}, {3, 1}, {0, 3}, {0, 2}, {0, 1}, {0, 0}}
}

// cmapSequences is where variation sequences (format 14) live.
func cmapSequences() cmapEncoding { return cmapEncoding{0, 5} }

// parseCmap is the font's Unicode mapping, from its fullest subtable, and
// its variation-sequence subtable as is (nil when there is none).
func parseCmap(t []byte) (map[rune]uint16, []byte, error) {
	if len(t) < 4 {
		return nil, nil, badFont("cmap table too short")
	}
	subtables := map[cmapEncoding][]byte{}
	for i := range int(be16(t[2:])) {
		record := 4 + 8*i
		if record+8 > len(t) {
			return nil, nil, badFont("cmap records truncated")
		}
		offset := int(be32(t[record+4:]))
		if offset >= len(t) {
			return nil, nil, badFont("cmap subtable out of range")
		}
		subtables[cmapEncoding{be16(t[record:]), be16(t[record+2:])}] = t[offset:]
	}
	sequences := subtables[cmapSequences()]
	if len(sequences) >= 6 && be16(sequences) == 14 {
		sequences = sequences[:min(len(sequences), int(be32(sequences[2:])))]
	} else {
		sequences = nil
	}
	for _, enc := range cmapReadOrder() {
		sub, ok := subtables[enc]
		if !ok || len(sub) < 2 {
			continue
		}
		switch be16(sub) {
		case 12:
			m, err := parseFormat12(sub)
			return m, sequences, err
		case 4:
			m, err := parseFormat4(sub)
			return m, sequences, err
		}
	}
	return nil, nil, badFont("no Unicode cmap subtable in format 4 or 12")
}

func parseFormat12(sub []byte) (map[rune]uint16, error) {
	if len(sub) < 16 {
		return nil, badFont("cmap format 12 truncated")
	}
	groups := int(be32(sub[12:]))
	if 16+12*groups > len(sub) {
		return nil, badFont("cmap format 12 groups truncated")
	}
	m := map[rune]uint16{}
	for g := range groups {
		at := 16 + 12*g
		start, end, glyph := be32(sub[at:]), be32(sub[at+4:]), be32(sub[at+8:])
		for c := start; c <= end && c <= 0x10FFFF; c++ {
			m[rune(c)] = uint16(glyph + c - start)
		}
	}
	return m, nil
}

func parseFormat4(sub []byte) (map[rune]uint16, error) {
	if len(sub) < 14 {
		return nil, badFont("cmap format 4 truncated")
	}
	segments := int(be16(sub[6:])) / 2
	ends, starts := 14, 16+2*segments
	deltas, ranges := starts+2*segments, starts+4*segments
	if ranges+2*segments > len(sub) {
		return nil, badFont("cmap format 4 segments truncated")
	}
	m := map[rune]uint16{}
	for s := range segments {
		start, end := int(be16(sub[starts+2*s:])), int(be16(sub[ends+2*s:]))
		delta, offset := be16(sub[deltas+2*s:]), int(be16(sub[ranges+2*s:]))
		for c := start; c <= end && c < 0xFFFF; c++ {
			glyph := uint16(c) + delta
			if offset != 0 {
				at := ranges + 2*s + offset + 2*(c-start)
				if at+2 > len(sub) {
					return nil, badFont("cmap format 4 glyph index out of range")
				}
				if glyph = be16(sub[at:]); glyph != 0 {
					glyph += delta
				}
			}
			if glyph != 0 {
				m[rune(c)] = glyph
			}
		}
	}
	return m, nil
}

// buildCmap writes a mapping as a format 12 subtable for the whole
// repertoire and a format 4 one for the BMP (left out when the BMP is too
// scattered for format 4's 64 KiB), plus the variation sequences as they
// were. Glyph indices of the sequences do not move: glyphs are only added.
func buildCmap(m map[rune]uint16, sequences []byte) []byte {
	codes := make([]rune, 0, len(m))
	for c := range m {
		codes = append(codes, c)
	}
	slices.Sort(codes)
	type subtable struct {
		encs []cmapEncoding
		data []byte
	}
	subs := []subtable{{[]cmapEncoding{{0, 4}, {3, 10}}, format12(codes, m)}}
	if bmp, ok := format4(codes, m); ok {
		subs = append(subs, subtable{[]cmapEncoding{{0, 3}, {3, 1}}, bmp})
	}
	if sequences != nil {
		subs = append(subs, subtable{[]cmapEncoding{cmapSequences()}, sequences})
	}
	type record struct {
		enc cmapEncoding
		sub int
	}
	var records []record
	for i, s := range subs {
		for _, enc := range s.encs {
			records = append(records, record{enc, i})
		}
	}
	slices.SortFunc(records, func(a, b record) int {
		if a.enc.platform != b.enc.platform {
			return int(a.enc.platform) - int(b.enc.platform)
		}
		return int(a.enc.encoding) - int(b.enc.encoding)
	})
	out := append(make([]byte, 0, 4+8*len(records)), make([]byte, 4+8*len(records))...)
	put16(out[2:], uint16(len(records)))
	offsets := make([]int, len(subs))
	for i, s := range subs {
		offsets[i] = len(out)
		out = append(out, s.data...)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
	}
	for i, r := range records {
		put16(out[4+8*i:], r.enc.platform)
		put16(out[6+8*i:], r.enc.encoding)
		put32(out[8+8*i:], uint32(offsets[r.sub]))
	}
	return out
}

func format12(codes []rune, m map[rune]uint16) []byte {
	var groups [][3]uint32
	for _, c := range codes {
		g := uint32(m[c])
		if n := len(groups); n > 0 && groups[n-1][1]+1 == uint32(c) && groups[n-1][2]+uint32(c)-groups[n-1][0] == g {
			groups[n-1][1] = uint32(c)
			continue
		}
		groups = append(groups, [3]uint32{uint32(c), uint32(c), g})
	}
	out := make([]byte, 16, 16+12*len(groups))
	put16(out, 12)
	put32(out[4:], uint32(16+12*len(groups)))
	put32(out[12:], uint32(len(groups)))
	for _, g := range groups {
		var b [12]byte
		put32(b[:], g[0])
		put32(b[4:], g[1])
		put32(b[8:], g[2])
		out = append(out, b[:]...)
	}
	return out
}

// format4 writes the BMP part: one segment per run of consecutive codes, by
// delta when the run's glyphs are consecutive too, by a glyph array when not.
func format4(codes []rune, m map[rune]uint16) ([]byte, bool) {
	type segment struct {
		start, end rune
		delta      uint16
		glyphs     []uint16 // nil: by delta
	}
	var segs []segment
	for _, c := range codes {
		if c >= 0xFFFF {
			break
		}
		g := m[c]
		if n := len(segs); n > 0 && segs[n-1].end+1 == c {
			s := &segs[n-1]
			if s.glyphs == nil && uint16(c)+s.delta != g {
				for k := s.start; k <= s.end; k++ {
					s.glyphs = append(s.glyphs, uint16(k)+s.delta)
				}
			}
			if s.glyphs != nil {
				s.glyphs = append(s.glyphs, g)
			}
			s.end = c
			continue
		}
		segs = append(segs, segment{start: c, end: c, delta: g - uint16(c)})
	}
	segs = append(segs, segment{start: 0xFFFF, end: 0xFFFF, delta: 1})
	n := len(segs)
	arrays := 0
	for _, s := range segs {
		arrays += len(s.glyphs)
	}
	length := 16 + 8*n + 2*arrays
	if length > 0xFFFF {
		return nil, false
	}
	out := make([]byte, length)
	put16(out, 4)
	put16(out[2:], uint16(length))
	selector := 0
	for 1<<(selector+1) <= n {
		selector++
	}
	put16(out[6:], uint16(2*n))
	put16(out[8:], uint16(2<<selector))
	put16(out[10:], uint16(selector))
	put16(out[12:], uint16(2*n-(2<<selector)))
	ends, starts := 14, 16+2*n
	deltas, ranges := starts+2*n, starts+4*n
	array := ranges + 2*n
	for i, s := range segs {
		put16(out[ends+2*i:], uint16(s.end))
		put16(out[starts+2*i:], uint16(s.start))
		if s.glyphs == nil {
			put16(out[deltas+2*i:], s.delta)
			continue
		}
		put16(out[ranges+2*i:], uint16(array-(ranges+2*i)))
		for _, g := range s.glyphs {
			put16(out[array:], g)
			array += 2
		}
	}
	return out, true
}
