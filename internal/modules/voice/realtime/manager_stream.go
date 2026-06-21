package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/safego"
)

type pendingTool struct {
	ID   string
	Name string
}

type streamReadResult struct {
	event Event
	err   error
}

type providerReadResult struct {
	output ProviderOutput
	err    error
}

func (m *Manager) ServeStream(ctx context.Context, sessionID string, stream Stream) error {
	if stream == nil {
		return fmt.Errorf("%w: stream is required", ErrInvalidRequest)
	}
	session, ok := m.session(sessionID)
	if !ok {
		return ErrSessionNotFound
	}
	info := m.markSessionStreaming(session)
	provider, _, ok := m.provider(ctx, info.ProviderID)
	if !ok || provider == nil {
		return fmt.Errorf("%w: %s", ErrProviderUnavailable, info.ProviderID)
	}
	coreSession, err := m.core.GetSession(ctx, info.CoreSessionID)
	if err != nil {
		m.markSessionClosed(session, err.Error(), SessionStatusFailed)
		return err
	}

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() { _ = stream.Close(nil) }()

	conn, err := provider.Connect(streamCtx, ProviderConnectRequest{
		VoiceSessionID:    info.ID,
		SessionID:         info.CoreSessionID,
		Client:            info.Client,
		WorkingDir:        firstNonEmpty(session.workingDir, coreSession.WorkingDir),
		ModelID:           info.ModelID,
		VoiceID:           info.VoiceID,
		Language:          info.Language,
		SystemInstruction: m.systemInstruction(session),
		InputAudio:        info.InputAudio,
		OutputAudio:       info.OutputAudio,
		Tools:             m.toolDeclarations(info.Client),
	})
	if err != nil {
		m.markSessionClosed(session, err.Error(), SessionStatusFailed)
		_ = stream.Write(streamCtx, newEvent(info.ID, EventError, ErrorPayload{Message: err.Error(), Recoverable: false}))
		return err
	}
	defer func() { _ = conn.Close(nil) }()

	if err := stream.Write(streamCtx, newEvent(info.ID, EventSessionReady, map[string]any{"session": info})); err != nil {
		m.markSessionClosed(session, err.Error(), SessionStatusFailed)
		return err
	}

	clientCh := make(chan streamReadResult, 16)
	providerCh := make(chan providerReadResult, 16)
	coreCh := m.core.SubscribeEvents(streamCtx, info.CoreSessionID)
	safego.Go("realtime.readClientEvents", func() { readClientEvents(streamCtx, stream, clientCh) })
	safego.Go("realtime.readProviderEvents", func() { readProviderEvents(streamCtx, conn, providerCh) })

	state := streamState{
		manager:     m,
		session:     session,
		info:        info,
		coreSession: coreSession,
		stream:      stream,
		provider:    conn,
		pending:     map[string]pendingTool{},
	}

	for {
		select {
		case item := <-clientCh:
			if item.err != nil {
				m.markSessionClosed(session, "", SessionStatusClosed)
				return nil
			}
			if done, err := state.handleClientEvent(streamCtx, item.event); done || err != nil {
				if err != nil {
					m.markSessionClosed(session, err.Error(), SessionStatusFailed)
					return err
				}
				m.markSessionClosed(session, "", SessionStatusClosed)
				return nil
			}
		case item := <-providerCh:
			if item.err != nil {
				if errors.Is(item.err, io.EOF) || streamCtx.Err() != nil {
					m.markSessionClosed(session, "", SessionStatusClosed)
					return nil
				}
				m.markSessionClosed(session, item.err.Error(), SessionStatusFailed)
				_ = stream.Write(streamCtx, newEvent(info.ID, EventError, ErrorPayload{Message: item.err.Error(), Recoverable: false}))
				return item.err
			}
			if err := state.handleProviderOutput(streamCtx, item.output); err != nil {
				m.markSessionClosed(session, err.Error(), SessionStatusFailed)
				return err
			}
		case event := <-coreCh:
			if err := state.handleCoreEvent(streamCtx, event); err != nil {
				m.markSessionClosed(session, err.Error(), SessionStatusFailed)
				return err
			}
		case <-streamCtx.Done():
			m.markSessionClosed(session, "", SessionStatusClosed)
			return nil
		}
	}
}

type streamState struct {
	manager             *Manager
	session             *voiceSession
	info                SessionInfo
	coreSession         core.Session
	stream              Stream
	provider            ProviderConnection
	pending             map[string]pendingTool
	inputTranscript     strings.Builder
	assistantTranscript strings.Builder
}

func (s *streamState) handleClientEvent(ctx context.Context, event Event) (bool, error) {
	switch event.Type {
	case EventInputAudioAppend:
		var payload InputAudioPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return false, err
		}
		payload.AudioBase64 = strings.TrimSpace(payload.AudioBase64)
		if payload.AudioBase64 == "" {
			return false, fmt.Errorf("%w: audio_base64 is required", ErrInvalidRequest)
		}
		return false, s.provider.Send(ctx, ProviderInput{
			Type:          ProviderInputAudioAppend,
			AudioBase64:   payload.AudioBase64,
			AudioMIMEType: firstNonEmpty(payload.MIMEType, audioMIMEType(s.info.InputAudio)),
		})
	case EventInputAudioEnd:
		return false, s.provider.Send(ctx, ProviderInput{Type: ProviderInputAudioEnd})
	case EventInputTextAppend:
		var payload InputTextPayload
		if err := decodePayload(event.Payload, &payload); err != nil {
			return false, err
		}
		text := strings.TrimSpace(payload.Text)
		if text == "" {
			return false, fmt.Errorf("%w: text is required", ErrInvalidRequest)
		}
		return false, s.provider.Send(ctx, ProviderInput{Type: ProviderInputTextAppend, Text: text, EndOfTurn: payload.EndOfTurn})
	case EventResponseCancel:
		return false, s.provider.Send(ctx, ProviderInput{Type: ProviderInputCancel})
	case EventSessionClose:
		return true, nil
	default:
		return false, fmt.Errorf("%w: unsupported event type %q", ErrInvalidRequest, event.Type)
	}
}

func (s *streamState) handleProviderOutput(ctx context.Context, output ProviderOutput) error {
	switch output.Type {
	case ProviderOutputInputTranscript:
		text := strings.TrimSpace(output.Text)
		if text == "" {
			return nil
		}
		appendTranscript(&s.inputTranscript, text)
		return s.stream.Write(ctx, newEvent(s.info.ID, EventInputTranscriptDelta, TranscriptPayload{Text: text}))
	case ProviderOutputAssistantTranscript:
		text := strings.TrimSpace(output.Text)
		if text == "" {
			return nil
		}
		appendTranscript(&s.assistantTranscript, text)
		return s.stream.Write(ctx, newEvent(s.info.ID, EventAssistantTranscriptDelta, TranscriptPayload{Text: text}))
	case ProviderOutputAssistantAudio:
		if strings.TrimSpace(output.AudioBase64) == "" {
			return nil
		}
		return s.stream.Write(ctx, newEvent(s.info.ID, EventAssistantAudioDelta, AssistantAudioPayload{
			AudioBase64: output.AudioBase64,
			MIMEType:    firstNonEmpty(output.MIMEType, audioMIMEType(s.info.OutputAudio)),
		}))
	case ProviderOutputTurnComplete:
		return s.finishTurn(ctx)
	case ProviderOutputToolCall:
		return s.handleToolCalls(ctx, output.ToolCalls)
	case ProviderOutputGoAway:
		return s.stream.Write(ctx, newEvent(s.info.ID, EventBackpressure, map[string]any{"message": "provider will close the realtime session soon"}))
	case ProviderOutputSessionResumption:
		return nil
	case ProviderOutputInterrupted:
		s.assistantTranscript.Reset()
		return s.stream.Write(ctx, newEvent(s.info.ID, EventInterrupted, map[string]any{}))
	case ProviderOutputError:
		return s.stream.Write(ctx, newEvent(s.info.ID, EventError, ErrorPayload{Message: output.Error, Recoverable: true}))
	default:
		return nil
	}
}

func (s *streamState) handleToolCalls(ctx context.Context, calls []ProviderToolCall) error {
	for i := range calls {
		call := calls[i]
		call.ID = firstNonEmpty(call.ID, fmt.Sprintf("%s_tool_%d", s.info.ID, i+1))
		call.Name = strings.TrimSpace(call.Name)
		if call.Name == "" {
			continue
		}
		if len(call.Args) == 0 {
			call.Args = json.RawMessage(`{}`)
		}
		if err := s.stream.Write(ctx, newEvent(s.info.ID, EventToolCall, ToolCallPayload(call))); err != nil {
			return err
		}
		result, err := s.manager.core.ExecuteTool(ctx, core.ExecuteToolInput{
			SessionID:   s.info.CoreSessionID,
			ToolName:    call.Name,
			ToolCallID:  call.ID,
			WorkingDir:  firstNonEmpty(s.session.workingDir, s.coreSession.WorkingDir),
			Client:      s.info.Client,
			ExternalKey: s.info.ExternalKey,
			Args:        call.Args,
		})
		if result.Approval != nil {
			s.pending[call.ID] = pendingTool{ID: call.ID, Name: call.Name}
			if err := s.stream.Write(ctx, newEvent(s.info.ID, EventApprovalRequested, ApprovalRequestedPayload{
				ID:          result.Approval.ID,
				ToolCallID:  call.ID,
				ToolName:    call.Name,
				Action:      result.Approval.Action,
				Path:        result.Approval.Path,
				Description: result.Approval.Description,
			})); err != nil {
				return err
			}
			continue
		}
		content, isError := toolResultContent(result, err)
		if err := s.sendProviderToolResult(ctx, call.ID, call.Name, content, isError); err != nil {
			return err
		}
		if err := s.stream.Write(ctx, newEvent(s.info.ID, EventToolResult, ToolResultPayload{ID: call.ID, Name: call.Name, Content: content, IsError: isError})); err != nil {
			return err
		}
	}
	return nil
}

func (s *streamState) handleCoreEvent(ctx context.Context, event core.Event) error {
	switch event.Type {
	case core.EventApprovalResult:
		var payload core.PermissionNotification
		if !payloadAs(event.Payload, &payload) {
			return nil
		}
		pending, ok := s.pending[payload.ToolCallID]
		if !ok {
			return nil
		}
		if err := s.stream.Write(ctx, newEvent(s.info.ID, EventApprovalResolved, ApprovalResolvedPayload{
			ID:         payload.ApprovalID,
			ToolCallID: payload.ToolCallID,
			Granted:    payload.Granted,
			Denied:     payload.Denied,
		})); err != nil {
			return err
		}
		if payload.Denied {
			delete(s.pending, payload.ToolCallID)
			if err := s.sendProviderToolResult(ctx, pending.ID, pending.Name, "approval denied", true); err != nil {
				return err
			}
		}
	case core.EventMessageCreated:
		var message core.Message
		if !payloadAs(event.Payload, &message) || message.Role != core.MessageRoleTool {
			return nil
		}
		for _, part := range message.Parts {
			if part.ToolResult == nil {
				continue
			}
			pending, ok := s.pending[part.ToolResult.ToolCallID]
			if !ok {
				continue
			}
			delete(s.pending, part.ToolResult.ToolCallID)
			content := strings.TrimSpace(part.ToolResult.Content)
			if content == "" {
				content = strings.TrimSpace(message.Content)
			}
			if err := s.sendProviderToolResult(ctx, pending.ID, pending.Name, content, part.ToolResult.IsError); err != nil {
				return err
			}
			if err := s.stream.Write(ctx, newEvent(s.info.ID, EventToolResult, ToolResultPayload{
				ID:      pending.ID,
				Name:    pending.Name,
				Content: content,
				IsError: part.ToolResult.IsError,
			})); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *streamState) finishTurn(ctx context.Context) error {
	input := strings.TrimSpace(s.inputTranscript.String())
	assistant := strings.TrimSpace(s.assistantTranscript.String())
	if err := s.stream.Write(ctx, newEvent(s.info.ID, EventInputTranscriptFinal, TranscriptPayload{Text: input, Final: true})); err != nil {
		return err
	}
	if err := s.stream.Write(ctx, newEvent(s.info.ID, EventAssistantTranscriptFinal, TranscriptPayload{Text: assistant, Final: true})); err != nil {
		return err
	}
	if err := s.stream.Write(ctx, newEvent(s.info.ID, EventTurnFinal, TurnFinalPayload{
		InputTranscript:     input,
		AssistantTranscript: assistant,
	})); err != nil {
		return err
	}
	if s.info.PersistMode != PersistModeNone && (input != "" || assistant != "") {
		if _, err := s.manager.core.CommitRealtimeVoiceTurn(ctx, core.CommitRealtimeVoiceTurnInput{
			SessionID:           s.info.CoreSessionID,
			ProviderID:          s.info.ProviderID,
			ModelID:             s.info.ModelID,
			UserTranscript:      input,
			AssistantTranscript: assistant,
		}); err != nil {
			return err
		}
	}
	s.inputTranscript.Reset()
	s.assistantTranscript.Reset()
	return nil
}

func (s *streamState) sendProviderToolResult(ctx context.Context, id string, name string, content string, isError bool) error {
	response := map[string]any{
		"content":  strings.TrimSpace(content),
		"is_error": isError,
	}
	if response["content"] == "" {
		response["content"] = "ok"
	}
	return s.provider.Send(ctx, ProviderInput{
		Type: ProviderInputToolResult,
		ToolResponses: []ProviderToolResponse{{
			ID:       id,
			Name:     name,
			Response: response,
		}},
	})
}

func readClientEvents(ctx context.Context, stream Stream, out chan<- streamReadResult) {
	for {
		event, err := stream.Read(ctx)
		select {
		case out <- streamReadResult{event: event, err: err}:
		case <-ctx.Done():
		}
		if err != nil || ctx.Err() != nil {
			return
		}
	}
}

func readProviderEvents(ctx context.Context, provider ProviderConnection, out chan<- providerReadResult) {
	for {
		event, err := provider.Receive(ctx)
		select {
		case out <- providerReadResult{output: event, err: err}:
		case <-ctx.Done():
		}
		if err != nil || ctx.Err() != nil {
			return
		}
	}
}
