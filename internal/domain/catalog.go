package domain

// knownAgents lists the agent kinds Scope recognises, in the order the glyph
// tables use, with the name a row shows when the agent never sets a title.
// Kinds are the ids Herdr reports, which is why Antigravity is "agy".
//
//nolint:goconst // catalogue data: each literal is one table cell, not a repeated constant
func knownAgents() []struct {
	Kind AgentKind
	Name string
} {
	return []struct {
		Kind AgentKind
		Name string
	}{
		{"claude", "Claude Code"},
		{"codex", "Codex"},
		{"opencode", "OpenCode"},
		{"omp", "Oh My Pi"},
		{"cline", "Cline"},
		{"mastracode", "Mastra"},
		{"kimi", "Kimi"},
		{"kilo", "Kilo"},
		{"maki", "Maki"},
		{"pi", "Pi"},
		{"hermes", "Hermes"},
		{"cursor", "Cursor"},
		{"copilot", "Copilot"},
		{"deepseek", "DeepSeek"},
		{"gemini", "Gemini"},
		{"gpt", "GPT"},
		{"qwen", "Qwen"},
		{"grok", "grok"},
		{"agy", "Antigravity"},
		{"kiro", "Kiro"},
		{"amp", "Amp"},
		{"devin", "Devin"},
		{"qodercli", "Qoder"},
		{"glm", "GLM"},
		{"kimchi", "Kimchi"},
		{"muse", "Muse"},
		{"crush", "Crush"},
	}
}

// IsKnown reports whether Scope has a mark and a name for the kind.
func (k AgentKind) IsKnown() bool {
	_, ok := k.DisplayName()
	return ok
}

// DisplayName is the readable name of a known kind.
func (k AgentKind) DisplayName() (string, bool) {
	for _, agent := range knownAgents() {
		if agent.Kind == k {
			return agent.Name, true
		}
	}
	return "", false
}
