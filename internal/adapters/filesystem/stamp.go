package filesystem

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// Stamp implements ports.SetupStamp as a one-line file: which build ran
// first-run setup. A later start does not redo — or undo — what the user
// changed by hand since.
type Stamp struct {
	path  string
	write func(path string, data []byte) error
}

// NewStamp keeps the stamp at <dir>/setup.done.
func NewStamp(dir string, write func(path string, data []byte) error) *Stamp {
	return &Stamp{path: filepath.Join(dir, "setup.done"), write: write}
}

// Read returns the stamped build.
func (s *Stamp) Read() (string, bool) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(raw)), true
}

// Write records build.
func (s *Stamp) Write(build string) error {
	if err := s.write(s.path, []byte(build+"\n")); err != nil {
		return fmt.Errorf("setup stamp: %w", err)
	}
	return nil
}

// Clear removes the stamp so the next start sets up again.
func (s *Stamp) Clear() error {
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("setup stamp: %w", err)
	}
	return nil
}

// Switch implements ports.OffSwitch as the presence of <dir>/off.
type Switch struct {
	path  string
	write func(path string, data []byte) error
}

// NewSwitch keeps the switch at <dir>/off.
func NewSwitch(dir string, write func(path string, data []byte) error) *Switch {
	return &Switch{path: filepath.Join(dir, "off"), write: write}
}

// Off reports whether Scope was turned off.
func (s *Switch) Off() bool {
	_, err := os.Stat(s.path)
	return err == nil
}

// SetOff turns Scope off or back on.
func (s *Switch) SetOff(off bool) error {
	if off {
		if err := s.write(s.path, []byte("unconfigured; the state-start or configure action turns Scope back on\n")); err != nil {
			return fmt.Errorf("off switch: %w", err)
		}
		return nil
	}
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("off switch: %w", err)
	}
	return nil
}

var (
	_ ports.SetupStamp = (*Stamp)(nil)
	_ ports.OffSwitch  = (*Switch)(nil)
)
