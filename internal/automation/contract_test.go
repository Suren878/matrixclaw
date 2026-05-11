package automation

import (
	"encoding/json"
	"testing"
)

func TestAutomationHTTPContractsUseStableJSONKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		in           any
		wantTopLevel []string
		wantValues   map[string]any
	}{
		{
			name: "create job request",
			in: CreateJobRequest{
				Kind:            JobKindReminder,
				SessionID:       "session-1",
				Client:          "terminal",
				ExternalKey:     "local",
				Title:           "Reminder",
				Timezone:        "UTC",
				ScheduleMode:    ScheduleModeOnce,
				RunAt:           "2026-05-04T12:00:00Z",
				IntervalSeconds: 60,
				CronExpr:        "0 * * * *",
				Prompt:          "check docs",
			},
			wantTopLevel: []string{
				"kind",
				"session_id",
				"client",
				"external_key",
				"title",
				"timezone",
				"schedule_mode",
				"run_at",
				"interval_seconds",
				"cron_expr",
				"prompt",
			},
			wantValues: map[string]any{
				"kind":          "reminder",
				"session_id":    "session-1",
				"schedule_mode": "once",
			},
		},
		{
			name:         "job response",
			in:           JobResponse{Job: Job{ID: "job-1"}},
			wantTopLevel: []string{"job"},
		},
		{
			name:         "jobs response",
			in:           JobsResponse{Jobs: []Job{{ID: "job-1"}}},
			wantTopLevel: []string{"jobs"},
		},
		{
			name:         "fire response",
			in:           FireResponse{Fire: Fire{ID: "fire-1"}},
			wantTopLevel: []string{"fire"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			var got map[string]any
			if err := json.Unmarshal(payload, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if len(got) != len(tt.wantTopLevel) {
				t.Fatalf("top-level keys = %#v, want exactly %#v; full payload %s", keys(got), tt.wantTopLevel, payload)
			}
			for _, key := range tt.wantTopLevel {
				if _, ok := got[key]; !ok {
					t.Fatalf("missing key %q in payload %s", key, payload)
				}
			}
			for key, want := range tt.wantValues {
				if !jsonEqual(got[key], want) {
					t.Fatalf("%s = %#v, want %#v; full payload %s", key, got[key], want, payload)
				}
			}
		})
	}
}

func jsonEqual(got any, want any) bool {
	gotPayload, err := json.Marshal(got)
	if err != nil {
		return false
	}
	wantPayload, err := json.Marshal(want)
	if err != nil {
		return false
	}
	return string(gotPayload) == string(wantPayload)
}

func keys(values map[string]any) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	return result
}
