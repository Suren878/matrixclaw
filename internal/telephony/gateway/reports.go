package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

func (s *Server) postCallReport(parent context.Context, call *Call, req createCallRequest) {
	if !shouldPostCallReport(call, req) {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	text := postCallReportPrompt(call, req)
	if strings.TrimSpace(text) == "" {
		return
	}
	payload := map[string]any{
		"client":              firstNonEmpty(req.OriginClient, call.OriginClient),
		"external_key":        firstNonEmpty(req.OriginExternalKey, call.OriginExternalKey),
		"session_id":          firstNonEmpty(req.OriginSessionID, call.OriginSessionID, call.CoreSessionID),
		"text":                text,
		"busy_mode":           "queue",
		"allow_auto_bind_one": true,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("telephony call %s post-call report encode failed: %v", callID(call), err)
		return
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.MatrixclawURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		log.Printf("telephony call %s post-call report request failed: %v", callID(call), err)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if s.cfg.MatrixclawToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+s.cfg.MatrixclawToken)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(httpReq)
	if err != nil {
		log.Printf("telephony call %s post-call report delivery failed: %v", callID(call), err)
		return
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		log.Printf("telephony call %s post-call report returned HTTP %d", callID(call), res.StatusCode)
		return
	}
	log.Printf("telephony call %s post-call report queued", callID(call))
}

func shouldPostCallReport(call *Call, req createCallRequest) bool {
	if call == nil || !strings.EqualFold(strings.TrimSpace(call.Direction), "outbound") {
		return false
	}
	if req.PostCallReport != nil && !*req.PostCallReport {
		return false
	}
	if firstNonEmpty(req.OriginClient, call.OriginClient) == "" {
		return false
	}
	if firstNonEmpty(req.OriginExternalKey, call.OriginExternalKey) == "" {
		return false
	}
	return true
}

func postCallReportPrompt(call *Call, req createCallRequest) string {
	if call == nil {
		return ""
	}
	turns := callTranscriptSnapshot(call)
	var sections []string
	sections = append(sections, strings.TrimSpace(`Phone call completed. Write a concise report to the user in the user's language. Do not place another phone call. Explain whether the phone objective succeeded, list the important facts, mention any errors or unanswered items, and include a readable transcript of the conversation.`))
	sections = append(sections, "Call status:\n"+formatCallStatus(call))
	if objective := firstNonEmpty(call.Objective, req.Objective, req.SystemInstruction); objective != "" {
		sections = append(sections, "Original phone objective:\n"+objective)
	}
	if transcript := formatTranscript(turns); transcript != "" {
		sections = append(sections, "Transcript:\n"+transcript)
	} else {
		sections = append(sections, "Transcript:\nNo transcript was captured.")
	}
	if recording := callRecordingSnapshot(call); recording != nil {
		sections = append(sections, "Recording:\n"+formatCallRecording(*recording))
	}
	return strings.Join(sections, "\n\n")
}

func formatCallStatus(call *Call) string {
	var lines []string
	lines = append(lines, "id: "+strings.TrimSpace(call.ID))
	lines = append(lines, "status: "+strings.TrimSpace(call.Status))
	if call.To != "" {
		lines = append(lines, "to: "+strings.TrimSpace(call.To))
	}
	if call.From != "" {
		lines = append(lines, "from: "+strings.TrimSpace(call.From))
	}
	if call.AnsweredAt != nil {
		lines = append(lines, "answered_at: "+call.AnsweredAt.Format(time.RFC3339))
	}
	if call.FinishedAt != nil {
		lines = append(lines, "finished_at: "+call.FinishedAt.Format(time.RFC3339))
	}
	if call.Error != "" {
		lines = append(lines, "error: "+strings.TrimSpace(call.Error))
	}
	return strings.Join(lines, "\n")
}
