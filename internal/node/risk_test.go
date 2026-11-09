package node

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRiskValues(t *testing.T) {
	assert.Equal(t, Risk("safe"), RiskSafe)
	assert.Equal(t, Risk("external"), RiskExternal)
	assert.Equal(t, Risk("privileged"), RiskPrivileged)

	for _, r := range []Risk{RiskSafe, RiskExternal, RiskPrivileged} {
		assert.True(t, r.Valid(), r)
	}
	for _, r := range []Risk{"", "Safe", "unknown"} {
		assert.False(t, r.Valid(), r)
	}
}

func TestNodeTypeInfoRiskJSON(t *testing.T) {
	data, err := json.Marshal(NodeTypeInfo{Type: "x", Risk: RiskExternal})
	require.NoError(t, err)
	assert.Contains(t, string(data), `"risk":"external"`)
}

func TestConfigPropertySensitivityJSON(t *testing.T) {
	data, err := json.Marshal(ConfigProperty{Type: "string", Sensitive: true, AgentVisible: true, Unit: "milliseconds"})
	require.NoError(t, err)
	assert.Contains(t, string(data), `"sensitive":true`)
	assert.Contains(t, string(data), `"agentVisible":true`)
	assert.Contains(t, string(data), `"unit":"milliseconds"`)

	// Defaults (not sensitive, redacted for agents, no unit) are omitted.
	data, err = json.Marshal(ConfigProperty{Type: "string"})
	require.NoError(t, err)
	assert.NotContains(t, string(data), "sensitive")
	assert.NotContains(t, string(data), "agentVisible")
	assert.NotContains(t, string(data), "unit")
}
