package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPDefaults(t *testing.T) {
	cfg, err := Load("")
	require.NoError(t, err)
	assert.False(t, cfg.MCP.Enabled, "MCP must be off unless explicitly enabled")
	assert.False(t, cfg.MCP.LANAccess)
	assert.Equal(t, 4, cfg.MCP.MaxInflight)
	assert.Equal(t, 16384, cfg.MCP.MaxResultBytes)
	assert.False(t, cfg.MCP.AuthorEnabled, "authoring tools are opt-in")
	require.NoError(t, cfg.Validate())
}

func TestMCPSectionFromTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "piworker.toml")
	require.NoError(t, os.WriteFile(path, []byte(`
[mcp]
enabled = true
lan_access = true
max_inflight = 8
max_result_bytes = 32768
author_enabled = true
`), 0o600))
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, MCPConfig{Enabled: true, LANAccess: true, MaxInflight: 8, MaxResultBytes: 32768, AuthorEnabled: true}, cfg.MCP)
}

func TestMCPSectionRejectsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "piworker.toml")
	require.NoError(t, os.WriteFile(path, []byte("[mcp]\nenabeld = true\n"), 0o600))
	_, err := Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "mcp.enabeld")
}

func TestMCPEnvOverridesTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "piworker.toml")
	require.NoError(t, os.WriteFile(path, []byte("[mcp]\nenabled = false\nmax_inflight = 8\n"), 0o600))
	t.Setenv("PIWORKER_MCP_ENABLED", "yes")
	t.Setenv("PIWORKER_MCP_LAN_ACCESS", "on")
	t.Setenv("PIWORKER_MCP_MAX_INFLIGHT", " 2 ")
	t.Setenv("PIWORKER_MCP_MAX_RESULT_BYTES", "4096")
	t.Setenv("PIWORKER_MCP_AUTHOR_ENABLED", "1")
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, MCPConfig{Enabled: true, LANAccess: true, MaxInflight: 2, MaxResultBytes: 4096, AuthorEnabled: true}, cfg.MCP)
}

func TestMCPBoolEnvsFailClosed(t *testing.T) {
	for _, name := range []string{"PIWORKER_MCP_ENABLED", "PIWORKER_MCP_LAN_ACCESS", "PIWORKER_MCP_AUTHOR_ENABLED"} {
		for _, v := range []string{"", "  ", "maybe", "2"} {
			t.Run(name+"="+v, func(t *testing.T) {
				t.Setenv(name, v)
				_, err := Load("")
				require.Error(t, err)
				require.Contains(t, err.Error(), name)
			})
		}
	}
}

func TestMCPIntEnvsRejectGarbage(t *testing.T) {
	for _, name := range []string{"PIWORKER_MCP_MAX_INFLIGHT", "PIWORKER_MCP_MAX_RESULT_BYTES"} {
		for _, v := range []string{"", "four", "1.5", "0x10"} {
			t.Run(name+"="+v, func(t *testing.T) {
				t.Setenv(name, v)
				_, err := Load("")
				require.Error(t, err)
				require.Contains(t, err.Error(), name)
			})
		}
	}
}

func TestValidateRejectsMCPWithoutAuth(t *testing.T) {
	cfg := Default()
	cfg.Auth = false
	require.NoError(t, cfg.Validate(), "auth off without MCP stays allowed for local development")
	cfg.MCP.Enabled = true
	err := cfg.Validate()
	require.Error(t, err, "agent tokens are meaningless without authentication")
	require.Contains(t, err.Error(), "mcp.enabled")
	cfg.Auth = true
	require.NoError(t, cfg.Validate())
}

func TestValidateMCPLimits(t *testing.T) {
	cases := map[string]func(*MCPConfig){
		"max_inflight zero":         func(m *MCPConfig) { m.MaxInflight = 0 },
		"max_inflight negative":     func(m *MCPConfig) { m.MaxInflight = -1 },
		"max_inflight too large":    func(m *MCPConfig) { m.MaxInflight = 65 },
		"max_result_bytes too low":  func(m *MCPConfig) { m.MaxResultBytes = 1023 },
		"max_result_bytes too high": func(m *MCPConfig) { m.MaxResultBytes = 1<<20 + 1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := Default()
			mutate(&cfg.MCP)
			err := cfg.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), "mcp.")
		})
	}
	cfg := Default()
	cfg.MCP.MaxInflight, cfg.MCP.MaxResultBytes = 64, 1<<20
	require.NoError(t, cfg.Validate())
	cfg.MCP.MaxInflight, cfg.MCP.MaxResultBytes = 1, 1024
	require.NoError(t, cfg.Validate())
}
