package builtin

import (
	"context"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/zigbee"
)

// zigbeeStateFunc is the expressions' zigbee(device) and
// zigbee(connection, device): the device's last reported state (a copy),
// from the connection named or the first one that knows the device, or nil.
// In a sandbox (dry-run, node test) it is always nil: an agent's dry-run
// never reads the house (ADR-0020).
func zigbeeStateFunc(ctx context.Context) func(args ...interface{}) interface{} {
	return func(args ...interface{}) interface{} {
		m := zigbee.Default
		if m == nil || node.InSandbox(ctx) {
			return nil
		}
		switch len(args) {
		case 1:
			device, _ := args[0].(string)
			for _, conn := range m.Connections() {
				if s, ok := m.DeviceState(conn, device); ok {
					return s
				}
			}
		case 2:
			conn, _ := args[0].(string)
			device, _ := args[1].(string)
			if s, ok := m.DeviceState(conn, device); ok {
				return s
			}
		}
		return nil
	}
}
