package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ErrConflict is a tool error kind of its own, with the stable code
// "conflict": the request was based on a state that changed (a stale base
// version, a draft no longer pending).
func TestConflictKind(t *testing.T) {
	te := &ToolError{Kind: ErrConflict, Message: "the flow changed", CurrentVersion: 7}
	assert.Equal(t, "conflict", te.Code())
	assert.ErrorIs(t, te, ErrConflict)
	assert.NotErrorIs(t, te, ErrInvalidArguments)
	assert.Equal(t, "the flow changed", te.Error())
	assert.Equal(t, "conflict", (&ToolError{Kind: ErrConflict}).Error())
}
