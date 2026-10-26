package ports

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultSettingsAreValid(t *testing.T) {
	require.NoError(t, DefaultSettings().Validate())
}

func TestSettingsValidation(t *testing.T) {
	s := DefaultSettings()
	s.Presentation.Variant = "emoji"
	s.Presentation.ActiveRowBgDark = ColorOverride{Set: true, Value: "not a colour!"}
	s.Observer.Heartbeat = time.Millisecond
	s.LogLevel = "trace"
	s.Policy.GroupIndent = -1
	err := s.Validate()
	require.ErrorIs(t, err, ErrInvalidSettings)
	for _, want := range []string{"variant", "active_row_bg_dark", "heartbeat_interval", "log_level", "group_indent"} {
		assert.Contains(t, err.Error(), want)
	}
}

func TestValidColor(t *testing.T) {
	for _, value := range []string{"#b9cdf2", "#FFF", "rgb(1, 2, 3)", "RGB(1,2,3)", "reset"} {
		assert.True(t, ValidColor(value), value)
	}
	for _, value := range []string{"", "#ffff", "rgb(1,2)", "not a colour!", "dark-blue"} {
		assert.False(t, ValidColor(value), value)
	}
}

func TestColorOverrides(t *testing.T) {
	for _, value := range []string{"#b9cdf2", "#fff", "rgb(1, 2, 3)", "reset", ""} {
		s := DefaultSettings()
		s.Presentation.ActiveRowBgLight = ColorOverride{Set: true, Value: value}
		assert.NoError(t, s.Validate(), value)
	}
}
