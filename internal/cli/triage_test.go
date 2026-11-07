package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestDowntimeCovers(t *testing.T) {
	services, roles := []string{"web"}, []string{"web:app"}
	tests := []struct {
		name string
		d    auditDowntime
		want bool
	}{
		{"no scope covers everything", auditDowntime{}, true},
		{"service", auditDowntime{ServiceScopes: []string{"web"}}, true},
		{"other service", auditDowntime{ServiceScopes: []string{"batch"}}, false},
		{"role with a space", auditDowntime{RoleScopes: []string{"web: app"}}, true},
		{"monitor", auditDowntime{MonitorScopes: []string{"mon1"}}, true},
		{"other monitor", auditDowntime{MonitorScopes: []string{"mon2"}}, false},
		{"excluded monitor", auditDowntime{ServiceScopes: []string{"web"}, MonitorExcludeScopes: []string{"mon1"}}, false},
		{"excluded role", auditDowntime{RoleExcludeScopes: []string{"web:app"}}, false},
	}
	for _, tt := range tests {
		if got := downtimeCovers(tt.d, "mon1", services, roles); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestTriageMetricPlan(t *testing.T) {
	for metric, want := range map[string][]string{
		"cpu%":     {"cpu.idle.percentage"},
		"disk%":    {"filesystem.*.used", "filesystem.*.size"},
		"loadavg5": {"loadavg5"},
	} {
		got, err := triageMetricPlan(metric)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("%s: got %v, %v; want %v", metric, got, err, want)
		}
	}
	if _, err := triageMetricPlan("container-cpu%"); err == nil {
		t.Error("container-cpu%: want an error")
	}
}

func TestMetricPattern(t *testing.T) {
	re := metricPattern("filesystem.*.used")
	for s, want := range map[string]bool{
		"filesystem.root.used": true, "filesystem.a.b.used": false, "filesystem..used": false, "filesystem.root.size": false,
	} {
		if re.MatchString(s) != want {
			t.Errorf("%s: got %v", s, !want)
		}
	}
}

func TestTruncateLogBody(t *testing.T) {
	long := strings.Repeat("あ", triageMaxLogBody) // 3 bytes each
	raw, _ := json.Marshal(map[string]any{"body": long, "severity": "ERROR"})
	out, err := truncateLogBody(raw)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Body      string
		Truncated bool
		Severity  string
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Truncated || len(got.Body) > triageMaxLogBody || !strings.HasPrefix(long, got.Body) || got.Severity != "ERROR" {
		t.Errorf("got %d bytes, truncated %v", len(got.Body), got.Truncated)
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{"web": "web", "id:abc": "id:abc", "disk usage": "'disk usage'", "it's": `'it'\''s'`} {
		if got := shellQuote(in); got != want {
			t.Errorf("%q: got %s, want %s", in, got, want)
		}
	}
}
