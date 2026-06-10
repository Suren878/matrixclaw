package setup

import (
	"fmt"
	"time"
)

type TimezoneOption struct {
	ID     string
	Name   string
	Offset string
	Label  string
}

func TimezoneOptions(now time.Time) []TimezoneOption {
	if now.IsZero() {
		now = time.Now()
	}
	zones := []struct {
		id   string
		name string
	}{
		{"UTC", "UTC"},
		{"America/New_York", "New York"},
		{"America/Chicago", "Chicago"},
		{"America/Denver", "Denver"},
		{"America/Los_Angeles", "Los Angeles"},
		{"Europe/London", "London"},
		{"Europe/Berlin", "Berlin"},
		{"Europe/Moscow", "Moscow"},
		{"Asia/Dubai", "Dubai"},
		{"Asia/Istanbul", "Istanbul"},
		{"Asia/Jerusalem", "Jerusalem"},
		{"Asia/Baku", "Baku"},
		{"Asia/Yerevan", "Yerevan"},
		{"Asia/Tbilisi", "Tbilisi"},
		{"Asia/Almaty", "Almaty"},
		{"Asia/Tashkent", "Tashkent"},
		{"Asia/Bangkok", "Bangkok"},
		{"Asia/Shanghai", "Shanghai"},
		{"Asia/Tokyo", "Tokyo"},
	}
	options := make([]TimezoneOption, 0, len(zones))
	for _, zone := range zones {
		loc, err := time.LoadLocation(zone.id)
		if err != nil {
			continue
		}
		_, offsetSeconds := now.In(loc).Zone()
		offset := formatUTCOffset(offsetSeconds)
		options = append(options, TimezoneOption{
			ID:     zone.id,
			Name:   zone.name,
			Offset: offset,
			Label:  offset + " · " + zone.name,
		})
	}
	return options
}

func formatUTCOffset(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign = "-"
		seconds = -seconds
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	return fmt.Sprintf("UTC%s%02d:%02d", sign, hours, minutes)
}
