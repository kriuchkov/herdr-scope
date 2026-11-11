package sidebar

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// maxTokensPerReport is Herdr's ceiling on one metadata report.
const maxTokensPerReport = 16

// Failure backoff: a target whose writes keep failing must not keep the
// daemon awake retrying every frame.
const (
	backoffBase = time.Second
	backoffCap  = time.Minute
)

// Host is the slice of Herdr the writer needs. The Herdr adapter implements
// it; the composition root wires the two together.
type Host interface {
	ReportPane(ctx context.Context, pane domain.PaneID, tokens map[string]*string) error
	ReportWorkspace(ctx context.Context, ws domain.WorkspaceID, tokens map[string]*string) error
	// SetAgentView installs a panel order by tokens, descending. A row
	// carrying the hide token is left out ("" hides nothing); here keeps the
	// agents of the workspace in front and of the rest of family (nil: that
	// workspace alone).
	SetAgentView(ctx context.Context, label string, tokens []string, hide string, here bool, family []domain.WorkspaceID) error
	ClearAgentView(ctx context.Context) error
	// PaneTokens lists the token names on every pane, all sources merged.
	PaneTokens(ctx context.Context) (map[domain.PaneID][]string, error)
	WorkspaceIDs(ctx context.Context) ([]domain.WorkspaceID, error)
	// Tabs lists every tab in Herdr's order; RenameTab names one.
	Tabs(ctx context.Context) ([]domain.Tab, error)
	RenameTab(ctx context.Context, tab domain.TabID, label string) error
}

// Writer implements ports.SidebarWriter.
type Writer struct {
	host  Host
	clock func() time.Time

	mu           sync.Mutex
	presentation Presentation
	// sent is what each target last accepted (set values only). A target
	// with no entry has not been written in this writer's life, so its first
	// write is the whole set, clears included — an earlier daemon may have
	// left names on it.
	sent   map[string]map[string]string
	agents map[domain.PaneID]domain.AgentID
	failed map[string]failure
	// members fingerprints the row set; a change triggers an orphan sweep.
	members string
	// alone are the titles of rows that show a fold line alone, which a
	// plain clear puts back so a stopped daemon leaves them readable.
	alone map[domain.PaneID]Tokens
	// tabs are the renames sent that the session read may not show yet.
	tabs map[domain.TabID]renamed
}

type failure struct {
	count int
	until time.Time
}

// NewWriter builds a writer; clock stamps failure backoffs.
func NewWriter(host Host, presentation Presentation, clock func() time.Time) *Writer {
	w := &Writer{host: host, clock: clock, presentation: presentation}
	w.reset()
	return w
}

func (w *Writer) reset() {
	w.sent = map[string]map[string]string{}
	w.agents = map[domain.PaneID]domain.AgentID{}
	w.failed = map[string]failure{}
	w.members = ""
	w.alone = map[domain.PaneID]Tokens{}
	w.tabs = map[domain.TabID]renamed{}
}

// Configure swaps the presentation (glyph variant, indent, worktree mark);
// the next Apply rewrites whatever it changes.
func (w *Writer) Configure(presentation Presentation) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.presentation = presentation
}

// Presentation returns the presentation in force.
func (w *Writer) Presentation() Presentation {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.presentation
}

// Resync forgets every write, so the next Apply sends everything again.
func (w *Writer) Resync() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.reset()
}

type job struct {
	key    string
	pane   domain.PaneID
	space  domain.WorkspaceID
	tokens Tokens
	forget bool
}

// Apply writes what changed since the last successful write of each target.
func (w *Writer) Apply(ctx context.Context, view domain.SidebarView) (time.Time, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.clock()
	var jobs []job
	live := map[domain.PaneID]domain.AgentID{}
	alone := map[domain.PaneID]Tokens{}
	for _, group := range view.Groups {
		for i, row := range group.Rows {
			live[row.Pane] = row.Agent
			jobs = append(jobs, job{key: paneKey(row.Pane), pane: row.Pane, tokens: w.presentation.PaneTokens(view, group, i)})
			if row.Fold != nil && row.Fold.Alone {
				alone[row.Pane] = titles(w.presentation.FoldedTokens(view, row))
			}
		}
	}
	for _, row := range view.Folded {
		live[row.Pane] = row.Agent
		jobs = append(jobs, job{key: paneKey(row.Pane), pane: row.Pane, tokens: w.presentation.FoldedTokens(view, row)})
	}
	w.alone = alone
	jobs = append(jobs, w.gonePanes(live)...)
	liveSpaces := map[domain.WorkspaceID]bool{}
	for _, space := range view.Spaces {
		liveSpaces[space.Workspace] = true
		jobs = append(jobs, job{key: spaceKey(space.Workspace), space: space.Workspace, tokens: w.presentation.SpaceTokens(space, view.At)})
	}
	jobs = append(jobs, w.goneSpaces(liveSpaces)...)

	err := errors.Join(w.run(ctx, now, jobs), w.markTabs(ctx, now, view.Tabs))
	w.agents = live
	if fingerprint := membersFingerprint(live); fingerprint != w.members {
		if sweepErr := w.sweep(ctx, live, paneTokenNames()); sweepErr != nil {
			err = errors.Join(err, sweepErr)
		} else if err == nil {
			w.members = fingerprint
		}
	}
	next := w.presentation.NextRepaint(view)
	if retry := w.nextRetry(now, jobs, view.Tabs); !retry.IsZero() && (next.IsZero() || retry.Before(next)) {
		next = retry
	}
	return next, err
}

// nextRetry is the earliest backoff still ahead. A failure nothing waits on
// any more is dropped: one this frame retried already or found nothing to
// send for, and one of a target that left. A deadline in the past is no
// deadline to the observer, which would fall back to its heartbeat and step
// every animation at that pace.
func (w *Writer) nextRetry(now time.Time, jobs []job, marks []domain.TabMark) time.Time {
	if len(w.failed) == 0 {
		return time.Time{}
	}
	pending := make(map[string]bool, len(jobs)+len(marks))
	for _, j := range jobs {
		pending[j.key] = true
	}
	for _, m := range marks {
		pending[tabKey(m.Tab)] = true
	}
	var next time.Time
	for key, f := range w.failed {
		if !pending[key] || !f.until.After(now) {
			delete(w.failed, key)
			continue
		}
		if next.IsZero() || f.until.Before(next) {
			next = f.until
		}
	}
	return next
}

// fail pushes a target's next try back, twice as far with each failure in a
// row, up to backoffCap.
func (w *Writer) fail(key string, now time.Time) {
	f := w.failed[key]
	f.count++
	f.until = now.Add(min(backoffCap, backoffBase<<min(f.count, 6)))
	w.failed[key] = f
}

// gonePanes clears panes that left the view — unless the agent on one moved
// to another pane: Herdr keeps resolving a moved pane's old id to it, so a
// clear sent there would wipe the live row. A moved pane is forgotten.
func (w *Writer) gonePanes(live map[domain.PaneID]domain.AgentID) []job {
	movedAgents := map[domain.AgentID]bool{}
	for _, agent := range live {
		movedAgents[agent] = true
	}
	var jobs []job
	for key := range w.sent {
		pane, ok := strings.CutPrefix(key, "pane:")
		if !ok {
			continue
		}
		id := domain.PaneID(pane)
		if _, stillLive := live[id]; stillLive {
			continue
		}
		if agent, known := w.agents[id]; known && movedAgents[agent] {
			delete(w.sent, key)
			delete(w.failed, key)
			continue
		}
		jobs = append(jobs, job{key: key, pane: id, tokens: clearAll(paneTokenNames()), forget: true})
	}
	return jobs
}

func (w *Writer) goneSpaces(live map[domain.WorkspaceID]bool) []job {
	var jobs []job
	for key := range w.sent {
		ws, ok := strings.CutPrefix(key, "space:")
		if !ok || live[domain.WorkspaceID(ws)] {
			continue
		}
		jobs = append(jobs, job{key: key, space: domain.WorkspaceID(ws), tokens: clearAll(spaceTokenNames()), forget: true})
	}
	return jobs
}

// run sends every job's delta in parallel; each target backs off on its own.
func (w *Writer) run(ctx context.Context, now time.Time, jobs []job) error {
	type outcome struct {
		job job
		err error
	}
	results := make(chan outcome, len(jobs))
	var wg sync.WaitGroup
	for _, j := range jobs {
		if f, ok := w.failed[j.key]; ok && now.Before(f.until) {
			continue
		}
		patch := delta(j.tokens, w.sent[j.key])
		if len(patch) == 0 {
			continue
		}
		wg.Go(func() {
			results <- outcome{job: j, err: w.report(ctx, j, patch)}
		})
	}
	wg.Wait()
	close(results)
	var errs []error
	for r := range results {
		if r.err != nil {
			w.fail(r.job.key, now)
			errs = append(errs, r.err)
			continue
		}
		delete(w.failed, r.job.key)
		if r.job.forget {
			delete(w.sent, r.job.key)
			continue
		}
		w.sent[r.job.key] = values(r.job.tokens)
	}
	if len(errs) > 0 {
		return fmt.Errorf("%w: %d of %d writes failed: %w", domain.ErrProjection, len(errs), len(jobs), errors.Join(errs...))
	}
	return nil
}

// report sends one target's patch in chunks of at most 16 tokens.
func (w *Writer) report(ctx context.Context, j job, patch Tokens) error {
	for _, chunk := range chunks(patch) {
		var err error
		if j.pane != "" {
			err = w.host.ReportPane(ctx, j.pane, chunk)
		} else {
			err = w.host.ReportWorkspace(ctx, j.space, chunk)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// sweep clears Scope's tokens from panes that are not live but still carry
// them: a token an earlier daemon wrote on a pane whose agent exited while no
// daemon ran outlives every writer. Only Scope's source is touched.
func (w *Writer) sweep(ctx context.Context, live map[domain.PaneID]domain.AgentID, names []string) error {
	panes, err := w.host.PaneTokens(ctx)
	if err != nil {
		return err
	}
	owned := map[string]bool{}
	for _, name := range names {
		owned[name] = true
	}
	clears := clearAll(names)
	var errs []error
	for pane, present := range panes {
		if _, isLive := live[pane]; isLive {
			continue
		}
		if !slices.ContainsFunc(present, func(name string) bool { return owned[name] }) {
			continue
		}
		errs = append(errs, w.report(ctx, job{pane: pane}, clears))
	}
	return errors.Join(errs...)
}

// SetOrder installs the panel order: active sorts by workspace, then split,
// then pane; recent by pane alone; here is active kept to the focused
// workspace and its family; off hands the panel back to Herdr. The grouped
// orders leave out the rows a fold hides.
func (w *Writer) SetOrder(ctx context.Context, mode domain.SortMode, family []domain.WorkspaceID) error {
	active := []string{tokenWsKey, tokenTabKey, tokenSortKey}
	switch mode {
	case domain.SortActive:
		return w.host.SetAgentView(ctx, "active", active, tokenFolded, false, nil)
	case domain.SortHere:
		return w.host.SetAgentView(ctx, "here", active, tokenFolded, true, family)
	case domain.SortRecent:
		return w.host.SetAgentView(ctx, "recent", []string{tokenSortKey}, "", false, nil)
	case domain.SortOff:
		return w.host.ClearAgentView(ctx)
	}
	return fmt.Errorf("%w: unknown sort mode %q", domain.ErrConfiguration, mode)
}

// Badge is "?1 ✓2": how many rows ask and how many hold an unseen result, in
// the variant's marks. Empty when nobody waits.
func (w *Writer) Badge(view domain.SidebarView) string {
	asking, finished := view.WaitingCounts()
	glyphs := w.Presentation().Glyphs
	var parts []string
	if asking > 0 {
		parts = append(parts, glyphs.State(domain.DisplayBlocked)+strconv.Itoa(asking))
	}
	if finished > 0 {
		parts = append(parts, glyphs.State(domain.DisplayDone)+strconv.Itoa(finished))
	}
	return strings.Join(parts, " ")
}

// Clear takes Scope's paint off every pane, workspace and tab. A plain stop keeps
// the title tokens (the rows are read through them — clearing them blanks
// every entry) and the sort keys a view may still order by; purge takes
// those and the vendor logo too.
func (w *Writer) Clear(ctx context.Context, purge bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	names := paneTokenNames()
	if !purge {
		names = slices.DeleteFunc(names, func(name string) bool {
			return strings.HasPrefix(name, titlePrefix) || name == tokenSortKey || name == tokenWsKey || name == tokenHarnessLogo
		})
	}
	errs := []error{w.sweep(ctx, nil, names)}
	if !purge {
		for pane, tokens := range w.alone {
			errs = append(errs, w.report(ctx, job{pane: pane}, tokens))
		}
	}
	errs = append(errs, w.unmarkTabs(ctx))
	workspaces, err := w.host.WorkspaceIDs(ctx)
	errs = append(errs, err)
	clears := clearAll(spaceTokenNames())
	for _, ws := range workspaces {
		errs = append(errs, w.report(ctx, job{space: ws}, clears))
	}
	w.reset()
	if joined := errors.Join(errs...); joined != nil {
		return fmt.Errorf("%w: %w", domain.ErrProjection, joined)
	}
	return nil
}

// delta is what differs from what was sent. A target never written gets the
// whole set, clears included; afterwards a clear is sent only for a name that
// is set.
func delta(tokens Tokens, sent map[string]string) Tokens {
	out := Tokens{}
	for name, value := range tokens {
		if sent == nil {
			out[name] = value
			continue
		}
		previous, wasSet := sent[name]
		switch {
		case value == nil && wasSet:
			out[name] = nil
		case value != nil && (!wasSet || previous != *value):
			out[name] = value
		}
	}
	return out
}

// titles are the title tokens a patch sets.
func titles(tokens Tokens) Tokens {
	out := Tokens{}
	for name, value := range tokens {
		if value != nil && strings.HasPrefix(name, titlePrefix) {
			out[name] = value
		}
	}
	return out
}

func values(tokens Tokens) map[string]string {
	out := map[string]string{}
	for name, value := range tokens {
		if value != nil {
			out[name] = *value
		}
	}
	return out
}

func clearAll(names []string) Tokens {
	out := Tokens{}
	for _, name := range names {
		out[name] = nil
	}
	return out
}

// chunks splits a patch into reports of at most maxTokensPerReport, in name
// order so writes are deterministic.
func chunks(tokens Tokens) []map[string]*string {
	names := slices.Sorted(maps.Keys(tokens))
	var out []map[string]*string
	for start := 0; start < len(names); start += maxTokensPerReport {
		chunk := map[string]*string{}
		for _, name := range names[start:min(start+maxTokensPerReport, len(names))] {
			chunk[name] = tokens[name]
		}
		out = append(out, chunk)
	}
	return out
}

func membersFingerprint(live map[domain.PaneID]domain.AgentID) string {
	panes := make([]string, 0, len(live))
	for pane := range live {
		panes = append(panes, string(pane))
	}
	sort.Strings(panes)
	return strings.Join(panes, ",")
}

func paneKey(pane domain.PaneID) string     { return "pane:" + string(pane) }
func spaceKey(ws domain.WorkspaceID) string { return "space:" + string(ws) }

var _ ports.SidebarWriter = (*Writer)(nil)
