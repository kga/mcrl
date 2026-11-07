package httpx

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRedactBody(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"nothing to hide is unchanged", `{"name": "web",  "memo":"x"}`, `{"name": "web",  "memo":"x"}`},
		{"not JSON", `metric.a 1 1700000000`, `metric.a 1 1700000000`},
		{"JSON Lines", `{"secretKey":"a"}` + "\n" + `{"secretKey":"b"}`, `{"secretKey":"a"}` + "\n" + `{"secretKey":"b"}`},
		{"AWS secret key", `{"name":"aws","key":"AKIAEXAMPLE","secretKey":"s3cr3t"}`, `{"key":"AKIAEXAMPLE","name":"aws","secretKey":"***"}`},
		{"case-insensitive", `{"Password":"p","APIKey":"k"}`, `{"APIKey":"***","Password":"***"}`},
		{"null and empty are kept", `{"secretKey":null,"token":"","headers":[],"requestBody":""}`, `{"secretKey":null,"token":"","headers":[],"requestBody":""}`},
		{"channel URL", `{"channels":[{"type":"slack","url":"https://hooks.slack.com/services/T0/B0/xyz"}]}`, `{"channels":[{"type":"slack","url":"https://hooks.slack.com/***"}]}`},
		{"URL with only a host is kept", `{"url":"https://example.com/"}`, `{"url":"https://example.com/"}`},
		{"URL with credentials", `{"url":"https://u:p@example.com"}`, `{"url":"https://example.com/***"}`},
		{"external monitor", `{"type":"external","url":"https://example.com/health?token=t","headers":[{"name":"Authorization","value":"Bearer t"}],"requestBody":"pw=t"}`, `{"headers":"***","requestBody":"***","type":"external","url":"https://example.com/***"}`},
		{"nested in arrays", `[{"a":[{"secret":"s"}]}]`, `[{"a":[{"secret":"***"}]}]`},
		{"numbers keep their form", `{"secret":"s","n":12345678901234567890,"f":1.50}`, `{"f":1.50,"n":12345678901234567890,"secret":"***"}`},
		{"HTML is not escaped", `{"secret":"s","memo":"<b>&</b>"}`, `{"memo":"<b>&</b>","secret":"***"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(redactBody([]byte(tt.in))); got != tt.want {
				t.Errorf("redactBody(%s)\n got %s\nwant %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestDebugRedactsBodies(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"abc","url":"https://hooks.slack.com/services/T0/B0/resp-secret"}`)
	})
	var buf bytes.Buffer
	c.Debug = &buf
	req := &Request{Method: "POST", Path: "/aws-integrations", Body: []byte(`{"name":"aws","secretKey":"req-secret"}`)}
	if _, err := c.Do(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, leak := range []string{"req-secret", "resp-secret", "T0/B0"} {
		if strings.Contains(out, leak) {
			t.Errorf("debug output leaks %q:\n%s", leak, out)
		}
	}
	for _, want := range []string{`"secretKey":"***"`, `"url":"https://hooks.slack.com/***"`} {
		if !strings.Contains(out, want) {
			t.Errorf("debug output lacks %q:\n%s", want, out)
		}
	}
	// 送る本文そのものは変えない
	if !strings.Contains(string(req.Body), "req-secret") {
		t.Errorf("request body was modified: %s", req.Body)
	}
}
