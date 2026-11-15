package node

import "context"

// NodeInfo identifies the node a context belongs to: the flow runtime puts
// the running flow's and node's IDs on the context of every node (Process
// and trigger Start). Agent nodes name themselves with it when they call
// tools (AgentCaller).
type NodeInfo struct {
	FlowID string
	NodeID string
}

type nodeInfoKey struct{}

// WithNodeInfo returns ctx carrying info.
func WithNodeInfo(ctx context.Context, info NodeInfo) context.Context {
	return context.WithValue(ctx, nodeInfoKey{}, info)
}

// NodeInfoFromContext returns the node ctx belongs to, or the zero NodeInfo
// outside a flow runtime (a node test, a nil ctx).
func NodeInfoFromContext(ctx context.Context) NodeInfo {
	if ctx == nil {
		return NodeInfo{}
	}
	info, _ := ctx.Value(nodeInfoKey{}).(NodeInfo)
	return info
}
