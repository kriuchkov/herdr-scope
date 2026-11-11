// Package state persists Scope's agent memory and sort-mode choice as small
// files in the plugin state directory, written atomically.
package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// SchemaVersion is the state file's format version.
const SchemaVersion = 1

// File names inside the state directory.
const (
	StateFile    = "state.json"
	SortModeFile = "view-mode"
	FoldsFile    = "folds"
)

// Repository implements ports.StateRepository, ports.SortModeStore and
// ports.FoldStore.
type Repository struct {
	dir    string
	logger ports.Logger
	now    func() time.Time

	mu sync.Mutex
	// written is the fingerprint of the memory last written.
	written []byte
}

// NewRepository stores files in dir, creating it on first write.
func NewRepository(dir string, logger ports.Logger, now func() time.Time) *Repository {
	return &Repository{dir: dir, logger: logger, now: now}
}

// Path is the state file's path.
func (r *Repository) Path() string { return filepath.Join(r.dir, StateFile) }

type document struct {
	SchemaVersion int      `json:"schema_version"`
	SavedAt       string   `json:"saved_at"`
	Agents        []record `json:"agents"`
}

type record struct {
	Agent      string     `json:"agent_id"`
	Pane       string     `json:"pane_id,omitempty"`
	Terminal   string     `json:"terminal_id,omitempty"`
	Workspace  string     `json:"workspace_id,omitempty"`
	Kind       string     `json:"kind,omitempty"`
	LastStatus string     `json:"last_status,omitempty"`
	Activity   *time.Time `json:"last_activity,omitempty"`
	Done       *done      `json:"done,omitempty"`
	Blocked    *blocked   `json:"blocked,omitempty"`
	LastSeen   *time.Time `json:"last_seen,omitempty"`
	TurnStart  *time.Time `json:"turn_start,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type done struct {
	SetAt     time.Time  `json:"set_at"`
	UntilSeen bool       `json:"until_seen"`
	Until     *time.Time `json:"until,omitempty"`
}

type blocked struct {
	Since     time.Time `json:"since"`
	HadWorked bool      `json:"had_worked"`
}

// Load reads the memory. A missing file is empty memory. A corrupt or
// unknown-version file is quarantined beside the original and Scope starts
// clean: a damaged local file must never stop the daemon.
func (r *Repository) Load(_ context.Context) ([]domain.AgentSession, error) {
	raw, err := os.ReadFile(r.Path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %w", domain.ErrPersistence, r.Path(), err)
	}
	var doc document
	if err = json.Unmarshal(raw, &doc); err != nil || doc.SchemaVersion != SchemaVersion {
		reason := "unsupported schema version"
		if err != nil {
			reason = err.Error()
		}
		r.quarantine(reason)
		return nil, nil
	}
	sessions := make([]domain.AgentSession, 0, len(doc.Agents))
	for _, rec := range doc.Agents {
		if rec.Agent != "" {
			sessions = append(sessions, rec.session())
		}
	}
	return sessions, nil
}

func (r *Repository) quarantine(reason string) {
	target := fmt.Sprintf("%s.corrupt-%s", r.Path(), r.now().UTC().Format("20060102T150405Z"))
	if err := os.Rename(r.Path(), target); err != nil {
		r.logger.Warn("state file unreadable and could not be moved aside; starting clean", "path", r.Path(), "error", err)
		return
	}
	r.logger.Warn("state file unreadable; moved aside, starting clean", "path", r.Path(), "backup", target, "reason", reason)
}

// Save writes the memory atomically, unless it differs from the memory last
// written only in times finer than the file needs (fingerprint): the daemon
// hands it over every few seconds for as long as it runs, and each frame
// moves some time. What is written is exact.
func (r *Repository) Save(_ context.Context, sessions []domain.AgentSession) error {
	records := make([]record, 0, len(sessions))
	for _, s := range sessions {
		records = append(records, recordOf(s))
	}
	coarse, err := fingerprint(records)
	if err != nil {
		return fmt.Errorf("%w: encode state: %w", domain.ErrPersistence, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if bytes.Equal(coarse, r.written) {
		return nil
	}
	doc := document{SchemaVersion: SchemaVersion, SavedAt: r.now().UTC().Format(time.RFC3339Nano), Agents: records}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: encode state: %w", domain.ErrPersistence, err)
	}
	if err = WriteAtomic(r.Path(), append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("%w: %w", domain.ErrPersistence, err)
	}
	r.written = coarse
	return nil
}

// fingerprint is what a save has to change to be written: the records with
// the times a row shows (activity, turn start, markers) at minute grain, the
// bookkeeping ones (last seen, updated-at) at hour grain, everything else
// exact. A working agent's activity and every agent's updated-at move with
// each frame; at these grains an idle memory is not written at all and a
// working one about once a minute.
func fingerprint(records []record) ([]byte, error) {
	coarse := make([]record, len(records))
	for i, rec := range records {
		rec.Activity = truncate(rec.Activity, time.Minute)
		rec.TurnStart = truncate(rec.TurnStart, time.Minute)
		rec.LastSeen = truncate(rec.LastSeen, time.Hour)
		rec.UpdatedAt = rec.UpdatedAt.Truncate(time.Hour)
		if rec.Done != nil {
			d := *rec.Done
			d.SetAt, d.Until = d.SetAt.Truncate(time.Minute), truncate(d.Until, time.Minute)
			rec.Done = &d
		}
		if rec.Blocked != nil {
			b := *rec.Blocked
			b.Since = b.Since.Truncate(time.Minute)
			rec.Blocked = &b
		}
		coarse[i] = rec
	}
	raw, err := json.Marshal(coarse)
	if err != nil {
		return nil, fmt.Errorf("fingerprint: %w", err)
	}
	return raw, nil
}

func truncate(t *time.Time, grain time.Duration) *time.Time {
	if t == nil {
		return nil
	}
	coarse := t.Truncate(grain)
	return &coarse
}

// LoadSortMode reads the persisted panel order. A missing file means nobody
// chose yet; "off" is written out as a word so the two stay distinct.
func (r *Repository) LoadSortMode() (domain.SortMode, bool, error) {
	raw, err := os.ReadFile(filepath.Join(r.dir, SortModeFile))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("%w: %w", domain.ErrPersistence, err)
	}
	mode, ok := domain.ParseSortMode(strings.TrimSpace(string(raw)))
	if !ok {
		return "", false, nil
	}
	return mode, true, nil
}

// SaveSortMode persists the panel order; always a write, never a delete.
func (r *Repository) SaveSortMode(mode domain.SortMode) error {
	if err := WriteAtomic(filepath.Join(r.dir, SortModeFile), []byte(string(mode)+"\n"), 0o600); err != nil {
		return fmt.Errorf("%w: %w", domain.ErrPersistence, err)
	}
	return nil
}

// LoadFolds reads the folded families' keys, one a line. A missing file means
// nothing is folded.
func (r *Repository) LoadFolds() ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(r.dir, FoldsFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrPersistence, err)
	}
	var keys []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		if key := strings.TrimSpace(line); key != "" {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// SaveFolds persists the folded families' keys.
func (r *Repository) SaveFolds(keys []string) error {
	text := ""
	if len(keys) > 0 {
		text = strings.Join(keys, "\n") + "\n"
	}
	if err := WriteAtomic(filepath.Join(r.dir, FoldsFile), []byte(text), 0o600); err != nil {
		return fmt.Errorf("%w: %w", domain.ErrPersistence, err)
	}
	return nil
}

func recordOf(s domain.AgentSession) record {
	rec := record{
		Agent: string(s.Agent), Pane: string(s.Pane), Terminal: string(s.Terminal),
		Workspace: string(s.Workspace), Kind: string(s.Kind),
		LastStatus: string(s.LastStatus), UpdatedAt: s.UpdatedAt.UTC(),
	}
	if s.Activity.Known() {
		at := s.Activity.At.UTC()
		rec.Activity = &at
	}
	if !s.LastSeen.IsZero() {
		seen := s.LastSeen.UTC()
		rec.LastSeen = &seen
	}
	if !s.TurnStart.IsZero() {
		start := s.TurnStart.UTC()
		rec.TurnStart = &start
	}
	if s.Done != nil {
		rec.Done = &done{SetAt: s.Done.SetAt.UTC(), UntilSeen: s.Done.UntilSeen}
		if !s.Done.UntilSeen {
			until := s.Done.Until.UTC()
			rec.Done.Until = &until
		}
	}
	if s.Blocked != nil {
		rec.Blocked = &blocked{Since: s.Blocked.Since.UTC(), HadWorked: s.Blocked.HadWorked}
	}
	return rec
}

func (rec record) session() domain.AgentSession {
	s := domain.AgentSession{
		Agent: domain.AgentID(rec.Agent), Pane: domain.PaneID(rec.Pane), Terminal: domain.TerminalID(rec.Terminal),
		Workspace: domain.WorkspaceID(rec.Workspace), Kind: domain.AgentKind(rec.Kind),
		LastStatus: domain.ParseAgentStatus(rec.LastStatus), UpdatedAt: rec.UpdatedAt,
	}
	if rec.Activity != nil {
		s.Activity = domain.Activity{At: *rec.Activity}
	}
	if rec.LastSeen != nil {
		s.LastSeen = *rec.LastSeen
	}
	if rec.TurnStart != nil {
		s.TurnStart = *rec.TurnStart
	}
	if rec.Done != nil {
		s.Done = &domain.CompletionMarker{SetAt: rec.Done.SetAt, UntilSeen: rec.Done.UntilSeen}
		if rec.Done.Until != nil {
			s.Done.Until = *rec.Done.Until
		}
	}
	if rec.Blocked != nil {
		s.Blocked = &domain.BlockedMarker{Since: rec.Blocked.Since, HadWorked: rec.Blocked.HadWorked}
	}
	return s
}
