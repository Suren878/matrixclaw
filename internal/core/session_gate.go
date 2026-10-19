package core

import "sync"

// sessionGate serialises the run bookkeeping of one session. The gate is
// dropped once its last holder unlocks it.
type sessionGate struct {
	c    *Core
	id   string
	mu   sync.Mutex
	refs int
}

func (g *sessionGate) Lock() { g.mu.Lock() }

func (g *sessionGate) Unlock() {
	g.mu.Unlock()
	g.c.mu.Lock()
	defer g.c.mu.Unlock()
	if g.refs--; g.refs == 0 {
		delete(g.c.sessionGates, g.id)
	}
}

// sessionGate returns the session's gate; the caller must Lock and Unlock it once.
func (c *Core) sessionGate(sessionID string) *sessionGate {
	if sessionID == "" {
		sessionID = "_"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	gate := c.sessionGates[sessionID]
	if gate == nil {
		gate = &sessionGate{c: c, id: sessionID}
		c.sessionGates[sessionID] = gate
	}
	gate.refs++
	return gate
}
