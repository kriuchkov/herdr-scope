package font

import "math"

// Simple-glyph flag bits (the glyf table).
const (
	flagOnCurve = 0x01
	flagXShort  = 0x02
	flagYShort  = 0x04
	flagRepeat  = 0x08
	flagXSame   = 0x10 // with flagXShort: the byte is positive
	flagYSame   = 0x20 // with flagYShort: the byte is positive
)

type point struct {
	x, y int
	on   bool
}

// outline is a simple glyph's contours, without its hinting instructions:
// scaled outlines would only be misled by them.
type outline struct {
	ends   []int
	points []point
}

// decodeGlyph reads a simple glyph; an empty one (a space) has no contours.
// A composite glyph refers to others by index and cannot move between fonts
// on its own, so it is refused.
func decodeGlyph(data []byte) (outline, error) {
	if len(data) == 0 {
		return outline{}, nil
	}
	if len(data) < 10 {
		return outline{}, badFont("glyph header truncated")
	}
	contours := int(int16(be16(data)))
	if contours < 0 {
		return outline{}, badFont("composite glyphs are not supported")
	}
	at := 10
	o := outline{ends: make([]int, contours)}
	for i := range contours {
		if at+2 > len(data) {
			return outline{}, badFont("glyph contours truncated")
		}
		o.ends[i] = int(be16(data[at:]))
		at += 2
	}
	if contours == 0 {
		return o, nil
	}
	if at+2 > len(data) {
		return outline{}, badFont("glyph instructions truncated")
	}
	at += 2 + int(be16(data[at:]))
	flags, at, err := readFlags(data, at, o.ends[contours-1]+1)
	if err != nil {
		return outline{}, err
	}
	o.points = make([]point, len(flags))
	at, err = readCoords(data, at, flags, flagXShort, flagXSame, func(i, v int) { o.points[i].x = v })
	if err != nil {
		return outline{}, err
	}
	if _, err = readCoords(data, at, flags, flagYShort, flagYSame, func(i, v int) { o.points[i].y = v }); err != nil {
		return outline{}, err
	}
	for i, f := range flags {
		o.points[i].on = f&flagOnCurve != 0
	}
	return o, nil
}

func readFlags(data []byte, at, count int) ([]byte, int, error) {
	flags := make([]byte, 0, count)
	for len(flags) < count {
		if at >= len(data) {
			return nil, 0, badFont("glyph flags truncated")
		}
		f := data[at]
		at++
		flags = append(flags, f)
		if f&flagRepeat != 0 {
			if at >= len(data) {
				return nil, 0, badFont("glyph flags truncated")
			}
			for n := int(data[at]); n > 0 && len(flags) < count; n-- {
				flags = append(flags, f)
			}
			at++
		}
	}
	return flags, at, nil
}

// readCoords decodes one axis: each value a delta from the previous point,
// a byte (signed by same) or a word, or no change.
func readCoords(data []byte, at int, flags []byte, short, same byte, set func(i, v int)) (int, error) {
	v := 0
	for i, f := range flags {
		switch {
		case f&short != 0:
			if at >= len(data) {
				return 0, badFont("glyph coordinates truncated")
			}
			d := int(data[at])
			at++
			if f&same == 0 {
				d = -d
			}
			v += d
		case f&same == 0:
			if at+2 > len(data) {
				return 0, badFont("glyph coordinates truncated")
			}
			v += int(int16(be16(data[at:])))
			at += 2
		}
		set(i, v)
	}
	return at, nil
}

// transform scales every point and shifts it right by dx, rounding to the
// unit grid.
func (o outline) transform(scale, dx float64) outline {
	out := outline{ends: o.ends, points: make([]point, len(o.points))}
	for i, p := range o.points {
		out.points[i] = point{
			x:  int(math.Round(float64(p.x)*scale + dx)),
			y:  int(math.Round(float64(p.y) * scale)),
			on: p.on,
		}
	}
	return out
}

// box is a bounding box in font units.
type box struct{ xMin, yMin, xMax, yMax int }

// bounds is the outline's bounding box; zero for an empty glyph.
func (o outline) bounds() box {
	var b box
	for i, p := range o.points {
		if i == 0 {
			b = box{p.x, p.y, p.x, p.y}
			continue
		}
		b.xMin, b.yMin = min(b.xMin, p.x), min(b.yMin, p.y)
		b.xMax, b.yMax = max(b.xMax, p.x), max(b.yMax, p.y)
	}
	return b
}

// encode writes the outline as a simple glyph with no instructions, padded to
// four bytes. An empty outline is an empty glyph.
func (o outline) encode() []byte {
	if len(o.points) == 0 {
		return nil
	}
	b := o.bounds()
	out := make([]byte, 10, 12+2*len(o.ends)+5*len(o.points))
	put16(out, uint16(len(o.ends)))
	put16(out[2:], uint16(int16(b.xMin)))
	put16(out[4:], uint16(int16(b.yMin)))
	put16(out[6:], uint16(int16(b.xMax)))
	put16(out[8:], uint16(int16(b.yMax)))
	for _, end := range o.ends {
		out = binary16(out, uint16(end))
	}
	out = binary16(out, 0)
	flags := make([]byte, len(o.points))
	var xs, ys []byte
	px, py := 0, 0
	for i, p := range o.points {
		if p.on {
			flags[i] |= flagOnCurve
		}
		xs = encodeDelta(xs, &flags[i], p.x-px, flagXShort, flagXSame)
		ys = encodeDelta(ys, &flags[i], p.y-py, flagYShort, flagYSame)
		px, py = p.x, p.y
	}
	out = append(out, flags...)
	out = append(out, xs...)
	out = append(out, ys...)
	for len(out)%4 != 0 {
		out = append(out, 0)
	}
	return out
}

func encodeDelta(buf []byte, flag *byte, d int, short, same byte) []byte {
	switch {
	case d == 0:
		*flag |= same
		return buf
	case d > 0 && d <= 0xFF:
		*flag |= short | same
		return append(buf, byte(d))
	case d < 0 && d >= -0xFF:
		*flag |= short
		return append(buf, byte(-d))
	}
	return binary16(buf, uint16(int16(d)))
}

func binary16(b []byte, v uint16) []byte {
	return append(b, byte(v>>8), byte(v))
}
