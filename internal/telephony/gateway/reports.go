package gateway

import (
	"context"
	"log"
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
	snapshot := callSnapshot(call)
	payload := map[string]any{
		"client":              firstNonEmpty(req.OriginClient, snapshot.OriginClient),
		"external_key":        firstNonEmpty(req.OriginExternalKey, snapshot.OriginExternalKey),
		"session_id":          firstNonEmpty(req.OriginSessionID, snapshot.OriginSessionID, snapshot.CoreSessionID),
		"text":                text,
		"busy_mode":           "queue",
		"allow_auto_bind_one": true,
	}
	if err := s.api.postJSON(ctx, "/v1/messages", payload, nil); err != nil {
		log.Printf("telephony call %s post-call report delivery failed: %v", callID(call), err)
		return
	}
	log.Printf("telephony call %s post-call report queued", callID(call))
}

func shouldPostCallReport(call *Call, req createCallRequest) bool {
	snapshot := callSnapshot(call)
	if strings.TrimSpace(snapshot.ID) == "" || !strings.EqualFold(strings.TrimSpace(snapshot.Direction), "outbound") {
		return false
	}
	if req.PostCallReport != nil && !*req.PostCallReport {
		return false
	}
	if firstNonEmpty(req.OriginClient, snapshot.OriginClient) == "" {
		return false
	}
	if firstNonEmpty(req.OriginExternalKey, snapshot.OriginExternalKey) == "" {
		return false
	}
	return true
}

func postCallReportPrompt(call *Call, req createCallRequest) string {
	if call == nil {
		return ""
	}
	turns := callTranscriptSnapshot(call)
	snapshot := callSnapshot(call)
	var sections []string
	sections = append(sections, strings.TrimSpace(`Phone call completed. Write a concise report to the user in the user's language. Do not place another phone call. Explain whether the phone objective succeeded, list the important facts, mention any errors or unanswered items, and include a readable transcript of the conversation.`))
	sections = append(sections, "Call status:\n"+formatCallStatus(call))
	if objective := firstNonEmpty(snapshot.Objective, req.Objective, req.SystemInstruction); objective != "" {
		sections = append(sections, "Original phone objective:\n"+objective)
	}
	if transcript := formatTranscript(turns); transcript != "" {
		sections = append(sections, "Transcript:\n"+transcript)
	} else {
		sections = append(sections, "Transcript:\nNo transcript was captured.")
	}
	if recording := snapshot.Recording; recording != nil {
		sections = append(sections, "Recording:\n"+formatCallRecording(*recording))
	}
	return strings.Join(sections, "\n\n")
}

func formatCallStatus(call *Call) string {
	snapshot := callSnapshot(call)
	var lines []string
	lines = append(lines, "id: "+strings.TrimSpace(snapshot.ID))
	lines = append(lines, "status: "+strings.TrimSpace(snapshot.Status))
	if snapshot.To != "" {
		lines = append(lines, "to: "+strings.TrimSpace(snapshot.To))
	}
	if snapshot.From != "" {
		lines = append(lines, "from: "+strings.TrimSpace(snapshot.From))
	}
	if snapshot.AnsweredAt != nil {
		lines = append(lines, "answered_at: "+snapshot.AnsweredAt.Format(time.RFC3339))
	}
	if snapshot.FinishedAt != nil {
		lines = append(lines, "finished_at: "+snapshot.FinishedAt.Format(time.RFC3339))
	}
	if snapshot.Error != "" {
		lines = append(lines, "error: "+strings.TrimSpace(snapshot.Error))
	}
	return strings.Join(lines, "\n")
}
