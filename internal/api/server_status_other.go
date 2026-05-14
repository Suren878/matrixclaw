//go:build !linux && !darwin

package api

func platformRSS() uint64 {
	return 0
}

func platformMemInfo() (uint64, uint64) {
	return 0, 0
}

func platformCPUSnapshot() (cpuSnapshot, bool) {
	return cpuSnapshot{}, false
}
