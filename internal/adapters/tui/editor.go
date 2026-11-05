// Package tui is the settings popup: every key of the plugin's config file,
// edited in place, shaped like Herdr's own settings popup. Herdr has no
// settings hook for plugins, so the popup is a small TUI of Scope's own: one
// row per key, arrows to move and change, one key to save.
package tui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kriuchkov/herdr-scope/internal/adapters/config"
	"github.com/kriuchkov/herdr-scope/internal/application"
	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// Deps is what the popup reads and saves through. The two live switches
// (agents_panel, order) are not in the config file, so they come through
// their own functions — the same state the view keys switch, so a key press
// and the popup never disagree about what is current.
type Deps struct {
	// Fields are the rows, in order.
	Fields []config.Field
	Load   func(ctx context.Context) (ports.Settings, error)
	// PanelOn reports whether the Agents panel's rows are Scope's.
	PanelOn func() bool
	// Order is the panel order in force.
	Order func() domain.SortMode
	Save  func(ctx context.Context, req application.SettingsRequest) (application.SaveOutcome, error)
	// Title sits left on the title row, PluginID right.
	Title    string
	PluginID string
	// ConfigPath is named in the status after a save.
	ConfigPath string
}

// Keys as the terminal sends them in raw mode.
const (
	keyUp        = "\x1b[A"
	keyDown      = "\x1b[B"
	keyRight     = "\x1b[C"
	keyLeft      = "\x1b[D"
	keyEnter     = "\r"
	keyEscape    = "\x1b"
	keyCtrlC     = "\x03"
	keyBackspace = "\x7f"
	keyCtrlH     = "\b"
)

// The agents_panel row's words; boolTrue is how a bool row's value reads.
const (
	panelPlugin = "plugin"
	panelHerdr  = "herdr"
	boolTrue    = "true"
)

// editor is the popup's state machine: the rows' values, the cursor, the text
// being typed and the status line. It produces screen text and never touches
// a terminal, so every key can be tested without one.
type editor struct {
	deps Deps
	// loaded is what the last Load returned; a save applies the rows onto it,
	// so whatever the popup has no row for (observer intervals, log level)
	// goes back unchanged.
	loaded ports.Settings
	// saved is each row's value as last read, values what the popup shows. A
	// row is changed while the two differ.
	saved  []string
	values []string
	cursor int
	// top is the first row on screen; Render settles it, since only the
	// height decides how far the list must scroll.
	top int
	// editing is set while a text value is typed into buffer.
	editing   bool
	buffer    string
	status    string
	quitArmed bool
}

// newEditor reads every row's current value.
func newEditor(ctx context.Context, deps Deps) (*editor, error) {
	if len(deps.Fields) == 0 {
		return nil, errors.New("settings popup: no rows")
	}
	e := &editor{deps: deps}
	if err := e.load(ctx); err != nil {
		return nil, err
	}
	return e, nil
}

// Key handles one key press, spelled as the terminal sends it ("\x1b[A",
// "\r", "q"), and reports whether the popup should close.
func (e *editor) Key(ctx context.Context, key string) bool {
	e.status = ""
	if e.editing {
		e.editKey(key)
		return false
	}
	if key == "q" || key == keyEscape || key == keyCtrlC {
		if e.dirty() && !e.quitArmed {
			e.quitArmed = true
			e.status = warnText("unsaved changes — q again to discard, s to save")
			return false
		}
		return true
	}
	e.quitArmed = false
	n := len(e.deps.Fields)
	switch key {
	case keyUp, "k":
		e.cursor = (e.cursor + n - 1) % n
	case keyDown, "j":
		e.cursor = (e.cursor + 1) % n
	case keyRight, "l", " ", "+":
		e.step(1)
	case keyLeft, "h", "-":
		e.step(-1)
	case keyEnter:
		if kind := e.field().Kind; kind == config.KindBool || kind == config.KindEnum {
			e.step(1)
		} else {
			e.beginEdit()
		}
	case "r":
		e.values[e.cursor] = e.field().Default(e.settings())
	case "s":
		e.save(ctx)
	}
	return false
}

// load reads every row afresh: the file's through Load, the live switches
// through their own functions.
func (e *editor) load(ctx context.Context) error {
	settings, err := e.deps.Load(ctx)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	values := make([]string, len(e.deps.Fields))
	for i, f := range e.deps.Fields {
		values[i] = e.read(f, settings)
	}
	e.loaded, e.saved, e.values = settings, values, slices.Clone(values)
	return nil
}

func (e *editor) read(f config.Field, settings ports.Settings) string {
	if !f.Virtual {
		return f.Get(settings)
	}
	switch f.Key {
	case config.FieldAgentsPanel:
		if e.deps.PanelOn() {
			return panelPlugin
		}
		return panelHerdr
	case config.FieldOrder:
		return string(e.deps.Order())
	}
	return ""
}

func (e *editor) field() config.Field { return e.deps.Fields[e.cursor] }

func (e *editor) changed(i int) bool { return e.values[i] != e.saved[i] }

func (e *editor) dirty() bool { return !slices.Equal(e.values, e.saved) }

// step moves the current row's value by dir (-1 or +1). A text kind has no
// neighbour to step to, so stepping it opens the editor instead.
func (e *editor) step(dir int) {
	f := e.field()
	current := e.values[e.cursor]
	switch f.Kind {
	case config.KindBool:
		e.values[e.cursor] = strconv.FormatBool(current != boolTrue)
	case config.KindEnum:
		// A value outside the options (a live switch in a state the row has
		// no word for) steps as if from the first.
		i, n := max(slices.Index(f.Options, current), 0), len(f.Options)
		e.values[e.cursor] = f.Options[(i+dir+n)%n]
	case config.KindNumber:
		n, _ := strconv.ParseFloat(current, 64)
		// Rounded to hundredths so half-steps do not drift into 2.9999999.
		next := math.Round((n+float64(dir)*f.Step)*100) / 100
		e.values[e.cursor] = config.FormatNumber(clamp(f, next))
	case config.KindGlyph, config.KindColor:
		e.beginEdit()
	}
}

// beginEdit starts typing from the current value; a glyph starts from its
// codepoint, since the mark itself may be a private-use character the
// terminal font cannot show.
func (e *editor) beginEdit() {
	current := e.values[e.cursor]
	if e.field().Kind == config.KindGlyph {
		current = config.Codepoint(current)
	}
	e.editing, e.buffer = true, current
}

func (e *editor) editKey(key string) {
	switch {
	case key == keyEnter:
		e.commitEdit()
	case key == keyEscape:
		e.editing = false
	case key == keyBackspace || key == keyCtrlH:
		_, size := utf8.DecodeLastRuneInString(e.buffer)
		e.buffer = e.buffer[:len(e.buffer)-size]
	case key >= " " && !strings.HasPrefix(key, keyEscape):
		e.buffer += key
	}
}

// commitEdit takes the typed text, or leaves the value alone and says why.
func (e *editor) commitEdit() {
	f := e.field()
	raw := strings.TrimSpace(e.buffer)
	e.editing = false
	switch f.Kind {
	case config.KindNumber:
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
			e.status = warnText("not a number")
			return
		}
		e.values[e.cursor] = config.FormatNumber(clamp(f, n))
	case config.KindGlyph:
		// One mark is one character: anything typed after it is dropped.
		e.values[e.cursor] = firstRune(config.ParseGlyph(raw))
	case config.KindColor:
		// The check ports.Settings.Validate applies: a value the popup
		// accepts is one the save accepts.
		if raw != "" && !ports.ValidColor(raw) {
			e.status = warnText("not a colour (hex, rgb(...) or a name)")
			return
		}
		e.values[e.cursor] = raw
	case config.KindBool, config.KindEnum:
		e.values[e.cursor] = raw
	}
}

// save sends every row at once and reads the values back, so the popup shows
// what the file now says, not what was typed.
func (e *editor) save(ctx context.Context) {
	if !e.dirty() {
		e.status = "nothing changed"
		return
	}
	request, err := e.request()
	if err != nil {
		e.status = warnText(err.Error())
		return
	}
	outcome, err := e.deps.Save(ctx, request)
	if err != nil {
		e.status = warnText(err.Error())
		return
	}
	note := "applied"
	if !outcome.Applied {
		note = "daemon not running, applies on next start"
	}
	e.status = "saved to " + e.deps.ConfigPath + " · " + note
	if err = e.load(ctx); err != nil {
		// The save went through; only the change markers would lie if the
		// rows kept showing as unsaved.
		e.saved = slices.Clone(e.values)
		e.status = warnText("saved, but " + err.Error())
	}
}

// request applies every config row onto the loaded settings and reads the
// two live switches off their rows.
func (e *editor) request() (application.SettingsRequest, error) {
	request := application.SettingsRequest{Settings: e.loaded}
	for i, f := range e.deps.Fields {
		if !f.Virtual {
			next, err := f.Set(request.Settings, e.values[i])
			if err != nil {
				return request, err
			}
			request.Settings = next
			continue
		}
		switch f.Key {
		case config.FieldAgentsPanel:
			request.PanelOn, request.PanelChanged = e.values[i] == panelPlugin, e.changed(i)
		case config.FieldOrder:
			request.Order, request.OrderChanged = domain.SortMode(e.values[i]), e.changed(i)
		}
	}
	return request, nil
}

// settings is what the rows say now, applied onto the loaded settings; a row
// whose value does not apply keeps the loaded one.
func (e *editor) settings() ports.Settings {
	s := e.loaded
	for i, f := range e.deps.Fields {
		if f.Virtual {
			continue
		}
		if next, err := f.Set(s, e.values[i]); err == nil {
			s = next
		}
	}
	return s
}

func clamp(f config.Field, n float64) float64 { return min(f.Max, max(f.Min, n)) }

func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return ""
}

func warnText(text string) string { return sgrWarn + text + sgrReset }
