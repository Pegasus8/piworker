package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/zigbee"
)

// The Zigbee nodes talk to Zigbee2MQTT through a named broker connection
// (ADR-0026) and the shared MQTT connections (ADR-0025).

// zigbeeConnectionName validates a node's connection setting: a literal
// connection name (no templates).
func zigbeeConnectionName(base *node.BaseNode) (string, error) {
	name := strings.TrimSpace(base.GetConfigString("connection", ""))
	if name == "" {
		return "", fmt.Errorf("connection is required (define one under Connections)")
	}
	if !brokers.ValidName(name) {
		return "", fmt.Errorf("connection must be the name of a broker connection (letters, digits, '_' or '-')")
	}
	return name, nil
}

// zigbeeLiteralDevice validates a device setting that takes no templates.
func zigbeeLiteralDevice(base *node.BaseNode) (string, error) {
	device := strings.TrimSpace(base.GetConfigString("device", ""))
	if strings.Contains(device, "{{") {
		return "", fmt.Errorf("device takes a device name, not a template")
	}
	if err := zigbee.ValidDeviceName(device); err != nil {
		return "", err
	}
	return device, nil
}

// zigbeeConnection resolves the named connection through ctx.
func zigbeeConnection(ctx context.Context, name string) (brokers.Connection, error) {
	c, ok := brokers.FromContext(ctx).Get(name)
	if !ok {
		return brokers.Connection{}, fmt.Errorf("broker connection %q is not defined", name)
	}
	return c, nil
}

// zigbeeSchemaConnection and zigbeeSchemaDevice are the settings every
// Zigbee node has.
func zigbeeSchemaConnection() node.ConfigProperty {
	return node.ConfigProperty{Type: "string", Title: "Connection",
		Description:  "Broker connection to Zigbee2MQTT (defined under Connections)",
		AgentVisible: true, Destination: true, Options: node.OptionsConnections}
}

func zigbeeSchemaDevice(templates bool) node.ConfigProperty {
	desc := "Zigbee2MQTT friendly name of the device or group"
	if templates {
		desc += " (supports {{templates}})"
	}
	return node.ConfigProperty{Type: "string", Title: "Device", Description: desc,
		AgentVisible: true, Destination: true, Options: node.OptionsZigbeeDevices}
}
