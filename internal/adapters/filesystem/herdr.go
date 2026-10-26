package filesystem

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

// InvokedByHook tells Herdr's own hooks — the startup hooks and the
// pane.agent_detected watchdog — from the user's actions: an event hook gets
// HERDR_PLUGIN_EVENT, a startup hook an invocation_source of "startup".
func InvokedByHook(env Env) bool {
	if env("HERDR_PLUGIN_EVENT") != "" {
		return true
	}
	var invocation struct {
		Source string `json:"invocation_source"`
	}
	raw := env("HERDR_PLUGIN_CONTEXT_JSON")
	return raw != "" && json.Unmarshal([]byte(raw), &invocation) == nil && invocation.Source == "startup"
}

// KnownAgents are the canonical ids Herdr validates rows_by_agent keys
// against: one detector manifest per id under its state directory. A key it
// does not know is a parse error for the whole file, so nothing is named
// when the list is missing.
func KnownAgents(herdrStateDir string) []domain.AgentKind {
	entries, err := os.ReadDir(filepath.Join(herdrStateDir, "agent-detection", "remote"))
	if err != nil {
		return nil
	}
	var kinds []domain.AgentKind
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".toml"); ok {
			kinds = append(kinds, domain.AgentKind(name))
		}
	}
	return kinds
}
