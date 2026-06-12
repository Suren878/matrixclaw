package core

import "time"

type ServerStatus struct {
	StartedAt            time.Time `json:"started_at"`
	UptimeSeconds        int64     `json:"uptime_seconds"`
	GoAllocBytes         uint64    `json:"go_alloc_bytes"`
	GoSysBytes           uint64    `json:"go_sys_bytes"`
	ProcessRSSBytes      uint64    `json:"process_rss_bytes"`
	MemoryTotalBytes     uint64    `json:"memory_total_bytes"`
	MemoryAvailableBytes uint64    `json:"memory_available_bytes"`
	MemoryUsedBytes      uint64    `json:"memory_used_bytes"`
	CPUUsedPercent       float64   `json:"cpu_used_percent"`
	CPUKnown             bool      `json:"cpu_known"`
}
