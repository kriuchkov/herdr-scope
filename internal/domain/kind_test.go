package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveKind(t *testing.T) {
	cases := []struct {
		name     string
		detected AgentKind
		declared string
		want     AgentKind
	}{
		{"nothing declared keeps detection", "claude", "", "claude"},
		{"a known vendor outranks detection", "claude", "gemini", "gemini"},
		{"declared case and spaces do not matter", "claude", "  Gemini ", "gemini"},
		{"free text is no vendor", "claude", "Claude: auth", "claude"},
		{"an unknown vendor is no vendor", "codex", "droid", "codex"},
		{"a declared vendor names an undetected agent", "", "codex", "codex"},
		{"an unknown detection is kept", "droid", "", "droid"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, ResolveKind(c.detected, c.declared), c.name)
	}
}
