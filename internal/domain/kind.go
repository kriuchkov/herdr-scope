package domain

import "strings"

// ResolveKind is the kind an agent is drawn as. declared is the vendor the
// pane names itself (Herdr's display_agent, free text such as "Claude:
// auth"); it outranks detected only when, trimmed and case-folded, it is a
// kind Scope knows. Anything else keeps the detected kind. The title
// fallback names the detected kind either way, so that is
// what callers hand ResolveTitle.
func ResolveKind(detected AgentKind, declared string) AgentKind {
	if kind := AgentKind(strings.ToLower(strings.TrimSpace(declared))); kind.IsKnown() {
		return kind
	}
	return detected
}
