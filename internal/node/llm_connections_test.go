package node

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type llmNode struct{ mockNode }

func (n *llmNode) RequiredLLMConnections() map[string]string {
	return map[string]string{"connection": "claude"}
}

// LLM nodes name their connection through LLMConnectionRequirer, apart from
// the broker connections (ConnectionRequirer): the two stores differ.
func TestLLMConnectionRequirer(t *testing.T) {
	var n Node = &llmNode{}
	req, ok := n.(LLMConnectionRequirer)
	assert.True(t, ok)
	assert.Equal(t, map[string]string{"connection": "claude"}, req.RequiredLLMConnections())
	_, broker := n.(ConnectionRequirer)
	assert.False(t, broker)
}
