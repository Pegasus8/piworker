// Package config loads PiWorker configuration from defaults, a TOML file, and
// environment variables. Command-line flags are applied on top by the caller, so
// the effective precedence is: flags > env > TOML file > built-in defaults.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds all runtime configuration for PiWorker.
type Config struct {
	Addr          string   `toml:"addr"`
	DBPath        string   `toml:"db"`
	Debug         bool     `toml:"debug"`
	Auth          bool     `toml:"auth"`
	SessionSecure bool     `toml:"session_secure"`
	CORSOrigins   []string `toml:"cors_origins"`
	AdminUser     string   `toml:"admin_user"`
	AdminPass     string   `toml:"admin_pass"`

	// Addr serves HTTPS unless TLS.Mode is "off"; HTTPAddr serves plain HTTP
	// that redirects to it (empty disables it). WebhooksHTTP serves /webhooks/
	// directly on HTTPAddr, for devices that cannot trust a local CA.
	HTTPAddr     string    `toml:"http_addr"`
	WebhooksHTTP bool      `toml:"webhooks_http"`
	TLS          TLSConfig `toml:"tls"`
}

// DefaultConfigPath is the config file looked up when none is specified.
const DefaultConfigPath = "piworker.toml"

// Default returns the built-in default configuration.
func Default() Config {
	return Config{
		Addr:        ":8443",
		DBPath:      "piworker.db",
		Debug:       false,
		Auth:        true,
		CORSOrigins: []string{"http://localhost:3000", "http://localhost:8080"},

		HTTPAddr:     ":8080",
		WebhooksHTTP: true,
		TLS:          TLSConfig{Mode: TLSModeAuto},
	}
}

// Load builds a configuration by layering, in order of increasing precedence:
// built-in defaults, the TOML file at path, and environment variables. Only the
// implicit DefaultConfigPath may be absent; an explicitly chosen file (via
// -config or PIWORKER_CONFIG) that does not exist is an error, so a typo never
// silently falls back to defaults. An empty path skips the file entirely.
// Flags are applied by the caller afterwards so explicitly-set flags win.
func Load(path string) (Config, error) {
	cfg := Default()

	if path != "" {
		if _, err := os.Stat(path); err == nil {
			md, err := toml.DecodeFile(path, &cfg)
			if err != nil {
				return cfg, fmt.Errorf("failed to parse config file %q: %w", path, err)
			}
			if undecoded := md.Undecoded(); len(undecoded) > 0 {
				keys := make([]string, len(undecoded))
				for i, k := range undecoded {
					keys[i] = k.String()
				}
				return cfg, fmt.Errorf("config file %q has unknown keys: %s", path, strings.Join(keys, ", "))
			}
			if err := cfg.normalizeTLSMode(); err != nil {
				return cfg, fmt.Errorf("config file %q: %w", path, err)
			}
		} else if !os.IsNotExist(err) || path != DefaultConfigPath {
			return cfg, fmt.Errorf("failed to access config file %q: %w", path, err)
		}
	}

	if err := cfg.applyEnv(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// applyEnv overlays environment variables onto the config. Boolean variables
// are parsed strictly: an empty or unrecognised value is an error rather than
// "false", so e.g. PIWORKER_AUTH= can never silently disable authentication.
func (c *Config) applyEnv() error {
	var errs []error
	boolEnv := func(name string, dst *bool) {
		v, ok := os.LookupEnv(name)
		if !ok {
			return
		}
		b, err := ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid %s=%q: expected true/false, 1/0, yes/no or on/off", name, v))
			return
		}
		*dst = b
	}
	boolEnv("PIWORKER_SESSION_SECURE", &c.SessionSecure)
	boolEnv("PIWORKER_DEBUG", &c.Debug)
	boolEnv("PIWORKER_AUTH", &c.Auth)
	boolEnv("PIWORKER_WEBHOOKS_HTTP", &c.WebhooksHTTP)
	if v, ok := os.LookupEnv("PIWORKER_HTTP_ADDR"); ok {
		c.HTTPAddr = v
	}
	if err := c.applyTLSEnv(); err != nil {
		errs = append(errs, err)
	}
	if v, ok := os.LookupEnv("PIWORKER_ADDR"); ok {
		c.Addr = v
	}
	if v, ok := os.LookupEnv("PIWORKER_DB"); ok {
		c.DBPath = v
	}
	if v, ok := os.LookupEnv("PIWORKER_CORS_ORIGINS"); ok {
		c.CORSOrigins = SplitOrigins(v)
	}
	if v, ok := os.LookupEnv("PIWORKER_ADMIN_USER"); ok {
		c.AdminUser = v
	}
	if v, ok := os.LookupEnv("PIWORKER_ADMIN_PASS"); ok {
		c.AdminPass = v
	}
	return errors.Join(errs...)
}

// ResolveConfigPath returns the config file path from the -config flag, then the
// PIWORKER_CONFIG env var, then DefaultConfigPath.
func ResolveConfigPath(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if env := os.Getenv("PIWORKER_CONFIG"); env != "" {
		return env
	}
	return DefaultConfigPath
}

// SplitOrigins parses a comma-separated list of origins, trimming blanks.
func SplitOrigins(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// ParseBool accepts strconv.ParseBool spellings plus yes/no and on/off (case
// and surrounding whitespace ignored). Anything else, including "", is an error.
func ParseBool(v string) (bool, error) {
	s := strings.ToLower(strings.TrimSpace(v))
	switch s {
	case "yes", "on":
		return true, nil
	case "no", "off":
		return false, nil
	}
	return strconv.ParseBool(s)
}
