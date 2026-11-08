package storage

import (
	"errors"
	"fmt"
)

// AgentDeployLevel is the opt-in risk setting that lets agents deploy, start
// and stop flows (docs/design/mcp.md §10). It lives in the settings table and
// is changed only from the editor with password step-up.
type AgentDeployLevel string

const (
	// AgentDeployOff (default): every agent deploy needs admin approval.
	AgentDeployOff AgentDeployLevel = "off"
	// AgentDeployLow: agents may deploy flows whose risk is at most external
	// and that change no recipients, destinations or secret references.
	AgentDeployLow AgentDeployLevel = "low"
	// AgentDeployAll: agents may deploy any flow, including privileged ones.
	AgentDeployAll AgentDeployLevel = "all"
)

const agentDeploySettingKey = "agent_deploy"

// ErrInvalidAgentDeployLevel is returned for values other than off, low, all.
var ErrInvalidAgentDeployLevel = errors.New("agent deploy level must be off, low or all")

// ParseAgentDeployLevel accepts exactly "off", "low" or "all".
func ParseAgentDeployLevel(s string) (AgentDeployLevel, error) {
	switch level := AgentDeployLevel(s); level {
	case AgentDeployOff, AgentDeployLow, AgentDeployAll:
		return level, nil
	}
	return AgentDeployOff, ErrInvalidAgentDeployLevel
}

// AgentDeployLevel returns the stored level (off when unset). A corrupt value
// returns an error together with AgentDeployOff, so callers that only look at
// the level still fail closed.
func (s *SQLiteStore) AgentDeployLevel() (AgentDeployLevel, error) {
	v, ok, err := s.GetSetting(agentDeploySettingKey)
	if err != nil {
		return AgentDeployOff, err
	}
	if !ok {
		return AgentDeployOff, nil
	}
	level, err := ParseAgentDeployLevel(v)
	if err != nil {
		return AgentDeployOff, fmt.Errorf("stored %s setting %q: %w", agentDeploySettingKey, v, err)
	}
	return level, nil
}

// SetAgentDeployLevel stores a validated level.
func (s *SQLiteStore) SetAgentDeployLevel(level AgentDeployLevel) error {
	if _, err := ParseAgentDeployLevel(string(level)); err != nil {
		return err
	}
	return s.SetSetting(agentDeploySettingKey, string(level))
}
