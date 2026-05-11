package core

import "github.com/Suren878/matrixclaw/internal/tools"

func (c *Core) ListToolSpecs() []tools.Spec {
	if c.tools == nil {
		return nil
	}
	return c.tools.List()
}
