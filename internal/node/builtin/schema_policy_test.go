package builtin

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registeredTypes returns every node type registered in the default registry,
// sorted for stable test output.
func registeredTypes(t *testing.T) []node.NodeTypeInfo {
	t.Helper()
	infos := node.DefaultRegistry.List()
	require.NotEmpty(t, infos)
	sort.Slice(infos, func(i, j int) bool { return infos[i].Type < infos[j].Type })
	return infos
}

// eachProperty calls fn for every config property of every registered type.
func eachProperty(t *testing.T, fn func(info node.NodeTypeInfo, name string, p node.ConfigProperty)) {
	t.Helper()
	for _, info := range registeredTypes(t) {
		if info.Config == nil {
			continue
		}
		for name, p := range info.Config.Properties {
			fn(info, name, p)
		}
	}
}

// Every registered node type must declare what it can affect; the MCP tool
// risk analysis and agent policies are built on it.
func TestEveryNodeTypeDeclaresRisk(t *testing.T) {
	for _, info := range registeredTypes(t) {
		assert.Truef(t, info.Risk.Valid(), "%s declares no valid Risk (got %q)", info.Type, info.Risk)
	}
}

func TestBuiltinRiskClassification(t *testing.T) {
	want := map[string]node.Risk{
		"action-command":  node.RiskPrivileged,
		"action-file":     node.RiskPrivileged,
		"action-database": node.RiskPrivileged,
		"action-gpio":     node.RiskPrivileged,
		"action-i2c":      node.RiskPrivileged,
		"action-http":     node.RiskExternal,
		"action-mqtt":     node.RiskExternal,
		"action-email":    node.RiskExternal,
		"action-telegram": node.RiskExternal,
		"action-notify":   node.RiskExternal,
	}
	for _, info := range registeredTypes(t) {
		expected, listed := want[info.Type]
		if !listed {
			expected = node.RiskSafe
		}
		assert.Equalf(t, expected, info.Risk, "risk of %s", info.Type)
	}
	for typ := range want {
		assert.Truef(t, node.DefaultRegistry.Has(typ), "%s is classified but not registered", typ)
	}
}

// credentialName matches property names that usually hold credentials.
var credentialName = regexp.MustCompile(`(?i)password|token|secret|dsn|key|auth`)

// notCredentials are properties whose name matches credentialName but which
// hold no credential. Adding to this list is a deliberate review decision.
var notCredentials = map[string]string{
	"get-var.key":          "variable name",
	"set-var.key":          "variable name",
	"process-join.key":     "correlation-key expression over the message",
	"process-join.maxKeys": "maximum number of pending groups",
}

func TestCredentialLikePropertiesAreSensitive(t *testing.T) {
	eachProperty(t, func(info node.NodeTypeInfo, name string, p node.ConfigProperty) {
		id := info.Type + "." + name
		if !credentialName.MatchString(name) {
			return
		}
		if _, ok := notCredentials[id]; ok {
			assert.Falsef(t, p.Sensitive, "%s is listed as not a credential but marked Sensitive", id)
			return
		}
		assert.Truef(t, p.Sensitive, "%s looks like a credential and must be Sensitive (or be listed in notCredentials)", id)
	})
	for id := range notCredentials {
		typ, prop, _ := strings.Cut(id, ".")
		info, ok := node.DefaultRegistry.Get(typ)
		require.Truef(t, ok, "stale notCredentials entry %s", id)
		_, ok = info.Config.Properties[prop]
		assert.Truef(t, ok, "stale notCredentials entry %s", id)
	}
}

// Credentials whose names do not give them away.
func TestKnownCredentialPropertiesAreSensitive(t *testing.T) {
	for _, id := range []string{
		"action-http.headers", // Authorization, API keys
		"action-notify.url",   // Discord/Slack webhook URLs embed a token
		"action-telegram.botToken",
		"action-database.dsn",
		"action-email.password",
		"action-mqtt.password",
		"trigger-mqtt.password",
		"trigger-webhook.token",
	} {
		typ, prop, _ := strings.Cut(id, ".")
		info, ok := node.DefaultRegistry.Get(typ)
		require.Truef(t, ok, "%s not registered", typ)
		require.NotNilf(t, info.Config, "%s has no schema", typ)
		p, ok := info.Config.Properties[prop]
		require.Truef(t, ok, "%s missing", id)
		assert.Truef(t, p.Sensitive, "%s must be Sensitive", id)
	}
}

func TestSensitivePropertiesAreNeverAgentVisible(t *testing.T) {
	eachProperty(t, func(info node.NodeTypeInfo, name string, p node.ConfigProperty) {
		if p.Sensitive {
			assert.Falsef(t, p.AgentVisible, "%s.%s is Sensitive and AgentVisible", info.Type, name)
		}
	})
}

// Enum choices, numbers and booleans carry no free text, so agents may see
// them (they are needed to diagnose a flow).
func TestHarmlessPropertiesAreAgentVisible(t *testing.T) {
	eachProperty(t, func(info node.NodeTypeInfo, name string, p node.ConfigProperty) {
		harmless := p.Type == "number" || p.Type == "integer" || p.Type == "boolean" || len(p.Enum) > 0
		if harmless && !p.Sensitive {
			assert.Truef(t, p.AgentVisible, "%s.%s (%s) should be AgentVisible", info.Type, name, p.Type)
		}
	})
}

// Free-text and destination fields stay redacted for agents.
func TestFreeTextPropertiesAreNotAgentVisible(t *testing.T) {
	for _, id := range []string{
		"action-command.command",
		"action-http.url",
		"action-http.body",
		"action-email.to",
		"action-email.body",
		"action-telegram.chatId",
		"action-mqtt.broker",
		"process-transform.expression",
		"process-template.template",
		"trigger-webhook.path",
		"trigger-rss.url",
		"trigger-manual.payload",
	} {
		typ, prop, _ := strings.Cut(id, ".")
		info, ok := node.DefaultRegistry.Get(typ)
		require.Truef(t, ok, "%s not registered", typ)
		assert.Falsef(t, info.Config.Properties[prop].AgentVisible, "%s must not be AgentVisible", id)
	}
}

// unitless numeric properties: counts, levels and values whose unit depends
// on another setting.
var unitless = map[string]bool{
	"count": true, "limit": true, "maxKeys": true, "qos": true, "port": true, "threshold": true,
}

// Numeric settings state their unit both in Unit (quoted by deploy errors)
// and in the description shown by the editor.
func TestNumericPropertiesStateUnits(t *testing.T) {
	eachProperty(t, func(info node.NodeTypeInfo, name string, p node.ConfigProperty) {
		if p.Type != "number" && p.Type != "integer" {
			return
		}
		id := info.Type + "." + name
		if unitless[name] {
			assert.Emptyf(t, p.Unit, "%s is listed as unitless", id)
			return
		}
		if assert.NotEmptyf(t, p.Unit, "%s needs a Unit", id) {
			assert.Containsf(t, p.Description, p.Unit, "description of %s must state its unit", id)
		}
	})
}

// The editor seeds new nodes from schema defaults, so they must pass the
// deploy-time type check.
func TestSchemaDefaultsPassConfigCheck(t *testing.T) {
	for _, info := range registeredTypes(t) {
		if info.Config == nil {
			continue
		}
		cfg := map[string]any{}
		for name, p := range info.Config.Properties {
			if p.Default != nil {
				cfg[name] = p.Default
			}
		}
		assert.Emptyf(t, node.CheckConfig(info.Config, cfg), "defaults of %s", info.Type)
	}
}

func TestSchemaTypesAreKnown(t *testing.T) {
	known := map[string]bool{"string": true, "number": true, "integer": true, "boolean": true, "array": true, "object": true}
	eachProperty(t, func(info node.NodeTypeInfo, name string, p node.ConfigProperty) {
		assert.Truef(t, known[p.Type], "%s.%s has unknown type %q", info.Type, name, p.Type)
		if len(p.Enum) > 0 {
			assert.Equalf(t, "string", p.Type, "%s.%s: enums are only supported on strings", info.Type, name)
		}
	})
}
