package sidebar

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

// renamed is a rename Scope sent: the label it replaced and the label it
// gave. The session is read less often than frames run, so until the next
// read the tab still shows from.
type renamed struct {
	from, to string
}

// markTabs names each tab for its agents: its own name, then the mark of the
// most urgent wait, in the sidebar's own glyph.
func (w *Writer) markTabs(ctx context.Context, now time.Time, marks []domain.TabMark) error {
	live := map[domain.TabID]renamed{}
	var errs []error
	for _, m := range marks {
		mark := ""
		if m.Waiting != "" {
			mark = w.presentation.Glyphs.State(m.Waiting)
		}
		want := domain.TabLabel(m.Name, mark)
		current := m.Label
		if r, ok := w.tabs[m.Tab]; ok && m.Label == r.from {
			current = r.to
			live[m.Tab] = r
		}
		if want == current {
			continue
		}
		key := tabKey(m.Tab)
		if f, ok := w.failed[key]; ok && now.Before(f.until) {
			continue
		}
		if err := w.host.RenameTab(ctx, m.Tab, want); err != nil {
			w.fail(key, now)
			errs = append(errs, err)
			continue
		}
		delete(w.failed, key)
		live[m.Tab] = renamed{from: m.Label, to: want}
	}
	w.tabs = live
	if len(errs) > 0 {
		return fmt.Errorf("%w: %d of %d tab renames failed: %w", domain.ErrProjection, len(errs), len(marks), errors.Join(errs...))
	}
	return nil
}

// unmarkTabs takes Scope's mark off every tab that still carries one.
func (w *Writer) unmarkTabs(ctx context.Context) error {
	tabs, err := w.host.Tabs(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, m := range domain.TabMarks(tabs, nil, false) {
		errs = append(errs, w.host.RenameTab(ctx, m.Tab, m.Name))
	}
	return errors.Join(errs...)
}

func tabKey(tab domain.TabID) string { return "tab:" + string(tab) }
