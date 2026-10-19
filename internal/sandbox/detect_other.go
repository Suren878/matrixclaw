//go:build !linux && !darwin

package sandbox

func detect() Status {
	return Status{Detail: "commands cannot be confined on this system"}
}
