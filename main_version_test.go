package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionCommandReportsBuildWithoutStartingServer(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "piworker")
	build := exec.Command("go", "build", "-ldflags=-X main.version=v0.0.0-release-test", "-o", binary, ".")
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))
	for _, flag := range []string{"-version", "--version"} {
		t.Run(flag, func(t *testing.T) {
			dir := t.TempDir()
			// Version queries must work even when the server config is invalid.
			require.NoError(t, os.WriteFile(filepath.Join(dir, "piworker.toml"), []byte("invalid = ["), 0600))
			cmd := exec.Command(binary, flag)
			cmd.Dir = dir
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, string(output))
			require.Equal(t, "PiWorker v0.0.0-release-test\n", string(output))
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, 1, "version must not create a database")
		})
	}
}
