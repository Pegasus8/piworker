package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/mqttpool"
	"github.com/Pegasus8/piworker/internal/node"
)

// mqttTarget is where an MQTT node connects (ADR-0029): a named broker
// connection defined in the editor (ADR-0026), whose URL and credentials
// are resolved through the node's context when it connects, or the node's
// own broker URL and optional credentials, as before. Exactly one of the
// two is set.
type mqttTarget struct {
	connection string // named connection, or ""
	broker     string // own broker URL when connection is ""
	username   string // own credentials ({{secret.NAME}} resolved on connect)
	password   string
}

// parseMqttTarget reads the connection, broker, username and password
// settings. A connection is a literal name (no templates) and brings its
// own credentials, so the node's must be empty.
func parseMqttTarget(base *node.BaseNode) (mqttTarget, error) {
	t := mqttTarget{
		connection: strings.TrimSpace(base.GetConfigString("connection", "")),
		broker:     strings.TrimSpace(base.GetConfigString("broker", "")),
		username:   base.GetConfigString("username", ""),
		password:   base.GetConfigString("password", ""),
	}
	switch {
	case t.connection != "" && t.broker != "":
		return t, fmt.Errorf("set either connection or broker, not both")
	case t.connection == "" && t.broker == "":
		return t, fmt.Errorf("connection (a broker connection defined under Connections) or broker (e.g. tcp://host:1883) is required")
	case t.connection != "" && !brokers.ValidName(t.connection):
		return t, fmt.Errorf("connection must be the name of a broker connection (letters, digits, '_' or '-')")
	case t.connection != "" && (t.username != "" || t.password != ""):
		return t, fmt.Errorf("a connection brings its own username and password: leave the node's empty, or use broker instead of connection")
	}
	return t, nil
}

// endpoint is what the node connects with, its credentials resolved
// through ctx, in the sandbox of ctx if any (nodes in a sandbox never share
// a connection with nodes outside it, ADR-0020). A named connection that
// the context's store does not define is an error.
func (t mqttTarget) endpoint(ctx context.Context, clientID string) (mqttpool.Endpoint, error) {
	if t.connection == "" {
		return mqttEndpointFor(ctx, t.broker, t.username, t.password, clientID), nil
	}
	c, ok := brokers.FromContext(ctx).Get(t.connection)
	if !ok {
		return mqttpool.Endpoint{}, fmt.Errorf("broker connection %q is not defined", t.connection)
	}
	ep := c.Endpoint(ctx)
	ep.ClientID = clientID
	return ep, nil
}

// requiredConnections is node.ConnectionRequirer's answer: the named
// connection, if any.
func (t mqttTarget) requiredConnections() map[string]string {
	if t.connection == "" {
		return map[string]string{}
	}
	return map[string]string{"connection": t.connection}
}

// secretReferences is node.SecretReferencer's answer: the secrets of the
// node's own credentials (a connection's are the connection's).
func (t mqttTarget) secretReferences() map[string][]string {
	return referencedSecrets(map[string]string{"username": t.username, "password": t.password})
}

// String names the target for logs, never its credentials.
func (t mqttTarget) String() string {
	if t.connection != "" {
		return "connection " + t.connection
	}
	return t.broker
}

// mqttTargetSchema returns the connection, broker, username and password
// settings every MQTT node has.
func mqttTargetSchema() map[string]node.ConfigProperty {
	return map[string]node.ConfigProperty{
		"connection": {Type: "string", Title: "Connection",
			Description:  "Broker connection defined under Connections (its URL and credentials are used); leave empty to set a broker below",
			AgentVisible: true, Destination: true, Options: node.OptionsConnections},
		"broker":   {Type: "string", Title: "Broker", Description: "Broker URL, e.g. tcp://host:1883, when no connection is chosen", Destination: true},
		"username": {Type: "string", Title: "Username", Description: "Optional, with a broker URL only ({{secret.NAME}} supported)"},
		"password": {Type: "string", Title: "Password", Description: "Optional, with a broker URL only; use {{secret.NAME}}", Sensitive: true},
	}
}

// mergeProps returns the properties of a and b in one map (b wins).
func mergeProps(a, b map[string]node.ConfigProperty) map[string]node.ConfigProperty {
	out := make(map[string]node.ConfigProperty, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
