package herdr

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTheLaunchCwdStandsInForAMissingForegroundOne(t *testing.T) {
	codex, launch, title := "codex", "/home/user/src/notes", "notes"
	agent := Mapper{Home: "/home/user"}.agent(wireAgent{PaneID: "w1:p1", Agent: &codex, Cwd: &launch, TerminalTitleStripped: &title})
	assert.Equal(t, "Codex", agent.Title, "a title naming only the launch directory becomes the vendor")
}
