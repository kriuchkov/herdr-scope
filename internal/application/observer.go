// Package application holds Herdr Scope's use cases. It depends on the
// domain and on ports only; adapters are wired in by the composition root.
package application

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// Observer timings that are not settings.
const (
	// catchAll is the longest a frame may sleep with nothing scheduled;
	// events and deadlines are accelerators, this is the guarantee.
	catchAll = 30 * time.Second
	// retryAfterFailure is when a frame whose read failed is retried.
	retryAfterFailure = time.Second
	// saveInterval bounds how often memory is written while it changes.
	saveInterval = 5 * time.Second
	// recordMaxAge is how long an agent record outlives its last sighting.
	recordMaxAge = 30 * 24 * time.Hour
)

// ObserverDeps are the observer's ports. Activity, Repositories, Mover and
// Folds may be nil.
type ObserverDeps struct {
	Agents       ports.AgentSource
	Session      ports.SessionSource
	Activity     ports.ActivitySource
	Repositories ports.RepositoryResolver
	State        ports.StateRepository
	Sidebar      ports.SidebarWriter
	Host         ports.HostConfig
	Mover        ports.WorkspaceMover
	Folds        ports.FoldStore
	Clock        ports.Clock
	Logger       ports.Logger
}

// Observer owns the model of the Herdr session and the agent memory, and
// turns them into one sidebar frame at a time.
type Observer struct {
	deps     ObserverDeps
	settings ports.Settings
	mode     domain.SortMode
	memory   *domain.Memory

	session domain.Session
	// tree is the session's workspace tree; only a reconcile or new settings
	// change what it is made of.
	tree domain.WorktreeTree
	// focused is the workspace in front: the snapshot's, then each focus
	// event's. hereFamily is the family the here view was last given.
	focused    domain.WorkspaceID
	hereFamily []domain.WorkspaceID
	// folds are the folded families' keys (domain.FoldKey); opened are the
	// folded roots open for a visit, until the focus leaves them.
	folds       map[string]bool
	opened      map[domain.WorkspaceID]bool
	sessionAt   time.Time
	agentsAt    time.Time
	needSession bool
	needAgents  bool
	repoCache   map[string]repoLookup
	view        domain.SidebarView

	// stale is set by whatever the projection reads — the model, settings,
	// mode, folds, focus — and by a side effect of a frame that has to be
	// retried. projectedAt and grouped say when, and for which host order,
	// the view was last built; due is its earliest domain deadline.
	stale       bool
	projectedAt time.Time
	grouped     bool
	due         time.Time

	stateDirty bool
	savedAt    time.Time
	stats      ObserverStats
}

// ObserverStats feed `herdr-scope status`.
type ObserverStats struct {
	Server        domain.ServerInfo
	Agents        int
	Frames        uint64
	Events        uint64
	LastEvent     time.Time
	LastReconcile time.Time
	LastSidebar   time.Time
	LastError     string
}

type repoLookup struct {
	info *domain.RepositoryInfo
	at   time.Time
}

// NewObserver builds an observer from persisted memory. The first frame
// always reconciles.
func NewObserver(deps ObserverDeps, settings ports.Settings, mode domain.SortMode, records []domain.AgentSession) *Observer {
	o := &Observer{
		deps:        deps,
		settings:    settings,
		mode:        mode,
		memory:      domain.NewMemory(records),
		folds:       map[string]bool{},
		opened:      map[domain.WorkspaceID]bool{},
		needSession: true,
		needAgents:  true,
		repoCache:   map[string]repoLookup{},
		stale:       true,
	}
	o.resolveTree()
	if deps.Folds != nil {
		keys, err := deps.Folds.LoadFolds()
		if err != nil {
			deps.Logger.Warn("folds unreadable; nothing folded", "error", err)
		}
		for _, key := range keys {
			o.folds[key] = true
		}
	}
	return o
}

// Settings returns the settings in force.
func (o *Observer) Settings() ports.Settings { return o.settings }

// UpdateSettings swaps the settings in force; the next frame applies them.
func (o *Observer) UpdateSettings(settings ports.Settings) {
	o.settings = settings
	o.needAgents = true
	o.resolveTree()
	o.stale = true
}

// Mode returns the panel order in force.
func (o *Observer) Mode() domain.SortMode { return o.mode }

// SetMode switches the panel order the projection is computed for.
func (o *Observer) SetMode(mode domain.SortMode) {
	o.mode = mode
	o.stale = true
}

// Stats returns a copy of the observer's counters.
func (o *Observer) Stats() ObserverStats { return o.stats }

// View returns the last projected view.
func (o *Observer) View() domain.SidebarView { return o.view }

// ProcessAgentEvent marks what an event invalidates. It never changes domain
// state: Herdr may replay history, throttle or drop events, so the next
// frame re-reads the authoritative state instead. It reports whether the
// event is worth a frame.
func (o *Observer) ProcessAgentEvent(event domain.AgentEvent) bool {
	o.stats.Events++
	o.stats.LastEvent = event.Timestamp
	focus := event.Kind == domain.EventWorkspaceFocused || event.Kind == domain.EventPaneFocused
	if focus && event.Workspace != "" && event.Workspace != o.focused {
		o.focused = event.Workspace
		o.stale = true
	}
	switch event.Refresh() {
	case domain.RefreshAll:
		o.needSession = true
		return true
	case domain.RefreshAgents:
		o.needAgents = true
		return true
	case domain.RefreshNone:
		return false
	}
	return false
}

// Invalidate forces the next frame to reconcile the whole session.
func (o *Observer) Invalidate() {
	o.needSession = true
}

// ReconcileAgents reads the whole session — workspaces, tabs, worktrees and
// agents — and makes it the model.
func (o *Observer) ReconcileAgents(ctx context.Context) error {
	session, err := o.deps.Session.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("reconcile: %w", err)
	}
	now := o.deps.Clock.Now()
	o.resolveRepositories(ctx, session.Workspaces, now)
	o.session = session
	if session.FocusedWorkspace != "" {
		o.focused = session.FocusedWorkspace
	}
	o.resolveTree()
	o.stale = true
	o.sessionAt, o.agentsAt = now, now
	o.needSession, o.needAgents = false, false
	o.stats.Server = session.Server
	o.stats.LastReconcile = now
	return nil
}

// ObserveAgents re-reads the agent list into the model.
func (o *Observer) ObserveAgents(ctx context.Context) error {
	agents, err := o.deps.Agents.ListAgents(ctx)
	if err != nil {
		return fmt.Errorf("observe: %w", err)
	}
	// The same list again changes nothing the projection reads.
	o.stale = o.stale || !slices.Equal(agents, o.session.Agents)
	o.session.Agents = agents
	o.agentsAt = o.deps.Clock.Now()
	o.needAgents = false
	return nil
}

// resolveTree recomputes the workspace tree from the model and settings.
func (o *Observer) resolveTree() {
	o.tree = domain.ResolveTree(o.session.Workspaces, o.settings.Policy.NestFolders, o.session.Home)
}

// resolveRepositories fills in the git identity of workspaces the host
// reported none for, from the workspace's root directory.
func (o *Observer) resolveRepositories(ctx context.Context, workspaces []domain.Workspace, now time.Time) {
	if o.deps.Repositories == nil {
		return
	}
	for i := range workspaces {
		ws := &workspaces[i]
		if ws.Repository != nil || ws.Root == "" {
			continue
		}
		cached, ok := o.repoCache[ws.Root]
		if !ok || now.Sub(cached.at) > o.settings.Observer.ReconcileInterval {
			cached = repoLookup{at: now}
			info, err := o.deps.Repositories.Resolve(ctx, ws.Root)
			switch {
			case err == nil:
				cached.info = &info
			case !errors.Is(err, domain.ErrNotRepository):
				o.deps.Logger.Debug("repository lookup failed", "workspace_id", ws.ID, "error", err)
			}
			o.repoCache[ws.Root] = cached
		}
		ws.Repository = cached.info
	}
}

// FrameResult says when the next frame is due.
type FrameResult struct {
	Next time.Time
}

// Frame runs one observation-and-projection cycle: refresh what is stale,
// advance every agent's session, build the sidebar, write it, persist. A
// frame that brings nothing new for the projection only moves the animation.
func (o *Observer) Frame(ctx context.Context) (FrameResult, error) {
	now := o.deps.Clock.Now()
	if err := o.refresh(ctx, now); err != nil {
		o.stats.LastError = err.Error()
		return FrameResult{Next: now.Add(retryAfterFailure)}, err
	}
	o.stats.Frames++

	deadlines := []time.Time{
		o.agentsAt.Add(o.settings.Observer.Heartbeat),
		o.sessionAt.Add(o.settings.Observer.ReconcileInterval),
		now.Add(catchAll),
	}
	grouped := o.hostGrouped()
	projected := !o.current(now, grouped)
	if projected {
		o.project(ctx, now, grouped)
	} else {
		o.memory.Stamp(o.session.Agents, now)
		o.view.At = now
	}
	o.stateDirty = true
	deadlines = append(deadlines, o.due)
	if animation, err := o.deps.Sidebar.Apply(ctx, o.view); err != nil {
		// A projection failure never touches the memory: the next frame
		// writes again from the same state.
		o.stats.LastError = err.Error()
		o.deps.Logger.Warn("sidebar projection failed", "error", err)
	} else {
		o.stats.LastSidebar = now
		if !animation.IsZero() {
			deadlines = append(deadlines, animation)
		}
	}
	if projected {
		o.reorderWorkspaces(ctx)
		o.followHere(ctx)
	}
	o.persist(ctx, now, false)
	return FrameResult{Next: earliestAfter(now, deadlines)}, nil
}

// current reports whether the last projection still stands at now: nothing it
// read has changed, the host order is the same, and none of its deadlines has
// come. The minute counts too: a working agent's activity is now, and the
// ordering keys carry it at minute grain. The last view itself feeds the
// fold visits, but only along with a new focus, which is a new agent list: a
// fold never hides the agent in focus, so no view gives it a line.
func (o *Observer) current(now time.Time, grouped bool) bool {
	return !o.stale && grouped == o.grouped &&
		now.Truncate(time.Minute).Equal(o.projectedAt.Truncate(time.Minute)) &&
		(o.due.IsZero() || now.Before(o.due))
}

// project advances every agent's session and builds the sidebar from the
// model.
func (o *Observer) project(ctx context.Context, now time.Time, grouped bool) {
	agents := o.session.Agents
	o.opened = domain.FollowVisits(o.opened, o.view, agents, o.focused, o.tree)
	o.memory.Track(agents)
	var deadlines []time.Time
	observed := make([]domain.ObservedAgent, 0, len(agents))
	for _, agent := range agents {
		session := o.memory.Session(agent.ID)
		o.recover(ctx, session, agent)
		obs := session.Observe(agent, now, o.settings.Policy)
		deadlines = append(deadlines, obs.Deadlines...)
		observed = append(observed, domain.ObservedAgent{Agent: agent, Display: obs.Display, Activity: session.Activity, Since: obs.Since})
	}
	o.stats.Agents = len(agents)

	o.view = domain.BuildSidebar(domain.SidebarInput{
		At: now, Mode: o.mode, HostGrouped: grouped, Policy: o.settings.Policy,
		Workspaces: o.session.Workspaces, Tabs: o.session.Tabs, Home: o.session.Home, Agents: observed,
		Folded: o.folded(), Focused: o.focused, Opened: o.opened, Tree: &o.tree,
	})
	o.stale, o.projectedAt, o.grouped, o.due = false, now, grouped, time.Time{}
	if len(deadlines) > 0 {
		o.due = slices.MinFunc(deadlines, time.Time.Compare)
	}
}

func (o *Observer) refresh(ctx context.Context, now time.Time) error {
	if o.needSession || !now.Before(o.sessionAt.Add(o.settings.Observer.ReconcileInterval)) {
		return o.ReconcileAgents(ctx)
	}
	if o.needAgents || !now.Before(o.agentsAt.Add(o.settings.Observer.Heartbeat)) {
		return o.ObserveAgents(ctx)
	}
	return nil
}

func (o *Observer) recover(ctx context.Context, session *domain.AgentSession, agent domain.Agent) {
	if !session.NeedsRecovery() {
		return
	}
	if o.deps.Activity == nil {
		session.Recovered(time.Time{})
		return
	}
	if agent.Session == "" {
		// The session id may be reported later, under a new agent id that
		// inherits this memory: the lookup waits for it.
		return
	}
	at, err := o.deps.Activity.LastActivity(ctx, agent.Kind, agent.Session)
	if err != nil {
		o.deps.Logger.Debug("activity recovery failed", "agent_id", agent.ID, "error", err)
	}
	session.Recovered(at)
}

// HereFamily is what the here view shows with the focused workspace: its
// family's root and members (nest_folders, worktrees). Nil before anything
// was focused.
func (o *Observer) HereFamily() []domain.WorkspaceID {
	if o.focused == "" {
		return nil
	}
	order := make([]domain.WorkspaceID, 0, len(o.session.Workspaces))
	for _, ws := range o.session.Workspaces {
		order = append(order, ws.ID)
	}
	return o.tree.Family(o.focused, order)
}

// followHere renames the here view's family when the focus moved to
// another one; Herdr follows the focused workspace itself.
func (o *Observer) followHere(ctx context.Context) {
	if o.mode != domain.SortHere {
		o.hereFamily = nil
		return
	}
	family := o.HereFamily()
	if equalIDs(family, o.hereFamily) {
		return
	}
	if err := o.deps.Sidebar.SetOrder(ctx, o.mode, family); err != nil {
		o.deps.Logger.Warn("here view not updated", "error", err)
		o.stale = true // the next frame tries again
		return
	}
	o.hereFamily = family
}

// folded are the workspaces whose folder a fold names.
func (o *Observer) folded() map[domain.WorkspaceID]bool {
	out := map[domain.WorkspaceID]bool{}
	for _, ws := range o.session.Workspaces {
		if o.folds[domain.FoldKey(ws)] {
			out[ws.ID] = true
		}
	}
	return out
}

// ToggleFold folds the family of ws (the focused workspace when empty) in
// the Agents panel, or unfolds it, and remembers the choice. It returns the
// family's name and whether it is folded now.
func (o *Observer) ToggleFold(ws domain.WorkspaceID) (string, bool, error) {
	if ws == "" {
		ws = o.focused
	}
	ws = o.tree.Root(ws)
	i := slices.IndexFunc(o.session.Workspaces, func(w domain.Workspace) bool { return w.ID == ws })
	if i < 0 {
		return "", false, fmt.Errorf("%w: no workspace %q to fold", domain.ErrConfiguration, ws)
	}
	root := o.session.Workspaces[i]
	o.stale = true
	if o.opened[root.ID] {
		// Open for a visit, it reads as unfolded: the key folds it now.
		delete(o.opened, root.ID)
		return root.Label, true, nil
	}
	folded := !o.folds[domain.FoldKey(root)]
	return root.Label, folded, o.setFold(root, folded)
}

// setFold folds or unfolds root's family and remembers it.
func (o *Observer) setFold(root domain.Workspace, folded bool) error {
	key := domain.FoldKey(root)
	if folded {
		o.folds[key] = true
	} else {
		delete(o.folds, key)
	}
	if o.deps.Folds == nil {
		return nil
	}
	if err := o.deps.Folds.SaveFolds(slices.Sorted(maps.Keys(o.folds))); err != nil {
		return fmt.Errorf("fold not saved: %w", err)
	}
	return nil
}

// Waiting are the panes the last frame showed waiting on the user.
func (o *Observer) Waiting() []domain.PaneID {
	return o.view.Waiting()
}

func (o *Observer) hostGrouped() bool {
	if o.deps.Host == nil {
		return true
	}
	return o.deps.Host.PanelGrouped()
}

// reorderWorkspaces moves Herdr's workspaces: into Scope's activity order
// (reorder_workspaces, not while the order is Herdr's own), or else just
// enough to keep each nested folder right under its parent (nest_folders).
func (o *Observer) reorderWorkspaces(ctx context.Context) {
	byActivity := o.settings.ReorderWorkspaces && o.mode != domain.SortOff
	if o.deps.Mover == nil || (!byActivity && !o.settings.Policy.NestFolders) {
		return
	}
	current := make([]domain.WorkspaceID, 0, len(o.session.Workspaces))
	for _, ws := range o.session.Workspaces {
		current = append(current, ws.ID)
	}
	if len(current) < 2 {
		return
	}
	wanted := domain.NestOrder(current, o.tree)
	if byActivity {
		wanted = domain.WorkspaceOrder(current, o.view, o.tree)
	}
	if equalIDs(wanted, current) {
		return
	}
	if _, err := o.deps.Mover.MoveWorkspaces(ctx, wanted); err != nil {
		o.deps.Logger.Warn("workspace reorder failed", "error", err)
		o.stale = true // the next frame tries again
		return
	}
	o.needSession = true
}

// persist hands the memory to the state repository at most every
// saveInterval unless forced. Nearly every frame moves some time in it, so
// the repository is what skips a save that changes nothing worth a write
// (ports.StateRepository).
func (o *Observer) persist(ctx context.Context, now time.Time, force bool) {
	if !o.stateDirty || (!force && now.Sub(o.savedAt) < saveInterval) {
		return
	}
	o.memory.Prune(now, recordMaxAge)
	if err := o.deps.State.Save(ctx, o.memory.Records()); err != nil {
		o.deps.Logger.Warn("state save failed", "error", err)
		return
	}
	o.stateDirty = false
	o.savedAt = now
}

// Flush persists the memory unconditionally; the daemon calls it on its way
// out.
func (o *Observer) Flush(ctx context.Context) {
	o.persist(ctx, o.deps.Clock.Now(), true)
}

func earliestAfter(now time.Time, deadlines []time.Time) time.Time {
	next := now.Add(catchAll)
	for _, at := range deadlines {
		if at.After(now) && at.Before(next) {
			next = at
		}
	}
	return next
}

func equalIDs(a, b []domain.WorkspaceID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
