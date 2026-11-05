// Package sidebar is the presentation adapter: it turns a domain.SidebarView
// into Herdr sidebar tokens, owns the glyphs and colours, writes the managed
// sidebar block's row layout, and renders golden text for tests.
package sidebar

import (
	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// GenericMark is worn by an agent Scope has no mark for: the row keeps its
// shape and nobody mistakes it for another vendor.
const GenericMark = "❖"

type vendorGlyphs struct {
	Kind domain.AgentKind
	// Font is the vendor's codepoint in the bundled icon font.
	Font string
	// Text is a plain-Unicode stand-in that renders in any terminal font.
	Text string
}

// vendorTable is every vendor's mark: its icon-font codepoint and its text form.
//
//nolint:goconst // table data: one literal per cell
func vendorTable() []vendorGlyphs {
	return []vendorGlyphs{
		{"claude", "\uE1A0", "§"}, {"codex", "\uE1A1", "Λ"}, {"opencode", "\uE1A2", "◇"},
		{"omp", "\uE1A3", "Π"}, {"cline", "\uE1A4", "∇"}, {"mastracode", "\uE1A5", "∑"},
		{"kimi", "\uE1A6", "✨"}, {"kilo", "\uE1A7", "♟"}, {"maki", "\uE1A8", "✳"},
		{"pi", "\uE1A9", "π"}, {"hermes", "\uE1AA", "☪"}, {"cursor", "\uE1AB", "◆"},
		{"copilot", "\uE1AC", "⊙"}, {"deepseek", "\uE1AD", "≋"}, {"gemini", "\uE1AE", "✦"},
		{"gpt", "\uE1AF", "✺"}, {"qwen", "\uE1B0", "Ϙ"}, {"grok", "\uE1B1", "✖"},
		{"agy", "\uE1B2", "△"}, {"kiro", "\uE1B3", "Ω"}, {"amp", "\uE1B4", "Ʌ"},
		{"devin", "\uE1B5", "ꓓ"}, {"qodercli", "\uE1B6", "Ǫ"}, {"glm", "\uE1B7", "Ƶ"},
		{"kimchi", "\uE1B8", "Ķ"}, {"muse", "\uE1B9", "∞"}, {"crush", "\uE1BA", "♥"},
	}
}

// fontStateGlyph is the state mark in the icon font. The three idle tiers and
// working share one ring: colour, not shape, tells the tiers apart.
func fontStateGlyph(state domain.DisplayState) string {
	switch state {
	case domain.DisplayDone:
		return "\uE1C0"
	case domain.DisplayBlocked:
		return "\uE1C1"
	case domain.DisplayUnknown:
		return "\uE1C3"
	case domain.DisplayWorking, domain.DisplayIdleFresh, domain.DisplayIdle, domain.DisplayIdleStale:
		return "\uE1C2"
	}
	return "\uE1C2"
}

// textStateGlyph is the plain-Unicode state mark.
func textStateGlyph(state domain.DisplayState) string {
	switch state {
	case domain.DisplayDone:
		return "✓"
	case domain.DisplayBlocked:
		return "?"
	case domain.DisplayWorking:
		return "○"
	case domain.DisplayUnknown:
		return "◌"
	case domain.DisplayIdleFresh, domain.DisplayIdle, domain.DisplayIdleStale:
		return "·"
	}
	return "·"
}

// spinnerFrames is the working spinner: eight-dot braille, the gap walking
// round a full cell — the heaviest mark that still reads as motion.
func spinnerFrames() []string {
	return []string{"⣷", "⣯", "⣟", "⡿", "⢿", "⣻", "⣽", "⣾"}
}

// pulseSteps is how many spinner steps one half of the blocked pulse lasts.
const pulseSteps = 5

// Icon is a resolved agent mark.
type Icon struct {
	Glyph string
	// Known is false for the generic mark.
	Known bool
}

// Glyphs is the glyph policy for one variant.
type Glyphs struct {
	variant   ports.Variant
	overrides map[domain.DisplayState]string
	// logos are the known vendors' marks in this variant; none in variant
	// none. Every row asks for one each frame, so NewGlyphs looks them up once.
	logos map[domain.AgentKind]string
	// blankRest rests the blocked pulse on a blank rather than the text
	// variant's dot (see rest).
	blankRest bool
}

// NewGlyphs resolves the variant: auto uses the font when fontAvailable says
// the terminal can reach it.
func NewGlyphs(settings ports.PresentationSettings, fontAvailable func() bool) Glyphs {
	variant := settings.Variant
	if variant == ports.VariantAuto || variant == "" {
		variant = ports.VariantText
		if fontAvailable != nil && fontAvailable() {
			variant = ports.VariantFont
		}
	}
	g := Glyphs{variant: variant, overrides: settings.Glyphs}
	if variant != ports.VariantNone {
		table := vendorTable()
		g.logos = make(map[domain.AgentKind]string, len(table))
		for _, v := range table {
			logo := v.Text
			if variant == ports.VariantFont {
				logo = v.Font
			}
			g.logos[v.Kind] = logo
		}
	}
	return g
}

// Variant is the resolved variant: font, text or none.
func (g Glyphs) Variant() ports.Variant { return g.variant }

// Resolve picks an agent's mark. With variant none nothing has a mark; an
// unknown kind gets the generic mark.
func (g Glyphs) Resolve(agent domain.Agent) Icon {
	if g.variant == ports.VariantNone {
		return Icon{}
	}
	if glyph, ok := g.Logo(agent.Kind); ok {
		return Icon{Glyph: glyph, Known: true}
	}
	return Icon{Glyph: GenericMark}
}

// Logo is a known vendor's mark in this variant.
func (g Glyphs) Logo(kind domain.AgentKind) (string, bool) {
	logo, ok := g.logos[kind]
	return logo, ok
}

// State is the mark for a display state. An explicit config choice always
// wins; variant none still draws state marks — a state line without a mark
// is not a state line.
func (g Glyphs) State(state domain.DisplayState) string {
	if chosen := g.overrides[state]; chosen != "" {
		return chosen
	}
	if g.variant == ports.VariantFont {
		return fontStateGlyph(state)
	}
	return textStateGlyph(state)
}

// Spinner is the working spinner frame for an animation step.
func (g Glyphs) Spinner(step int64) string {
	frames := spinnerFrames()
	return frames[int(step%int64(len(frames)))]
}

// BlockedFrame is the pulsing mark in front of a blocked row: the loud mark
// and a quiet one take turns in one cell, so nothing beside it shifts.
func (g Glyphs) BlockedFrame(step int64) string {
	if resting(step) {
		return g.rest()
	}
	return g.State(domain.DisplayBlocked)
}

// resting is whether a step falls in the quiet half of the blocked pulse.
func resting(step int64) bool { return (step/pulseSteps)%2 == 1 }

// rest is the quiet half of the pulse: the idle mark, except the text
// variant's dot in a one-cell line, where `Λ · Which port?` would read as
// Herdr's own cell separator. There the mark blinks instead. A mark the user
// chose for idle is kept.
func (g Glyphs) rest() string {
	if g.blankRest && g.variant != ports.VariantFont && g.overrides[domain.DisplayIdle] == "" {
		return " "
	}
	return g.State(domain.DisplayIdle)
}

// Lead is what goes in front of a title: motion for working and blocked, the
// state mark for done and unknown, nothing for the idle tiers (their colour
// already says how long ago).
func (g Glyphs) Lead(state domain.DisplayState, step int64) string {
	switch state {
	case domain.DisplayWorking:
		return g.Spinner(step)
	case domain.DisplayBlocked:
		return g.BlockedFrame(step)
	case domain.DisplayDone, domain.DisplayUnknown:
		return g.State(state)
	case domain.DisplayIdleFresh, domain.DisplayIdle, domain.DisplayIdleStale:
		return ""
	}
	return ""
}

// SpaceMark is a workspace's mark in the Spaces column; a workspace with no
// agent gets a hollow bullet so names stay aligned without reading as a
// second separator.
func (g Glyphs) SpaceMark(state domain.DisplayState) string {
	if state == "" {
		return "◦"
	}
	return g.State(state)
}
