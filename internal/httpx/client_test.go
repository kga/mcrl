package httpx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *[]time.Duration) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	var slept []time.Duration
	return &Client{
		BaseURL:   u,
		APIKey:    "secret-key",
		UserAgent: "mcrl/test",
		Retries:   3,
		Sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			return nil
		},
	}, &slept
}

func TestNormalizePath(t *testing.T) {
	for in, want := range map[string]string{
		"hosts":         "/api/v0/hosts",
		"/hosts":        "/api/v0/hosts",
		"/api/v0/hosts": "/api/v0/hosts",
		"/api/v1/x":     "/api/v1/x",
	} {
		if got := NormalizePath(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

// The path is taken as escaped: url.PathEscape'd segments are not escaped
// twice, and "*" is sent as is (the API does not decode "%2A" in graph names).
func TestURLKeepsEscapedPath(t *testing.T) {
	base, _ := url.Parse("https://api.example.com/base/")
	c := &Client{BaseURL: base}
	for in, want := range map[string]string{
		"/hosts":                               "https://api.example.com/base/api/v0/hosts",
		"/services/web/graph-defs/a.b.*":       "https://api.example.com/base/api/v0/services/web/graph-defs/a.b.*",
		"/metadata/" + url.PathEscape("a b/c"): "https://api.example.com/base/api/v0/metadata/a%20b%2Fc",
		"/hosts/a b":                           "https://api.example.com/base/api/v0/hosts/a%20b",
		"/hosts/100%":                          "https://api.example.com/base/api/v0/hosts/100%25",
	} {
		if got := c.URL(in, nil).String(); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestDoSendsHeaders(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0/org" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Api-Key"); got != "secret-key" {
			t.Errorf("X-Api-Key = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "mcrl/test" {
			t.Errorf("User-Agent = %q", got)
		}
		io.WriteString(w, `{"name":"org"}`)
	})
	resp, err := c.Do(context.Background(), &Request{Path: "org"})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != `{"name":"org"}` {
		t.Errorf("body = %s", resp.Body)
	}
}

func TestDoSetsContentTypeOnBodylessDelete(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("%s Content-Type = %q", r.Method, got)
		}
		io.WriteString(w, `{}`)
	})
	if _, err := c.Do(context.Background(), &Request{Method: http.MethodDelete, Path: "/monitors/x"}); err != nil {
		t.Fatal(err)
	}
}

func TestDoAPIError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error":{"message":"Host Not Found."}}`)
	})
	_, err := c.Do(context.Background(), &Request{Path: "/hosts/abc"})
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %T %v", err, err)
	}
	if ae.StatusCode != http.StatusNotFound || ae.Message != "Host Not Found." || ae.Path != "/api/v0/hosts/abc" {
		t.Errorf("got %+v", ae)
	}
	if want := "Host Not Found. (HTTP 404 GET /api/v0/hosts/abc)"; ae.Error() != want {
		t.Errorf("Error() = %q", ae.Error())
	}
}

func TestDoRetriesTransientErrors(t *testing.T) {
	var n atomic.Int32
	c, slept := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch n.Add(1) {
		case 1:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 2:
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			io.WriteString(w, `{}`)
		}
	})
	if _, err := c.Do(context.Background(), &Request{Path: "/hosts"}); err != nil {
		t.Fatal(err)
	}
	if n.Load() != 3 {
		t.Errorf("attempts = %d", n.Load())
	}
	if len(*slept) < 2 || (*slept)[1] != 7*time.Second {
		t.Errorf("slept = %v (want Retry-After 7s honored)", *slept)
	}
}

func TestDoDoesNotRetryPOST(t *testing.T) {
	var n atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	_, err := c.Do(context.Background(), &Request{Method: "POST", Path: "/monitors", Body: []byte(`{}`)})
	if err == nil || n.Load() != 1 {
		t.Errorf("attempts = %d, err = %v", n.Load(), err)
	}

	n.Store(0)
	_, _ = c.Do(context.Background(), &Request{Method: "POST", Path: "/tsdb", Body: []byte(`[]`), Idempotent: true})
	if n.Load() != 4 {
		t.Errorf("idempotent POST attempts = %d, want 4", n.Load())
	}
}

func TestDoGivesUpAfterRetries(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	c.Retries = 1
	_, err := c.Do(context.Background(), &Request{Path: "/hosts"})
	var ae *APIError
	if !errors.As(err, &ae) || ae.StatusCode != http.StatusBadGateway {
		t.Errorf("err = %v", err)
	}
}

func TestDebugRedactsAPIKey(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"ok":true}`)
	})
	var buf bytes.Buffer
	c.Debug = &buf
	if _, err := c.Do(context.Background(), &Request{Path: "/org"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "secret-key") {
		t.Errorf("debug output leaks API key:\n%s", out)
	}
	for _, want := range []string{"> GET ", "/api/v0/org", "> X-Api-Key: ***", "< 200 OK", `{"ok":true}`} {
		if !strings.Contains(out, want) {
			t.Errorf("debug output lacks %q:\n%s", want, out)
		}
	}
}

func TestRateLimitLogs(t *testing.T) {
	c, slept := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) })
	now := time.Unix(1_000_000, 0)
	c.Now = func() time.Time { return now }
	for range 3 {
		if _, err := c.Do(context.Background(), &Request{Method: "POST", Path: "/logs", Body: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	// 時計を止めているので、2 回目は 1 秒、3 回目は 2 秒待つ
	if len(*slept) != 2 || (*slept)[0] != time.Second || (*slept)[1] != 2*time.Second {
		t.Errorf("slept = %v", *slept)
	}
	*slept = nil
	if _, err := c.Do(context.Background(), &Request{Path: "/hosts"}); err != nil {
		t.Fatal(err)
	}
	if len(*slept) != 0 {
		t.Errorf("non-limited path slept %v", *slept)
	}
}

func TestErrorMessage(t *testing.T) {
	for body, want := range map[string]string{
		`{"error":{"message":"Bad request."}}`: "Bad request.",
		`{"error":"plain"}`:                    "plain",
		"oops\nsecond line":                    "oops",
		``:                                     "",
	} {
		if got := errorMessage([]byte(body)); got != want {
			t.Errorf("%q: got %q, want %q", body, got, want)
		}
	}
}

func TestRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	if d, ok := retryAfter("3", now); !ok || d != 3*time.Second {
		t.Errorf("seconds: %v %v", d, ok)
	}
	if d, ok := retryAfter(now.Add(10*time.Second).Format(http.TimeFormat), now); !ok || d != 10*time.Second {
		t.Errorf("date: %v %v", d, ok)
	}
	if _, ok := retryAfter("soon", now); ok {
		t.Error("garbage should not parse")
	}
}
