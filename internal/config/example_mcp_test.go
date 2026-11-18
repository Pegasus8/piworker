package config

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shipped example documents every [mcp] key, set to its default, and the
// PIWORKER_MCP_* variable that overrides it.
func TestExampleConfigDocumentsMCP(t *testing.T) {
	path := filepath.Join("..", "..", "piworker.example.toml")
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, DefaultMCP(), cfg.MCP, "the example keeps the defaults")

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(raw)
	_, section, found := strings.Cut(text, "\n[mcp]\n")
	require.True(t, found, "the example has an [mcp] section")
	if next := regexp.MustCompile(`(?m)^\[`).FindStringIndex(section); next != nil {
		section = section[:next[0]]
	}
	typ := reflect.TypeFor[MCPConfig]()
	for i := range typ.NumField() {
		key := typ.Field(i).Tag.Get("toml")
		assert.Regexp(t, `(?m)^`+regexp.QuoteMeta(key)+` = `, section, "key %s", key)
		assert.Contains(t, section, "PIWORKER_MCP_"+strings.ToUpper(key), "variable for %s", key)
	}
}
