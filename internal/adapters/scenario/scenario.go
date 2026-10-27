// Package scenario replays a recorded or hand-written Herdr session through
// Scope's whole pipeline — wire mapping, agent memory, the status state
// machine, the sidebar projection and the writer — into a simulated Herdr
// token store. It backs the golden tests and `herdr-scope render`.
package scenario

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/adapters/config"
	"github.com/kriuchkov/herdr-scope/internal/adapters/herdr"
	"github.com/kriuchkov/herdr-scope/internal/adapters/sidebar"
	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// Home is the home directory scenarios are written against.
const Home = "/home/user"

// Scenario is one replayable session. Workspaces, tabs and agents use
// Herdr's wire shapes, verbatim.
type Scenario struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Start is the wall-clock moment step offsets count from.
	Start time.Time `json:"start"`
	// Config is the plugin config.toml text.
	Config string `json:"config"`
	// Layout, when set, overrides the config's layout.
	Layout ports.Layout `json:"layout,omitempty"`
	// Mode is the panel order: active, recent or off.
	Mode string `json:"mode"`
	// HostGrouped is Herdr's own panel order when Mode is off.
	HostGrouped *bool             `json:"host_grouped,omitempty"`
	Workspaces  []json.RawMessage `json:"workspaces"`
	Tabs        []json.RawMessage `json:"tabs"`
	// Activity seeds when panes last worked, as offsets from Start ("-30m").
	Activity map[string]string `json:"activity"`
	Steps    []Step            `json:"steps"`
}

// Step is one frame: the agent list at a moment.
type Step struct {
	At     string                       `json:"at"`
	Agents []map[string]json.RawMessage `json:"agents"`
}

// Load reads a scenario file.
func Load(path string) (Scenario, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Scenario{}, fmt.Errorf("scenario: %w", err)
	}
	var s Scenario
	if err = json.Unmarshal(raw, &s); err != nil {
		return Scenario{}, fmt.Errorf("scenario %s: %w", path, err)
	}
	if len(s.Steps) == 0 {
		return Scenario{}, fmt.Errorf("scenario %s: no steps", path)
	}
	return s, nil
}

// Result is the token store after the last step.
type Result struct {
	Panes      map[string]map[string]string `json:"panes"`
	Workspaces map[string]map[string]string `json:"workspaces"`
	View       domain.SidebarView           `json:"-"`
	Layout     sidebar.Layout               `json:"-"`
}

// storeHost is a Herdr that only stores tokens. The writer reports in
// parallel, so the stores are guarded.
type storeHost struct {
	mu         sync.Mutex
	panes      sidebar.Store
	workspaces sidebar.Store
	paneIDs    func() []domain.PaneID
	wsIDs      []domain.WorkspaceID
}

func (h *storeHost) ReportPane(_ context.Context, pane domain.PaneID, tokens map[string]*string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.panes.Apply(string(pane), tokens)
	return nil
}

func (h *storeHost) ReportWorkspace(_ context.Context, ws domain.WorkspaceID, tokens map[string]*string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.workspaces.Apply(string(ws), tokens)
	return nil
}

func (h *storeHost) SetAgentView(context.Context, string, []string, string, bool, []domain.WorkspaceID) error {
	return nil
}
func (h *storeHost) ClearAgentView(context.Context) error { return nil }

func (h *storeHost) PaneTokens(context.Context) (map[domain.PaneID][]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[domain.PaneID][]string{}
	for _, pane := range h.paneIDs() {
		for name := range h.panes[string(pane)] {
			out[pane] = append(out[pane], name)
		}
	}
	return out, nil
}

func (h *storeHost) WorkspaceIDs(context.Context) ([]domain.WorkspaceID, error) { return h.wsIDs, nil }

// Tab names are no token; the store keeps none.
func (h *storeHost) Tabs(context.Context) ([]domain.Tab, error)            { return nil, nil }
func (h *storeHost) RenameTab(context.Context, domain.TabID, string) error { return nil }

// replay is one scenario's run in progress.
type replay struct {
	s           Scenario
	settings    ports.Settings
	mode        domain.SortMode
	hostGrouped bool
	mapper      herdr.Mapper
	host        *storeHost
	writer      *sidebar.Writer
	memory      *domain.Memory
	now         time.Time
	live        []domain.PaneID
	result      Result
}

// Run replays every step and returns the final store.
func Run(s Scenario) (Result, error) {
	settings, warnings := config.ParseLayout(s.Config, s.Layout)
	if len(warnings) > 0 {
		return Result{}, fmt.Errorf("scenario %s: config: %s", s.Name, strings.Join(warnings, "; "))
	}
	mode := domain.DefaultSortMode
	if s.Mode != "" {
		parsed, ok := domain.ParseSortMode(s.Mode)
		if !ok {
			return Result{}, fmt.Errorf("scenario %s: unknown mode %q", s.Name, s.Mode)
		}
		mode = parsed
	}
	presentation := sidebar.NewPresentation(settings, func() bool { return false })
	r := &replay{
		s: s, settings: settings, mode: mode, hostGrouped: s.HostGrouped == nil || *s.HostGrouped,
		mapper: herdr.Mapper{Home: Home, Showing: sidebar.ReadBack},
		memory: domain.NewMemory(nil), now: s.Start,
	}
	r.host = &storeHost{panes: sidebar.Store{}, workspaces: sidebar.Store{}, paneIDs: func() []domain.PaneID { return r.live }}
	r.writer = sidebar.NewWriter(r.host, presentation, func() time.Time { return r.now })
	for i, step := range s.Steps {
		if err := r.step(i, step); err != nil {
			return Result{}, fmt.Errorf("scenario %s: step %d: %w", s.Name, i, err)
		}
	}
	layout, err := sidebar.NewLayout(presentation, ports.AppearanceLight, nil)
	if err != nil {
		return Result{}, err //nolint:wrapcheck // names the limit
	}
	r.result.Layout = layout
	r.result.Panes, r.result.Workspaces = r.host.panes, r.host.workspaces
	return r.result, nil
}

// step is one frame, the way the observer runs it.
func (r *replay) step(index int, step Step) error {
	offset, err := time.ParseDuration(step.At)
	if err != nil {
		return fmt.Errorf("at: %w", err)
	}
	r.now = r.s.Start.Add(offset)
	session, err := decodeStep(r.mapper, r.s, step, r.host.panes)
	if err != nil {
		return err
	}
	r.host.wsIDs = r.host.wsIDs[:0]
	for _, ws := range session.Workspaces {
		r.host.wsIDs = append(r.host.wsIDs, ws.ID)
	}
	r.live = r.live[:0]
	for _, a := range session.Agents {
		r.live = append(r.live, a.Pane)
	}
	r.memory.Track(session.Agents)
	if index == 0 {
		if err = seed(r.memory, r.s, session.Agents); err != nil {
			return err
		}
	}
	observed := make([]domain.ObservedAgent, 0, len(session.Agents))
	for _, agent := range session.Agents {
		rec := r.memory.Session(agent.ID)
		rec.Recovered(time.Time{})
		obs := rec.Observe(agent, r.now, r.settings.Policy)
		observed = append(observed, domain.ObservedAgent{Agent: agent, Display: obs.Display, Activity: rec.Activity, Since: obs.Since})
	}
	r.result.View = domain.BuildSidebar(domain.SidebarInput{
		At: r.now, Mode: r.mode, HostGrouped: r.hostGrouped, Policy: r.settings.Policy,
		Workspaces: session.Workspaces, Tabs: session.Tabs, Home: session.Home, Agents: observed,
	})
	_, err = r.writer.Apply(context.Background(), r.result.View)
	return err //nolint:wrapcheck // the caller names the step
}

// decodeStep builds the step's snapshot the way Herdr would report it: the
// agents carry whatever tokens the store holds for their pane.
func decodeStep(mapper herdr.Mapper, s Scenario, step Step, store sidebar.Store) (domain.Session, error) {
	agents := make([]map[string]json.RawMessage, 0, len(step.Agents))
	for _, a := range step.Agents {
		copied := maps.Clone(a)
		var pane string
		_ = json.Unmarshal(a["pane_id"], &pane)
		if tokens, ok := store[pane]; ok && len(tokens) > 0 {
			raw, err := json.Marshal(tokens)
			if err != nil {
				return domain.Session{}, fmt.Errorf("tokens: %w", err)
			}
			copied["tokens"] = raw
		}
		agents = append(agents, copied)
	}
	raw, err := json.Marshal(map[string]any{"snapshot": map[string]any{
		"version": "scenario", "protocol": 22,
		"workspaces": s.Workspaces, "tabs": s.Tabs, "panes": []any{}, "agents": agents,
	}})
	if err != nil {
		return domain.Session{}, fmt.Errorf("snapshot: %w", err)
	}
	return herdr.DecodeSnapshot(mapper, raw) //nolint:wrapcheck // names the decode
}

// seed gives agents the activity stamps the scenario says they had before it
// started.
func seed(memory *domain.Memory, s Scenario, agents []domain.Agent) error {
	for _, agent := range agents {
		spec, ok := s.Activity[string(agent.Pane)]
		if !ok {
			continue
		}
		offset, err := time.ParseDuration(spec)
		if err != nil {
			return fmt.Errorf("scenario %s: activity %s: %w", s.Name, agent.Pane, err)
		}
		memory.Session(agent.ID).Activity = domain.Activity{At: s.Start.Add(offset)}
	}
	return nil
}

// Render draws the Agents panel in display order, then the Spaces column.
func (r Result) Render(opts sidebar.RenderOptions) string {
	var agents []sidebar.Entry
	for _, g := range r.View.Groups {
		for _, row := range g.Rows {
			agents = append(agents, sidebar.Entry{Rows: r.Layout.RowsFor(row.Kind), Tokens: r.Panes[string(row.Pane)]})
		}
	}
	var spaces []sidebar.Entry
	for _, space := range r.View.Spaces {
		spaces = append(spaces, sidebar.Entry{Rows: r.Layout.Spaces, Tokens: r.Workspaces[string(space.Workspace)]})
	}
	return "Agents\n" + sidebar.Render(agents, opts) + "\nSpaces\n" + sidebar.Render(spaces, opts)
}
