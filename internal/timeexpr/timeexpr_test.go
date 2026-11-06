package timeexpr

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"90s":   90 * time.Second,
		"30m":   30 * time.Minute,
		"1d12h": 36 * time.Hour,
		"1w":    7 * 24 * time.Hour,
		"1.5h":  90 * time.Minute,
	} {
		got, err := ParseDuration(in)
		if err != nil || got != want {
			t.Errorf("%q: got %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "h", "10", "1y", "1h-"} {
		if _, err := ParseDuration(bad); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}

func TestParse(t *testing.T) {
	jst := time.FixedZone("JST", 9*3600)
	now := time.Date(2026, 11, 6, 22, 30, 0, 0, jst)
	tests := map[string]time.Time{
		"now":                       now,
		"1h":                        now.Add(-time.Hour),
		"-30m":                      now.Add(-30 * time.Minute),
		"+2h":                       now.Add(2 * time.Hour),
		"@1762390800":               time.Unix(1762390800, 0),
		"1762390800":                time.Unix(1762390800, 0),
		"2026-11-06T10:00:00Z":      time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC),
		"2026-11-06T10:00:00+09:00": time.Date(2026, 11, 6, 1, 0, 0, 0, time.UTC),
		"2026-11-06 10:00":          time.Date(2026, 11, 6, 10, 0, 0, 0, jst),
		"2026-11-06":                time.Date(2026, 11, 6, 0, 0, 0, 0, jst),
		"10:00":                     time.Date(2026, 11, 6, 10, 0, 0, 0, jst),
	}
	for in, want := range tests {
		got, err := Parse(in, now, jst)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if !got.Equal(want) {
			t.Errorf("%q: got %v, want %v", in, got, want)
		}
	}
	for _, bad := range []string{"", "yesterday", "2026-13-01", "@x"} {
		if _, err := Parse(bad, now, jst); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}
