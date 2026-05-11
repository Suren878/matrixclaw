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
		{"Europe/Moscow", "Moscow"},
		{"Europe/Berlin", "Berlin"},
		{"UTC", "UTC"},
		{"Europe/London", "London"},
		{"Asia/Dubai", "Dubai"},
		{"Asia/Yerevan", "Yerevan"},
		{"Asia/Tbilisi", "Tbilisi"},
		{"Asia/Almaty", "Almaty"},
		{"Asia/Tashkent", "Tashkent"},
		{"Asia/Baku", "Baku"},
		{"Asia/Jerusalem", "Jerusalem"},
		{"Asia/Istanbul", "Istanbul"},
		{"Asia/Bangkok", "Bangkok"},
		{"Asia/Shanghai", "Shanghai"},
		{"Asia/Tokyo", "Tokyo"},
		{"America/New_York", "New York"},
		{"America/Chicago", "Chicago"},
		{"America/Denver", "Denver"},
		{"America/Los_Angeles", "Los Angeles"},
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
