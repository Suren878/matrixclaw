//go:build !linux && !darwin

package procsup

import "os/exec"

// Prepare is a no-op on this platform.
func Prepare(*exec.Cmd) {}

func rssBytes(int) uint64 { return 0 }
