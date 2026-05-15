package daemonclient

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

type LiveEvent struct {
	ID        uint64          `json:"id,omitempty"`
	Type      core.EventType  `json:"type"`
	SessionID string          `json:"session_id"`
	RunID     string          `json:"run_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	At        time.Time       `json:"at,omitempty"`
}

func (e LiveEvent) DecodeMessage() (core.Message, error) {
	var message core.Message
	err := json.Unmarshal(e.Payload, &message)
	return message, err
}

func (e LiveEvent) DecodeRun() (core.Run, error) {
	var run core.Run
	err := json.Unmarshal(e.Payload, &run)
	return run, err
}

func (e LiveEvent) DecodeSessionPlan() (core.SessionPlan, error) {
	var plan core.SessionPlan
	err := json.Unmarshal(e.Payload, &plan)
	return plan, err
}

func (e LiveEvent) DecodeApproval() (core.Approval, error) {
	var approval core.Approval
	err := json.Unmarshal(e.Payload, &approval)
	return approval, err
}

func (e LiveEvent) DecodePermissionRequest() (core.PermissionRequest, error) {
	var request core.PermissionRequest
	err := json.Unmarshal(e.Payload, &request)
	return request, err
}

func (e LiveEvent) DecodePermissionNotification() (core.PermissionNotification, error) {
	var notification core.PermissionNotification
	err := json.Unmarshal(e.Payload, &notification)
	return notification, err
}

func (e LiveEvent) DecodeFileSnapshot() (core.FileSnapshot, error) {
	var fileSnapshot core.FileSnapshot
	err := json.Unmarshal(e.Payload, &fileSnapshot)
	return fileSnapshot, err
}

func (e LiveEvent) DecodeToolUpdate() (core.ToolUpdate, error) {
	var update core.ToolUpdate
	err := json.Unmarshal(e.Payload, &update)
	return update, err
}

func (c *Client) SubscribeEvents(ctx context.Context, sessionID string, afterID uint64) (<-chan LiveEvent, <-chan error, error) {
	httpClient := c.EventHTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	path := c.BaseURL + "/v1/events?session_id=" + url.QueryEscape(sessionID) + "&after=" + strconv.FormatUint(afterID, 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	c.authorize(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, nil, decodeAPIError(resp)
	}

	events := make(chan LiveEvent, 16)
	errs := make(chan error, 1)
	go readSSE(ctx, resp.Body, events, errs)
	return events, errs, nil
}

func readSSE(ctx context.Context, body io.ReadCloser, events chan<- LiveEvent, errs chan<- error) {
	defer close(events)
	defer close(errs)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var eventType string
	var eventID uint64
	var data strings.Builder
	flush := func() {
		payload := strings.TrimSpace(data.String())
		if payload == "" || eventType == "ready" {
			eventType = ""
			eventID = 0
			data.Reset()
			return
		}
		var event LiveEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			select {
			case errs <- err:
			default:
			}
			eventType = ""
			eventID = 0
			data.Reset()
			return
		}
		if event.ID == 0 {
			event.ID = eventID
		}
		select {
		case <-ctx.Done():
			return
		case events <- event:
		}
		eventType = ""
		eventID = 0
		data.Reset()
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		switch {
		case strings.HasPrefix(line, "id:"):
			value := strings.TrimSpace(strings.TrimPrefix(line, "id:"))
			if value == "" {
				eventID = 0
				continue
			}
			parsed, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				select {
				case errs <- err:
				default:
				}
				continue
			}
			eventID = parsed
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		select {
		case errs <- err:
		default:
		}
	}
}
