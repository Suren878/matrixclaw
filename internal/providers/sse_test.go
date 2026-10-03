package providers

import (
	"context"
	"strings"
	"testing"
)

func TestScanSSEReadsEventsOverAMegabyte(t *testing.T) {
	payload := `{"text":"` + strings.Repeat("x", 3<<20) + `"}`
	var got string
	err := ScanSSE(context.Background(), strings.NewReader("data: "+payload+"\n\n"), func(event SSEEvent) error {
		got = event.Data
		return nil
	})
	if err != nil || got != payload {
		t.Fatalf("err = %v, read %d of %d bytes", err, len(got), len(payload))
	}
}
