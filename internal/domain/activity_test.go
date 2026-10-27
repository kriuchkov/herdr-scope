package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFreshnessTiers(t *testing.T) {
	p := DefaultDisplayPolicy()
	last := Activity{At: at(0)}
	cases := []struct {
		age  time.Duration
		want DisplayState
	}{
		{0, DisplayIdleFresh},
		{15 * time.Minute, DisplayIdleFresh},
		{15*time.Minute + time.Millisecond, DisplayIdle},
		{119 * time.Minute, DisplayIdle},
		{120 * time.Minute, DisplayIdleStale},
		{48 * time.Hour, DisplayIdleStale},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, last.Freshness(at(c.age), p), "age %v", c.age)
	}
}

func TestFreshnessWithoutStampIsPlainIdle(t *testing.T) {
	assert.Equal(t, DisplayIdle, Activity{}.Freshness(at(0), DefaultDisplayPolicy()))
	assert.Empty(t, Activity{}.Crossings(at(0), DefaultDisplayPolicy()))
}

func TestFreshnessFollowsConfiguredThresholds(t *testing.T) {
	p := DefaultDisplayPolicy()
	p.FreshFor = 5 * time.Minute
	p.StaleAfter = 30 * time.Minute
	last := Activity{At: at(0)}
	assert.Equal(t, DisplayIdle, last.Freshness(at(6*time.Minute), p))
	assert.Equal(t, DisplayIdleStale, last.Freshness(at(30*time.Minute), p))
}

func TestCrossingsOnlyInTheFuture(t *testing.T) {
	p := DefaultDisplayPolicy()
	last := Activity{At: at(0)}
	assert.Equal(t, []time.Time{at(120 * time.Minute)}, last.Crossings(at(time.Hour), p))
	assert.Empty(t, last.Crossings(at(3*time.Hour), p))
}

func TestPolicyValidation(t *testing.T) {
	require.NoError(t, DefaultDisplayPolicy().Validate())

	p := DefaultDisplayPolicy()
	p.IdleGrace = time.Minute
	p.GroupIndent = 9
	p.RowLabel = "pane"
	p.StaleAfter = 10 * time.Minute
	err := p.Validate()
	require.ErrorIs(t, err, ErrInvalidPolicy)
	for _, want := range []string{"idle_grace_seconds", "group_indent", "row_label", "activity_stale_minutes"} {
		assert.Contains(t, err.Error(), want)
	}

	p = DefaultDisplayPolicy()
	p.DoneHold = DoneHold{}
	assert.ErrorContains(t, p.Validate(), "done_hold")
}
