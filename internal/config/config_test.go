package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaultsWhenNoFile(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "does-not-exist.toml"))
	require.NoError(t, err)
	assert.Equal(t, ":8080", cfg.Addr)
	assert.Equal(t, "piworker.db", cfg.DBPath)
	assert.True(t, cfg.Auth)
	assert.Equal(t, []string{"http://localhost:3000", "http://localhost:8080"}, cfg.CORSOrigins)
}

func TestLoadTOMLFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "piworker.toml")
	require.NoError(t, os.WriteFile(path, []byte(`
addr = ":9090"
db = "/data/pw.db"
auth = false
cors_origins = ["https://example.com"]
`), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, ":9090", cfg.Addr)
	assert.Equal(t, "/data/pw.db", cfg.DBPath)
	assert.False(t, cfg.Auth)
	assert.Equal(t, []string{"https://example.com"}, cfg.CORSOrigins)
}

func TestEnvOverridesTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "piworker.toml")
	require.NoError(t, os.WriteFile(path, []byte(`addr = ":9090"`+"\n"), 0o644))

	t.Setenv("PIWORKER_ADDR", ":7000")
	t.Setenv("PIWORKER_AUTH", "false")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, ":7000", cfg.Addr, "env var must override the TOML file")
	assert.False(t, cfg.Auth)
}

func TestLoadInvalidTOMLReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.toml")
	require.NoError(t, os.WriteFile(path, []byte("addr = = ="), 0o644))

	_, err := Load(path)
	require.Error(t, err)
}
