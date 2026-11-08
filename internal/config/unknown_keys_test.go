package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// A typo in piworker.toml (e.g. "atuh = true") must not be silently ignored:
// the setting would fall back to its default without any warning.
func TestLoadRejectsUnknownKeys(t *testing.T) {
	for _, body := range []string{
		"atuh = false\n",
		"jwt_secret = \"old\"\n", // removed setting
	} {
		path := filepath.Join(t.TempDir(), "piworker.toml")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		_, err := Load(path)
		require.Error(t, err, body)
		require.Contains(t, err.Error(), "unknown")
	}
}

// The shipped example must stay loadable now that unknown keys are rejected.
func TestExampleConfigLoads(t *testing.T) {
	_, err := Load(filepath.Join("..", "..", "piworker.example.toml"))
	require.NoError(t, err)
}
