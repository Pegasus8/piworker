package mcpserver

import (
	"sync"

	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
)

// gates are what PiWorker's settings let /mcp serve now, besides the token's
// scopes: the author scope needs mcp.author_enabled, the deploy scope an
// agent deploy level above off (docs/design/mcp.md §7, §10). A built-in tool
// or prompt of a scope that is not served is, to the token, a name that does
// not exist: neither listed nor callable. The services check the same
// settings on every call; the gates only decide what is advertised.
type gates struct {
	author bool
	deploy bool
}

// serves reports whether actor may use the built-in tools and prompts of
// scope now.
func (g gates) serves(actor service.Actor, scope string) bool {
	if !actor.HasScope(scope) {
		return false
	}
	switch scope {
	case storage.ScopeAuthor:
		return g.author
	case storage.ScopeDeploy:
		return g.deploy
	}
	return true
}

// gatesFor returns the gates of a request of actor at tokens epoch: the
// deploy level is read only for tokens with the deploy scope.
func (h *Handler) gatesFor(actor service.Actor, epoch int64) gates {
	g := gates{author: h.authorEnabled}
	if actor.HasScope(storage.ScopeDeploy) {
		g.deploy = h.deployLevelAt(epoch) != storage.AgentDeployOff
	}
	return g
}

// levelCache remembers the agent deploy level read at one tokens epoch:
// changing the level bumps the epoch (storage.SetAgentDeployLevel), so the
// level is read again only when the verifier sees a new epoch.
type levelCache struct {
	mu    sync.Mutex
	known bool
	epoch int64
	level storage.AgentDeployLevel
}

// deployLevelAt returns the agent deploy level at tokens epoch. Without a
// reader it is off; a failed read is off too (fail closed), logged and not
// remembered, so the next request reads it again.
func (h *Handler) deployLevelAt(epoch int64) storage.AgentDeployLevel {
	if h.deployLevel == nil {
		return storage.AgentDeployOff
	}
	c := &h.levels
	c.mu.Lock()
	if c.known && c.epoch == epoch {
		level := c.level
		c.mu.Unlock()
		return level
	}
	c.mu.Unlock()
	level, err := h.deployLevel.AgentDeployLevel()
	if err != nil {
		h.log.Error().Err(err).Msg("Cannot read the agent deploy level; serving no deploy tools")
		return storage.AgentDeployOff
	}
	c.mu.Lock()
	c.known, c.epoch, c.level = true, epoch, level
	c.mu.Unlock()
	return level
}

// logServed logs once, at construction, what the settings let the endpoint
// serve besides flow and read tools: the author and deploy tools (and
// prompts) registered, whether mcp.author_enabled serves the author ones and
// whether the agent deploy level is read (it is read per tokens epoch, not
// here: pw_system_status and the editor show it). A setting with nothing
// registered to serve is a wiring mistake, warned about.
func (h *Handler) logServed() {
	count := func(scope string) int {
		n := 0
		for _, st := range h.static {
			if st.Scope == scope {
				n++
			}
		}
		for _, p := range h.prompts {
			if p.Scope == scope {
				n++
			}
		}
		return n
	}
	author, deploy := count(storage.ScopeAuthor), count(storage.ScopeDeploy)
	h.log.Info().Int("authorTools", author).Bool("authorEnabled", h.authorEnabled).Int("deployTools", deploy).
		Bool("deployLevelReader", h.deployLevel != nil).Msg("MCP built-in tools registered (with their prompts)")
	if h.authorEnabled && author == 0 {
		h.log.Warn().Msg("mcp.author_enabled is set, but no author tool is registered: agents see none")
	}
	if h.deployLevel != nil && deploy == 0 {
		h.log.Warn().Msg("The agent deploy level is read, but no deploy tool is registered: agents see none")
	}
}
