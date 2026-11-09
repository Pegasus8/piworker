package builtin

import (
	"strings"
	"testing"
	"unicode"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// caseInsensitiveEnums are the enum properties whose node normalises case. For
// each, normalised returns what the node makes of a configured value, so the
// test can check that every spelling CheckConfig accepts behaves the same.
var caseInsensitiveEnums = map[string]func(t *testing.T, value string) any{
	"action-http.method": func(t *testing.T, v string) any {
		return mustCreate[*HTTPAction](t, "action-http", map[string]any{"url": "https://example.com", "method": v}).method
	},
	"action-database.driver": func(t *testing.T, v string) any {
		return mustCreate[*DatabaseAction](t, "action-database", map[string]any{"driver": v, "dsn": "file::memory:", "query": "SELECT 1"}).driverName
	},
	"action-database.mode": func(t *testing.T, v string) any {
		return mustCreate[*DatabaseAction](t, "action-database", map[string]any{"mode": v, "dsn": "file::memory:", "query": "SELECT 1"}).exec
	},
	"action-email.tls": func(t *testing.T, v string) any {
		return mustCreate[*EmailAction](t, "action-email", map[string]any{"tls": v, "host": "smtp.example.com", "from": "a@example.com", "to": "b@example.com"}).tlsMode
	},
	"action-gpio.value": func(t *testing.T, v string) any {
		return mustCreate[*GPIOAction](t, "action-gpio", map[string]any{"value": v, "pin": "GPIO17"}).value
	},
	"trigger-gpio.edge": func(t *testing.T, v string) any {
		return parseEdge(mustCreate[*GPIOTrigger](t, "trigger-gpio", map[string]any{"edge": v, "pin": "GPIO17"}).edge)
	},
	"trigger-gpio.pull": func(t *testing.T, v string) any {
		return parsePull(mustCreate[*GPIOTrigger](t, "trigger-gpio", map[string]any{"pull": v, "pin": "GPIO17"}).pull)
	},
	"process-join.mode": func(t *testing.T, v string) any {
		return mustCreate[*JoinProcess](t, "process-join", map[string]any{"mode": v, "sources": "a, b"}).mode
	},
	"process-parse.format": func(t *testing.T, v string) any {
		return mustCreate[*ParseProcess](t, "process-parse", map[string]any{"format": v}).format
	},
}

func mustCreate[T node.Node](t *testing.T, nodeType string, cfg map[string]any) T {
	t.Helper()
	n, err := node.DefaultRegistry.Create(nodeType, cfg)
	require.NoError(t, err)
	typed, ok := n.(T)
	require.Truef(t, ok, "%s created a %T", nodeType, n)
	return typed
}

// swapCase flips the case of every letter: "Markdown" -> "mARKDOWN".
func swapCase(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsUpper(r) {
			return unicode.ToLower(r)
		}
		return unicode.ToUpper(r)
	}, s)
}

// Most nodes compare enum values exactly and fall back to a default for any
// other spelling (sysmetric "Temperature" would monitor cpu), so CheckConfig
// must refuse other spellings unless the property is listed above.
func TestEnumSpellingsAcceptedByCheckConfigAreHonoured(t *testing.T) {
	marked := map[string]bool{}
	eachProperty(t, func(info node.NodeTypeInfo, name string, p node.ConfigProperty) {
		key := info.Type + "." + name
		if p.CaseInsensitive {
			marked[key] = true
		}
		for _, allowed := range p.Enum {
			variant := swapCase(allowed)
			if variant == allowed {
				continue
			}
			issues := node.CheckConfig(info.Config, map[string]any{name: variant})
			if !p.CaseInsensitive {
				assert.NotEmptyf(t, issues, "%s accepts %q, but the node does not normalise case", key, variant)
				continue
			}
			assert.Emptyf(t, issues, "%s is case-insensitive but refuses %q", key, variant)
			normalised, listed := caseInsensitiveEnums[key]
			if !assert.Truef(t, listed, "%s is marked CaseInsensitive; list it in caseInsensitiveEnums", key) {
				continue
			}
			assert.Equalf(t, normalised(t, allowed), normalised(t, variant), "%s: the node treats %q differently from %q", key, variant, allowed)
		}
	})
	for key := range caseInsensitiveEnums {
		assert.Truef(t, marked[key], "%s normalises case but is not marked CaseInsensitive", key)
	}
}

func TestSysmetricEnumIsCaseSensitive(t *testing.T) {
	info, ok := node.DefaultRegistry.Get("trigger-sysmetric")
	require.True(t, ok)
	issues := node.CheckConfig(info.Config, map[string]any{"metric": "Temperature"})
	require.Len(t, issues, 1)
	assert.Equal(t, `expected one of "cpu", "memory", "temperature", got "Temperature"`, issues[0].Message)
}
