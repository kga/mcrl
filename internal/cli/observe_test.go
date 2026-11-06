package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseWhere(t *testing.T) {
	tests := map[string]string{
		"res.status>=500":   `{"keyPath":["res","status"],"operator":"GTE","type":"int","value":"500"}`,
		"req.method=GET":    `{"keyPath":["req","method"],"operator":"EQ","type":"string","value":"GET"}`,
		"path^=/api":        `{"keyPath":["path"],"operator":"STARTS_WITH","type":"string","value":"/api"}`,
		"latency<0.5":       `{"keyPath":["latency"],"operator":"LT","type":"double","value":"0.5"}`,
		"ok=true":           `{"keyPath":["ok"],"operator":"EQ","type":"bool","value":"true"}`,
		"code=200:string":   `{"keyPath":["code"],"operator":"EQ","type":"string","value":"200"}`,
		`a\.b.c!=3`:         `{"keyPath":["a.b","c"],"operator":"NEQ","type":"int","value":"3"}`,
		"url=http://x/?a=b": `{"keyPath":["url"],"operator":"EQ","type":"string","value":"http://x/?a=b"}`,
	}
	for in, want := range tests {
		got, err := parseWhere(in, true)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		b, _ := json.Marshal(got)
		if string(b) != want {
			t.Errorf("%q: got %s, want %s", in, b, want)
		}
	}
	for _, bad := range []string{"novalue", "=x", "name>abc", "n^=5:int", "x>=1:string"} {
		if _, err := parseWhere(bad, true); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
	got, _ := parseWhere("http.route=/users", false)
	if got["key"] != "http.route" {
		t.Errorf("trace attributes use key, got %v", got)
	}
}

func TestParseSeverities(t *testing.T) {
	got, err := parseSeverities([]string{"warn+", "info"})
	if err != nil || strings.Join(got, ",") != "ERROR,FATAL,INFO,WARN" {
		t.Errorf("got %v %v", got, err)
	}
	got, _ = parseSeverities([]string{"error,fatal"})
	if strings.Join(got, ",") != "ERROR,FATAL" {
		t.Errorf("comma: %v", got)
	}
	if _, err := parseSeverities([]string{"loud"}); err == nil {
		t.Error("unknown severity should fail")
	}
}
