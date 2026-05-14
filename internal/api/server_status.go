package api

import (
	"net/http"
	"runtime"
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
	total, available := platformMemInfo()
	cpuUsed, cpuKnown := s.cpuUsedPercent()
	return core.ServerStatus{
		StartedAt:            startedAt,
		UptimeSeconds:        int64(time.Since(startedAt).Seconds()),
		GoAllocBytes:         mem.Alloc,
		GoSysBytes:           mem.Sys,
		ProcessRSSBytes:      platformRSS(),
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
	current, ok := platformCPUSnapshot()
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
