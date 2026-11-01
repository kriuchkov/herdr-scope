package state

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/herdr-scope/internal/domain"
)

type recordingLogger struct{ warnings []string }

func (l *recordingLogger) Debug(string, ...any)       {}
func (l *recordingLogger) Info(string, ...any)        {}
func (l *recordingLogger) Warn(msg string, _ ...any)  { l.warnings = append(l.warnings, msg) }
func (l *recordingLogger) Error(msg string, _ ...any) { l.warnings = append(l.warnings, msg) }

func fixedNow() time.Time { return time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC) }

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	repo := NewRepository(dir, &recordingLogger{}, fixedNow)
	now := fixedNow()
	sessions := []domain.AgentSession{
		{
			Agent: "claude:s1", Pane: "w1:p1", Terminal: "term_a", Workspace: "w1", Kind: "claude",
			LastStatus: domain.StatusIdle, Activity: domain.Activity{At: now.Add(-time.Hour)},
			Done:     &domain.CompletionMarker{SetAt: now, UntilSeen: true},
			LastSeen: now.Add(-2 * time.Hour), TurnStart: now.Add(-time.Minute), UpdatedAt: now,
		},
		{
			Agent: "term:t2", Pane: "w1:p2", UpdatedAt: now,
			Done:    &domain.CompletionMarker{SetAt: now, Until: now.Add(30 * time.Second)},
			Blocked: &domain.BlockedMarker{Since: now, HadWorked: true},
		},
	}
	require.NoError(t, repo.Save(context.Background(), sessions))
	loaded, err := repo.Load(context.Background())
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	assert.Equal(t, sessions[0].Activity.At, loaded[0].Activity.At)
	assert.True(t, loaded[0].Done.UntilSeen)
	assert.Equal(t, sessions[1].Done.Until, loaded[1].Done.Until)
	assert.True(t, loaded[1].Blocked.HadWorked)
	assert.False(t, loaded[1].Activity.Known())
	assert.Equal(t, domain.WorkspaceID("w1"), loaded[0].Workspace)
	assert.Equal(t, sessions[0].TurnStart, loaded[0].TurnStart, "a turn's start survives a restart")
	assert.True(t, loaded[1].TurnStart.IsZero())

	info, err := os.Stat(repo.Path())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	raw, err := os.ReadFile(repo.Path())
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"schema_version": 1`)
}

func TestSavesThatChangeNothingWorthAWriteAreSkipped(t *testing.T) {
	ticks := fixedNow()
	repo := NewRepository(t.TempDir(), &recordingLogger{}, func() time.Time {
		ticks = ticks.Add(time.Second) // every write has its own saved_at
		return ticks
	})
	ctx := context.Background()
	file := func() string {
		raw, err := os.ReadFile(repo.Path())
		require.NoError(t, err)
		return string(raw)
	}
	t0 := fixedNow()
	agent := domain.AgentSession{
		Agent: "claude:s1", Pane: "w1:p1", Kind: "claude", LastStatus: domain.StatusWorking,
		Activity: domain.Activity{At: t0.Add(10 * time.Second)}, TurnStart: t0, LastSeen: t0, UpdatedAt: t0.Add(10 * time.Second),
	}
	require.NoError(t, repo.Save(ctx, []domain.AgentSession{agent}))
	written := file()

	agent.Activity.At, agent.LastSeen, agent.UpdatedAt = t0.Add(50*time.Second), t0.Add(40*time.Minute), t0.Add(50*time.Minute)
	require.NoError(t, repo.Save(ctx, []domain.AgentSession{agent}))
	assert.Equal(t, written, file(), "activity within its minute, bookkeeping within its hour: not rewritten")

	agent.Activity.At = t0.Add(70 * time.Second)
	require.NoError(t, repo.Save(ctx, []domain.AgentSession{agent}))
	assert.NotEqual(t, written, file(), "the activity reached the next minute")
	loaded, err := repo.Load(ctx)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, t0.Add(70*time.Second), loaded[0].Activity.At, "what is written is exact")
	assert.Equal(t, t0.Add(50*time.Minute), loaded[0].UpdatedAt)

	for name, change := range map[string]func(){
		"a new marker": func() { agent.Done = &domain.CompletionMarker{SetAt: t0.Add(70 * time.Second), UntilSeen: true} },
		"a new status": func() { agent.LastStatus = domain.StatusIdle },
		"another hour": func() { agent.UpdatedAt = t0.Add(61 * time.Minute) },
		"another pane": func() { agent.Pane = "w2:p1" },
	} {
		written = file()
		change()
		require.NoError(t, repo.Save(ctx, []domain.AgentSession{agent}))
		assert.NotEqual(t, written, file(), name)
	}
}

func TestFileWithARepositoryIDStillLoads(t *testing.T) {
	repo := NewRepository(t.TempDir(), &recordingLogger{}, fixedNow)
	old := `{"schema_version": 1, "saved_at": "2026-10-23T12:00:00Z", "agents": [
		{"agent_id": "claude:s1", "workspace_id": "w1", "repository_id": "/src/app/.git",
		 "last_activity": "2026-10-23T11:00:00Z", "updated_at": "2026-10-23T12:00:00Z"}]}`
	require.NoError(t, os.WriteFile(repo.Path(), []byte(old), 0o600))
	loaded, err := repo.Load(context.Background())
	require.NoError(t, err)
	require.Len(t, loaded, 1, "a key Scope no longer writes is no reason to start clean")
	assert.Equal(t, domain.AgentID("claude:s1"), loaded[0].Agent)
	assert.Equal(t, fixedNow().Add(-time.Hour), loaded[0].Activity.At)
	_, err = os.Stat(repo.Path())
	require.NoError(t, err, "not quarantined")
}

func TestMissingFileIsEmptyMemory(t *testing.T) {
	loaded, err := NewRepository(t.TempDir(), &recordingLogger{}, fixedNow).Load(context.Background())
	require.NoError(t, err)
	assert.Empty(t, loaded)
}

func TestCorruptFileIsQuarantined(t *testing.T) {
	dir := t.TempDir()
	logger := &recordingLogger{}
	repo := NewRepository(dir, logger, fixedNow)
	require.NoError(t, os.WriteFile(repo.Path(), []byte(`{"schema_version": 1, "agents": [`), 0o600))
	loaded, err := repo.Load(context.Background())
	require.NoError(t, err, "a damaged local file must not stop the daemon")
	assert.Empty(t, loaded)
	assert.NotEmpty(t, logger.warnings)
	backups, _ := filepath.Glob(filepath.Join(dir, "state.json.corrupt-*"))
	assert.Len(t, backups, 1, "the damaged file is kept for inspection")
	_, err = os.Stat(repo.Path())
	assert.True(t, os.IsNotExist(err))

	require.NoError(t, repo.Save(context.Background(), []domain.AgentSession{{Agent: "a"}}))
	loaded, err = repo.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, loaded, 1, "it continues with a clean file")
}

func TestUnknownSchemaVersionIsQuarantined(t *testing.T) {
	dir := t.TempDir()
	repo := NewRepository(dir, &recordingLogger{}, fixedNow)
	require.NoError(t, os.WriteFile(repo.Path(), []byte(`{"schema_version": 99, "agents": []}`), 0o600))
	loaded, err := repo.Load(context.Background())
	require.NoError(t, err)
	assert.Empty(t, loaded)
}

func TestSortMode(t *testing.T) {
	repo := NewRepository(t.TempDir(), &recordingLogger{}, fixedNow)
	_, ok, err := repo.LoadSortMode()
	require.NoError(t, err)
	assert.False(t, ok, "never chosen is not off")
	require.NoError(t, repo.SaveSortMode(domain.SortOff))
	mode, ok, err := repo.LoadSortMode()
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, domain.SortOff, mode)
}

func TestFolds(t *testing.T) {
	repo := NewRepository(t.TempDir(), &recordingLogger{}, fixedNow)
	keys, err := repo.LoadFolds()
	require.NoError(t, err)
	assert.Empty(t, keys, "no file: nothing folded")
	require.NoError(t, repo.SaveFolds([]string{"/p/acme", "w3"}))
	keys, err = repo.LoadFolds()
	require.NoError(t, err)
	assert.Equal(t, []string{"/p/acme", "w3"}, keys)
	require.NoError(t, repo.SaveFolds(nil))
	keys, err = repo.LoadFolds()
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestWriteAtomicFollowsSymlinksAndLeavesNoTemporaries(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "config.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))
	link := filepath.Join(dir, "config.toml")
	require.NoError(t, os.Symlink(target, link))

	require.NoError(t, WriteAtomic(link, []byte("new"), 0o600))
	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the link stays a link")
	raw, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "new", string(raw))
	entries, err := os.ReadDir(filepath.Dir(target))
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasSuffix(e.Name(), ".tmp"), "a temporary file was left behind")
	}
}
