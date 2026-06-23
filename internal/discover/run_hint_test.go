package discover

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/project"
)

func TestShouldHintDiscover(t *testing.T) {
	assert.True(t, ShouldHintDiscover(nil, true), "empty services + compose present → hint")
	assert.False(t, ShouldHintDiscover(nil, false), "no compose → no hint")
	assert.False(t, ShouldHintDiscover([]project.Service{{Name: "x"}}, true), "services configured → no hint")
}

func TestHintMessage_MentionsDiscover(t *testing.T) {
	assert.Contains(t, HintMessage, "cerberus discover")
}
