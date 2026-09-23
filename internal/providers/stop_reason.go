package providers

// ResolveStopReason reconciles a mapped wire reason with the reply itself:
// gateways report "stop" alongside tool calls, or "tool_calls" without any.
func ResolveStopReason(reason StopReason, toolCalls int) StopReason {
	switch reason {
	case "", StopEndTurn, StopToolUse:
		if toolCalls > 0 {
			return StopToolUse
		}
		return StopEndTurn
	default:
		return reason
	}
}

// AllowsEmptyReply reports whether a reply with neither text nor tool calls is a
// valid outcome for this reason rather than a failed generation.
func (r StopReason) AllowsEmptyReply() bool {
	return r == StopMaxTokens || r == StopRefusal || r == StopContentFilter
}
