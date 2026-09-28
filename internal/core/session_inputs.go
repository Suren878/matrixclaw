package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// normalizeBusyInputMode reads how a message for a busy session is taken; one
// that names no mode steers the run.
func normalizeBusyInputMode(mode BusyInputMode) BusyInputMode {
	switch BusyInputMode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case BusyInputModeQueue:
		return BusyInputModeQueue
	case BusyInputModeInterrupt:
		return BusyInputModeInterrupt
	default:
		return BusyInputModeSteer
	}
}

func acceptRunStatusForInputMode(mode BusyInputMode) AcceptRunStatus {
	switch normalizeBusyInputMode(mode) {
	case BusyInputModeSteer:
		return AcceptRunStatusSteered
	case BusyInputModeInterrupt:
		return AcceptRunStatusInterrupting
	default:
		return AcceptRunStatusQueued
	}
}

func (c *Core) sessionGate(sessionID string) *sync.Mutex {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		sessionID = "_"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessionGates == nil {
		c.sessionGates = map[string]*sync.Mutex{}
	}
	gate := c.sessionGates[sessionID]
	if gate == nil {
		gate = &sync.Mutex{}
		c.sessionGates[sessionID] = gate
	}
	return gate
}

func (c *Core) createAcceptedRun(ctx context.Context, session Session, text string, parts []transcript.MessagePart, client string, externalKey string, capabilities ClientCapabilities, deliveryAddress json.RawMessage, continuesRunID string, trigger RunTrigger) (AcceptRunResult, error) {
	autoTitle := c.firstMessageAutoTitle(ctx, session, text)
	now := c.now().UTC()
	runID := c.newID("run")
	messageID := c.newID("msg")

	message := transcript.Message{
		ID:        messageID,
		SessionID: session.ID,
		RunID:     runID,
		Role:      transcript.MessageRoleUser,
		Content:   text,
		Parts:     parts,
		CreatedAt: now,
		UpdatedAt: now,
	}
	run := Run{
		ID:                 runID,
		SessionID:          session.ID,
		UserMessageID:      messageID,
		Client:             normalizeText(client),
		ExternalKey:        normalizeText(externalKey),
		ClientCapabilities: capabilities,
		ContinuesRunID:     normalizeText(continuesRunID),
		Trigger:            trigger,
		Status:             RunStatusAccepted,
		StartedAt:          now,
		UpdatedAt:          now,
	}
	delivery, hasDelivery, err := c.prepareSessionRunDelivery(run, text, parts, client, externalKey, deliveryAddress)
	if err != nil {
		return AcceptRunResult{}, err
	}
	var deliveries []ClientDelivery
	if hasDelivery {
		deliveries = append(deliveries, delivery)
	}
	if err := c.store.AcceptMessage(ctx, message, run, deliveries...); err != nil {
		return AcceptRunResult{}, err
	}
	c.applyAutoSessionTitle(ctx, session, autoTitle)
	c.publishEvent(Event{Type: EventMessageCreated, SessionID: session.ID, RunID: run.ID, Payload: message})
	c.publishEvent(Event{Type: EventRunUpdated, SessionID: session.ID, RunID: run.ID, Payload: run})
	return AcceptRunResult{
		SessionID:   session.ID,
		Status:      AcceptRunStatusStarted,
		UserMessage: message,
		Run:         run,
	}, nil
}

func (c *Core) createPendingSessionInput(ctx context.Context, session Session, active Run, input HandleMessageInput, text string, parts []transcript.MessagePart) (SessionInput, error) {
	mode := normalizeBusyInputMode(input.BusyMode)
	if mode == BusyInputModeQueue && active.Status == RunStatusWaitingEvents {
		// A run waiting for events takes any message at once.
		mode = BusyInputModeSteer
	}
	if mode == BusyInputModeSteer && !sessionAcceptsNativeSteer(session) {
		mode = BusyInputModeQueue
	}
	now := c.now().UTC()
	pending := SessionInput{
		ID:                 c.newID("input"),
		SessionID:          session.ID,
		TargetRunID:        active.ID,
		Mode:               mode,
		Status:             SessionInputStatusPending,
		Text:               text,
		Parts:              parts,
		Client:             normalizeText(input.Client),
		ExternalKey:        normalizeText(input.ExternalKey),
		ClientCapabilities: input.ClientCapabilities,
		DeliveryAddress:    cloneRawMessage(input.DeliveryAddress),
		WorkingDir:         normalizeText(input.WorkingDir),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := c.store.CreateSessionInput(ctx, pending); err != nil {
		return SessionInput{}, err
	}
	c.publishSessionInputUpdated(pending)
	return pending, nil
}

func sessionAcceptsNativeSteer(session Session) bool {
	return NormalizeSessionRuntime(session.RuntimeID) != SessionRuntimeExternalAgent &&
		NormalizeSessionKind(session.Kind) != SessionKindExternalAgent
}

func (c *Core) publishSessionInputUpdated(input SessionInput) {
	c.publishEvent(Event{
		Type:      EventInputUpdated,
		SessionID: input.SessionID,
		RunID:     input.TargetRunID,
		Payload:   input,
	})
}

func (c *Core) startNextPendingSessionInput(ctx context.Context, sessionID string) (bool, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" || c == nil || c.store == nil {
		return false, nil
	}

	var startRunID string
	var result AcceptRunResult
	gate := c.sessionGate(sessionID)
	gate.Lock()

	if _, err := c.store.GetActiveRunBySession(ctx, sessionID); err == nil {
		gate.Unlock()
		return false, nil
	} else if !errors.Is(err, ErrNotFound) {
		gate.Unlock()
		return false, err
	}
	if err := c.queuePendingSteersForInactiveSession(ctx, sessionID); err != nil {
		gate.Unlock()
		return false, err
	}
	input, err := c.store.NextPendingSessionInput(ctx, sessionID)
	if errors.Is(err, ErrNotFound) {
		gate.Unlock()
		return false, nil
	}
	if err != nil {
		gate.Unlock()
		return false, err
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		gate.Unlock()
		return false, err
	}
	result, err = c.consumeSessionInputAsRun(ctx, session, input)
	if err != nil {
		gate.Unlock()
		return false, err
	}
	startRunID = result.Run.ID
	gate.Unlock()

	if startRunID == "" {
		return false, nil
	}
	if err := c.startRun(ctx, startRunID); err != nil {
		_, failErr := c.failAcceptedRun(ctx, result, err)
		return true, failErr
	}
	return true, nil
}

func (c *Core) consumeSessionInputAsRun(ctx context.Context, session Session, input SessionInput) (AcceptRunResult, error) {
	parts := transcript.NormalizeMessageParts(input.Text, input.Parts)
	result, err := c.createAcceptedRun(ctx, session, input.Text, parts, input.Client, input.ExternalKey, input.ClientCapabilities, input.DeliveryAddress, "", "")
	if err != nil {
		return AcceptRunResult{}, err
	}
	now := c.now().UTC()
	input.Status = SessionInputStatusConsumed
	input.ConsumedRunID = result.Run.ID
	input.ConsumedAt = &now
	input.UpdatedAt = now
	if err := c.store.UpdateSessionInput(ctx, input); err != nil {
		return AcceptRunResult{}, err
	}
	result.Input = &input
	c.publishSessionInputUpdated(input)
	return result, nil
}

func (c *Core) prepareSessionRunDelivery(run Run, text string, parts []transcript.MessagePart, client string, externalKey string, address json.RawMessage) (ClientDelivery, bool, error) {
	client = normalizeText(client)
	externalKey = normalizeText(externalKey)
	if client == "" || externalKey == "" {
		return ClientDelivery{}, false, nil
	}
	delivery := ClientDelivery{
		Type:        ClientDeliveryTypeRun,
		Client:      client,
		ExternalKey: externalKey,
		SessionID:   normalizeText(run.SessionID),
		RunID:       normalizeText(run.ID),
		Summary:     sessionRunDeliverySummary(text, parts),
		Address:     cloneRawMessage(address),
		Status:      ClientDeliveryStatusPending,
	}
	prepared, err := c.prepareClientDelivery(delivery)
	if err != nil {
		return ClientDelivery{}, false, err
	}
	return prepared, true, nil
}

func sessionRunDeliverySummary(text string, parts []transcript.MessagePart) string {
	summary := strings.Join(strings.Fields(text), " ")
	if summary == "" {
		for _, part := range parts {
			if part.Text == nil {
				continue
			}
			summary = strings.Join(strings.Fields(part.Text.Text), " ")
			if summary != "" {
				break
			}
		}
	}
	if summary == "" {
		summary = "User message"
	}
	runes := []rune(summary)
	if len(runes) > 240 {
		return string(runes[:237]) + "..."
	}
	return summary
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func (c *Core) queuePendingSteersForRun(ctx context.Context, sessionID string, runID string) error {
	inputs, err := c.store.ListPendingSteerInputs(ctx, sessionID, runID)
	if err != nil {
		return err
	}
	for _, input := range inputs {
		input.Mode = BusyInputModeQueue
		input.TargetRunID = ""
		input.UpdatedAt = c.now().UTC()
		if err := c.store.UpdateSessionInput(ctx, input); err != nil {
			return err
		}
		c.publishSessionInputUpdated(input)
	}
	return nil
}

func (c *Core) queuePendingSteersForInactiveSession(ctx context.Context, sessionID string) error {
	inputs, err := c.store.ListPendingSessionInputs(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, input := range inputs {
		if input.Mode != BusyInputModeSteer {
			continue
		}
		input.Mode = BusyInputModeQueue
		input.TargetRunID = ""
		input.UpdatedAt = c.now().UTC()
		if err := c.store.UpdateSessionInput(ctx, input); err != nil {
			return err
		}
		c.publishSessionInputUpdated(input)
	}
	return nil
}

func (c *Core) RecoverSessionInputs(ctx context.Context) error {
	if c == nil || c.store == nil {
		return nil
	}
	inputs, err := c.store.ListPendingSessionInputs(ctx, "")
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, input := range inputs {
		sessionID := normalizeText(input.SessionID)
		if sessionID == "" {
			continue
		}
		if _, ok := seen[sessionID]; ok {
			continue
		}
		seen[sessionID] = struct{}{}
		if _, err := c.startNextPendingSessionInput(ctx, sessionID); err != nil {
			return fmt.Errorf("recover session input %s: %w", sessionID, err)
		}
	}
	return nil
}
