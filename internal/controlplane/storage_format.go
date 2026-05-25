package controlplane

import (
	"fmt"
	"strconv"
	"strings"

	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
)

func formatTempSettings(settings localstorage.TempSettings) string {
	return fmt.Sprintf("%s · %s",
		formatTempRetention(settings),
		formatStorageGB(settings.MaxBytes),
	)
}

func formatTempCleanupImpact(settings localstorage.TempSettings) string {
	if settings.TotalFiles == 0 {
		return "0 files"
	}
	return formatFileCountSize(settings.TotalFiles, settings.TotalBytes)
}

func formatStoredFilesInfo(files []localstorage.Entry) string {
	var total int64
	for _, file := range files {
		total += file.Size
	}
	return formatFileCountSize(len(files), total)
}

func formatFileCountSize(count int, bytes int64) string {
	if count == 0 {
		return "0 files"
	}
	label := "files"
	if count == 1 {
		label = "file"
	}
	return fmt.Sprintf("%d %s · %s", count, label, formatStorageSize(bytes))
}

func formatEnabled(enabled bool) string {
	if enabled {
		return "On"
	}
	return "Off"
}

func formatTempRetention(settings localstorage.TempSettings) string {
	days := settings.TTLSeconds / (24 * 3600)
	if days <= 0 {
		days = 1
	}
	return fmt.Sprintf("%d days", days)
}

func formatStorageGB(bytes int64) string {
	gb := float64(bytes) / (1024 * 1024 * 1024)
	text := strconv.FormatFloat(gb, 'f', 1, 64)
	text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	if text == "" {
		text = "0"
	}
	return text + " GB"
}

func formatStorageSize(bytes int64) string {
	if bytes >= 1024*1024*1024 {
		return formatStorageGB(bytes)
	}
	if bytes >= 1024*1024 {
		return fmt.Sprintf("%d MB", bytes/(1024*1024))
	}
	if bytes >= 1024 {
		return fmt.Sprintf("%d KB", bytes/1024)
	}
	return fmt.Sprintf("%d bytes", bytes)
}
