package secrets

import "context"

type storeKey struct{}

// WithStore returns a context whose secret lookups use store instead of
// DefaultStore. The builtin nodes resolve every {{secret.NAME}} through the
// context they are given (FromContext), so a sandboxed execution such as a
// dry-run passes an empty store and never sees the real values. A nil store
// is ignored.
func WithStore(ctx context.Context, store *Store) context.Context {
	if store == nil {
		return ctx
	}
	return context.WithValue(ctx, storeKey{}, store)
}

// FromContext returns the secret store carried by ctx (see WithStore), or
// DefaultStore when there is none (or ctx is nil).
func FromContext(ctx context.Context) *Store {
	if ctx != nil {
		if store, ok := ctx.Value(storeKey{}).(*Store); ok && store != nil {
			return store
		}
	}
	return DefaultStore
}
