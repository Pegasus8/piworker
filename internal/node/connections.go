package node

// ConnectionRequirer is implemented by nodes whose settings name a broker
// connection (brokers.Store, ADR-0026), such as the Zigbee nodes. Deploy
// refuses a flow (a validation error, 400) while a named connection is not
// defined; Lint reports it as undefined_connection, an environment issue the
// admin may still fix. The connection is resolved when the node runs, through
// its context (brokers.FromContext), never at deploy.
type ConnectionRequirer interface {
	// RequiredConnections returns, by config property, the connection
	// name that property holds.
	RequiredConnections() map[string]string
}
