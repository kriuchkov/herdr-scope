package domain

import "time"

// Activity is when an agent last did any work. Herdr reports what an agent is
// doing, never when it last did it; this is what turns one idle into three.
type Activity struct {
	At time.Time
}

// Known reports whether there is a stamp at all.
func (a Activity) Known() bool {
	return !a.At.IsZero()
}

// Freshness classifies an idle pane by time since its last turn. A pane with
// no stamp reads as plain idle: an agent Scope never saw work is evidence of a
// restart, not of neglect.
func (a Activity) Freshness(now time.Time, policy DisplayPolicy) DisplayState {
	if !a.Known() {
		return DisplayIdle
	}
	age := now.Sub(a.At)
	if age <= policy.FreshFor {
		return DisplayIdleFresh
	}
	if age >= policy.StaleAfter {
		return DisplayIdleStale
	}
	return DisplayIdle
}

// Crossings are the future moments at which an idle pane changes tier; a
// sleeping observer has to wake for them. The first lands just past the fresh
// threshold, because a pane exactly at it still reads as fresh.
func (a Activity) Crossings(now time.Time, policy DisplayPolicy) []time.Time {
	if !a.Known() {
		return nil
	}
	var out []time.Time
	for _, at := range []time.Time{a.At.Add(policy.FreshFor + time.Millisecond), a.At.Add(policy.StaleAfter)} {
		if at.After(now) {
			out = append(out, at)
		}
	}
	return out
}
