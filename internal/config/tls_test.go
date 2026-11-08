package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTOML(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "piworker.toml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestHTTPSIsTheDefault(t *testing.T) {
	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, ":8443", cfg.Addr)
	assert.Equal(t, ":8080", cfg.HTTPAddr)
	assert.True(t, cfg.WebhooksHTTP)
	assert.Equal(t, TLSModeAuto, cfg.TLS.Mode)
	assert.Empty(t, cfg.TLS.Hosts)
	assert.True(t, cfg.TLSEnabled())
	require.NoError(t, cfg.Validate())
}

func TestTLSSettingsFromTOML(t *testing.T) {
	cfg, err := Load(writeTOML(t, `
addr = ":9443"
http_addr = ""
webhooks_http = false

[tls]
mode = "custom"
hosts = ["pi.home", "10.0.0.5"]
cert_file = "/etc/piworker/cert.pem"
key_file = "/etc/piworker/key.pem"
`))
	require.NoError(t, err)
	assert.Equal(t, ":9443", cfg.Addr)
	assert.Equal(t, "", cfg.HTTPAddr, "an empty http_addr disables the HTTP listener")
	assert.False(t, cfg.WebhooksHTTP)
	assert.Equal(t, TLSConfig{Mode: TLSModeCustom, Hosts: []string{"pi.home", "10.0.0.5"}, CertFile: "/etc/piworker/cert.pem", KeyFile: "/etc/piworker/key.pem"}, cfg.TLS)
	require.NoError(t, cfg.Validate())
}

func TestTLSModeIsNormalized(t *testing.T) {
	cfg, err := Load(writeTOML(t, "[tls]\nmode = \" OFF \"\n"))
	require.NoError(t, err)
	assert.Equal(t, TLSModeOff, cfg.TLS.Mode)
	assert.False(t, cfg.TLSEnabled())
}

func TestTLSSettingsFromEnv(t *testing.T) {
	path := writeTOML(t, "http_addr = \":8081\"\n[tls]\nmode = \"auto\"\n")
	t.Setenv("PIWORKER_TLS_MODE", "custom")
	t.Setenv("PIWORKER_HTTP_ADDR", "")
	t.Setenv("PIWORKER_WEBHOOKS_HTTP", "no")
	t.Setenv("PIWORKER_TLS_HOSTS", "pi.home, 10.0.0.5 ,")
	t.Setenv("PIWORKER_TLS_CERT_FILE", "/run/cert.pem")
	t.Setenv("PIWORKER_TLS_KEY_FILE", "/run/key.pem")
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, TLSModeCustom, cfg.TLS.Mode)
	assert.Equal(t, "", cfg.HTTPAddr, "a set but empty PIWORKER_HTTP_ADDR disables the HTTP listener")
	assert.False(t, cfg.WebhooksHTTP)
	assert.Equal(t, []string{"pi.home", "10.0.0.5"}, cfg.TLS.Hosts)
	assert.Equal(t, "/run/cert.pem", cfg.TLS.CertFile)
	assert.Equal(t, "/run/key.pem", cfg.TLS.KeyFile)
}

func TestInvalidTLSModeIsRejected(t *testing.T) {
	for _, v := range []string{"", "on", "true", "https", "autoo"} {
		t.Run("env "+v, func(t *testing.T) {
			t.Setenv("PIWORKER_TLS_MODE", v)
			_, err := Load("")
			require.Error(t, err)
			require.Contains(t, err.Error(), "PIWORKER_TLS_MODE")
		})
	}
	_, err := Load(writeTOML(t, "[tls]\nmode = \"secure\"\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "tls.mode")
}

func TestWebhooksHTTPEnvIsStrict(t *testing.T) {
	for _, v := range []string{"", "maybe"} {
		t.Setenv("PIWORKER_WEBHOOKS_HTTP", v)
		_, err := Load("")
		require.Error(t, err)
		require.Contains(t, err.Error(), "PIWORKER_WEBHOOKS_HTTP")
	}
}

func TestUnknownTLSKeyIsRejected(t *testing.T) {
	_, err := Load(writeTOML(t, "[tls]\nmod = \"auto\"\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown")
}

func TestValidateTLS(t *testing.T) {
	cases := map[string]struct {
		mutate func(*Config)
		errMsg string
	}{
		"custom without files": {func(c *Config) { c.TLS.Mode = TLSModeCustom }, "cert_file"},
		"custom without key":   {func(c *Config) { c.TLS.Mode, c.TLS.CertFile = TLSModeCustom, "c.pem" }, "key_file"},
		"same port as addr":    {func(c *Config) { c.Addr = "127.0.0.1:8080" }, "http_addr"},
		"same explicit ports":  {func(c *Config) { c.Addr, c.HTTPAddr = ":9000", "0.0.0.0:9000" }, "http_addr"},
		"invalid mode":         {func(c *Config) { c.TLS.Mode = "maybe" }, "tls.mode"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := Default()
			tc.mutate(&cfg)
			err := cfg.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errMsg)
		})
	}

	ok := map[string]func(*Config){
		"defaults":                      func(*Config) {},
		"http listener disabled":        func(c *Config) { c.Addr, c.HTTPAddr = ":8080", "" },
		"off ignores the http listener": func(c *Config) { c.Addr, c.TLS.Mode = ":8080", TLSModeOff },
		"custom with files":             func(c *Config) { c.TLS = TLSConfig{Mode: TLSModeCustom, CertFile: "c", KeyFile: "k"} },
		"ephemeral ports":               func(c *Config) { c.Addr, c.HTTPAddr = "127.0.0.1:0", "127.0.0.1:0" },
	}
	for name, mutate := range ok {
		t.Run(name, func(t *testing.T) {
			cfg := Default()
			mutate(&cfg)
			require.NoError(t, cfg.Validate())
		})
	}
}

func TestTLSDirIsNextToTheDatabase(t *testing.T) {
	cfg := Default()
	cfg.DBPath = "/var/lib/piworker/piworker.db"
	assert.Equal(t, "/var/lib/piworker/tls", cfg.TLSDir())
	cfg.DBPath = "piworker.db"
	assert.Equal(t, "tls", cfg.TLSDir())
}

func TestExampleConfigMatchesHTTPSDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "piworker.example.toml"))
	require.NoError(t, err)
	def := Default()
	assert.Equal(t, def.Addr, cfg.Addr)
	assert.Equal(t, def.HTTPAddr, cfg.HTTPAddr)
	assert.Equal(t, def.WebhooksHTTP, cfg.WebhooksHTTP)
	assert.Equal(t, def.TLS.Mode, cfg.TLS.Mode)
	require.NoError(t, cfg.Validate())
}
