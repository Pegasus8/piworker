package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testToken = "pwk_abcdefghijklmnop_abcdefghijklmnopqrstuvwxyz234567abcdefghijklmnopqrstuvwx"

func writeTokenFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "piworker-token")
	require.NoError(t, os.WriteFile(path, []byte(content), mode))
	require.NoError(t, os.Chmod(path, mode)) // WriteFile honours the umask
	return path
}

func TestReadTokenTrimsWhitespace(t *testing.T) {
	path := writeTokenFile(t, "  "+testToken+"\r\n\n", 0o600)
	got, err := readToken(path)
	require.NoError(t, err)
	assert.Equal(t, testToken, got)
}

func TestReadTokenRejectsBadContentWithoutEchoingIt(t *testing.T) {
	for name, content := range map[string]string{
		"empty":              "  \n",
		"no pwk_ prefix":     "Bearer abcdefSECRET",
		"inner space":        "pwk_abc SECRET",
		"two lines":          "pwk_abc\nSECRET",
		"control characters": "pwk_abc\x00SECRET",
		"too long":           "pwk_" + strings.Repeat("SECRET", 1000),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := readToken(writeTokenFile(t, content, 0o600))
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "SECRET", "the file's content is never echoed")
			assert.Contains(t, err.Error(), "token file")
		})
	}
}

func TestReadTokenMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope")
	_, err := readToken(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), path)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestReadTokenRejectsDirectory(t *testing.T) {
	_, err := readToken(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a regular file")
}

func TestReadTokenRequired(t *testing.T) {
	_, err := readToken("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--token-file is required")
}

func TestReadTokenRefusesFilesOthersCanRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows permissions are ACLs; the README explains icacls")
	}
	for _, mode := range []os.FileMode{0o644, 0o640, 0o604, 0o660, 0o602, 0o620} {
		t.Run(mode.String(), func(t *testing.T) {
			path := writeTokenFile(t, testToken, mode)
			_, err := readToken(path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "chmod 600 "+path)
			assert.NotContains(t, err.Error(), testToken)
		})
	}
	for _, mode := range []os.FileMode{0o600, 0o400} {
		t.Run(mode.String(), func(t *testing.T) {
			got, err := readToken(writeTokenFile(t, testToken, mode))
			require.NoError(t, err)
			assert.Equal(t, testToken, got)
		})
	}
}

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	got, err := expandHome("~/.piworker-token")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".piworker-token"), got)

	got, err = expandHome("~")
	require.NoError(t, err)
	assert.Equal(t, home, got)

	for _, unchanged := range []string{"/etc/piworker-token", "relative/token", "~other/token", ""} {
		got, err := expandHome(unchanged)
		require.NoError(t, err)
		assert.Equal(t, unchanged, got)
	}
}

func TestReadTokenExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".piworker-token"), []byte(testToken+"\n"), 0o600))

	got, err := readToken("~/.piworker-token")
	require.NoError(t, err)
	assert.Equal(t, testToken, got)
}
