package vars

import (
	"context"
	"fmt"
	"sync"
)

type flowStoreKey struct{}

// WithFlowStore supplies the flow variable store (scope "flow") of the
// execution: the runtime passes the flow's persistent store, a sandboxed
// execution an isolated one.
func WithFlowStore(ctx context.Context, store *Store) context.Context {
	return context.WithValue(ctx, flowStoreKey{}, store)
}

// FlowFromContext returns the flow variable store carried by ctx, or nil when
// there is none (or ctx is nil).
func FlowFromContext(ctx context.Context) *Store {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(flowStoreKey{}).(*Store)
	return s
}
func Scoped(ctx context.Context, scope string) (*Store, error) {
	if scope == "" || scope == "global" {
		return FromContext(ctx), nil
	}
	if scope != "flow" {
		return nil, fmt.Errorf("invalid variable scope %q", scope)
	}
	s := FlowFromContext(ctx)
	if s == nil {
		return nil, fmt.Errorf("flow variables require a saved flow context")
	}
	return s, nil
}

// FlowStores shares one write-through store between the API and each runtime.
// Configure the persistence factory before starting the server.
type FlowStores struct {
	mu      sync.Mutex
	stores  map[string]*Store
	Factory func(string) Persister
}

var DefaultFlows = &FlowStores{}

func (f *FlowStores) Get(id string) (*Store, error) {
	if id == "" {
		return nil, fmt.Errorf("flow ID is required")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if s := f.stores[id]; s != nil {
		return s, nil
	}
	s := NewStore()
	if f.Factory != nil {
		if err := s.Attach(f.Factory(id)); err != nil {
			return nil, err
		}
	}
	if f.stores == nil {
		f.stores = make(map[string]*Store)
	}
	f.stores[id] = s
	return s, nil
}
func (f *FlowStores) Forget(id string) { f.mu.Lock(); defer f.mu.Unlock(); delete(f.stores, id) }
