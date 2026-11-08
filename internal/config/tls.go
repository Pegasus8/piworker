package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// TLS modes.
const (
	// TLSModeAuto serves HTTPS with a local CA generated on first start.
	TLSModeAuto = "auto"
	// TLSModeCustom serves HTTPS with an administrator-provided certificate.
	TLSModeCustom = "custom"
	// TLSModeOff serves plain HTTP (for example behind a reverse proxy).
	TLSModeOff = "off"
)

// TLSConfig is the [tls] section.
type TLSConfig struct {
	Mode     string   `toml:"mode"`
	Hosts    []string `toml:"hosts"`
	CertFile string   `toml:"cert_file"`
	KeyFile  string   `toml:"key_file"`
}

// ParseTLSMode normalizes a TLS mode (case and surrounding whitespace are
// ignored). Anything other than auto, custom or off is an error.
func ParseTLSMode(v string) (string, error) {
	switch s := strings.ToLower(strings.TrimSpace(v)); s {
	case TLSModeAuto, TLSModeCustom, TLSModeOff:
		return s, nil
	}
	return "", fmt.Errorf("expected %s, %s or %s", TLSModeAuto, TLSModeCustom, TLSModeOff)
}

// TLSEnabled reports whether the main listener serves HTTPS.
func (c Config) TLSEnabled() bool { return c.TLS.Mode != TLSModeOff }

// TLSDir is where the local CA and server certificate live: the tls directory
// next to the database.
func (c Config) TLSDir() string { return filepath.Join(filepath.Dir(c.DBPath), "tls") }

// Validate checks settings that depend on each other. It runs after
// command-line flags are applied, because a flag can resolve a conflict between
// the file and the environment.
func (c Config) Validate() error {
	var errs []error
	if _, err := ParseTLSMode(c.TLS.Mode); err != nil {
		errs = append(errs, fmt.Errorf("invalid tls.mode %q: %w", c.TLS.Mode, err))
	}
	if c.TLS.Mode == TLSModeCustom {
		if c.TLS.CertFile == "" {
			errs = append(errs, errors.New(`tls.mode = "custom" requires tls.cert_file`))
		}
		if c.TLS.KeyFile == "" {
			errs = append(errs, errors.New(`tls.mode = "custom" requires tls.key_file`))
		}
	}
	if c.TLSEnabled() && c.HTTPAddr != "" && samePort(c.Addr, c.HTTPAddr) {
		errs = append(errs, fmt.Errorf("http_addr %q uses the same port as addr %q: choose another port, set http_addr = \"\" to disable the HTTP listener, or set tls.mode = \"off\"", c.HTTPAddr, c.Addr))
	}
	return errors.Join(errs...)
}

func samePort(a, b string) bool {
	_, pa, errA := net.SplitHostPort(a)
	_, pb, errB := net.SplitHostPort(b)
	return errA == nil && errB == nil && pa == pb
}

// normalizeTLSMode validates the mode read from the config file.
func (c *Config) normalizeTLSMode() error {
	mode, err := ParseTLSMode(c.TLS.Mode)
	if err != nil {
		return fmt.Errorf("invalid tls.mode %q: %w", c.TLS.Mode, err)
	}
	c.TLS.Mode = mode
	return nil
}

// applyTLSEnv overlays the PIWORKER_TLS_* variables.
func (c *Config) applyTLSEnv() error {
	if v, ok := os.LookupEnv("PIWORKER_TLS_MODE"); ok {
		mode, err := ParseTLSMode(v)
		if err != nil {
			return fmt.Errorf("invalid PIWORKER_TLS_MODE=%q: %w", v, err)
		}
		c.TLS.Mode = mode
	}
	if v, ok := os.LookupEnv("PIWORKER_TLS_HOSTS"); ok {
		c.TLS.Hosts = SplitOrigins(v)
	}
	if v, ok := os.LookupEnv("PIWORKER_TLS_CERT_FILE"); ok {
		c.TLS.CertFile = v
	}
	if v, ok := os.LookupEnv("PIWORKER_TLS_KEY_FILE"); ok {
		c.TLS.KeyFile = v
	}
	return nil
}
