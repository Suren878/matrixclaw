package telegram

import (
	"context"
	"log"
	"strings"
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
			assistant: map[string]sentAssistantMessage{},
			approvals: map[string]int64{},
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

	go func() {
		if err := w.monitorRun(monitorCtx, target, sessionID, runID, state); err != nil && monitorCtx.Err() == nil {
			log.Printf("telegram: run monitor %s failed: %v", runID, err)
		}
	}()
}

func (w *Worker) monitorRun(ctx context.Context, target chatTarget, sessionID string, runID string, state *runDeliveryState) error {
	var afterID uint64
	for {
		done, err := w.monitorRunEvents(ctx, target, sessionID, runID, state, &afterID)
		if done || ctx.Err() != nil {
			return nil
		}
		if err != nil {
			log.Printf("telegram: live monitor %s failed: %v", runID, err)
		}
		if !sleepContext(ctx, w.config.PollRetryDelay) {
			return nil
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
