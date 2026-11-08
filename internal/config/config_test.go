package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaultsWhenNoFile(t *testing.T) {
	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, ":8443", cfg.Addr, "HTTPS is the default listener")
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

func TestSessionCookieTransportIsExplicit(t *testing.T) {
	cfg := Default()
	require.False(t, cfg.SessionSecure)
	path := filepath.Join(t.TempDir(), "piworker.toml")
	require.NoError(t, os.WriteFile(path, []byte("session_secure = true\n"), 0600))
	cfg, err := Load(path)
	require.NoError(t, err)
	require.True(t, cfg.SessionSecure)
	t.Setenv("PIWORKER_SESSION_SECURE", "false")
	cfg, err = Load(path)
	require.NoError(t, err)
	require.False(t, cfg.SessionSecure)
}

func TestAuthEnvFailsClosedOnEmptyOrUnknownValue(t *testing.T) {
	for _, v := range []string{"", "  ", "disabled", "flase", "2"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("PIWORKER_AUTH", v)
			_, err := Load("")
			require.Error(t, err, "an unparseable PIWORKER_AUTH must not silently disable auth")
			require.Contains(t, err.Error(), "PIWORKER_AUTH")
		})
	}
}

func TestBoolEnvAcceptsStrictSpellings(t *testing.T) {
	cases := map[string]bool{
		"true": true, "TRUE": true, "1": true, "t": true, "yes": true, "On": true, " true ": true,
		"false": false, "0": false, "F": false, "no": false, "off": false,
	}
	for v, want := range cases {
		t.Run(v, func(t *testing.T) {
			t.Setenv("PIWORKER_AUTH", v)
			t.Setenv("PIWORKER_DEBUG", v)
			t.Setenv("PIWORKER_SESSION_SECURE", v)
			cfg, err := Load("")
			require.NoError(t, err)
			assert.Equal(t, want, cfg.Auth)
			assert.Equal(t, want, cfg.Debug)
			assert.Equal(t, want, cfg.SessionSecure)
		})
	}
}

func TestOtherBoolEnvsRejectUnknownValues(t *testing.T) {
	for _, name := range []string{"PIWORKER_DEBUG", "PIWORKER_SESSION_SECURE"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "maybe")
			_, err := Load("")
			require.Error(t, err)
			require.Contains(t, err.Error(), name)
		})
	}
}

func TestExplicitMissingConfigFileIsAnError(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "typo.toml"))
	require.Error(t, err, "an explicitly requested config file that does not exist must not be ignored")
}

func TestMissingDefaultConfigFileIsTolerated(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg, err := Load(DefaultConfigPath)
	require.NoError(t, err)
	assert.True(t, cfg.Auth)
}

func TestConfigEnvPointingToMissingFileIsAnError(t *testing.T) {
	t.Setenv("PIWORKER_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	_, err := Load(ResolveConfigPath(""))
	require.Error(t, err, "PIWORKER_CONFIG pointing to a missing file must fail")
}
