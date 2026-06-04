package telegram

import (
	"context"
	"log"
	"strings"

	"github.com/Suren878/matrixclaw/internal/safego"
)

func (w *Worker) isMonitoringRun(externalKey string, runID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.runs[monitorKey(externalKey, runID)] != nil
}

func (w *Worker) startMonitor(ctx context.Context, target chatTarget, sessionID string, runID string) {
	w.startMonitorWithDelivery(ctx, target, sessionID, runID, "")
}

func (w *Worker) startMonitorWithDelivery(ctx context.Context, target chatTarget, sessionID string, runID string, deliveryID string) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(runID) == "" {
		return
	}

	w.mu.Lock()
	key := monitorKey(target.externalKey, runID)
	state := w.states[key]
	if state == nil {
		state = &runDeliveryState{
			assistant:         map[string]sentAssistantMessage{},
			approvals:         map[string]int64{},
			voiceResults:      map[string]int64{},
			voiceFingerprints: map[string]int64{},
		}
		w.states[key] = state
	}
	if strings.TrimSpace(deliveryID) != "" {
		state.deliveryID = strings.TrimSpace(deliveryID)
	}
	if w.runs[key] != nil {
		w.mu.Unlock()
		return
	}
	monitorCtx, cancel := context.WithCancel(ctx)
	w.runs[key] = cancel
	w.mu.Unlock()

	safego.Go("telegram.monitorRun", func() {
		w.monitorRun(monitorCtx, target, sessionID, runID, state)
	})
}

func (w *Worker) monitorRun(ctx context.Context, target chatTarget, sessionID string, runID string, state *runDeliveryState) {
	var afterID uint64
	for {
		done, err := w.monitorRunEvents(ctx, target, sessionID, runID, state, &afterID)
		if done || ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("telegram: live monitor %s failed: %v", runID, err)
		}
		if !sleepContext(ctx, w.config.PollRetryDelay) {
			return
		}
	}
}

func (w *Worker) finishMonitor(externalKey string, runID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	key := monitorKey(externalKey, runID)
	delete(w.runs, key)
	delete(w.states, key)
}

func monitorKey(externalKey string, runID string) string {
	return strings.TrimSpace(externalKey) + ":" + strings.TrimSpace(runID)
}
