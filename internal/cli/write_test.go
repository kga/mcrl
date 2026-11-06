package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLineDiff(t *testing.T) {
	if lineDiff("a\nb\n", "a\nb\n") != "" {
		t.Error("equal texts should have no diff")
	}
	got := lineDiff("name: x\nwarning: 80\ncritical: 90\n", "name: x\nwarning: 85\ncritical: 90\nisMute: true\n")
	want := "--- current\n+++ edited\n  name: x\n- warning: 80\n+ warning: 85\n  critical: 90\n+ isMute: true\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestStripFieldsKeepsOrder(t *testing.T) {
	got, err := stripFields(json.RawMessage(`{"id":"x","z":1,"a":{"id":"inner"},"createdAt":2}`), []string{"id", "createdAt"})
	if err != nil || string(got) != `{"z":1,"a":{"id":"inner"}}` {
		t.Errorf("got %s %v", got, err)
	}
}

func TestValidateDashboard(t *testing.T) {
	widget := func(typ string, x, y, w, h int) map[string]any {
		return map[string]any{"type": typ, "title": typ, "layout": map[string]any{"x": float64(x), "y": float64(y), "width": float64(w), "height": float64(h)}}
	}
	ok := map[string]any{"widgets": []any{widget("graph", 0, 0, 12, 6), widget("value", 12, 0, 12, 6), widget("markdown", 0, 6, 24, 2)}}
	if err := validateDashboard(ok); err != nil {
		t.Errorf("valid layout: %v", err)
	}
	bad := map[string]any{"widgets": []any{widget("graph", 0, 0, 12, 6), widget("graph", 6, 3, 12, 6), widget("value", 20, 0, 6, 4), widget("graph", 0, 20, 4, 6)}}
	err := validateDashboard(bad)
	if err == nil {
		t.Fatal("invalid layout should fail")
	}
	for _, want := range []string{"widgets[1] (graph \"graph\") overlaps widgets[0]", "exceeds the 24 columns", "size 4x6 is outside 6x6..24x32"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
}

func TestDowntimeActive(t *testing.T) {
	loc := time.FixedZone("JST", 9*3600)
	// 2026-11-02 (月) 02:00 JST に始まる 60 分
	start := time.Date(2026, 11, 2, 2, 0, 0, 0, loc)
	dt := func(rec string) json.RawMessage {
		s := `{"start":` + itoa(start.Unix()) + `,"duration":60`
		if rec != "" {
			s += `,"recurrence":` + rec
		}
		return json.RawMessage(s + "}")
	}
	at := func(y int, m time.Month, d, h, mi int) time.Time { return time.Date(y, m, d, h, mi, 0, 0, loc) }
	tests := []struct {
		name string
		raw  json.RawMessage
		now  time.Time
		want bool
	}{
		{"once, during", dt(""), at(2026, 11, 2, 2, 30), true},
		{"once, after", dt(""), at(2026, 11, 2, 3, 0), false},
		{"once, before", dt(""), at(2026, 11, 2, 1, 59), false},
		{"daily, next day", dt(`{"type":"daily","interval":1}`), at(2026, 11, 5, 2, 10), true},
		{"daily every 2 days, odd day", dt(`{"type":"daily","interval":2}`), at(2026, 11, 3, 2, 10), false},
		{"weekly mon,thu on thu", dt(`{"type":"weekly","interval":1,"weekdays":["Monday","Thursday"]}`), at(2026, 11, 5, 2, 30), true},
		{"weekly mon,thu on wed", dt(`{"type":"weekly","interval":1,"weekdays":["Monday","Thursday"]}`), at(2026, 11, 4, 2, 30), false},
		{"weekly next month", dt(`{"type":"weekly","interval":1,"weekdays":["Monday"]}`), at(2026, 12, 7, 2, 30), true},
		{"until passed", dt(`{"type":"daily","interval":1,"until":` + itoa(at(2026, 11, 4, 0, 0).Unix()) + `}`), at(2026, 11, 6, 2, 10), false},
		{"hourly", dt(`{"type":"hourly","interval":3}`), at(2026, 11, 2, 5, 15), true},
		{"monthly", dt(`{"type":"monthly","interval":1}`), at(2027, 1, 2, 2, 59), true},
	}
	for _, tt := range tests {
		if got := downtimeActive(tt.raw, tt.now); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestParseRecurrence(t *testing.T) {
	r, err := parseRecurrence("weekly:mon,Thursday", 2)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(r)
	if string(b) != `{"interval":2,"type":"weekly","weekdays":["Monday","Thursday"]}` {
		t.Errorf("got %s", b)
	}
	for _, bad := range []string{"fortnightly", "daily:mon", "weekly:funday"} {
		if _, err := parseRecurrence(bad, 1); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}
