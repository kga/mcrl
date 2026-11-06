// Package timeexpr parses the time and duration expressions accepted by
// --since, --until, --from, --to, --start and --duration.
package timeexpr

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseDuration accepts Go-style durations extended with days and weeks:
// "90s", "30m", "2h", "1d12h", "1w".
func ParseDuration(s string) (time.Duration, error) {
	orig := s
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty duration")
	}
	var total time.Duration
	for s != "" {
		i := 0
		for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
			i++
		}
		if i == 0 {
			return 0, fmt.Errorf("invalid duration %q", orig)
		}
		n, err := strconv.ParseFloat(s[:i], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", orig)
		}
		s = s[i:]
		j := 0
		for j < len(s) && (s[j] < '0' || s[j] > '9') && s[j] != '.' {
			j++
		}
		var unit time.Duration
		switch s[:j] {
		case "s":
			unit = time.Second
		case "m":
			unit = time.Minute
		case "h":
			unit = time.Hour
		case "d":
			unit = 24 * time.Hour
		case "w":
			unit = 7 * 24 * time.Hour
		case "ms":
			unit = time.Millisecond
		default:
			return 0, fmt.Errorf("invalid duration %q (units: s, m, h, d, w)", orig)
		}
		total += time.Duration(n * float64(unit))
		s = s[j:]
	}
	return total, nil
}

// localLayouts are interpreted in the given location.
var localLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04",
	"2006-01-02",
}

// Parse interprets a time expression relative to now:
//
//	now            the current time
//	1h, 30m, 7d    that long ago (for --since)
//	-1h, +30m      relative to now
//	@1762390800    epoch seconds
//	1762390800     epoch seconds (9 digits or more)
//	2026-11-06T10:00:00+09:00   RFC 3339
//	2026-11-06 10:00, 2026-11-06, 10:00   in loc (a bare clock time means today)
func Parse(s string, now time.Time, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errors.New("empty time")
	}
	if loc == nil {
		loc = time.Local
	}
	switch {
	case s == "now":
		return now, nil
	case strings.HasPrefix(s, "@"):
		return parseEpoch(s[1:], s)
	case s[0] == '-' || s[0] == '+':
		d, err := ParseDuration(s[1:])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid time %q: %w", s, err)
		}
		if s[0] == '-' {
			d = -d
		}
		return now.Add(d), nil
	}
	if isDigits(s) && len(s) >= 9 {
		return parseEpoch(s, s)
	}
	if d, err := ParseDuration(s); err == nil {
		return now.Add(-d), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	for _, layout := range localLayouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	for _, layout := range []string{"15:04:05", "15:04"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			n := now.In(loc)
			return time.Date(n.Year(), n.Month(), n.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time %q (examples: now, 1h, -30m, 2026-11-06T10:00:00+09:00, \"2026-11-06 10:00\", @1762390800)", s)
}

func parseEpoch(digits, orig string) (time.Time, error) {
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid epoch time %q", orig)
	}
	return time.Unix(n, 0), nil
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}
