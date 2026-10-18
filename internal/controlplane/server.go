package controlplane

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

var (
	serverRoot     = verb(CommandServer, "", 0)
	statusRoot     = verb(CommandStatus, "", 0)
	restartRoot    = verb(CommandRestart, "", 0)
	restartConfirm = verb(CommandRestart, "confirm", 0)
	stopRoot       = verb(CommandStop, "", 0)
	stopConfirm    = verb(CommandStop, "confirm", 0)
)

// RestartRequested reports whether command is the confirmed daemon restart,
// which clients run themselves to show its progress and the notice after it.
func RestartRequested(command string) bool {
	route, ok := DecodeRoute(command)
	return ok && route.Is(restartConfirm)
}

func (d *Dispatcher) handleServer() Result {
	picker := NewPickerData(PickerServer, "Server").
		Row("status", "Status", "Uptime, CPU, memory", statusRoot.command())
	if d.owner() {
		picker.Row("restart", "Restart", "", restartRoot.command()).
			Row("stop", "Stop", "", stopRoot.command())
	}
	return Result{Screen: picker.Build()}
}

func (d *Dispatcher) handleStatus(ctx context.Context) (Result, error) {
	status, err := d.daemon.ServerStatus(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{Screen: InfoData{Title: "Server Status", Rows: FormatServerStatusRows(status)}}, nil
}

// handleRestart asks to confirm; clients run the confirmed restart themselves.
func (d *Dispatcher) handleRestart() Result {
	if !d.owner() {
		return textResult("Only the owner can restart the daemon.")
	}
	return Result{Screen: ConfirmData{
		Message:        "Restart server daemon?",
		ConfirmLabel:   "Restart",
		CancelLabel:    "Close",
		ConfirmCommand: restartConfirm.command(),
		CancelCommand:  serverRoot.command(),
		ConfirmDanger:  true,
	}}
}

func (d *Dispatcher) handleStop(ctx context.Context, r Route) (Result, error) {
	if !d.owner() {
		return textResult("Only the owner can stop the daemon."), nil
	}
	if !r.Is(stopConfirm) {
		return Result{Screen: ConfirmData{
			Message:        "Stop server daemon?",
			ConfirmLabel:   "Stop",
			CancelLabel:    "Close",
			ConfirmCommand: stopConfirm.command(),
			CancelCommand:  serverRoot.command(),
			ConfirmDanger:  true,
		}}, nil
	}
	if err := d.daemon.StopDaemon(ctx); err != nil {
		return Result{}, err
	}
	return textResult("Server daemon stop requested."), nil
}

func FormatServerStatus(status core.ServerStatus) string {
	rows := FormatServerStatusRows(status)
	labelWidth := 0
	for _, row := range rows {
		if width := len(row.Label); width > labelWidth {
			labelWidth = width
		}
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, fmt.Sprintf("%-*s %s", labelWidth, row.Label, row.Value))
	}
	return strings.Join(lines, "\n")
}

func FormatServerStatusRows(status core.ServerStatus) []InfoRow {
	rows := []InfoRow{
		{
			Label: "Daemon",
			Value: formatBytes(status.ProcessRSSBytes) + "  " + formatDuration(time.Duration(status.UptimeSeconds)*time.Second),
		},
	}
	if status.MemoryTotalBytes > 0 {
		rows = append(rows, InfoRow{Label: "Sys RAM", Value: formatBytes(status.MemoryUsedBytes) + " / " + formatBytes(status.MemoryAvailableBytes)})
	}
	rows = append(rows, InfoRow{Label: "CPU", Value: formatCPUUsedFree(status)})
	return rows
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	days := d / (24 * time.Hour)
	d -= days * 24 * time.Hour
	hours := d / time.Hour
	d -= hours * time.Hour
	minutes := d / time.Minute
	d -= minutes * time.Minute
	seconds := d / time.Second
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm %ds", days, hours, minutes, seconds)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value := float64(bytes)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/unit)
}

func formatCPUUsedFree(status core.ServerStatus) string {
	if !status.CPUKnown {
		return "0% / 100%"
	}
	used := status.CPUUsedPercent
	if used < 0 {
		used = 0
	}
	if used > 100 {
		used = 100
	}
	return fmt.Sprintf("%.0f%% / %.0f%%", used, 100-used)
}
