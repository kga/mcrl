package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"github.com/zalando/go-keyring"

	"github.com/kga/mcrl/internal/cli"
	"github.com/kga/mcrl/internal/cli/cmdutil"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"mcrl": func() {
			// テストでは本物のキーチェーンに触れない
			kr := fileKeyring(os.Getenv("MCRL_TEST_KEYRING"))
			os.Exit(cli.Main(context.Background(), os.Args[1:], cmdutil.System(), cli.WithKeyring(kr)))
		},
	})
}

func TestScript(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: filepath.Join("testdata", "script"),
		Setup: func(env *testscript.Env) error {
			srv := httptest.NewServer(newFakeAPI(filepath.Join(env.WorkDir, "requests.log")))
			env.Defer(srv.Close)
			env.Setenv("MCRL_API_BASE", srv.URL)
			env.Setenv("HOME", filepath.Join(env.WorkDir, "home"))
			env.Setenv("XDG_CONFIG_HOME", "")
			env.Setenv("MCRL_TEST_KEYRING", filepath.Join(env.WorkDir, "keyring.json"))
			env.Setenv("SERVER", srv.URL)
			return nil
		},
	})
}

// fileKeyring is a keychain stand-in that persists across mcrl invocations.
type fileKeyring string

func (k fileKeyring) load() map[string]string {
	m := map[string]string{}
	if b, err := os.ReadFile(string(k)); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func (k fileKeyring) save(m map[string]string) error {
	b, _ := json.Marshal(m)
	return os.WriteFile(string(k), b, 0o600)
}

func (k fileKeyring) Get(s, u string) (string, error) {
	if k == "" {
		return "", errors.New("keyring unavailable")
	}
	if v, ok := k.load()[s+"/"+u]; ok {
		return v, nil
	}
	return "", keyring.ErrNotFound
}

func (k fileKeyring) Set(s, u, p string) error {
	if k == "" || strings.Contains(string(k), "unavailable") {
		return errors.New("keyring unavailable")
	}
	m := k.load()
	m[s+"/"+u] = p
	return k.save(m)
}

func (k fileKeyring) Delete(s, u string) error {
	m := k.load()
	if _, ok := m[s+"/"+u]; !ok {
		return keyring.ErrNotFound
	}
	delete(m, s+"/"+u)
	return k.save(m)
}

// newFakeAPI is a tiny stand-in for the Mackerel API. Every request is
// appended to logPath as "METHOD /path?query".
func newFakeAPI(logPath string) http.Handler {
	var (
		mu    sync.Mutex
		flaky atomic.Int32
	)
	writeJSON := func(w http.ResponseWriter, status int, body string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			line := r.Method + " " + r.URL.Path
			if r.URL.RawQuery != "" {
				line += "?" + r.URL.RawQuery
			}
			io.WriteString(f, line+"\n")
			f.Close()
		}
		mu.Unlock()

		switch r.Header.Get("X-Api-Key") {
		case "good-key", "other-key":
		default:
			writeJSON(w, 401, `{"error":{"message":"Authentication failed. Please try with valid Api Key."}}`)
			return
		}

		route := r.Method + " " + r.URL.Path
		switch {
		case route == "GET /api/v0/org":
			name := "test-org"
			if r.Header.Get("X-Api-Key") == "other-key" {
				name = "other-org"
			}
			writeJSON(w, 200, `{"name":"`+name+`","displayName":null}`)
		case route == "GET /api/v0/hosts":
			writeJSON(w, 200, `{"hosts":[{"id":"5Vf7Be7guuy","name":"web-1","status":"working","createdAt":1793970084},{"id":"5Vf7Be7guuz","name":"db-1","status":"standby","createdAt":1793970085}]}`)
		case route == "GET /api/v0/hosts/5Vf7Be7guuy":
			writeJSON(w, 200, `{"host":{"id":"5Vf7Be7guuy","name":"web-1","status":"working"}}`)
		case strings.HasPrefix(route, "GET /api/v0/hosts/"):
			writeJSON(w, 404, `{"error":{"message":"Host Not Found."}}`)
		case route == "GET /api/v0/alerts":
			if r.URL.Query().Get("nextId") == "" {
				writeJSON(w, 200, `{"alerts":[{"id":"a1"},{"id":"a2"}],"nextId":"a2"}`)
			} else {
				writeJSON(w, 200, `{"alerts":[{"id":"a3"}]}`)
			}
		case route == "POST /api/v0/graph-annotations":
			b, _ := io.ReadAll(r.Body)
			var m map[string]any
			if json.Unmarshal(b, &m) != nil || r.Header.Get("Content-Type") != "application/json" {
				writeJSON(w, 400, `{"error":{"message":"Bad request."}}`)
				return
			}
			m["id"] = "anno1"
			out, _ := json.Marshal(m)
			writeJSON(w, 200, string(out))
		case route == "GET /api/v0/flaky":
			if flaky.Add(1) == 1 {
				writeJSON(w, 503, `{"error":{"message":"Service Unavailable"}}`)
				return
			}
			writeJSON(w, 200, `{"ok":true}`)
		case route == "DELETE /api/v0/monitors/m1":
			w.WriteHeader(http.StatusNoContent)
		default:
			writeJSON(w, 404, `{"error":{"message":"Not found."}}`)
		}
	})
}
