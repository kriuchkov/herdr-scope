package sidebar

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// fakeHost records reports and keeps a token store like Herdr's.
type fakeHost struct {
	mu        sync.Mutex
	store     Store
	reports   []string
	fail      map[string]bool
	panes     map[domain.PaneID][]string
	views     []string
	scopes    []agentView
	cleared   int
	workspace []domain.WorkspaceID
	// tabs is Herdr's tab list; renames are the labels given, in order.
	tabs    []domain.Tab
	renames []string
}

// agentView is what a panel order hides and where it keeps to.
type agentView struct {
	hide   string
	here   bool
	family []domain.WorkspaceID
}

func newFakeHost() *fakeHost {
	return &fakeHost{store: Store{}, fail: map[string]bool{}, panes: map[domain.PaneID][]string{}}
}

func (h *fakeHost) ReportPane(_ context.Context, pane domain.PaneID, tokens map[string]*string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(tokens) > maxTokensPerReport {
		return errors.New("too many tokens")
	}
	h.reports = append(h.reports, string(pane))
	if h.fail[string(pane)] {
		return errors.New("refused")
	}
	h.store.Apply(string(pane), tokens)
	return nil
}

func (h *fakeHost) ReportWorkspace(_ context.Context, ws domain.WorkspaceID, tokens map[string]*string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(tokens) > maxTokensPerReport {
		return errors.New("too many tokens")
	}
	h.store.Apply("ws:"+string(ws), tokens)
	return nil
}

func (h *fakeHost) SetAgentView(
	_ context.Context, label string, tokens []string, hide string, here bool, family []domain.WorkspaceID,
) error {
	h.views = append(h.views, label+":"+tokens[0])
	h.scopes = append(h.scopes, agentView{hide: hide, here: here, family: family})
	return nil
}

func (h *fakeHost) ClearAgentView(context.Context) error {
	h.cleared++
	return nil
}

func (h *fakeHost) PaneTokens(context.Context) (map[domain.PaneID][]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := maps.Clone(h.panes)
	for target, tokens := range h.store {
		if len(target) > 3 && target[:3] == "ws:" {
			continue
		}
		for name := range tokens {
			out[domain.PaneID(target)] = append(out[domain.PaneID(target)], name)
		}
	}
	return out, nil
}

func (h *fakeHost) WorkspaceIDs(context.Context) ([]domain.WorkspaceID, error) {
	return h.workspace, nil
}

func (h *fakeHost) Tabs(context.Context) ([]domain.Tab, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.tabs), nil
}

func (h *fakeHost) RenameTab(_ context.Context, tab domain.TabID, label string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.fail[string(tab)] {
		return errors.New("refused")
	}
	h.renames = append(h.renames, string(tab)+"="+label)
	for i := range h.tabs {
		if h.tabs[i].ID == tab {
			h.tabs[i].Label = label
		}
	}
	return nil
}

func (h *fakeHost) count(pane string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.reports {
		if r == pane {
			n++
		}
	}
	return n
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func view(rows ...domain.SidebarRow) domain.SidebarView {
	g := domain.SidebarGroup{Workspace: "w1", Label: "dashboard", Header: true, Gap: true, Rows: rows}
	return domain.SidebarView{At: at0(), Grouped: true, Groups: []domain.SidebarGroup{g},
		Spaces: []domain.SpaceView{{Workspace: "w1", Label: "dashboard"}}}
}

func idleRow(agent domain.AgentID, pane domain.PaneID) domain.SidebarRow {
	return domain.SidebarRow{Agent: agent, Pane: pane, Workspace: "w1", Kind: "claude", Display: domain.DisplayIdle, Title: "task"}
}

func TestWriterSendsOnlyWhatChanged(t *testing.T) {
	host := newFakeHost()
	c := &clock{now: at0()}
	w := NewWriter(host, textPresentation(), c.Now)
	_, err := w.Apply(context.Background(), view(idleRow("a1", "w1:p1")))
	require.NoError(t, err)
	first := host.count("w1:p1")
	assert.Equal(t, 2, first, "the first write is the whole set, in chunks of 16")
	assert.Equal(t, "task", host.store["w1:p1"]["title_idle"])

	_, err = w.Apply(context.Background(), view(idleRow("a1", "w1:p1")))
	require.NoError(t, err)
	assert.Equal(t, first, host.count("w1:p1"), "an unchanged pane is not written again")

	done := idleRow("a1", "w1:p1")
	done.Display = domain.DisplayDone
	_, err = w.Apply(context.Background(), view(done))
	require.NoError(t, err)
	assert.Equal(t, first+1, host.count("w1:p1"), "a state change is one small report")
	assert.Equal(t, "✓ task", host.store["w1:p1"]["title_done"])
	assert.NotContains(t, host.store["w1:p1"], "title_idle", "the old state's name is cleared")
}

func TestWriterBacksOffAFailingPane(t *testing.T) {
	host := newFakeHost()
	host.fail["w1:p1"] = true
	c := &clock{now: at0()}
	w := NewWriter(host, textPresentation(), c.Now)
	next, err := w.Apply(context.Background(), view(idleRow("a1", "w1:p1")))
	require.ErrorIs(t, err, domain.ErrProjection)
	assert.Equal(t, at0().Add(2*time.Second), next, "the retry is a deadline")
	calls := host.count("w1:p1")

	_, _ = w.Apply(context.Background(), view(idleRow("a1", "w1:p1")))
	assert.Equal(t, calls, host.count("w1:p1"), "inside the backoff the pane is not retried")

	host.fail["w1:p1"] = false
	c.now = c.now.Add(3 * time.Second)
	_, err = w.Apply(context.Background(), view(idleRow("a1", "w1:p1")))
	require.NoError(t, err)
	assert.Equal(t, "task", host.store["w1:p1"]["title_idle"])
}

// A deadline in the past is no deadline to the observer: it drops it and
// falls back to the 1 s heartbeat, which steps the spinner at 1 Hz. A
// failure nothing retries any more must not leave its old backoff behind.
func TestWriterForgetsFailuresNothingRetries(t *testing.T) {
	spinning := idleRow("a9", "w2:p9")
	spinning.Workspace, spinning.Display = "w2", domain.DisplayWorking
	// settle draws rows as they were and, in a group of its own, a working
	// row once every backoff is over.
	settle := func(t *testing.T, w *Writer, c *clock, rows ...domain.SidebarRow) {
		t.Helper()
		c.now = c.now.Add(backoffCap)
		v := view(rows...)
		v.Groups = append(v.Groups, domain.SidebarGroup{Workspace: "w2", Label: "api", Header: true, Rows: []domain.SidebarRow{spinning}})
		v.At = c.now
		next, err := w.Apply(context.Background(), v)
		require.NoError(t, err)
		assert.Equal(t, NextAnimation(v), next, "the next frame is the spinner's next step")
		assert.Empty(t, w.failed)
	}

	t.Run("a pane that left", func(t *testing.T) {
		host := newFakeHost()
		host.fail["w1:p2"] = true
		c := &clock{now: at0()}
		w := NewWriter(host, textPresentation(), c.Now)
		_, err := w.Apply(context.Background(), view(idleRow("a1", "w1:p1"), idleRow("a2", "w1:p2")))
		require.ErrorIs(t, err, domain.ErrProjection)
		settle(t, w, c, idleRow("a1", "w1:p1"))
	})

	t.Run("a pane back to what it last accepted", func(t *testing.T) {
		host := newFakeHost()
		c := &clock{now: at0()}
		w := NewWriter(host, textPresentation(), c.Now)
		_, err := w.Apply(context.Background(), view(idleRow("a1", "w1:p1")))
		require.NoError(t, err)
		host.fail["w1:p1"] = true
		done := idleRow("a1", "w1:p1")
		done.Display = domain.DisplayDone
		_, err = w.Apply(context.Background(), view(done))
		require.ErrorIs(t, err, domain.ErrProjection)
		delete(host.fail, "w1:p1")
		settle(t, w, c, idleRow("a1", "w1:p1"))
	})
}

func TestWriterClearsAClosedPane(t *testing.T) {
	host := newFakeHost()
	w := NewWriter(host, textPresentation(), (&clock{now: at0()}).Now)
	_, err := w.Apply(context.Background(), view(idleRow("a1", "w1:p1"), idleRow("a2", "w1:p2")))
	require.NoError(t, err)
	_, err = w.Apply(context.Background(), view(idleRow("a1", "w1:p1")))
	require.NoError(t, err)
	assert.Empty(t, host.store["w1:p2"], "a pane that left the list loses every token")
}

func TestWriterDoesNotClearAMovedPane(t *testing.T) {
	host := newFakeHost()
	w := NewWriter(host, textPresentation(), (&clock{now: at0()}).Now)
	_, err := w.Apply(context.Background(), view(idleRow("a1", "w12:p1")))
	require.NoError(t, err)
	before := host.count("w12:p1")
	// Herdr moves the pane: its tokens travel with it and only the new id is listed.
	host.mu.Lock()
	host.store["w0:p2"] = host.store["w12:p1"]
	delete(host.store, "w12:p1")
	host.mu.Unlock()

	_, err = w.Apply(context.Background(), view(idleRow("a1", "w0:p2")))
	require.NoError(t, err)
	assert.Equal(t, before, host.count("w12:p1"), "Herdr resolves the old id to the moved pane: no clear may go there")
	assert.Equal(t, "task", host.store["w0:p2"]["title_idle"], "the new id is painted in full")
}

func TestWriterSweepsOrphans(t *testing.T) {
	host := newFakeHost()
	host.panes["w9:p9"] = []string{"group", "foreign_token"}
	host.panes["w9:p8"] = []string{"foreign_token"}
	w := NewWriter(host, textPresentation(), (&clock{now: at0()}).Now)
	_, err := w.Apply(context.Background(), view(idleRow("a1", "w1:p1")))
	require.NoError(t, err)
	assert.Positive(t, host.count("w9:p9"), "a pane still carrying our token is cleared")
	assert.Zero(t, host.count("w9:p8"), "a pane with only foreign tokens is left alone")
}

func TestWriterResyncRepaintsEverything(t *testing.T) {
	host := newFakeHost()
	w := NewWriter(host, textPresentation(), (&clock{now: at0()}).Now)
	_, _ = w.Apply(context.Background(), view(idleRow("a1", "w1:p1")))
	host.store = Store{}
	w.Resync()
	_, err := w.Apply(context.Background(), view(idleRow("a1", "w1:p1")))
	require.NoError(t, err)
	assert.Equal(t, "task", host.store["w1:p1"]["title_idle"], "a new server gets the whole set")
}

func TestWriterClearKeepsTitlesUnlessPurged(t *testing.T) {
	host := newFakeHost()
	host.workspace = []domain.WorkspaceID{"w1"}
	w := NewWriter(host, textPresentation(), (&clock{now: at0()}).Now)
	row := idleRow("a1", "w1:p1")
	row.Keys = domain.SortKeys{Pane: "000000000001", Workspace: "ws", Tab: "tab"}
	_, err := w.Apply(context.Background(), view(row))
	require.NoError(t, err)

	require.NoError(t, w.Clear(context.Background(), false))
	tokens := host.store["w1:p1"]
	assert.Equal(t, "task", tokens["title_idle"], "a stopped plugin leaves a readable list")
	assert.Equal(t, "000000000001", tokens["sort_key"])
	assert.NotContains(t, tokens, "logo")
	assert.NotContains(t, tokens, "group")
	assert.NotContains(t, tokens, "tab_key")
	assert.Empty(t, host.store["ws:w1"])

	require.NoError(t, w.Clear(context.Background(), true))
	assert.Empty(t, host.store["w1:p1"], "purge takes everything")
}

func TestWriterOrders(t *testing.T) {
	host := newFakeHost()
	w := NewWriter(host, textPresentation(), (&clock{now: at0()}).Now)
	require.NoError(t, w.SetOrder(context.Background(), domain.SortActive, nil))
	require.NoError(t, w.SetOrder(context.Background(), domain.SortRecent, nil))
	require.NoError(t, w.SetOrder(context.Background(), domain.SortOff, nil))
	assert.Equal(t, []string{"active:ws_key", "recent:sort_key"}, host.views)
	assert.Equal(t, 1, host.cleared)
}

func TestChunksNeverExceedTheLimit(t *testing.T) {
	for _, c := range chunks(clearAll(paneTokenNames())) {
		assert.LessOrEqual(t, len(c), maxTokensPerReport)
	}
}

func TestHereKeepsToTheFocusAndItsFamily(t *testing.T) {
	host := newFakeHost()
	w := NewWriter(host, textPresentation(), (&clock{now: at0()}).Now)
	family := []domain.WorkspaceID{"w1", "w4", "w5"}
	require.NoError(t, w.SetOrder(context.Background(), domain.SortHere, nil))
	require.NoError(t, w.SetOrder(context.Background(), domain.SortHere, family))
	require.NoError(t, w.SetOrder(context.Background(), domain.SortActive, nil))
	require.NoError(t, w.SetOrder(context.Background(), domain.SortRecent, nil))
	assert.Equal(t, []string{"here:ws_key", "here:ws_key", "active:ws_key", "recent:sort_key"}, host.views, "here keeps active's order")
	assert.Equal(t, []agentView{
		{hide: "folded", here: true},
		{hide: "folded", here: true, family: family},
		{hide: "folded"},
		{},
	}, host.scopes, "the grouped orders leave out what a fold hides; recent shows every agent")
}

func TestWriterHidesFoldedRowsAndAStopShowsThemAgain(t *testing.T) {
	host := newFakeHost()
	w := NewWriter(host, textPresentation(), (&clock{now: at0()}).Now)
	carrier := idleRow("a1", "w1:p1")
	carrier.Fold = &domain.Fold{Root: "w1", Label: "dashboard", Hidden: 2, Alone: true}
	v := view(carrier)
	v.Folded = []domain.SidebarRow{idleRow("a2", "w1:p2")}
	_, err := w.Apply(context.Background(), v)
	require.NoError(t, err)
	assert.Equal(t, "1", host.store["w1:p2"]["folded"])
	assert.Equal(t, "task", host.store["w1:p2"]["title_idle"])
	assert.Equal(t, "▸ dashboard", host.store["w1:p1"]["fold"])
	assert.NotContains(t, host.store["w1:p1"], "title_idle", "a lone fold line is all its row shows")

	require.NoError(t, w.Clear(context.Background(), false))
	assert.NotContains(t, host.store["w1:p2"], "folded", "a stopped daemon hides nothing")
	assert.NotContains(t, host.store["w1:p1"], "fold")
	assert.Equal(t, "task", host.store["w1:p1"]["title_idle"], "the lone line's row reads again")
}

// busyView is a full panel: fifteen workspaces of two agents each, every
// state and a spread of vendors, grouped, with the Spaces column.
func busyView() domain.SidebarView {
	kinds := []domain.AgentKind{"claude", "codex", "gemini", "kimi", "droid"}
	states := domain.DisplayStates()
	v := domain.SidebarView{At: at0(), Grouped: true}
	for i := range 15 {
		ws := domain.WorkspaceID("w" + strconv.Itoa(i))
		g := domain.SidebarGroup{Workspace: ws, Label: "space " + strconv.Itoa(i), Header: true, Gap: true}
		space := domain.SpaceView{Workspace: ws, Label: g.Label}
		for j := range 2 {
			n := 2*i + j
			g.Rows = append(g.Rows, domain.SidebarRow{
				Agent: domain.AgentID("a" + strconv.Itoa(n)), Pane: domain.PaneID(string(ws) + ":p" + strconv.Itoa(j)), Workspace: ws,
				Kind: kinds[n%len(kinds)], Display: states[n%len(states)], Title: "task " + strconv.Itoa(n), Depth: j,
				Keys: domain.SortKeys{Pane: strconv.Itoa(n), Workspace: string(ws), Tab: "1"},
			})
			space.Kinds = append(space.Kinds, kinds[n%len(kinds)])
		}
		space.Mark, space.MarkKind = g.Rows[0].Display, g.Rows[0].Kind
		v.Groups = append(v.Groups, g)
		v.Spaces = append(v.Spaces, space)
	}
	return v
}

// BenchmarkApply is one frame of a full panel that sends nothing new: the
// projection and the delta, which run at the spinner's cadence.
func BenchmarkApply(b *testing.B) {
	w := NewWriter(newFakeHost(), textPresentation(), (&clock{now: at0()}).Now)
	v := busyView()
	_, err := w.Apply(b.Context(), v)
	require.NoError(b, err)
	b.ReportAllocs()
	for b.Loop() {
		if _, err = w.Apply(b.Context(), v); err != nil {
			b.Fatal(err)
		}
	}
}

func TestBadgeCountsWhoWaits(t *testing.T) {
	view := domain.SidebarView{Groups: []domain.SidebarGroup{{Rows: []domain.SidebarRow{
		{Display: domain.DisplayBlocked}, {Display: domain.DisplayDone}, {Display: domain.DisplayDone},
	}}}}
	w := NewWriter(newFakeHost(), textPresentation(), (&clock{now: at0()}).Now)
	assert.Equal(t, "?1 ✓2", w.Badge(view))
	assert.Empty(t, w.Badge(domain.SidebarView{}), "nobody waits: no badge")
	s := ports.DefaultSettings()
	s.Presentation.Variant = ports.VariantFont
	w.Configure(NewPresentation(s, nil))
	assert.Equal(t, "\uE1C11 \uE1C02", w.Badge(view), "in the icon font's marks when the sidebar uses them")
}
