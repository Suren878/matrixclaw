package store

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestClientCapabilitiesRoundTrip(t *testing.T) {
	for _, capabilities := range []core.ClientCapabilities{
		{},
		{ReceivesDeliveries: true},
		{SupportsVoiceDelivery: true, SupportsDocumentDelivery: true, ReceivesDeliveries: true},
		{SupportsVoiceDelivery: true},
	} {
		if got := unmarshalClientCapabilities(marshalClientCapabilities(capabilities)); got != capabilities {
			t.Fatalf("round trip of %+v = %+v", capabilities, got)
		}
	}
	legacy := unmarshalClientCapabilities(`{"supports_voice_delivery":true,"supports_document_delivery":true}`)
	if !legacy.ReceivesDeliveries {
		t.Fatalf("a Telegram row from before receives_deliveries = %+v", legacy)
	}
}
