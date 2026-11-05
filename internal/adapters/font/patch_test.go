package font

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

func bundledIcons(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "dist", "HerdrAgentIconsMax-Regular.ttf"))
	require.NoError(t, err)
	return data
}

func testNames(entries map[uint16]string) []nameRecord {
	var out []nameRecord
	for _, id := range []uint16{nameFamily, 2, nameUnique, nameFull, namePostScript} {
		if text, ok := entries[id]; ok {
			out = append(out, nameRecord{platform: platformWindows, encoding: 1, language: 0x409, id: id}.with(text))
		}
	}
	return out
}

// testFace is a two-glyph TrueType face on Meslo's grid (2048 units, a 1233
// cell): .notdef and a square for "0".
func testFace(t *testing.T) []byte {
	t.Helper()
	square := outline{ends: []int{3}, points: []point{{100, 0, true}, {100, 1400, true}, {1100, 1400, true}, {1100, 0, true}}}.encode()
	head := make([]byte, 54)
	put32(head, 0x00010000)
	put32(head[12:], 0x5F0F3CF5)
	put16(head[18:], 2048)
	put16(head[40:], 1100)
	put16(head[42:], 1400)
	hhea := make([]byte, 36)
	put32(hhea, 0x00010000)
	put16(hhea[10:], 1233)
	put16(hhea[34:], 1)
	maxp := make([]byte, 32)
	put32(maxp, 0x00010000)
	put16(maxp[4:], 2)
	put16(maxp[6:], 4)
	put16(maxp[8:], 1)
	loca := make([]byte, 6)
	put16(loca[4:], uint16(len(square)/2))
	hmtx := make([]byte, 6)
	put16(hmtx, 1233)
	put16(hmtx[4:], 100)
	post := make([]byte, 32)
	put32(post, 0x00020000)
	os2 := make([]byte, 78)
	put16(os2[64:], '0')
	put16(os2[66:], '0')
	face := &sfnt{version: sfntTrueType, tables: map[string][]byte{
		"head": head, "hhea": hhea, "maxp": maxp, "loca": loca, "glyf": square, "hmtx": hmtx,
		"cmap": buildCmap(map[rune]uint16{'0': 1}, nil), "post": post, "OS/2": os2, "hdmx": {0, 0},
		"name": buildName(testNames(map[uint16]string{
			nameFamily: "Test Mono", 2: "Regular", nameUnique: "Test Mono Regular 1.0",
			nameFull: "Test Mono Regular", namePostScript: "TestMono-Regular",
		})),
	}}
	return face.bytes()
}

func TestSFNTRoundTrip(t *testing.T) {
	icons := bundledIcons(t)
	f, err := parseSFNT(icons)
	require.NoError(t, err)
	again, err := parseSFNT(f.bytes())
	require.NoError(t, err)
	require.Len(t, again.tables, len(f.tables))
	for tag, table := range f.tables {
		if tag == "head" {
			assert.Equal(t, table[12:], again.tables[tag][12:], "head past checkSumAdjustment")
			continue
		}
		assert.Equal(t, table, again.tables[tag], tag)
	}
	assert.Equal(t, uint32(checksumMagic), checksum(f.bytes()), "the whole file sums to the magic number")
}

func TestGlyphCodecRoundTrip(t *testing.T) {
	f, err := parseSFNT(bundledIcons(t))
	require.NoError(t, err)
	glyphs, err := readGlyphs(f)
	require.NoError(t, err)
	for i, data := range glyphs.data {
		o, decodeErr := decodeGlyph(data)
		require.NoError(t, decodeErr, "glyph %d", i)
		back, decodeErr := decodeGlyph(o.encode())
		require.NoError(t, decodeErr)
		assert.Equal(t, o.points, back.points, "glyph %d", i)
		assert.Equal(t, o.ends, back.ends, "glyph %d", i)
	}
}

func TestPatchScalesIconsOntoTheFacesCell(t *testing.T) {
	icons := bundledIcons(t)
	patched, err := patch(testFace(t), icons)
	require.NoError(t, err)
	assert.Equal(t, "TestMonoHerdr-Regular.ttf", patched.File)
	assert.Equal(t, "Test Mono Herdr", patched.Family)
	assert.Equal(t, 33, patched.Icons)
	assert.Equal(t, uint32(checksumMagic), checksum(patched.Data))

	out, err := parseSFNT(patched.Data)
	require.NoError(t, err)
	glyphs, err := readGlyphs(out)
	require.NoError(t, err)
	cmap, _, err := parseCmap(out.tables["cmap"])
	require.NoError(t, err)
	assert.Len(t, glyphs.data, 2+33)
	assert.Equal(t, uint16(1), cmap['0'], "the face's own glyphs keep their indices")

	src, err := parseSFNT(icons)
	require.NoError(t, err)
	srcGlyphs, err := readGlyphs(src)
	require.NoError(t, err)
	srcCmap, _, err := parseCmap(src.tables["cmap"])
	require.NoError(t, err)
	for c, g := range srcCmap {
		o, decodeErr := decodeGlyph(glyphs.data[cmap[c]])
		require.NoError(t, decodeErr)
		assert.Equal(t, 1233, glyphs.advance[cmap[c]], "U+%04X takes one cell", c)
		if len(o.points) == 0 {
			continue
		}
		xMin := o.bounds().xMin
		assert.Equal(t, xMin, glyphs.lsb[cmap[c]], "U+%04X: the bearing matches the outline", c)
		// Drawn where the icon font draws it: its bearing, not its raw
		// xMin, scaled, plus half the slack between the two cells.
		want := float64(srcGlyphs.lsb[g])*2.048 + (1233-float64(srcGlyphs.advance[g])*2.048)/2
		assert.InDelta(t, want, float64(xMin), 1, "U+%04X", c)
	}

	records, err := parseName(out.tables["name"])
	require.NoError(t, err)
	assert.Equal(t, "Test Mono Herdr Regular", lookupName(records, nameFull))
	assert.Contains(t, lookupName(records, nameUnique), "Test Mono Herdr Regular 1.0; herdr icons ")
	assert.Equal(t, uint32(0x00030000), be32(out.tables["post"]), "glyph names dropped")
	assert.NotContains(t, out.tables, "hdmx", "per-glyph tables dropped")
	assert.Equal(t, uint16(1), be16(out.tables["head"][50:]), "long loca")
	assert.Equal(t, uint16(0xE1C5), be16(out.tables["OS/2"][66:]))

	again, err := patch(patched.Data, icons)
	require.NoError(t, err)
	assert.Equal(t, patched.File, again.File, "patching a patched face keeps its name")
}

func TestPatchKeepsEveryGlyphOfARealFace(t *testing.T) {
	base, err := os.ReadFile(filepath.Join("..", "..", "..", "dist", "JetBrainsMonoHerdr-Regular.ttf"))
	require.NoError(t, err)
	patched, err := patch(base, bundledIcons(t))
	require.NoError(t, err)
	before, err := parseSFNT(base)
	require.NoError(t, err)
	after, err := parseSFNT(patched.Data)
	require.NoError(t, err)
	oldGlyphs, err := readGlyphs(before)
	require.NoError(t, err)
	newGlyphs, err := readGlyphs(after)
	require.NoError(t, err)
	oldCmap, _, err := parseCmap(before.tables["cmap"])
	require.NoError(t, err)
	newCmap, _, err := parseCmap(after.tables["cmap"])
	require.NoError(t, err)
	for c, g := range oldCmap {
		if c >= 0xE1A0 && c <= 0xE1C5 {
			continue
		}
		require.Equal(t, g, newCmap[c], "U+%04X", c)
		require.Equal(t, oldGlyphs.data[g], newGlyphs.data[g][:len(oldGlyphs.data[g])], "U+%04X", c)
		require.Equal(t, oldGlyphs.advance[g], newGlyphs.advance[g])
	}
	assert.Equal(t, "JetBrainsMonoHerdr-Regular.ttf", patched.File, "already a Herdr face: the name stays")
}

func TestPatchRefusesWhatItCannotGraft(t *testing.T) {
	icons := bundledIcons(t)
	for name, data := range map[string][]byte{
		"cff":        append([]byte("OTTO"), make([]byte, 8)...),
		"collection": append([]byte("ttcf"), make([]byte, 8)...),
		"junk":       []byte("not a font at all"),
	} {
		_, err := patch(data, icons)
		require.ErrorIs(t, err, domain.ErrConfiguration, name)
	}
	f, err := parseSFNT(testFace(t))
	require.NoError(t, err)
	f.tables["fvar"] = []byte{0}
	_, err = patch(f.bytes(), icons)
	require.ErrorIs(t, err, domain.ErrConfiguration, "a variable face")
}

const itermPrefs = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Default Bookmark Guid</key>
	<string>B</string>
	<key>New Bookmarks</key>
	<array>
		<dict>
			<key>Guid</key><string>A</string>
			<key>Normal Font</key><string>Menlo-Regular 12</string>
			<key>Background Image</key><data>AAAA</data>
		</dict>
		<dict>
			<key>Guid</key><string>B</string>
			<key>Normal Font</key><string>Menlo-Regular 13</string>
			<key>Use Non-ASCII Font</key><%s/>
			<key>Non Ascii Font</key><string>TestMonoHerdr-Regular 13</string>
			<key>Columns</key><integer>80</integer>
		</dict>
	</array>
</dict>
</plist>`

func TestTerminalFontReadsITermsDefaultProfile(t *testing.T) {
	f := newFixture(t, goosDarwin, nil)
	require.NoError(t, os.WriteFile(f.source, bundledIcons(t), 0o644))
	notes, err := f.inst.PatchFonts([]string{writeTemp(t, testFace(t))}, "")
	require.NoError(t, err)
	require.Len(t, notes, 1)
	assert.FileExists(t, filepath.Join(f.inst.UserFontDir(), "TestMonoHerdr-Regular.ttf"))

	f.run.answers[helperDefaults] = fmt.Sprintf(itermPrefs, "true")
	tf, ok := f.inst.TerminalFont()
	require.True(t, ok)
	assert.Equal(t, TerminalFont{
		Terminal: "iTerm2", Name: "TestMonoHerdr-Regular", Size: "13",
		Path: filepath.Join(f.inst.UserFontDir(), "TestMonoHerdr-Regular.ttf"), Icons: true,
	}, tf, "the default profile's non-ASCII font draws the icons")
	assert.True(t, f.inst.Renderable(), "auto takes the icons: the terminal's own font carries them")
	assert.Equal(t, []string{helperDefaults, "export", itermDomain, "-"}, f.run.calls[0])

	f.run.answers[helperDefaults] = fmt.Sprintf(itermPrefs, "false")
	tf, ok = f.inst.TerminalFont()
	require.True(t, ok)
	assert.Equal(t, "Menlo-Regular", tf.Name, "with the non-ASCII font off, the normal one draws everything")
	assert.False(t, tf.Icons)
	assert.False(t, f.inst.Renderable())

	f.run.fail[helperDefaults] = os.ErrNotExist
	_, ok = f.inst.TerminalFont()
	assert.False(t, ok, "no iTerm2 preferences")
	_, ok = newFixture(t, goosLinux, nil).inst.TerminalFont()
	assert.False(t, ok, "only macOS has iTerm2")
}

func TestPatchFontsRegistersOnWindows(t *testing.T) {
	f := newFixture(t, goosWindows, nil)
	require.NoError(t, os.WriteFile(f.source, bundledIcons(t), 0o644))
	_, err := f.inst.PatchFonts([]string{writeTemp(t, testFace(t))}, "")
	require.NoError(t, err)
	require.Len(t, f.run.calls, 1)
	assert.Contains(t, f.run.calls[0], "Test Mono Herdr (TrueType)")

	out := t.TempDir()
	f.run.calls = nil
	_, err = f.inst.PatchFonts([]string{writeTemp(t, testFace(t))}, out)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(out, "TestMonoHerdr-Regular.ttf"))
	assert.Empty(t, f.run.calls, "--out installs nothing")
}

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "face.ttf")
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}
