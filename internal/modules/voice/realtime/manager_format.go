package realtime

import (
	"fmt"
	"strings"
)

func DefaultInputAudioFormat() AudioFormat {
	return AudioFormat{Encoding: "pcm_s16le", SampleRateHz: 16000, Channels: 1}
}

func DefaultOutputAudioFormat() AudioFormat {
	return AudioFormat{Encoding: "pcm_s16le", SampleRateHz: 24000, Channels: 1}
}

func normalizeAudioFormat(format AudioFormat, fallback AudioFormat) AudioFormat {
	format.Encoding = strings.ToLower(strings.TrimSpace(format.Encoding))
	if format.Encoding == "" {
		format.Encoding = fallback.Encoding
	}
	if format.SampleRateHz <= 0 {
		format.SampleRateHz = fallback.SampleRateHz
	}
	if format.Channels <= 0 {
		format.Channels = fallback.Channels
	}
	return format
}

func validateAudioFormat(format AudioFormat, expected AudioFormat, field string) error {
	if format != expected {
		return fmt.Errorf("%w: %s must be %s %dHz %dch", ErrInvalidRequest, field, expected.Encoding, expected.SampleRateHz, expected.Channels)
	}
	return nil
}

func audioMIMEType(format AudioFormat) string {
	if strings.EqualFold(format.Encoding, "pcm_s16le") {
		return fmt.Sprintf("audio/pcm;rate=%d", format.SampleRateHz)
	}
	return "application/octet-stream"
}

func normalizeConfig(cfg Config) Config {
	cfg.ProviderID = normalizeID(cfg.ProviderID)
	if cfg.ProviderID == "" {
		cfg.ProviderID = ProviderGemini
	}
	cfg.PersistMode = normalizePersistMode(cfg.PersistMode, PersistModeTurnsAndSummary)
	return cfg
}

func normalizePersistMode(value PersistMode, fallback PersistMode) PersistMode {
	switch PersistMode(strings.ToLower(strings.TrimSpace(string(value)))) {
	case PersistModeNone:
		return PersistModeNone
	case PersistModeTurnsAndSummary:
		return PersistModeTurnsAndSummary
	default:
		if fallback == "" {
			return PersistModeTurnsAndSummary
		}
		return fallback
	}
}

func maxSessions(value int) int {
	if value < 0 {
		return 0
	}
	if value == 0 {
		return 8
	}
	return value
}

func providerDescriptorByID(providers []ProviderDescriptor, id string) ProviderDescriptor {
	id = normalizeID(id)
	for _, provider := range providers {
		if normalizeID(provider.ID) == id {
			return provider
		}
	}
	if len(providers) > 0 {
		return providers[0]
	}
	return ProviderDescriptor{}
}
