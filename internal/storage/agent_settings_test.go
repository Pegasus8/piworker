package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentDeployLevelDefaultsToOff(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	level, err := s.AgentDeployLevel()
	require.NoError(t, err)
	assert.Equal(t, AgentDeployOff, level)
}

func TestAgentDeployLevelRoundTrip(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	for _, level := range []AgentDeployLevel{AgentDeployLow, AgentDeployAll, AgentDeployOff} {
		require.NoError(t, s.SetAgentDeployLevel(level))
		got, err := s.AgentDeployLevel()
		require.NoError(t, err)
		assert.Equal(t, level, got)
	}
}

func TestSetAgentDeployLevelRejectsUnknown(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	require.NoError(t, s.SetAgentDeployLevel(AgentDeployLow))
	for _, bad := range []AgentDeployLevel{"", "LOW", "on", "everything"} {
		require.ErrorIs(t, s.SetAgentDeployLevel(bad), ErrInvalidAgentDeployLevel)
	}
	got, err := s.AgentDeployLevel()
	require.NoError(t, err)
	assert.Equal(t, AgentDeployLow, got)
}

func TestCorruptAgentDeployLevelFailsClosed(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	require.NoError(t, s.SetSetting("agent_deploy", "yolo"))
	level, err := s.AgentDeployLevel()
	require.Error(t, err)
	assert.Equal(t, AgentDeployOff, level, "an unreadable level must never widen agent power")
}

func TestParseAgentDeployLevel(t *testing.T) {
	for _, ok := range []string{"off", "low", "all"} {
		level, err := ParseAgentDeployLevel(ok)
		require.NoError(t, err)
		assert.Equal(t, AgentDeployLevel(ok), level)
	}
	_, err := ParseAgentDeployLevel("ALL")
	require.ErrorIs(t, err, ErrInvalidAgentDeployLevel)
}
