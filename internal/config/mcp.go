package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// MCPConfig is the [mcp] section: the Model Context Protocol endpoint that lets
// AI agents call published flows (docs/design/mcp.md §8, §11).
type MCPConfig struct {
	// Enabled mounts /mcp. Requires authentication (see Validate).
	Enabled bool `toml:"enabled"`
	// LANAccess allows non-loopback clients when TLS is off.
	LANAccess bool `toml:"lan_access"`
	// MaxInflight bounds concurrent agent tool calls across all tokens.
	MaxInflight int `toml:"max_inflight"`
	// MaxResultBytes caps the serialized result returned to an agent.
	MaxResultBytes int `toml:"max_result_bytes"`
	// AuthorEnabled exposes authoring tools and allows the author scope.
	AuthorEnabled bool `toml:"author_enabled"`
}

// Bounds for the numeric [mcp] settings.
const (
	MinMCPMaxInflight    = 1
	MaxMCPMaxInflight    = 64
	MinMCPMaxResultBytes = 1024
	MaxMCPMaxResultBytes = 1 << 20
)

// DefaultMCP returns the built-in [mcp] defaults: disabled, loopback-only when
// TLS is off, and no authoring.
func DefaultMCP() MCPConfig {
	return MCPConfig{MaxInflight: 4, MaxResultBytes: 16384}
}

// applyEnv overlays the PIWORKER_MCP_* variables. Booleans use the strict
// ParseBool spellings and integers must be plain base-10 numbers; anything
// else is an error so a typo never silently changes the agent surface.
func (m *MCPConfig) applyEnv() error {
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
	intEnv := func(name string, dst *int) {
		v, ok := os.LookupEnv(name)
		if !ok {
			return
		}
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid %s=%q: expected a whole number", name, v))
			return
		}
		*dst = n
	}
	boolEnv("PIWORKER_MCP_ENABLED", &m.Enabled)
	boolEnv("PIWORKER_MCP_LAN_ACCESS", &m.LANAccess)
	intEnv("PIWORKER_MCP_MAX_INFLIGHT", &m.MaxInflight)
	intEnv("PIWORKER_MCP_MAX_RESULT_BYTES", &m.MaxResultBytes)
	boolEnv("PIWORKER_MCP_AUTHOR_ENABLED", &m.AuthorEnabled)
	return errors.Join(errs...)
}

// validateMCP checks the [mcp] section against the rest of the configuration.
func (c Config) validateMCP() error {
	var errs []error
	if c.MCP.Enabled && !c.Auth {
		errs = append(errs, errors.New("mcp.enabled = true requires authentication (auth = true): agent tokens are managed by the signed-in administrator"))
	}
	if n := c.MCP.MaxInflight; n < MinMCPMaxInflight || n > MaxMCPMaxInflight {
		errs = append(errs, fmt.Errorf("mcp.max_inflight = %d: must be between %d and %d", n, MinMCPMaxInflight, MaxMCPMaxInflight))
	}
	if n := c.MCP.MaxResultBytes; n < MinMCPMaxResultBytes || n > MaxMCPMaxResultBytes {
		errs = append(errs, fmt.Errorf("mcp.max_result_bytes = %d: must be between %d and %d", n, MinMCPMaxResultBytes, MaxMCPMaxResultBytes))
	}
	return errors.Join(errs...)
}
