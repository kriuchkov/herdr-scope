package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

// fakeRepo lays out a main checkout and a linked worktree by hand, the way
// git does, so the test needs no git binary.
func fakeRepo(t *testing.T) (string, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	main := filepath.Join(base, "app")
	feature := filepath.Join(base, "app-feature")
	gitDir := filepath.Join(main, ".git")
	worktreeGit := filepath.Join(gitDir, "worktrees", "app-feature")
	require.NoError(t, os.MkdirAll(worktreeGit, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(main, "src", "deep"), 0o755))
	require.NoError(t, os.MkdirAll(feature, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(worktreeGit, "HEAD"), []byte("ref: refs/heads/feature/auth\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(worktreeGit, "commondir"), []byte("../..\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(feature, ".git"), []byte("gitdir: "+worktreeGit+"\n"), 0o600))
	return main, feature
}

func TestResolveMainCheckoutAndWorktree(t *testing.T) {
	main, feature := fakeRepo(t)
	r := Resolver{}
	info, err := r.Resolve(context.Background(), filepath.Join(main, "src", "deep"))
	require.NoError(t, err)
	assert.Equal(t, domain.RepositoryID(filepath.Join(main, ".git")), info.ID, "the key is the common git directory, as Herdr's repo_key")
	assert.Equal(t, "app", info.Name)
	assert.Equal(t, main, info.Root)
	assert.Equal(t, domain.WorktreeID(main), info.Checkout)
	assert.False(t, info.Linked)

	wt, err := r.Resolve(context.Background(), feature)
	require.NoError(t, err)
	assert.Equal(t, info.ID, wt.ID, "a worktree shares its repository's key")
	assert.True(t, wt.Linked)
	assert.Equal(t, "app", wt.Name)
	assert.Equal(t, domain.WorktreeID(feature), wt.Checkout)
}

func TestResolveOutsideAnyCheckout(t *testing.T) {
	_, err := Resolver{}.Resolve(context.Background(), t.TempDir())
	require.ErrorIs(t, err, domain.ErrNotRepository)
}

func TestBranch(t *testing.T) {
	main, feature := fakeRepo(t)
	assert.Equal(t, "main", Branch(filepath.Join(main, "src")))
	assert.Equal(t, "feature/auth", Branch(feature))
	require.NoError(t, os.WriteFile(filepath.Join(main, ".git", "HEAD"), []byte("0123456789abcdef0123456789abcdef01234567\n"), 0o600))
	assert.Equal(t, "0123456", Branch(main), "a detached HEAD shows seven characters")
	assert.Empty(t, Branch(t.TempDir()))
	assert.Empty(t, Branch(""))
}

// TestAgainstRealGit cross-checks the hand layout with git itself when it is
// installed; Herdr's own keys were recorded the same way.
func TestAgainstRealGit(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	main := filepath.Join(base, "repo")
	run := func(dir string, args ...string) {
		cmd := exec.Command(gitBin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, runErr := cmd.CombinedOutput()
		require.NoError(t, runErr, string(out))
	}
	require.NoError(t, os.MkdirAll(main, 0o755))
	run(main, "init", "-q", "-b", "main")
	run(main, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	run(main, "worktree", "add", "-q", filepath.Join(base, "wt"), "-b", "feature/x")
	wt, err := Resolver{}.Resolve(context.Background(), filepath.Join(base, "wt"))
	require.NoError(t, err)
	assert.True(t, wt.Linked)
	assert.Equal(t, domain.RepositoryID(filepath.Join(main, ".git")), wt.ID)
	assert.Equal(t, "feature/x", Branch(filepath.Join(base, "wt")))
}
