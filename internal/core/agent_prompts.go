package core

func (c *Core) webResearchPromptAvailable() bool {
	if c == nil || c.tools == nil {
		return false
	}
	_, ok := c.tools.Spec("web_research")
	return ok
}

func (c *Core) fileDeliveryPromptAvailable() bool {
	if c == nil || c.tools == nil {
		return false
	}
	_, ok := c.tools.Spec("send_file")
	return ok
}

func (c *Core) telephonyCallPromptAvailable() bool {
	if c == nil || c.tools == nil {
		return false
	}
	_, ok := c.tools.Spec("telephony_call")
	return ok
}

func (c *Core) delegateTaskPromptAvailable() bool {
	if c == nil || c.tools == nil {
		return false
	}
	if _, ok := c.tools.Spec(delegateTaskToolName); ok {
		return true
	}
	_, ok := c.tools.Spec(spawnSubagentToolName)
	return ok
}
