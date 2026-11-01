package font

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The short hashes of the two fake font builds below, worked out once by hand
// (`printf 'font bytes v1' | shasum -a 256`) so the test does not just repeat
// the code's own arithmetic.
const (
	buildV1 = "font bytes v1"
	buildV2 = "font bytes v2"
	nameV1  = "HerdrAgentIconsMax-6b43b9ee.ttf"
	nameV2  = "HerdrAgentIconsMax-2ff031a5.ttf"
)

// fakeRun records every helper call and answers from a table.
type fakeRun struct {
	calls   [][]string
	answers map[string]string
	fail    map[string]error
}

func newFakeRun() *fakeRun {
	return &fakeRun{answers: map[string]string{}, fail: map[string]error{}}
}

func (f *fakeRun) run(name string, args ...string) (string, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if err := f.fail[name]; err != nil {
		return "", err
	}
	return f.answers[name], nil
}

type fixture struct {
	inst   *Installer
	home   string
	source string
	run    *fakeRun
}

func newFixture(t *testing.T, goos string, configure func(*Options)) fixture {
	t.Helper()
	home := t.TempDir()
	source := filepath.Join(t.TempDir(), "HerdrAgentIconsMax-Regular.ttf")
	require.NoError(t, os.WriteFile(source, []byte(buildV1), 0o644))
	run := newFakeRun()
	opts := Options{Source: source, GOOS: goos, Home: home, Run: run.run}
	if configure != nil {
		configure(&opts)
	}
	return fixture{inst: New(opts), home: home, source: source, run: run}
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(raw)
}

func write(t *testing.T, path, text string, perm os.FileMode) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(text), perm))
}

func TestUserFontDirPerPlatform(t *testing.T) {
	home := "/home/u"
	cases := []struct {
		name string
		opts Options
		want string
	}{
		{"macOS", Options{GOOS: "darwin", Home: home}, "/home/u/Library/Fonts"},
		{"linux default", Options{GOOS: "linux", Home: home}, "/home/u/.local/share/fonts"},
		{"linux XDG", Options{GOOS: "linux", Home: home, XDGDataHome: "/data"}, "/data/fonts"},
		{"windows default", Options{GOOS: "windows", Home: home}, filepath.Join(home, "AppData", "Local", "Microsoft", "Windows", "Fonts")},
		{
			"windows LOCALAPPDATA",
			Options{GOOS: "windows", Home: home, LocalAppData: "/lad"},
			filepath.Join("/lad", "Microsoft", "Windows", "Fonts"),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, filepath.FromSlash(c.want), New(c.opts).UserFontDir())
		})
	}
}

func TestInstallNamesTheCopyByContentHash(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	_, ok := f.inst.InstalledPath()
	assert.False(t, ok)
	assert.False(t, f.inst.Installed())

	notes, err := f.inst.Install()
	require.NoError(t, err)
	target := filepath.Join(f.home, "Library", "Fonts", nameV1)
	assert.Equal(t, []string{"font: installed " + target}, notes)
	assert.Equal(t, buildV1, read(t, target))
	path, ok := f.inst.InstalledPath()
	assert.True(t, ok)
	assert.Equal(t, target, path)
	assert.True(t, f.inst.Installed())
	assert.Empty(t, f.run.calls, "macOS needs no helper")

	entries, err := os.ReadDir(filepath.Dir(target))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file is left beside the font")
}

func TestInstallNeverOverwritesAnInstalledPath(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	_, err := f.inst.Install()
	require.NoError(t, err)
	target := filepath.Join(f.home, "Library", "Fonts", nameV1)
	// A file that differs from the source proves the second install never
	// wrote over the path.
	require.NoError(t, os.WriteFile(target, []byte("held open by a terminal"), 0o644))

	notes, err := f.inst.Install()
	require.NoError(t, err)
	assert.Equal(t, []string{"font: already installed (" + nameV1 + ")"}, notes)
	assert.Equal(t, "held open by a terminal", read(t, target))
}

func TestInstallOfARebuildRemovesOlderCopies(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	_, err := f.inst.Install()
	require.NoError(t, err)
	dir := f.inst.UserFontDir()
	write(t, filepath.Join(dir, "SomeoneElse.ttf"), "theirs", 0o644)
	write(t, filepath.Join(dir, "HerdrAgentIconsMax-Notes.txt"), "not a font", 0o644)
	require.NoError(t, os.WriteFile(f.source, []byte(buildV2), 0o644))

	notes, err := f.inst.Install()
	require.NoError(t, err)
	assert.Equal(t, []string{
		"font: installed " + filepath.Join(dir, nameV2),
		"font: removed old " + nameV1,
	}, notes)
	assert.NoFileExists(t, filepath.Join(dir, nameV1))
	assert.Equal(t, buildV2, read(t, filepath.Join(dir, nameV2)))
	assert.FileExists(t, filepath.Join(dir, "SomeoneElse.ttf"), "only our copies go")
	assert.FileExists(t, filepath.Join(dir, "HerdrAgentIconsMax-Notes.txt"), "ours by name means .ttf too")
}

func TestInstallLeavesACopyThatCannotBeRemoved(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	// A non-empty directory under a copy's name refuses removal the way a
	// file a terminal locks does on Windows, without needing either.
	stuck := filepath.Join(f.inst.UserFontDir(), "HerdrAgentIconsMax-00000000.ttf")
	write(t, filepath.Join(stuck, "inside"), "x", 0o644)

	notes, err := f.inst.Install()
	require.NoError(t, err)
	assert.Contains(t, notes, "font: HerdrAgentIconsMax-00000000.ttf is in use, left for now (restart the terminal and run again)")
	assert.DirExists(t, stuck)
}

func TestInstallRegistersForTheUserOnWindows(t *testing.T) {
	appData := t.TempDir()
	f := newFixture(t, "windows", func(o *Options) { o.LocalAppData = appData })

	notes, err := f.inst.Install()
	require.NoError(t, err)
	target := filepath.Join(appData, "Microsoft", "Windows", "Fonts", nameV1)
	assert.Equal(t, [][]string{{
		"reg", "add", `HKCU\Software\Microsoft\Windows NT\CurrentVersion\Fonts`,
		"/v", "Herdr Agent Icons Max (TrueType)", "/t", "REG_SZ", "/d", target, "/f",
	}}, f.run.calls)
	assert.Equal(t, []string{"font: installed " + target, "font: registered for this user (HKCU)"}, notes)

	f.run.fail["reg"] = errors.New("access denied")
	notes, err = f.inst.Install()
	require.NoError(t, err, "a registry refusal is a note, not a failed install")
	assert.Equal(t, "font: registry write failed: access denied", notes[len(notes)-1])
}

func TestInstallRefreshesFontconfigOnLinux(t *testing.T) {
	data := t.TempDir()
	f := newFixture(t, "linux", func(o *Options) { o.XDGDataHome = data })

	notes, err := f.inst.Install()
	require.NoError(t, err)
	dir := filepath.Join(data, "fonts")
	assert.Equal(t, [][]string{{"fc-cache", "-f", dir}}, f.run.calls)
	assert.Equal(t, []string{"font: installed " + filepath.Join(dir, nameV1), "font: fontconfig cache refreshed"}, notes)

	f.run.fail["fc-cache"] = errors.New("executable file not found")
	notes, err = f.inst.Install()
	require.NoError(t, err)
	assert.Equal(t, "font: fc-cache not run (fontconfig missing?)", notes[len(notes)-1])
}

func TestInstallFailsWithoutTheBundledFont(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	require.NoError(t, os.Remove(f.source))
	_, err := f.inst.Install()
	require.Error(t, err)
	assert.False(t, f.inst.Installed())
}

func TestUninstallRemovesEveryCopy(t *testing.T) {
	data := t.TempDir()
	f := newFixture(t, "linux", func(o *Options) { o.XDGDataHome = data })
	_, err := f.inst.Install()
	require.NoError(t, err)
	dir := filepath.Join(data, "fonts")
	write(t, filepath.Join(dir, "HerdrAgentIconsMax-deadbeef.ttf"), "older build", 0o644)
	f.run.calls = nil

	notes, err := f.inst.Uninstall()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"font: removed " + nameV1, "font: removed HerdrAgentIconsMax-deadbeef.ttf"}, notes)
	assert.NoFileExists(t, filepath.Join(dir, nameV1))
	assert.NoFileExists(t, filepath.Join(dir, "HerdrAgentIconsMax-deadbeef.ttf"))
	assert.Equal(t, [][]string{{"fc-cache", "-f", dir}}, f.run.calls)
	assert.False(t, f.inst.Installed())

	notes, err = f.inst.Uninstall()
	require.NoError(t, err)
	assert.Equal(t, []string{"font: nothing installed"}, notes)
}

func TestUninstallDropsTheRegistryValueOnWindows(t *testing.T) {
	appData := t.TempDir()
	f := newFixture(t, "windows", func(o *Options) { o.LocalAppData = appData })
	_, err := f.inst.Install()
	require.NoError(t, err)
	f.run.calls = nil

	notes, err := f.inst.Uninstall()
	require.NoError(t, err)
	assert.Equal(t, [][]string{{
		"reg", "delete", `HKCU\Software\Microsoft\Windows NT\CurrentVersion\Fonts`,
		"/v", "Herdr Agent Icons Max (TrueType)", "/f",
	}}, f.run.calls)
	assert.Equal(t, []string{"font: registry value removed", "font: removed " + nameV1}, notes)

	// Nothing registered: reg refuses the delete, and that is no news.
	f.run.fail["reg"] = errors.New("unable to find the specified registry key")
	notes, err = f.inst.Uninstall()
	require.NoError(t, err)
	assert.Equal(t, []string{"font: nothing installed"}, notes)
}

func TestAvailable(t *testing.T) {
	t.Run("installed copy counts everywhere", func(t *testing.T) {
		f := newFixture(t, "darwin", nil)
		_, err := f.inst.Install()
		require.NoError(t, err)
		assert.True(t, f.inst.available())
	})
	t.Run("macOS without the copy never asks fontconfig", func(t *testing.T) {
		f := newFixture(t, "darwin", nil)
		f.run.answers["fc-match"] = iconFamily + "\n"
		assert.False(t, f.inst.available())
		assert.Empty(t, f.run.calls)
	})
	cases := []struct {
		name   string
		answer string
		fail   error
		want   bool
	}{
		{"fontconfig names the family", "Herdr Agent Icons Max\n", nil, true},
		{"one of several families", "DejaVu Sans,Herdr Agent Icons Max\n", nil, true},
		{"fontconfig falls back to another face", "DejaVu Sans\n", nil, false},
		{"a longer family is not ours", "Herdr Agent Icons Max Mono\n", nil, false},
		{"no fontconfig", "", errors.New("executable file not found"), false},
	}
	for _, c := range cases {
		t.Run("linux: "+c.name, func(t *testing.T) {
			f := newFixture(t, "linux", func(o *Options) { o.XDGDataHome = t.TempDir() })
			f.run.answers["fc-match"] = c.answer
			if c.fail != nil {
				f.run.fail["fc-match"] = c.fail
			}
			assert.Equal(t, c.want, f.inst.available())
			assert.Equal(t, [][]string{{"fc-match", "--format", "%{family}\n", "Herdr Agent Icons Max"}}, f.run.calls)
		})
	}
}

func TestRenderable(t *testing.T) {
	t.Run("macOS: an installed copy alone draws boxes", func(t *testing.T) {
		f := newFixture(t, "darwin", nil)
		_, err := f.inst.Install()
		require.NoError(t, err)
		assert.True(t, f.inst.available())
		assert.False(t, f.inst.Renderable(), "iTerm2 and Terminal.app never look in the font directory for a PUA codepoint")

		write(t, filepath.Join(f.home, ".config", "ghostty", "config"), "font-size = 14\n", 0o600)
		_, err = f.inst.ConfigureTerminals()
		require.NoError(t, err)
		assert.True(t, f.inst.Renderable(), "a mapped terminal draws them")
	})
	t.Run("macOS: a map without the face is not enough", func(t *testing.T) {
		f := newFixture(t, "darwin", nil)
		write(t, filepath.Join(f.home, ".config", "kitty", "kitty.conf"), kittyBlock+"\n", 0o600)
		assert.False(t, f.inst.Renderable())
	})
	t.Run("linux: fontconfig's answer", func(t *testing.T) {
		f := newFixture(t, "linux", func(o *Options) { o.XDGDataHome = t.TempDir() })
		f.run.answers["fc-match"] = iconFamily + "\n"
		assert.True(t, f.inst.Renderable())
	})
}

// The probes run on the way to a glyph variant, by any command that paints;
// only fc-cache may take the half minute a cold cache needs.
func TestProbesGetSecondsNotHalfAMinute(t *testing.T) {
	for _, probe := range []string{helperFcMatch, helperDefaults} {
		assert.Equal(t, probeTimeout, helperTimeoutOf(probe), probe)
	}
	assert.Less(t, probeTimeout, helperTimeoutOf(helperFcCache))
	assert.Equal(t, regTimeout, helperTimeoutOf(helperReg))
}

const (
	ghosttyBlock = "# >>> HERDR-SCOPE-BEGIN font\n" +
		"font-codepoint-map = U+E1A0-U+E1BA=Herdr Agent Icons Max\n" +
		"font-codepoint-map = U+E1C0-U+E1C5=Herdr Agent Icons Max\n" +
		"# <<< HERDR-SCOPE-END font"
	kittyBlock = "# >>> HERDR-SCOPE-BEGIN font\n" +
		"symbol_map U+E1A0-U+E1BA Herdr Agent Icons Max\n" +
		"symbol_map U+E1C0-U+E1C5 Herdr Agent Icons Max\n" +
		"# <<< HERDR-SCOPE-END font"
	ghosttyReload = " — reload the config (cmd+shift+, on macOS) or reopen the terminal"
	kittyReload   = " — reload the config (ctrl+shift+f5) or reopen the terminal"
)

func TestConfigureGhosttyAppendsTheMap(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	file := filepath.Join(f.home, ".config", "ghostty", "config")
	write(t, file, "font-family = JetBrains Mono\nfont-size = 14\n", 0o640)

	notes, err := f.inst.ConfigureTerminals()
	require.NoError(t, err)
	assert.Equal(t, []string{"ghostty: codepoint map written to " + file + ghosttyReload}, notes)
	text := read(t, file)
	assert.Equal(t, "font-family = JetBrains Mono\nfont-size = 14\n\n"+ghosttyBlock+"\n", text)
	assert.Equal(t, 1, strings.Count(text, "font-family"), "the user's primary font is never set by us")
	assert.NotContains(t, text, `"`+iconFamily+`"`, "Ghostty reads quotes as part of the name")

	info, err := os.Stat(file)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm(), "the config keeps its mode")
	entries, err := os.ReadDir(filepath.Dir(file))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file is left beside the config")

	notes, err = f.inst.ConfigureTerminals()
	require.NoError(t, err)
	assert.Equal(t, []string{"ghostty: codepoint map already in " + file}, notes)
	assert.Equal(t, text, read(t, file))
}

func TestConfigureGhosttyFindsEveryConfigItReads(t *testing.T) {
	xdg := t.TempDir()
	f := newFixture(t, "darwin", func(o *Options) { o.XDGConfigHome = xdg })
	appSupport := filepath.Join(f.home, "Library", "Application Support", "com.mitchellh.ghostty", "config.ghostty")
	xdgConfig := filepath.Join(xdg, "ghostty", "config")
	ignored := filepath.Join(f.home, ".config", "ghostty", "config")
	write(t, appSupport, "theme = dark\n", 0o600)
	write(t, xdgConfig, "theme = light\n", 0o600)
	write(t, ignored, "theme = other\n", 0o600)

	notes, err := f.inst.ConfigureTerminals()
	require.NoError(t, err)
	assert.Equal(t, []string{
		"ghostty: codepoint map written to " + xdgConfig + ghosttyReload,
		"ghostty: codepoint map written to " + appSupport + ghosttyReload,
	}, notes)
	assert.Contains(t, read(t, appSupport), ghosttyBlock)
	assert.Contains(t, read(t, xdgConfig), ghosttyBlock)
	assert.Equal(t, "theme = other\n", read(t, ignored), "XDG_CONFIG_HOME replaces ~/.config")
}

func TestConfigureKittyAppendsTheMap(t *testing.T) {
	xdg := t.TempDir()
	f := newFixture(t, "linux", func(o *Options) { o.XDGConfigHome = xdg })
	file := filepath.Join(xdg, "kitty", "kitty.conf")
	write(t, file, "font_size 12.0", 0o644)

	notes, err := f.inst.ConfigureTerminals()
	require.NoError(t, err)
	assert.Equal(t, []string{"kitty: codepoint map written to " + file + kittyReload}, notes)
	assert.Equal(t, "font_size 12.0\n\n"+kittyBlock+"\n", read(t, file))
	assert.Empty(t, f.run.calls)
}

func TestConfigureReplacesTheBlockInPlace(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	file := filepath.Join(f.home, ".config", "kitty", "kitty.conf")
	before := "# my kitty\nfont_size 12.0\n\n\n"
	after := "\n# added later by hand\nmap ctrl+t new_tab $HOME\n"
	stale := "# >>> HERDR-SCOPE-BEGIN font\nsymbol_map U+E1A0-U+E1B6 Herdr Agent Icons Max\n# <<< HERDR-SCOPE-END font"
	write(t, file, before+stale+after, 0o644)

	_, err := f.inst.ConfigureTerminals()
	require.NoError(t, err)
	assert.Equal(t, "# my kitty\nfont_size 12.0\n\n"+kittyBlock+after, read(t, file),
		"only the block changes; what follows it is kept byte for byte")
}

func TestUnconfigureRemovesOnlyTheBlock(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	ghostty := filepath.Join(f.home, ".config", "ghostty", "config")
	kitty := filepath.Join(f.home, ".config", "kitty", "kitty.conf")
	untouched := filepath.Join(f.home, "Library", "Application Support", "com.mitchellh.ghostty", "config")
	ghosttyOriginal := "font-size = 14\n\n\n# three blank lines above are the user's\nkeybind = a=b\n"
	write(t, ghostty, ghosttyOriginal, 0o600)
	write(t, kitty, "", 0o600)
	write(t, untouched, "theme = dark\n", 0o600)
	_, err := f.inst.ConfigureTerminals()
	require.NoError(t, err)
	// The block in the Application Support copy is removed by hand, so that
	// file has nothing of ours and must come through Unconfigure unchanged.
	require.NoError(t, os.WriteFile(untouched, []byte("theme = dark\n"), 0o600))

	notes, err := f.inst.UnconfigureTerminals()
	require.NoError(t, err)
	assert.Equal(t, []string{
		"ghostty: codepoint map removed from " + ghostty,
		"kitty: codepoint map removed from " + kitty,
	}, notes)
	assert.Equal(t, ghosttyOriginal, read(t, ghostty), "a configure/unconfigure round trip restores the file")
	assert.Empty(t, read(t, kitty))
	assert.Equal(t, "theme = dark\n", read(t, untouched))

	notes, err = f.inst.UnconfigureTerminals()
	require.NoError(t, err)
	assert.Empty(t, notes)
}

func TestUnconfigureKeepsContentAfterTheBlock(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	file := filepath.Join(f.home, ".config", "ghostty", "config")
	write(t, file, "a = 1\n\n"+ghosttyBlock+"\nb = 2\n\n\nc = 3\n", 0o600)

	_, err := f.inst.UnconfigureTerminals()
	require.NoError(t, err)
	assert.Equal(t, "a = 1\n\nb = 2\n\n\nc = 3\n", read(t, file))
}

func TestConfigureKeepsASymlinkedConfigASymlink(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	dotfile := filepath.Join(t.TempDir(), "dotfiles", "ghostty.conf")
	write(t, dotfile, "font-size = 14\n", 0o644)
	link := filepath.Join(f.home, ".config", "ghostty", "config")
	require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o755))
	require.NoError(t, os.Symlink(dotfile, link))

	_, err := f.inst.ConfigureTerminals()
	require.NoError(t, err)
	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the link survives")
	assert.Equal(t, "font-size = 14\n\n"+ghosttyBlock+"\n", read(t, dotfile), "the write went through to the target")

	_, err = f.inst.UnconfigureTerminals()
	require.NoError(t, err)
	info, err = os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink)
	assert.Equal(t, "font-size = 14\n", read(t, dotfile))
}

func TestConfigureWithoutATerminalConfigGivesInstructions(t *testing.T) {
	f := newFixture(t, "linux", func(o *Options) { o.XDGConfigHome = t.TempDir() })

	notes, err := f.inst.ConfigureTerminals()
	require.NoError(t, err)
	assert.Equal(t, []string{
		"terminal: no ghostty/kitty config found. Point your terminal at the font by codepoint:",
		`  U+E1A0-U+E1BA -> "Herdr Agent Icons Max"`,
		`  U+E1C0-U+E1C5 -> "Herdr Agent Icons Max"`,
		"  (tty7: add the family to font_fallbacks; Windows Terminal / iTerm: no codepoint map, use the merged font instead)",
	}, notes)
	assert.NoDirExists(t, filepath.Join(f.home, ".config"), "a config is never created")
	assert.NoDirExists(t, filepath.Join(f.home, "Library"))
}
