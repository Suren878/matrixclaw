package gateway

import (
	"fmt"
	"log"
	"strings"
)

func logCallTimeline(call *Call, sessionID string, event string, fields ...any) {
	id := ""
	if call != nil {
		id = callID(call)
		if sessionID == "" {
			sessionID = callRealtimeSessionID(call)
		}
	}
	logTelephonyTimeline(event, id, sessionID, fields...)
}

func logTelephonyTimeline(event string, callID string, sessionID string, fields ...any) {
	event = strings.TrimSpace(event)
	if event == "" {
		return
	}
	parts := []string{"telephony timeline", "event=" + logfmtValue(event)}
	if callID = strings.TrimSpace(callID); callID != "" {
		parts = append(parts, "call="+logfmtValue(callID))
	}
	if sessionID = strings.TrimSpace(sessionID); sessionID != "" {
		parts = append(parts, "session="+logfmtValue(sessionID))
	}
	for i := 0; i+1 < len(fields); i += 2 {
		key := strings.TrimSpace(fmt.Sprint(fields[i]))
		if key == "" {
			continue
		}
		parts = append(parts, key+"="+logfmtValue(fmt.Sprint(fields[i+1])))
	}
	log.Print(strings.Join(parts, " "))
}

func logfmtValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " \t\r\n\"=") {
		return fmt.Sprintf("%q", value)
	}
	return value
}
