package filesystem

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// Tab-bar budget, calibrated on a real row: the status area shares
// the tab row with the tabs, and Herdr drops the whole area when it is one
// column too wide rather than truncating it — so the line errs short.
const (
	tabBarReserveFixed  = 24
	tabBarReservePerTab = 5
	tabBarFallbackWidth = 80
	tabBarMaxLine       = 48
	tabBarMinBudget     = 12
)

// tabBarRowFor bounds how long the tab row's width and the workspace's tab
// count are kept. Both are read again whenever the focus moves to another
// pane or workspace, which a new or closed tab normally does; a resize moves
// nothing, so they are read again this often anyway.
const tabBarRowFor = 10 * time.Second

// TabBarHost is the slice of Herdr the tab bar reads.
type TabBarHost interface {
	FocusedPaneCwd(ctx context.Context) (domain.PaneID, domain.WorkspaceID, string, error)
	ForegroundCwd(ctx context.Context, pane domain.PaneID) (string, error)
	TabRowWidth(ctx context.Context, pane domain.PaneID) (int, error)
	TabCount(ctx context.Context, ws domain.WorkspaceID) (int, error)
}

// TabBar implements ports.TabBarPublisher: it precomputes the focused pane's
// path (and branch) into a file Herdr's status command merely types out, so
// Herdr does not start a process to compute it every interval.
type TabBar struct {
	host   TabBarHost
	file   string
	home   string
	branch func(cwd string) string
	write  func(path string, data []byte) error
	now    func() time.Time
	maxLen int

	mu        sync.Mutex
	published string
	row       tabRow
}

// tabRow is the focused pane's tab row as last read.
type tabRow struct {
	pane        domain.PaneID
	ws          domain.WorkspaceID
	width, tabs int
	at          time.Time
}

// NewTabBar writes the line to file.
func NewTabBar(
	host TabBarHost,
	file, home string,
	branch func(string) string,
	write func(string, []byte) error,
	now func() time.Time,
) *TabBar {
	return &TabBar{host: host, file: file, home: home, branch: branch, write: write, now: now, maxLen: tabBarMaxLine}
}

// Publish recomputes the line, badge first, and writes it when it changed.
// An empty line (nothing focused, a failed read, nobody waiting) hides the
// segment, which beats showing a stale one confidently.
func (t *TabBar) Publish(ctx context.Context, badge string) error {
	line, err := t.line(ctx, badge)
	if err != nil {
		line = badge
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	text := line + "\n"
	if text == t.published {
		return err
	}
	if writeErr := t.write(t.file, []byte(text)); writeErr != nil {
		return fmt.Errorf("tab bar: %w", writeErr)
	}
	t.published = text
	return err
}

func (t *TabBar) line(ctx context.Context, badge string) (string, error) {
	pane, ws, launchCwd, err := t.host.FocusedPaneCwd(ctx)
	if err != nil || pane == "" {
		return badge, err
	}
	cwd, cwdErr := t.host.ForegroundCwd(ctx, pane)
	if cwdErr != nil || cwd == "" {
		cwd = launchCwd
	}
	width, tabs := t.focusedRow(ctx, pane, ws)
	if badge == "" {
		return compose(cwd, t.branch(cwd), t.home, width, tabs, t.maxLen), nil
	}
	// Who waits matters more than where you are: the path gives up the room.
	room := len([]rune(badge)) + len([]rune(tabBarSeparator))
	rest := compose(cwd, t.branch(cwd), t.home, width-room, tabs, t.maxLen-room)
	if rest == "" || width-room-(tabBarReserveFixed+tabBarReservePerTab*tabs) < tabBarMinBudget {
		return badge, nil
	}
	return badge + tabBarSeparator + rest, nil
}

// focusedRow is the width of the pane's tab row and the number of tabs in its
// workspace, kept per pane and workspace for tabBarRowFor: they are two of
// the tab bar's four Herdr requests, each a connection of its own, and they
// rarely change. A failed read falls back and is not kept.
func (t *TabBar) focusedRow(ctx context.Context, pane domain.PaneID, ws domain.WorkspaceID) (int, int) {
	now := t.now()
	t.mu.Lock()
	row := t.row
	t.mu.Unlock()
	if row.pane == pane && row.ws == ws && !row.at.IsZero() && now.Sub(row.at) < tabBarRowFor {
		return row.width, row.tabs
	}
	width, widthErr := t.host.TabRowWidth(ctx, pane)
	if widthErr != nil || width <= 0 {
		width = tabBarFallbackWidth
	}
	tabs, tabsErr := t.host.TabCount(ctx, ws)
	if tabsErr != nil || tabs <= 0 {
		tabs = 1
	}
	if widthErr == nil && tabsErr == nil {
		t.mu.Lock()
		t.row = tabRow{pane: pane, ws: ws, width: width, tabs: tabs, at: now}
		t.mu.Unlock()
	}
	return width, tabs
}

// tabBarSeparator joins the badge to the path, as Herdr joins segments.
const tabBarSeparator = " · "

// compose picks the longest candidate that fits. A path loses its leading
// segments first — its information sits at the right end — while the branch,
// short already and the reason for the line, is kept until the path has
// nothing left to give.
func compose(cwd, branch, home string, width, tabs, maxLen int) string {
	if cwd == "" {
		return ""
	}
	budget := max(tabBarMinBudget, min(width-(tabBarReserveFixed+tabBarReservePerTab*tabs), maxLen))
	var paths []string
	for _, candidate := range []string{
		shortenPath(cwd, home, 200), shortenPath(cwd, home, 40), shortenPath(cwd, home, 24), filepath.Base(cwd),
	} {
		if candidate != "" {
			paths = append(paths, candidate)
		}
	}
	options := paths
	if branch != "" {
		options = nil
		for _, p := range paths {
			options = append(options, p+" · "+branch)
		}
		options = append(append(options, paths...), branch)
	}
	for _, option := range options {
		if len([]rune(option)) <= budget {
			return option
		}
	}
	return options[len(options)-1]
}

// shortenPath is home-relative, forward-slashed and elided from the left.
func shortenPath(cwd, home string, maxLength int) string {
	text := strings.TrimRight(strings.ReplaceAll(cwd, `\`, "/"), "/")
	h := strings.TrimRight(strings.ReplaceAll(home, `\`, "/"), "/")
	if h != "" && strings.HasPrefix(strings.ToLower(text), strings.ToLower(h)) {
		text = "~" + text[len(h):]
	}
	if len([]rune(text)) <= maxLength {
		if text == "" {
			return "/"
		}
		return text
	}
	parts := strings.Split(text, "/")
	for len(parts) > 2 && len([]rune(strings.Join(parts, "/"))) > maxLength {
		parts = parts[1:]
	}
	return "…/" + strings.Join(parts, "/")
}

var _ ports.TabBarPublisher = (*TabBar)(nil)
