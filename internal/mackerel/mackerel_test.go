package mackerel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/kga/mcrl/internal/httpx"
)

func newClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return New(&httpx.Client{BaseURL: u, APIKey: "k"})
}

func TestIsID(t *testing.T) {
	for s, want := range map[string]bool{"exampleHst1": true, "exampleHst": false, "web-server1": false, "abcdefghijk": true} {
		if IsID(s) != want {
			t.Errorf("IsID(%q) = %v", s, !want)
		}
	}
}

const monitors = `{"monitors":[
 {"id":"m1aaaaaaaaa","name":"cpu","type":"host"},
 {"id":"m2aaaaaaaaa","name":"dup","type":"host"},
 {"id":"m3aaaaaaaaa","name":"dup","type":"service"},
 {"id":"m4aaaaaaaaa","name":"m1aaaaaaaaa","type":"host"}]}`

func monitorServer(t *testing.T) *Client {
	return newClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/monitors":
			io.WriteString(w, monitors)
		case "/api/v0/monitors/m1aaaaaaaaa":
			io.WriteString(w, `{"monitor":{"id":"m1aaaaaaaaa","name":"cpu","type":"host"}}`)
		case "/api/v0/monitors/xxxxxxxxxxx":
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":{"message":"Bad request."}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":{"message":"monitor is not found."}}`)
		}
	})
}

func TestResolve(t *testing.T) {
	c := monitorServer(t)
	ctx := context.Background()

	got, err := c.Resolve(ctx, KindMonitor, "m1aaaaaaaaa")
	if err != nil || StringField(got, "name") != "cpu" {
		t.Errorf("by ID: %s %v", got, err)
	}
	got, err = c.Resolve(ctx, KindMonitor, "cpu")
	if err != nil || StringField(got, "id") != "m1aaaaaaaaa" {
		t.Errorf("by name: %s %v", got, err)
	}
	// name: 接頭辞で ID の形をした名前を名前として探す
	got, err = c.Resolve(ctx, KindMonitor, "name:m1aaaaaaaaa")
	if err != nil || StringField(got, "id") != "m4aaaaaaaaa" {
		t.Errorf("name prefix: %s %v", got, err)
	}

	_, err = c.Resolve(ctx, KindMonitor, "dup")
	var amb *AmbiguousError
	if !errors.As(err, &amb) || len(amb.Candidates) != 2 || !strings.Contains(err.Error(), "m2aaaaaaaaa, m3aaaaaaaaa") {
		t.Errorf("ambiguous: %v", err)
	}

	var nf *NotFoundError
	if _, err := c.Resolve(ctx, KindMonitor, "nope"); !errors.As(err, &nf) {
		t.Errorf("not found by name: %v", err)
	}
	// 形式が正しくない ID は API が 400 を返すが、見つからないとして扱う
	if _, err := c.Resolve(ctx, KindMonitor, "id:xxxxxxxxxxx"); !errors.As(err, &nf) {
		t.Errorf("400 by ID: %v", err)
	}
}

func TestResolveHost(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v0/hosts/exampleHst1":
			io.WriteString(w, `{"host":{"id":"exampleHst1","name":"web-1"}}`)
		case r.URL.Path == "/api/v0/hosts":
			if got := r.URL.Query()["status"]; len(got) != 4 {
				t.Errorf("status = %v, want all statuses", got)
			}
			switch r.URL.Query().Get("name") {
			case "web-1":
				io.WriteString(w, `{"hosts":[{"id":"exampleHst1","name":"web-1"}]}`)
			case "dup":
				io.WriteString(w, `{"hosts":[{"id":"a","name":"dup"},{"id":"b","name":"dup"}]}`)
			default:
				io.WriteString(w, `{"hosts":[]}`)
			}
		case strings.HasPrefix(r.URL.Path, "/api/v0/hosts-by-custom-identifier/"):
			if strings.HasSuffix(r.URL.Path, "/i-123") {
				io.WriteString(w, `{"host":{"id":"exampleHst1","name":"web-1"}}`)
			} else {
				io.WriteString(w, `{"host":null}`)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	ctx := context.Background()
	for _, ref := range []string{"exampleHst1", "web-1", "cid:i-123"} {
		h, err := c.ResolveHost(ctx, ref)
		if err != nil || StringField(h, "id") != "exampleHst1" {
			t.Errorf("%s: %s %v", ref, h, err)
		}
	}
	var nf *NotFoundError
	for _, ref := range []string{"nope", "cid:i-999"} {
		if _, err := c.ResolveHost(ctx, ref); !errors.As(err, &nf) {
			t.Errorf("%s: %v", ref, err)
		}
	}
	var amb *AmbiguousError
	if _, err := c.ResolveHost(ctx, "dup"); !errors.As(err, &amb) {
		t.Errorf("dup: %v", err)
	}
}

func TestNextIDPages(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "2" {
			t.Errorf("limit = %s", r.URL.Query().Get("limit"))
		}
		switch r.URL.Query().Get("nextId") {
		case "":
			io.WriteString(w, `{"alerts":[{"id":"1"},{"id":"2"}],"nextId":"2"}`)
		case "2":
			io.WriteString(w, `{"alerts":[{"id":"3"}]}`)
		}
	})
	var ids []string
	for it, err := range c.NextIDPages(context.Background(), "/alerts", "alerts", nil, 2) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, StringField(it, "id"))
	}
	if strings.Join(ids, ",") != "1,2,3" {
		t.Errorf("ids = %v", ids)
	}
}

func TestWithExpanded(t *testing.T) {
	got, err := WithExpanded(json.RawMessage(`{"b":1.50,"a":2}`), map[string]string{"x": "y"})
	if err != nil || string(got) != `{"b":1.50,"a":2,"_expanded":{"x":"y"}}` {
		t.Errorf("got %s %v", got, err)
	}
	got, _ = WithExpanded(json.RawMessage(`{}`), 1)
	if string(got) != `{"_expanded":1}` {
		t.Errorf("empty: %s", got)
	}
}
