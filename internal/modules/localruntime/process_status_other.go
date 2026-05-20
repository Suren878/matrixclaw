//go:build !linux && !darwin

package localruntime

import "github.com/Suren878/matrixclaw/internal/setup"

func voiceRuntimeRSSBytes(setup.VoiceProviderOption) uint64 {
	return 0
}
