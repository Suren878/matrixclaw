// Package ids makes the random record IDs used across MatrixClaw.
package ids

import (
	"crypto/rand"
	"encoding/hex"
)

// New returns prefix, an underscore and 16 random hex digits.
func New(prefix string) string {
	var buf [8]byte
	_, _ = rand.Read(buf[:]) // crypto/rand.Read never fails
	return prefix + "_" + hex.EncodeToString(buf[:])
}
