package sidebar

import (
	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// The palette is every colour Scope puts on screen. Sidebar token colours
// are static hex, so each must read on light
// and dark panels alike; where one value cannot, the scale is split per
// appearance and the managed block is rewritten when the desktop flips.

// brandColor is a vendor's own published hue. Green and red stay out — they
// mean done and blocked. A vendor absent here has no hue and draws in ink.
//
//nolint:goconst // table data: one literal per cell
func brandColors() []struct {
	Kind  domain.AgentKind
	Color string
} {
	return []struct {
		Kind  domain.AgentKind
		Color string
	}{
		{"claude", "#d97757"},
		{"gemini", "#4285f4"},
		{"kimi", "#1783ff"},
		{"deepseek", "#4d6bfe"},
		{"qwen", "#615ced"},
		{"kiro", "#9046ff"},
		{"cline", "#586876"},
		{"kilo", "#9a9808"},
		{"kimchi", "#ff521d"},
		{"muse", "#0082fb"},
		{"crush", "#ff388b"},
	}
}

// otherBrand colours a working title whose vendor has no hue of its own.
const otherBrand = "#c78a1f"

// BrandColor is a vendor's hue, if it has one.
func BrandColor(kind domain.AgentKind) (string, bool) {
	for _, b := range brandColors() {
		if b.Kind == kind {
			return b.Color, true
		}
	}
	return "", false
}

// BrandVendors are the vendors with a hue, in table order.
func BrandVendors() []domain.AgentKind {
	out := make([]domain.AgentKind, 0, len(brandColors()))
	for _, b := range brandColors() {
		out = append(out, b.Kind)
	}
	return out
}

// Spaces-row token budget: Herdr allows 16 tokens a row and one more rejects
// the whole config file, so only the first vendors get a working mark of
// their own; the rest work in `other`'s colour.
const (
	spaceRowFixedCells = 7
	rowTokenLimit      = 16
)

// SpaceWorkingVendors are the vendors whose working mark in the Spaces
// column is their own colour.
func SpaceWorkingVendors() []domain.AgentKind {
	return BrandVendors()[:rowTokenLimit-spaceRowFixedCells]
}

// ink is how a hueless mark is drawn: black on a light panel, white on dark.
func ink(appearance ports.Appearance) string {
	if appearance == ports.AppearanceDark {
		return "#e9e9f0"
	}
	return "#16161c"
}

// stateColors are the colours for one appearance.
type stateColors struct {
	Done, Blocked, Unknown, None string
	IdleFresh, IdleNormal        string
	IdleStale, Idle, Subtle      string
}

// colorsFor is the palette for one appearance. The idle tiers are
// brightness alone, fresh to stale, so green means done and nothing else: a
// green idle tier beside a done row reads as a second tick. A workspace name
// (None) is a grey that reads on dark; on light that grey falls to 2.4:1
// against the panel, so the light side draws it darker.
func colorsFor(appearance ports.Appearance) stateColors {
	shared := stateColors{Done: "#4c9a5a", Blocked: "#c04a4a", Unknown: "#907aa9", None: "#9a9eb3"}
	if appearance == ports.AppearanceDark {
		shared.IdleFresh, shared.IdleNormal, shared.IdleStale = "#c8cde0", "#9499ad", "#6b6f82"
		shared.Idle, shared.Subtle = "#8f95ab", "#a8abbd"
		return shared
	}
	shared.IdleFresh, shared.IdleNormal, shared.IdleStale = "#2f3347", "#6a6e82", "#9a9da8"
	shared.Idle, shared.Subtle, shared.None = "#6e738d", "#7c7f93", "#5c5f77"
	return shared
}
