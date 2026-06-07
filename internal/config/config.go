// Package config loads PiWorker configuration from defaults, a TOML file, and
// environment variables. Command-line flags are applied on top by the caller, so
// the effective precedence is: flags > env > TOML file > built-in defaults.
package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds all runtime configuration for PiWorker.
type Config struct {
	Addr        string   `toml:"addr"`
	DBPath      string   `toml:"db"`
	Debug       bool     `toml:"debug"`
	Auth        bool     `toml:"auth"`
	JWTSecret   string   `toml:"jwt_secret"`
	CORSOrigins []string `toml:"cors_origins"`
	AdminUser   string   `toml:"admin_user"`
	AdminPass   string   `toml:"admin_pass"`
}

// DefaultConfigPath is the config file looked up when none is specified.
const DefaultConfigPath = "piworker.toml"

// Default returns the built-in default configuration.
func Default() Config {
	return Config{
		Addr:        ":8080",
		DBPath:      "piworker.db",
		Debug:       false,
		Auth:        true,
		CORSOrigins: []string{"http://localhost:3000", "http://localhost:8080"},
	}
}

// Load builds a configuration by layering, in order of increasing precedence:
// built-in defaults, the TOML file at path (if it exists), and environment
// variables. A missing config file is not an error. Flags are applied by the
// caller afterwards via Apply* helpers so explicitly-set flags win.
func Load(path string) (Config, error) {
	cfg := Default()

	if path != "" {
		if _, err := os.Stat(path); err == nil {
			if _, err := toml.DecodeFile(path, &cfg); err != nil {
				return cfg, fmt.Errorf("failed to parse config file %q: %w", path, err)
			}
		} else if !os.IsNotExist(err) {
			return cfg, fmt.Errorf("failed to access config file %q: %w", path, err)
		}
	}

	cfg.applyEnv()
	return cfg, nil
}

// applyEnv overlays environment variables onto the config.
func (c *Config) applyEnv() {
	if v, ok := os.LookupEnv("PIWORKER_ADDR"); ok {
		c.Addr = v
	}
	if v, ok := os.LookupEnv("PIWORKER_DB"); ok {
		c.DBPath = v
	}
	if v, ok := os.LookupEnv("PIWORKER_DEBUG"); ok {
		c.Debug = isTruthy(v)
	}
	if v, ok := os.LookupEnv("PIWORKER_AUTH"); ok {
		c.Auth = isTruthy(v)
	}
	if v, ok := os.LookupEnv("PIWORKER_JWT_SECRET"); ok {
		c.JWTSecret = v
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

// isTruthy interprets common boolean-ish string values as true/false.
func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
