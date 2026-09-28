//go:build !linux && !darwin

package shelltask

import "errors"

// Processes cannot be told apart here, so leftovers are never killed.

func processStart(int) (string, error) {
	return "", errors.New("shelltask: process start times are not supported on this system")
}

func bootID() (string, error) {
	return "", errors.New("shelltask: boot IDs are not supported on this system")
}
