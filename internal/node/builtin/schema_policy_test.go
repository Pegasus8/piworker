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
		// Zigbee commands and requests go out through the broker.
		"action-zigbee":      node.RiskExternal,
		"process-zigbee-get": node.RiskExternal,
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
	"minChange": true, // in the unit of the Zigbee property it watches
	"duration":  true, // the timer's, in its durationUnit
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
	known := map[string]bool{"string": true, "number": true, "integer": true, "boolean": true, "array": true, "object": true, "json": true}
	eachProperty(t, func(info node.NodeTypeInfo, name string, p node.ConfigProperty) {
		assert.Truef(t, known[p.Type], "%s.%s has unknown type %q", info.Type, name, p.Type)
		if len(p.Enum) > 0 {
			assert.Equalf(t, "string", p.Type, "%s.%s: enums are only supported on strings", info.Type, name)
		}
	})
}

// destinationWords are the name parts of properties that say where a node
// sends data or connects (docs/design/mcp.md §10).
var destinationWords = map[string]bool{
	"url": true, "urls": true, "host": true, "hosts": true, "broker": true, "brokers": true,
	"topic": true, "to": true, "cc": true, "bcc": true, "chat": true, "path": true,
	"server": true, "address": true, "webhook": true, "recipient": true, "recipients": true,
	"endpoint": true, "dsn": true,
}

// nameWords splits a camelCase property name into lower-case words.
func nameWords(name string) []string {
	var words []string
	start := 0
	for i := 1; i < len(name); i++ {
		if name[i] >= 'A' && name[i] <= 'Z' && (name[i-1] < 'A' || name[i-1] > 'Z') {
			words = append(words, strings.ToLower(name[start:i]))
			start = i
		}
	}
	return append(words, strings.ToLower(name[start:]))
}

func destinationLike(name string) bool {
	for _, w := range nameWords(name) {
		if destinationWords[w] {
			return true
		}
	}
	return false
}

func TestNameWords(t *testing.T) {
	assert.Equal(t, []string{"chat", "id"}, nameWords("chatId"))
	assert.Equal(t, []string{"to"}, nameWords("to"))
	assert.Equal(t, []string{"bot", "token"}, nameWords("botToken"))
	assert.Equal(t, []string{"max", "response", "size"}, nameWords("maxResponseSize"))
	assert.False(t, destinationLike("timeoutMs"))
	assert.False(t, destinationLike("toolName"))
	assert.True(t, destinationLike("webhookUrl"))
}

// notDestinations are destination-like names that say nothing about where a
// node sends data. Adding to this list is a deliberate review decision.
var notDestinations = map[string]string{
	"trigger-manual.topic": "a label copied into the emitted message",
}

// A property naming where a node sends data or connects must be marked
// Destination: the approval page and the low agent-deploy policy flag
// changes to it. The rule covers every node type (external and privileged
// ones send data; triggers connect to brokers, feeds and webhook paths).
func TestDestinationPropertiesAreMarked(t *testing.T) {
	eachProperty(t, func(info node.NodeTypeInfo, name string, p node.ConfigProperty) {
		id := info.Type + "." + name
		if !destinationLike(name) {
			return
		}
		if _, ok := notDestinations[id]; ok {
			assert.Falsef(t, p.Destination, "%s is listed as not a destination but marked Destination", id)
			return
		}
		assert.Truef(t, p.Destination, "%s looks like a destination and must be Destination (or be listed in notDestinations)", id)
	})
	for id := range notDestinations {
		typ, prop, _ := strings.Cut(id, ".")
		info, ok := node.DefaultRegistry.Get(typ)
		require.Truef(t, ok, "stale notDestinations entry %s", id)
		_, ok = info.Config.Properties[prop]
		assert.Truef(t, ok, "stale notDestinations entry %s", id)
	}
}

// Destinations whose names do not give them away.
func TestKnownDestinationPropertiesAreMarked(t *testing.T) {
	for _, id := range []string{
		"action-database.dsn",
		"action-email.port",
		"action-file.path",
		"action-gpio.pin",
		"action-i2c.bus",
		"action-i2c.register",
		"action-telegram.chatId",
		"trigger-mqtt.broker",
		"trigger-rss.url",
		"trigger-webhook.path",
	} {
		typ, prop, _ := strings.Cut(id, ".")
		info, ok := node.DefaultRegistry.Get(typ)
		require.Truef(t, ok, "%s not registered", typ)
		p, ok := info.Config.Properties[prop]
		require.Truef(t, ok, "%s missing", id)
		assert.Truef(t, p.Destination, "%s must be Destination", id)
	}
}

// riskSwitchName matches opt-ins that widen what a node may do.
var riskSwitchName = regexp.MustCompile(`^(allow|insecure|skip|disable)[A-Z]`)

// allow*/insecure* booleans widen what a node may do (private addresses,
// dash arguments, skipped TLS checks): they must be RiskSwitch, and nothing
// else is.
func TestRiskSwitchesAreMarked(t *testing.T) {
	seen := 0
	eachProperty(t, func(info node.NodeTypeInfo, name string, p node.ConfigProperty) {
		id := info.Type + "." + name
		if riskSwitchName.MatchString(name) {
			seen++
			assert.Equalf(t, "boolean", p.Type, "%s is a risk switch and must be a boolean", id)
			assert.Truef(t, p.RiskSwitch, "%s widens what the node may do and must be RiskSwitch", id)
			return
		}
		assert.Falsef(t, p.RiskSwitch, "%s is marked RiskSwitch but is not named like one (allow*/insecure*)", id)
	})
	assert.Positive(t, seen, "allowPrivate and allowDashArgs exist")
}

// operations select what a request does at its destination; changing one
// on a node that uses a credential is flagged (credentialed_request_changed).
// Message content (a Telegram text, an email body) is not an operation.
var operations = []string{
	"action-database.query",
	"action-http.body",
	"action-http.method",
	// What a Zigbee command does to the device. The node uses no credential
	// of its own (its connection holds them), like MQTT Publish, so the
	// marks raise no credentialed_request_changed alert today.
	"action-zigbee.command",
	"action-zigbee.payload",
	"action-zigbee.property",
}

func TestOperationPropertiesAreMarked(t *testing.T) {
	want := map[string]bool{}
	for _, id := range operations {
		want[id] = true
	}
	eachProperty(t, func(info node.NodeTypeInfo, name string, p node.ConfigProperty) {
		id := info.Type + "." + name
		assert.Equalf(t, want[id], p.Operation, "%s: Operation must be %v (the list above is a review decision)", id, want[id])
		if p.Operation {
			assert.Falsef(t, p.Destination || p.Sensitive, "%s: an operation is neither a destination nor a credential (those have their own alerts)", id)
		}
	})
}

// rates set how often a trigger fires or how many messages a node lets
// through (an interval, a cron expression, a limit, a batch size, a
// cooldown); changing one on a flow that reaches other services is flagged
// (rate_changed): a daily notification turned into ten a second spends the
// admin's credentials as surely as a new destination. A review decision,
// like operations.
var rates = []string{
	"process-aggregate.count",
	"process-join.count",
	"process-ratelimit.limit",
	"process-ratelimit.windowMs",
	"process-timer.duration", // with restart off, at most one emission per duration
	"process-timer.durationUnit",
	"trigger-cron.expression",
	"trigger-ds18b20.intervalSec",
	"trigger-interval.interval",
	"trigger-rss.intervalSec",
	"trigger-sysmetric.interval",
	"trigger-tool.cooldownMs",
	"trigger-zigbee.minChange",
	"trigger-zigbee.mode",
}

// rateName matches names that say they set a rate.
var rateName = regexp.MustCompile(`(?i)(interval|cooldown|window|cron|rate)`)

func TestRatePropertiesAreMarked(t *testing.T) {
	want := map[string]bool{}
	for _, id := range rates {
		want[id] = true
	}
	eachProperty(t, func(info node.NodeTypeInfo, name string, p node.ConfigProperty) {
		id := info.Type + "." + name
		if rateName.MatchString(name) {
			assert.Truef(t, want[id], "%s looks like a rate: list it in rates (and mark it Rate) or rename it", id)
		}
		assert.Equalf(t, want[id], p.Rate, "%s: Rate must be %v (the list above is a review decision)", id, want[id])
		if p.Rate {
			assert.Falsef(t, p.Destination || p.Sensitive || p.Operation, "%s: a rate is neither a destination, a credential nor an operation", id)
		}
	})
	for _, id := range rates {
		typ, prop, _ := strings.Cut(id, ".")
		info, ok := node.DefaultRegistry.Get(typ)
		require.Truef(t, ok, "stale rates entry %s", id)
		_, ok = info.Config.Properties[prop]
		assert.Truef(t, ok, "stale rates entry %s", id)
	}
}
