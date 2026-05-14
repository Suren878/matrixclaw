//go:build darwin

package api

import (
	"encoding/binary"
	"os"

	"golang.org/x/sys/unix"
)

func platformRSS() uint64 {
	pageSize := uint64(os.Getpagesize())
	if proc, err := unix.SysctlKinfoProc("kern.proc.pid", os.Getpid()); err == nil {
		if proc.Eproc.Xrssize > 0 {
			return uint64(proc.Eproc.Xrssize) * pageSize
		}
	}
	var usage unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &usage); err == nil && usage.Maxrss > 0 {
		return uint64(usage.Maxrss)
	}
	return 0
}

func platformMemInfo() (uint64, uint64) {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil || total == 0 {
		return 0, 0
	}
	pageSize := uint64(os.Getpagesize())
	free := darwinPageCount("vm.page_free_count")
	inactive := darwinPageCount("vm.page_inactive_count")
	speculative := darwinPageCount("vm.page_speculative_count")
	available := (free + inactive + speculative) * pageSize
	if available > total {
		available = total
	}
	return total, available
}

func darwinPageCount(name string) uint64 {
	if value, err := unix.SysctlUint64(name); err == nil {
		return value
	}
	if value, err := unix.SysctlUint32(name); err == nil {
		return uint64(value)
	}
	return 0
}

func platformCPUSnapshot() (cpuSnapshot, bool) {
	raw, err := unix.SysctlRaw("kern.cp_time")
	if err != nil || len(raw) < 16 {
		return cpuSnapshot{}, false
	}
	values := darwinCPUValues(raw)
	if len(values) < 4 {
		return cpuSnapshot{}, false
	}
	var total uint64
	for _, value := range values {
		total += value
	}
	return cpuSnapshot{total: total, idle: values[3]}, true
}

func darwinCPUValues(raw []byte) []uint64 {
	switch {
	case len(raw)%8 == 0:
		values := make([]uint64, 0, len(raw)/8)
		for len(raw) >= 8 {
			values = append(values, binary.LittleEndian.Uint64(raw[:8]))
			raw = raw[8:]
		}
		return values
	case len(raw)%4 == 0:
		values := make([]uint64, 0, len(raw)/4)
		for len(raw) >= 4 {
			values = append(values, uint64(binary.LittleEndian.Uint32(raw[:4])))
			raw = raw[4:]
		}
		return values
	default:
		return nil
	}
}
