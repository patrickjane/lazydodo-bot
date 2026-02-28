package utils

import (
	"fmt"
	"time"
)

// Language represents the output language for duration formatting.
type Language int

const (
	English Language = iota
	German
)

// unit holds the singular and plural forms of a time unit in a given language.
type unit struct {
	singular string
	plural   string
}

var units = map[Language]map[string]unit{
	English: {
		"day":    {singular: "day", plural: "days"},
		"hour":   {singular: "hour", plural: "hours"},
		"minute": {singular: "minute", plural: "minutes"},
	},
	German: {
		"day":    {singular: "Tag", plural: "Tage"},
		"hour":   {singular: "Stunde", plural: "Stunden"},
		"minute": {singular: "Minute", plural: "Minuten"},
	},
}

// pluralize returns the correctly pluralized unit label for the given count.
func pluralize(count int, u unit) string {
	if count == 1 {
		return u.singular
	}
	return u.plural
}

// FormatDuration pretty-formats a time.Duration in the given language.
//
// Output format:
//   - d >= 1 day:  "XX days [YY hours]"    /  "XX Tage [YY Stunden]"
//   - d >= 1 hour: "XX hours [YY minutes]" / "XX Stunden [YY Minuten]"
//   - d <  1 hour: "XX minutes"            / "XX Minuten"
//
// The secondary unit (in brackets) is omitted when its value is zero.
func FormatDuration(d time.Duration, lang Language) string {
	// Work with absolute value so negative durations are handled gracefully.
	if d < 0 {
		return ""
	}

	u, ok := units[lang]
	if !ok {
		u = units[English]
	}

	totalMinutes := int(d.Minutes())
	totalHours := int(d.Hours())
	days := totalHours / 24
	hours := totalHours % 24
	minutes := totalMinutes % 60

	switch {
	case days >= 1:
		if hours == 0 {
			return fmt.Sprintf("%d %s", days, pluralize(days, u["day"]))
		}
		return fmt.Sprintf("%d %s %d %s",
			days, pluralize(days, u["day"]),
			hours, pluralize(hours, u["hour"]),
		)
	case totalHours >= 1:
		if minutes == 0 {
			return fmt.Sprintf("%d %s", totalHours, pluralize(totalHours, u["hour"]))
		}
		return fmt.Sprintf("%d %s %d %s",
			totalHours, pluralize(totalHours, u["hour"]),
			minutes, pluralize(minutes, u["minute"]),
		)
	default:
		return fmt.Sprintf("%d %s", totalMinutes, pluralize(totalMinutes, u["minute"]))
	}
}
