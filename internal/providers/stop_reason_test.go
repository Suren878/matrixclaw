package providers

import "testing"

func TestResolveStopReasonFollowsTheReply(t *testing.T) {
	for _, tc := range []struct {
		reason    StopReason
		toolCalls int
		want      StopReason
	}{
		{"", 0, StopEndTurn},
		{StopEndTurn, 2, StopToolUse},
		{StopToolUse, 0, StopEndTurn},
		{StopMaxTokens, 1, StopMaxTokens},
		{StopContentFilter, 0, StopContentFilter},
		{StopRefusal, 0, StopRefusal},
	} {
		if got := ResolveStopReason(tc.reason, tc.toolCalls); got != tc.want {
			t.Errorf("ResolveStopReason(%q, %d) = %q, want %q", tc.reason, tc.toolCalls, got, tc.want)
		}
	}
}

func TestOnlyTruncatedOrBlockedRepliesMayBeEmpty(t *testing.T) {
	for reason, want := range map[StopReason]bool{
		StopEndTurn: false, StopToolUse: false, StopMaxTokens: true, StopRefusal: true, StopContentFilter: true,
	} {
		if got := reason.AllowsEmptyReply(); got != want {
			t.Errorf("%q.AllowsEmptyReply() = %v, want %v", reason, got, want)
		}
	}
}
