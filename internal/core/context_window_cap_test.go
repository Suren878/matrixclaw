package core_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestContextWindowCapBoundsRunsAndTheContextReport(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cap       int
		window    int
		summaries int
	}{
		{name: "default", cap: 0, window: core.DefaultContextWindowCap, summaries: 1},
		{name: "negative", cap: -1, window: core.DefaultContextWindowCap, summaries: 1},
		{name: "disabled", cap: 2_000_000, window: 1_000_000, summaries: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, db, cleanup := newCrashRecoveryCore(t)
			defer cleanup()
			summaries := 0
			app.WithContextWindowCap(tc.cap).WithSessionLLMs(windowLLMs{window: 1_000_000, recoveryLLMs: recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
				if strings.HasPrefix(request.SystemPrompt, "You compact matrixclaw chat histories") {
					summaries++
					return providers.Response{Text: "SUMMARY"}, nil
				}
				return providers.Response{Text: "Done."}, nil
			})}})
			big := strings.Repeat("x", 700_000)
			session, run := saveNativeRunWithHistory(t, db, "cap_"+tc.name, transcript.Message{
				ID: "msg_old_" + tc.name, Role: transcript.MessageRoleUser, Content: big, Parts: transcript.NormalizeMessageParts(big, nil),
			})
			report, err := app.SessionContext(context.Background(), session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if report.WindowTokens != tc.window {
				t.Fatalf("report window = %d, want %d", report.WindowTokens, tc.window)
			}

			if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
				t.Fatal(err)
			}

			assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
			if summaries != tc.summaries {
				t.Fatalf("summaries = %d, want %d for a ~175k-token history", summaries, tc.summaries)
			}
		})
	}
}

type outputLimitedRuntime struct {
	generationRuntimeFunc
	output int64
}

func (r outputLimitedRuntime) OutputLimits() (int64, int64) { return r.output, r.output }

func TestContextReportRecommendsCompactingWhereRunsWouldSummarise(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	// A 64k output limit leaves 50k of a 100k window, so ~45k tokens of
	// history are past the 80% mark.
	app.WithSessionLLMs(windowLLMs{window: 100_000, recoveryLLMs: recoveryLLMs{runtime: outputLimitedRuntime{output: 65_536, generationRuntimeFunc: func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Text: "Done."}, nil
	}}}})
	big := strings.Repeat("x", 180_000)
	session, _ := saveNativeRunWithHistory(t, db, "report", transcript.Message{
		ID: "msg_old_report", Role: transcript.MessageRoleUser, Content: big, Parts: transcript.NormalizeMessageParts(big, nil),
	})

	report, err := app.SessionContext(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Compact.Recommended {
		t.Fatalf("report of ~%d tokens does not recommend compacting", report.TokenEstimate)
	}
}
