package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllowedHostsDefaultsToNone(t *testing.T) {
	cfg, err := Load("")
	require.NoError(t, err)
	assert.Empty(t, cfg.AllowedHosts, "the built-in names (localhost, hostname, IPs) are added by the host guard")
}

func TestAllowedHostsFromTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "piworker.toml")
	require.NoError(t, os.WriteFile(path, []byte(`allowed_hosts = ["piworker.example.com", "backend"]`+"\n"), 0o600))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"piworker.example.com", "backend"}, cfg.AllowedHosts)
}

func TestAllowedHostsEnvOverridesTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "piworker.toml")
	require.NoError(t, os.WriteFile(path, []byte(`allowed_hosts = ["from-file"]`+"\n"), 0o600))
	t.Setenv("PIWORKER_ALLOWED_HOSTS", " backend, pi.lan ,,")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"backend", "pi.lan"}, cfg.AllowedHosts)
}

func TestAllowedHostsEmptyEnvClearsList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "piworker.toml")
	require.NoError(t, os.WriteFile(path, []byte(`allowed_hosts = ["from-file"]`+"\n"), 0o600))
	t.Setenv("PIWORKER_ALLOWED_HOSTS", "")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Empty(t, cfg.AllowedHosts)
}

func TestSplitListTrimsBlanks(t *testing.T) {
	assert.Nil(t, SplitList(""))
	assert.Equal(t, []string{"a", "b"}, SplitList(" a ,, b ,"))
}
