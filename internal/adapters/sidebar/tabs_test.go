package sidebar

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

func tabView(marks ...domain.TabMark) domain.SidebarView {
	v := view(idleRow("a1", "w1:p1"))
	v.Tabs = marks
	return v
}

func TestWriterMarksATabWhileItsAgentWaits(t *testing.T) {
	host := newFakeHost()
	c := &clock{now: at0()}
	w := NewWriter(host, textPresentation(), c.Now)
	asking := domain.TabMark{Tab: "w1:t2", Label: "2", Name: "2", Waiting: domain.DisplayBlocked}
	_, err := w.Apply(context.Background(), tabView(asking, domain.TabMark{Tab: "w1:t3", Label: "3", Name: "3"}))
	require.NoError(t, err)
	assert.Equal(t, []string{"w1:t2=" + domain.TabLabel("2", "?")}, host.renames, "only the tab that waits is renamed")

	_, err = w.Apply(context.Background(), tabView(asking))
	require.NoError(t, err)
	assert.Len(t, host.renames, 1, "a session read from before the rename does not send it again")

	seen := domain.TabMark{Tab: "w1:t2", Label: domain.TabLabel("2", "?"), Name: "2"}
	_, err = w.Apply(context.Background(), tabView(seen))
	require.NoError(t, err)
	assert.Equal(t, "w1:t2=2", host.renames[1], "the wait over, the mark comes off")
}

func TestWriterRenamesATabTheUserRenamedMeanwhile(t *testing.T) {
	host := newFakeHost()
	c := &clock{now: at0()}
	w := NewWriter(host, textPresentation(), c.Now)
	_, err := w.Apply(context.Background(), tabView(domain.TabMark{Tab: "w1:t1", Label: "1", Name: "1", Waiting: domain.DisplayDone}))
	require.NoError(t, err)
	_, err = w.Apply(context.Background(), tabView(domain.TabMark{Tab: "w1:t1", Label: "api", Name: "api", Waiting: domain.DisplayDone}))
	require.NoError(t, err)
	assert.Equal(t, []string{"w1:t1=" + domain.TabLabel("1", "✓"), "w1:t1=" + domain.TabLabel("api", "✓")}, host.renames)
}

func TestWriterBacksOffAFailingRename(t *testing.T) {
	host := newFakeHost()
	host.fail["w1:t1"] = true
	c := &clock{now: at0()}
	w := NewWriter(host, textPresentation(), c.Now)
	asking := domain.TabMark{Tab: "w1:t1", Label: "1", Name: "1", Waiting: domain.DisplayBlocked}
	_, err := w.Apply(context.Background(), tabView(asking))
	require.ErrorIs(t, err, domain.ErrProjection)

	delete(host.fail, "w1:t1")
	_, err = w.Apply(context.Background(), tabView(asking))
	require.NoError(t, err)
	assert.Empty(t, host.renames, "inside the backoff the tab is not retried")

	c.now = c.now.Add(backoffCap)
	_, err = w.Apply(context.Background(), tabView(asking))
	require.NoError(t, err)
	assert.Len(t, host.renames, 1)
}

func TestWriterForgetsARenameNoLongerWanted(t *testing.T) {
	host := newFakeHost()
	host.fail["w1:t1"] = true
	c := &clock{now: at0()}
	w := NewWriter(host, textPresentation(), c.Now)
	_, err := w.Apply(context.Background(), tabView(domain.TabMark{Tab: "w1:t1", Label: "1", Name: "1", Waiting: domain.DisplayBlocked}))
	require.ErrorIs(t, err, domain.ErrProjection)

	// The wait ends before the retry: the tab already reads right.
	delete(host.fail, "w1:t1")
	c.now = c.now.Add(backoffCap)
	v := tabView(domain.TabMark{Tab: "w1:t1", Label: "1", Name: "1"})
	v.Groups[0].Rows[0].Display = domain.DisplayWorking
	v.At = c.now
	next, err := w.Apply(context.Background(), v)
	require.NoError(t, err)
	assert.Empty(t, host.renames)
	assert.Equal(t, NextAnimation(v), next, "an old backoff is no deadline: the spinner keeps its step")
	assert.Empty(t, w.failed)
}

func TestWriterClearTakesTheMarksOff(t *testing.T) {
	host := newFakeHost()
	host.tabs = []domain.Tab{
		{ID: "w1:t2", Workspace: "w1", Label: domain.TabLabel("2", "?")},
		{ID: "w1:t3", Workspace: "w1", Label: "api"},
	}
	w := NewWriter(host, textPresentation(), (&clock{now: at0()}).Now)
	require.NoError(t, w.Clear(context.Background(), false))
	assert.Equal(t, []string{"w1:t2=1"}, host.renames, "the mark comes off, the number follows the place")
}
