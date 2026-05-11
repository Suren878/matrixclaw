package api

import (
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleServerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, core.ServerStatusResponse{Status: s.collectServerStatus()})
}

func (s *Server) collectServerStatus() core.ServerStatus {
	startedAt := s.runtimeStartedAt()
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	total, available := procMemInfo()
	cpuUsed, cpuKnown := s.cpuUsedPercent()
	return core.ServerStatus{
		StartedAt:            startedAt,
		UptimeSeconds:        int64(time.Since(startedAt).Seconds()),
		GoAllocBytes:         mem.Alloc,
		GoSysBytes:           mem.Sys,
		ProcessRSSBytes:      procRSS(),
		MemoryTotalBytes:     total,
		MemoryAvailableBytes: available,
		MemoryUsedBytes:      usedMemory(total, available),
		CPUUsedPercent:       cpuUsed,
		CPUKnown:             cpuKnown,
	}
}

func (s *Server) runtimeStartedAt() time.Time {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	return s.startedAt
}

func (s *Server) markRuntimeReloaded() {
	s.statusMu.Lock()
	s.startedAt = time.Now().UTC()
	s.statusMu.Unlock()

	s.cpuMu.Lock()
	s.lastCPU = cpuSnapshot{}
	s.hasLastCPU = false
	s.cpuMu.Unlock()
}

func procRSS() uint64 {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return pages * uint64(os.Getpagesize())
}

func procMemInfo() (uint64, uint64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	var total uint64
	var available uint64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			total = value * 1024
		case "MemAvailable":
			available = value * 1024
		}
	}
	return total, available
}

func usedMemory(total uint64, available uint64) uint64 {
	if total <= available {
		return 0
	}
	return total - available
}

type cpuSnapshot struct {
	total uint64
	idle  uint64
}

func (s *Server) cpuUsedPercent() (float64, bool) {
	current, ok := procCPUSnapshot()
	if !ok {
		return 0, false
	}

	s.cpuMu.Lock()
	defer s.cpuMu.Unlock()

	if !s.hasLastCPU {
		s.lastCPU = current
		s.hasLastCPU = true
		return 0, false
	}

	previous := s.lastCPU
	s.lastCPU = current
	if current.total < previous.total || current.idle < previous.idle {
		return 0, false
	}

	totalDelta := current.total - previous.total
	idleDelta := current.idle - previous.idle
	if totalDelta == 0 || idleDelta > totalDelta {
		return 0, false
	}
	return float64(totalDelta-idleDelta) * 100 / float64(totalDelta), true
}

func procCPUSnapshot() (cpuSnapshot, bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuSnapshot{}, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var values []uint64
		for _, field := range fields[1:] {
			value, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return cpuSnapshot{}, false
			}
			values = append(values, value)
		}
		var total uint64
		for _, value := range values {
			total += value
		}
		idle := values[3]
		if len(values) > 4 {
			idle += values[4]
		}
		return cpuSnapshot{total: total, idle: idle}, true
	}
	return cpuSnapshot{}, false
}
