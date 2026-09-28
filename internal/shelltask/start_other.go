//go:build !linux && !darwin

package shelltask

import "errors"

// processStart cannot tell processes apart here, so leftovers are never killed.
func processStart(int) (string, error) {
	return "", errors.New("shelltask: process start times are not supported on this system")
}
