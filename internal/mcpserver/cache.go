package mcpserver

import (
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// serverCache keeps the newest per-token server of at most max tokens; the
// least recently used one is dropped first. A token's server is replaced as
// soon as its key changes (a redeploy, a grant or epoch change), so stale
// servers never pile up.
type serverCache struct {
	mu      sync.Mutex
	max     int
	tick    uint64
	byToken map[string]*cachedServer
}

type cachedServer struct {
	key  serverKey
	srv  *mcp.Server
	used uint64
}

func newServerCache(max int) *serverCache {
	return &serverCache{max: max, byToken: map[string]*cachedServer{}}
}

// get returns the cached server of key, or nil.
func (c *serverCache) get(key serverKey) *mcp.Server {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.byToken[key.tokenID]
	if e == nil || e.key != key {
		return nil
	}
	c.tick++
	e.used = c.tick
	return e.srv
}

// put caches srv as the token's server and returns the server to use: srv,
// or one that a concurrent request cached for the same key first.
func (c *serverCache) put(key serverKey, srv *mcp.Server) *mcp.Server {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tick++
	if e := c.byToken[key.tokenID]; e != nil && e.key == key {
		e.used = c.tick
		return e.srv
	}
	c.byToken[key.tokenID] = &cachedServer{key: key, srv: srv, used: c.tick}
	for len(c.byToken) > c.max {
		var oldest string
		var least uint64
		for id, e := range c.byToken {
			if oldest == "" || e.used < least {
				oldest, least = id, e.used
			}
		}
		delete(c.byToken, oldest)
	}
	return srv
}

func (c *serverCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.byToken)
}
