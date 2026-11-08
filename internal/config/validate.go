package config

import "errors"

// Validate performs cross-field checks on a fully resolved configuration
// (after flags are applied). main refuses to start when it returns an error,
// so an unsafe combination can never run silently (ADR-0004).
func (c Config) Validate() error {
	return errors.Join(
		c.validateMCP(),
	)
}
