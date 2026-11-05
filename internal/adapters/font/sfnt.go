package font

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"sort"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

// sfnt is a TrueType font held as its tables: enough to graft glyphs from
// one face into another and write the result back out.
type sfnt struct {
	version uint32
	tables  map[string][]byte
}

// The sfnt versions a TrueType face may carry, and the two it must not.
const (
	sfntTrueType   = 0x00010000
	sfntApple      = 0x74727565 // "true"
	sfntCFF        = 0x4F54544F // "OTTO"
	sfntCollection = 0x74746366 // "ttcf"
)

// tagHead is the table that carries the units, the loca format and the
// whole-file checksum.
const tagHead = "head"

// checksumMagic is what a whole font sums to once head.checkSumAdjustment is
// in place.
const checksumMagic = 0xB1B0AFBA

func be16(b []byte) uint16 { return binary.BigEndian.Uint16(b) }
func be32(b []byte) uint32 { return binary.BigEndian.Uint32(b) }

func put16(b []byte, v uint16) { binary.BigEndian.PutUint16(b, v) }
func put32(b []byte, v uint32) { binary.BigEndian.PutUint32(b, v) }

func badFont(format string, args ...any) error {
	return fmt.Errorf("%w: font: "+format, append([]any{domain.ErrConfiguration}, args...)...)
}

// parseSFNT reads the table directory. Tables alias data.
func parseSFNT(data []byte) (*sfnt, error) {
	if len(data) < 12 {
		return nil, badFont("too short for a font file")
	}
	version := be32(data)
	switch version {
	case sfntTrueType, sfntApple:
	case sfntCFF:
		return nil, badFont("PostScript (CFF) outlines; give the TrueType (.ttf) build of the face")
	case sfntCollection:
		return nil, badFont("a font collection (.ttc); give one face's .ttf")
	default:
		return nil, badFont("not a TrueType font")
	}
	count := int(be16(data[4:]))
	if len(data) < 12+16*count {
		return nil, badFont("truncated table directory")
	}
	f := &sfnt{version: version, tables: make(map[string][]byte, count)}
	for i := range count {
		record := data[12+16*i:]
		offset, length := uint64(be32(record[8:])), uint64(be32(record[12:]))
		if offset+length > uint64(len(data)) {
			return nil, badFont("table %q runs past the end of the file", record[:4])
		}
		f.tables[string(record[:4])] = data[offset : offset+length]
	}
	return f, nil
}

// table is a required table, or an error naming it.
func (f *sfnt) table(tag string, size int) ([]byte, error) {
	t, ok := f.tables[tag]
	if !ok {
		return nil, badFont("no %q table", tag)
	}
	if len(t) < size {
		return nil, badFont("%q table too short", tag)
	}
	return t, nil
}

// checksum is the sfnt checksum of a table: big-endian words, the tail
// padded with zeros.
func checksum(b []byte) uint32 {
	var sum uint32
	for i := 0; i < len(b); i += 4 {
		var word [4]byte
		copy(word[:], b[i:])
		sum += be32(word[:])
	}
	return sum
}

func pad4(n int) int { return (n + 3) &^ 3 }

// bytes writes the font: tables in tag order, each on a four-byte boundary,
// every checksum and head.checkSumAdjustment recomputed.
func (f *sfnt) bytes() []byte {
	tags := make([]string, 0, len(f.tables))
	size := 12 + 16*len(f.tables)
	for tag, t := range f.tables {
		tags = append(tags, tag)
		size += pad4(len(t))
	}
	sort.Strings(tags)
	out := make([]byte, size)
	count := len(tags)
	selector := bits.Len(uint(count)) - 1
	searchRange := (1 << selector) * 16
	put32(out, f.version)
	put16(out[4:], uint16(count))
	put16(out[6:], uint16(searchRange))
	put16(out[8:], uint16(selector))
	put16(out[10:], uint16(count*16-searchRange))
	offset, headAt := 12+16*count, -1
	for i, tag := range tags {
		t := f.tables[tag]
		copy(out[offset:], t)
		if tag == tagHead && len(t) >= 12 {
			headAt = offset
			put32(out[offset+8:], 0)
		}
		record := out[12+16*i:]
		copy(record, tag)
		put32(record[4:], checksum(out[offset:offset+len(t)]))
		put32(record[8:], uint32(offset))
		put32(record[12:], uint32(len(t)))
		offset += pad4(len(t))
	}
	if headAt >= 0 {
		put32(out[headAt+8:], checksumMagic-checksum(out))
	}
	return out
}
