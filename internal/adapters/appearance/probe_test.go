package appearance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/ports"
)

type answer struct {
	out  string
	code int
	err  error
}

func fake(a *answer, calls *int) Runner {
	return func(context.Context, string, ...string) (string, int, error) {
		*calls++
		return a.out, a.code, a.err
	}
}

func TestDarwin(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	calls := 0
	a := &answer{out: "Dark\n"}
	p := NewProbe("darwin", fake(a, &calls), clock)
	v, err := p.Current(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ports.AppearanceDark, v)

	a.out, a.code = "", 1
	v, _ = p.Current(context.Background())
	assert.Equal(t, ports.AppearanceDark, v, "cached for a minute")
	assert.Equal(t, 1, calls)

	now = now.Add(2 * time.Minute)
	v, _ = p.Current(context.Background())
	assert.Equal(t, ports.AppearanceLight, v, "an absent key is light mode")

	a.err = errors.New("timed out")
	now = now.Add(2 * time.Minute)
	v, _ = p.Current(context.Background())
	assert.Equal(t, ports.AppearanceUnknown, v, "a probe that never completed is no answer, not light")
}

func TestLinuxAndWindows(t *testing.T) {
	calls := 0
	v, _ := NewProbe("linux", fake(&answer{out: "'prefer-dark'\n"}, &calls), time.Now).Current(context.Background())
	assert.Equal(t, ports.AppearanceDark, v)
	v, _ = NewProbe("linux", fake(&answer{code: 1}, &calls), time.Now).Current(context.Background())
	assert.Equal(t, ports.AppearanceUnknown, v)

	v, _ = NewProbe(
		"windows",
		fake(&answer{out: "    AppsUseLightTheme    REG_DWORD    0x0\n"}, &calls),
		time.Now,
	).Current(context.Background())
	assert.Equal(t, ports.AppearanceDark, v)
	v, _ = NewProbe(
		"windows",
		fake(&answer{out: "    AppsUseLightTheme    REG_DWORD    0x1\n"}, &calls),
		time.Now,
	).Current(context.Background())
	assert.Equal(t, ports.AppearanceLight, v)
	v, _ = NewProbe("plan9", fake(&answer{}, &calls), time.Now).Current(context.Background())
	assert.Equal(t, ports.AppearanceUnknown, v)
}
