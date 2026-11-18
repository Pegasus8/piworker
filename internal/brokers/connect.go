package brokers

import (
	"context"
	"fmt"

	"github.com/Pegasus8/piworker/internal/mqttpool"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Endpoint is the pooled connection (ADR-0025) for c, its credentials'
// {{secret.NAME}} references resolved through ctx and in the sandbox of ctx,
// if any.
func (c Connection) Endpoint(ctx context.Context) mqttpool.Endpoint {
	return mqttpool.Endpoint{
		Broker:   c.Broker,
		Username: secrets.Expand(ctx, c.Username),
		Password: secrets.Expand(ctx, c.Password),
		Sandbox:  node.SandboxFrom(ctx),
	}
}

// Tester tries connections with a client of its own, outside the pool.
type Tester struct{}

// TestConnection connects to c's broker and disconnects, within ctx.
func (Tester) TestConnection(ctx context.Context, c Connection) error {
	ep := c.Endpoint(ctx)
	opts := mqttpool.ClientOptions(ep.Broker, mqttpool.DefaultClientID("piworker-test"), ep.Username, ep.Password).
		SetAutoReconnect(false)
	client := mqtt.NewClient(opts)
	tok := client.Connect()
	select {
	case <-tok.Done():
	case <-ctx.Done():
		client.Disconnect(0)
		return fmt.Errorf("no answer from the broker: %w", ctx.Err())
	}
	if err := tok.Error(); err != nil {
		return err
	}
	client.Disconnect(100)
	return nil
}
