package providers

import (
	"bufio"
	"context"
	"io"
	"strings"
)

type SSEEvent struct {
	Type string
	Data string
}

// ScanSSE reads SSE frames from body and invokes handle for each event payload.
func ScanSSE(ctx context.Context, body io.Reader, handle func(SSEEvent) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var eventType string
	var data strings.Builder

	flush := func() error {
		payload := strings.TrimSpace(data.String())
		event := SSEEvent{
			Type: eventType,
			Data: payload,
		}
		eventType = ""
		data.Reset()
		if payload == "" {
			return nil
		}
		return handle(event)
	}

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}
