// Package git reads git metadata straight from the .git directory. It never
// runs git: the observer asks per workspace and the tab bar every couple of
// seconds, and a process spawn per question would cost more than everything
// the answers are used for.
package git

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// Resolver implements ports.RepositoryResolver.
type Resolver struct{}

// layout is where one checkout's git data lives.
type layout struct {
	// worktree is the checkout's top directory.
	worktree string
	// gitDir is this checkout's own git directory.
	gitDir string
	// commonDir is the directory shared by every worktree of the repository.
	commonDir string
	linked    bool
}

// Resolve finds the checkout path belongs to. The repository id is the
// common git directory, absolute and with symlinks resolved — the value Herdr
// reports as repo_key, so the two sources agree.
func (Resolver) Resolve(_ context.Context, path string) (domain.RepositoryInfo, error) {
	l, ok := find(path)
	if !ok {
		return domain.RepositoryInfo{}, domain.ErrNotRepository
	}
	root := l.commonDir
	if filepath.Base(root) == ".git" {
		root = filepath.Dir(root)
	}
	return domain.RepositoryInfo{
		ID:       domain.RepositoryID(canonical(l.commonDir)),
		Name:     filepath.Base(root),
		Root:     canonical(root),
		Checkout: domain.WorktreeID(canonical(l.worktree)),
		Linked:   l.linked,
	}, nil
}

// find walks up from start to the nearest .git. A directory is a main
// checkout; a file holds `gitdir: <path>`, and a gitdir with a commondir file
// is a linked worktree (a submodule's has none).
func find(start string) (layout, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return layout{}, false
	}
	for {
		candidate := filepath.Join(dir, ".git")
		if info, statErr := os.Stat(candidate); statErr == nil {
			if info.IsDir() {
				return layout{worktree: dir, gitDir: candidate, commonDir: candidate}, true
			}
			if l, ok := fromPointer(dir, candidate); ok {
				return l, true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return layout{}, false
		}
		dir = parent
	}
}

var gitdirLine = regexp.MustCompile(`(?m)^gitdir:\s*(.+)$`)

func fromPointer(dir, file string) (layout, bool) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return layout{}, false
	}
	m := gitdirLine.FindStringSubmatch(string(raw))
	if m == nil {
		return layout{}, false
	}
	gitDir := resolveFrom(dir, strings.TrimSpace(m[1]))
	l := layout{worktree: dir, gitDir: gitDir, commonDir: gitDir}
	commondir := filepath.Join(gitDir, "commondir")
	//nolint:gosec // the git directory a .git file in a workspace points at
	if common, readErr := os.ReadFile(commondir); readErr == nil {
		l.commonDir = resolveFrom(gitDir, strings.TrimSpace(string(common)))
		l.linked = true
	}
	return l, true
}

func resolveFrom(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(base, path))
}

func canonical(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

var (
	refLine = regexp.MustCompile(`^ref:\s*refs/heads/(.+)$`)
	shaLine = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)
)

// Branch is the branch cwd is on, read from HEAD; a detached HEAD shows the
// first seven characters of its commit, as git does. Empty outside a checkout.
// Always current, unlike a branch cached when an agent started.
func Branch(cwd string) string {
	if cwd == "" {
		return ""
	}
	l, ok := find(cwd)
	if !ok {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(l.gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(raw))
	if m := refLine.FindStringSubmatch(head); m != nil {
		return m[1]
	}
	if shaLine.MatchString(head) {
		return head[:7]
	}
	return ""
}

var _ ports.RepositoryResolver = Resolver{}
