package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
			// os.Executable はテストバイナリそのものを指すので、拡張には PATH 上の mcrl を渡す
			bin, _ := exec.LookPath("mcrl")
			bin, _ = filepath.Abs(bin)
			code := cli.Main(context.Background(), os.Args[1:], cmdutil.System(), cli.WithKeyring(kr), cli.WithExecutable(bin))
			// 終了コードはファイルに残し、スクリプトから grep で確かめる（sh -c は Windows でパスを書き換えるため使わない）
			if p := os.Getenv("MCRL_TEST_EXIT_FILE"); p != "" {
				_ = os.WriteFile(p, []byte(strconv.Itoa(code)+"\n"), 0o600)
			}
			os.Exit(code)
		},
		// PATH 上の拡張 "mcrl testext": 受け取った引数・標準入力・環境変数を JSON で出し、
		// TESTEXT_EXIT の終了コードで終わる。"run-mcrl ..." なら $MCRL_BIN を呼ぶ
		"mcrl-testext": func() {
			args := os.Args[1:]
			if len(args) > 0 && args[0] == "run-mcrl" {
				cmd := exec.Command(os.Getenv("MCRL_BIN"), args[1:]...)
				cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
				_ = cmd.Run()
				os.Exit(cmd.ProcessState.ExitCode())
			}
			stdin, _ := io.ReadAll(os.Stdin)
			env := map[string]string{}
			for _, kv := range os.Environ() {
				k, v, _ := strings.Cut(kv, "=")
				env[k] = v
			}
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"args": args, "stdin": string(stdin), "env": env})
			code, _ := strconv.Atoi(os.Getenv("TESTEXT_EXIT"))
			os.Exit(code)
		},
		// edit のテスト用エディタ: FAKE_EDIT_FROM を FAKE_EDIT_TO に置き換えて保存する
		"fakeeditor": func() {
			path := os.Args[len(os.Args)-1]
			b, err := os.ReadFile(path)
			if err != nil {
				os.Exit(1)
			}
			out := strings.ReplaceAll(string(b), os.Getenv("FAKE_EDIT_FROM"), os.Getenv("FAKE_EDIT_TO"))
			if os.Getenv("FAKE_EDIT_EMPTY") != "" {
				out = ""
			}
			if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
				os.Exit(1)
			}
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
			home := filepath.Join(env.WorkDir, "home")
			env.Setenv("HOME", home)
			env.Setenv("USERPROFILE", home)
			env.Setenv("XDG_CONFIG_HOME", "")
			// Windows でも設定ファイルが $HOME/.config/mcrl/config.toml になるようにする
			env.Setenv("APPDATA", filepath.Join(home, ".config"))
			env.Setenv("LOCALAPPDATA", filepath.Join(home, ".cache"))
			env.Setenv("MCRL_TEST_KEYRING", filepath.Join(env.WorkDir, "keyring.json"))
			env.Setenv("SERVER", srv.URL)
			env.Setenv("MCRL_TEST_EXIT_FILE", filepath.Join(env.WorkDir, "exitcode"))
			env.Setenv("XDG_DATA_HOME", "")
			examples, err := filepath.Abs(filepath.Join("..", "..", "examples"))
			if err != nil {
				return err
			}
			env.Setenv("EXAMPLES", examples)
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
	if os.Getenv("MCRL_TEST_KEYRING_READONLY") != "" {
		return errors.New("keyring is read-only")
	}
	m := k.load()
	m[s+"/"+u] = p
	return k.save(m)
}

func (k fileKeyring) Delete(s, u string) error {
	if k == "" {
		return errors.New("keyring unavailable")
	}
	if os.Getenv("MCRL_TEST_KEYRING_READONLY") != "" {
		return errors.New("keyring is read-only")
	}
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
		reqBody, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(reqBody))
		mu.Lock()
		if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			line := r.Method + " " + r.URL.Path
			if r.URL.RawQuery != "" {
				line += "?" + r.URL.RawQuery
			}
			io.WriteString(f, line+"\n")
			if len(reqBody) > 0 {
				io.WriteString(f, "BODY "+string(reqBody)+"\n")
			}
			f.Close()
		}
		mu.Unlock()
		// echo は受け取ったボディに id を足して返す
		echo := func(id string) {
			var m map[string]any
			if json.Unmarshal(reqBody, &m) != nil {
				writeJSON(w, 400, `{"error":{"message":"Bad request."}}`)
				return
			}
			m["id"] = id
			out, _ := json.Marshal(m)
			writeJSON(w, 200, string(out))
		}
		success := func() { writeJSON(w, 200, `{"success":true}`) }

		// 本物の API と同じく、GET 以外は Content-Type: application/json がないと 400
		if r.Method != http.MethodGet && r.Header.Get("Content-Type") != "application/json" {
			writeJSON(w, 400, `{"error":{"message":"Illegal Content-Type. Please try with \"Content-Type: application/json\""}}`)
			return
		}
		switch r.Header.Get("X-Api-Key") {
		case "good-key", "other-key":
		case "audit-key":
			// mcrl audit のテスト用の組織（audit_fake_test.go）
			if serveAuditFixture(w, r) {
				return
			}
		case "triage-key":
			// mcrl triage のテスト用の組織（triage_fake_test.go）
			if serveTriageFixture(w, r, reqBody) {
				return
			}
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
			switch r.URL.Query().Get("name") {
			case "":
				writeJSON(w, 200, `{"hosts":[{"id":"exampleHst1","name":"web-1","status":"working","createdAt":1793970084},{"id":"exampleHst2","name":"db-1","status":"standby","createdAt":1793970085}]}`)
			case "web-1":
				writeJSON(w, 200, `{"hosts":[{"id":"exampleHst1","name":"web-1","status":"working","createdAt":1793970084,"roles":{"web":["app"]}}]}`)
			case "dup":
				writeJSON(w, 200, `{"hosts":[{"id":"dupaaaaaaa1","name":"dup"},{"id":"dupaaaaaaa2","name":"dup"}]}`)
			default:
				writeJSON(w, 200, `{"hosts":[]}`)
			}
		case route == "GET /api/v0/hosts/exampleHst1/monitored-statuses":
			writeJSON(w, 200, `{"monitoredStatuses":[{"monitorId":"mon0000cpu1","status":"CRITICAL"},{"monitorId":"mon0000conn","status":"OK"}]}`)
		case route == "GET /api/v0/hosts/exampleHst1/metrics":
			writeJSON(w, 200, `{"metrics":[{"time":1762390800,"value":0.5},{"time":1762390860,"value":1.5}]}`)
		case route == "GET /api/v0/tsdb/latest":
			writeJSON(w, 200, `{"tsdbLatest":{"exampleHst1":{"loadavg5":{"time":1762390860,"value":1.5}}}}`)
		case route == "GET /api/v0/monitors":
			writeJSON(w, 200, `{"monitors":[{"id":"mon0000conn","name":"connectivity","type":"connectivity"},{"id":"mon0000cpu1","name":"CPU high","type":"host"},{"id":"mon0000svc1","name":"latency","type":"service"}]}`)
		case route == "GET /api/v0/monitors/mon0000cpu1":
			writeJSON(w, 200, `{"monitor":{"id":"mon0000cpu1","name":"CPU high","type":"host"}}`)
		case route == "GET /api/v0/notification-groups":
			writeJSON(w, 200, `{"notificationGroups":[{"id":"ng000000001","name":"Default","type":"group-default"},{"id":"ng000000002","name":"ops","type":"group"}]}`)
		case route == "GET /api/v0/graph-annotations":
			if r.URL.Query().Get("service") == "" || r.URL.Query().Get("from") == "" || r.URL.Query().Get("to") == "" {
				writeJSON(w, 400, `{"error":{"message":"Bad request."}}`)
				return
			}
			writeJSON(w, 200, `{"graphAnnotations":[{"id":"anno1","title":"deploy","service":"web","from":1,"to":2}]}`)
		case route == "GET /api/v0/hosts/exampleHst1":
			writeJSON(w, 200, `{"host":{"id":"exampleHst1","name":"web-1","status":"working","roles":{"web":["app"]}}}`)
		case strings.HasPrefix(route, "GET /api/v0/hosts/bulk"):
			id := strings.TrimPrefix(r.URL.Path, "/api/v0/hosts/")
			writeJSON(w, 200, `{"host":{"id":"`+id+`","name":"`+id+`"}}`)
		case route == "POST /api/v0/hosts/exampleHst1/status", route == "POST /api/v0/hosts/exampleHst1/retire",
			route == "PUT /api/v0/hosts/exampleHst1/role-fullnames", route == "POST /api/v0/tsdb",
			route == "POST /api/v0/services/web/tsdb", route == "POST /api/v0/monitoring/checks/report",
			route == "POST /api/v0/graph-defs/create", route == "DELETE /api/v0/graph-defs":
			success()
		case route == "DELETE /api/v0/services/web/graph-defs/custom.b.*":
			// 本物の API はグラフ名の "%2A" を "*" と解釈しない
			if strings.Contains(r.URL.EscapedPath(), "%2A") {
				writeJSON(w, 404, `{"error":{"message":"Graph is not found."}}`)
				return
			}
			success()
		case route == "POST /api/v0/hosts/bulk-update-statuses", route == "POST /api/v0/hosts/bulk-retire":
			var b struct{ IDs []string }
			if json.Unmarshal(reqBody, &b) != nil || len(b.IDs) > 30 {
				writeJSON(w, 400, `{"error":{"message":"too many ids"}}`)
				return
			}
			success()
		case route == "POST /api/v0/alerts/a1/close":
			writeJSON(w, 200, `{"id":"a1","status":"OK","reason":"done","closedAt":1762390900}`)
		case route == "PUT /api/v0/alerts/a1":
			writeJSON(w, 200, `{"id":"a1","memo":"noted"}`)
		case route == "POST /api/v0/monitors":
			echo("monNEW00001")
		case route == "PUT /api/v0/monitors/mon0000cpu1":
			echo("mon0000cpu1")
		case route == "DELETE /api/v0/monitors/mon0000cpu1":
			writeJSON(w, 200, `{"id":"mon0000cpu1","name":"CPU high","type":"host"}`)
		case route == "GET /api/v0/downtimes":
			writeJSON(w, 200, `{"downtimes":[{"id":"dt000000001","name":"backup","start":1762390800,"duration":60,"serviceScopes":["web"]},{"id":"dt000000002","name":"always","start":1762390800,"duration":10080,"recurrence":{"type":"daily","interval":1}}]}`)
		case route == "GET /api/v0/channels":
			writeJSON(w, 200, `{"channels":[{"id":"ch000000001","name":"ops","type":"slack","url":"https://hooks.slack.com/x","mentions":{},"enabledGraphImage":false,"events":["alert"],"suspendedAt":null},{"id":"ch000000002","name":"phone","type":"twilio","suspendedAt":null}]}`)
		case route == "POST /api/v0/channels", route == "POST /api/v0/notification-groups", route == "POST /api/v0/alert-group-settings", route == "POST /api/v0/dashboards":
			echo("new00000001")
		case route == "PUT /api/v0/channels/ch000000001":
			echo("ch000000001")
		case route == "GET /api/v0/dashboards":
			writeJSON(w, 200, `{"dashboards":[{"id":"db000000001","title":"Overview","urlPath":"overview","memo":"","widgets":[],"createdAt":1,"updatedAt":2}]}`)
		case route == "GET /api/v0/dashboards/db000000001":
			writeJSON(w, 200, `{"id":"db000000001","title":"Overview","urlPath":"overview","memo":"","widgets":[],"createdAt":1,"updatedAt":2}`)
		case route == "PUT /api/v0/dashboards/db000000001":
			echo("db000000001")
		case route == "PUT /api/v0/hosts/exampleHst1/metadata/inventory", route == "POST /api/v0/settings/ip-restriction/api",
			route == "POST /api/v0/invitations/revoke":
			success()
		case route == "GET /api/v0/hosts/exampleHst1/metadata/inventory":
			writeJSON(w, 200, `{"rack":"A1"}`)
		case route == "GET /api/v0/settings/ip-restriction/api":
			writeJSON(w, 200, `{"cidrs":["203.0.113.0/24"]}`)
		case route == "POST /api/v0/invitations":
			writeJSON(w, 200, string(reqBody))
		case route == "POST /api/v0/downtimes":
			echo("dtNEW000001")
		case route == "PUT /api/v0/downtimes/dt000000001":
			echo("dt000000001")
		case route == "PUT /api/v0/graph-annotations/anno1":
			echo("anno1")
		case route == "POST /api/v0/hosts", route == "PUT /api/v0/hosts/exampleHst1":
			writeJSON(w, 200, `{"id":"exampleHst1"}`)
		case route == "POST /api/v0/logs":
			var b struct {
				PageCursor  *struct{ Cursor string } `json:"pageCursor"`
				ServiceName string                   `json:"serviceName"`
			}
			_ = json.Unmarshal(reqBody, &b)
			if b.ServiceName == "" {
				writeJSON(w, 400, `{"error":{"message":"Invalid Parameter: obj.serviceName error.path.missing."}}`)
				return
			}
			if b.PageCursor == nil {
				writeJSON(w, 200, `{"results":[{"cursor":"c1","timestamp":"2026-11-07T00:00:02Z","severity":"ERROR","body":"boom"},{"cursor":"c2","timestamp":"2026-11-07T00:00:01Z","severity":"INFO","body":"ok"}],"pagination":{"hasNextPage":true,"hasPreviousPage":false,"startCursor":"c1","endCursor":"c2"}}`)
			} else {
				writeJSON(w, 200, `{"results":[{"cursor":"c3","timestamp":"2026-11-07T00:00:00Z","severity":"INFO","body":"start"}],"pagination":{"hasNextPage":false,"hasPreviousPage":true,"startCursor":"c3","endCursor":"c3"}}`)
			}
		case route == "GET /api/v0/saved-log-searches":
			writeJSON(w, 200, `{"results":[{"id":"s1","name":"errors","description":null,"definition":{"serviceName":"web","serviceNamespace":null,"keywords":[],"severities":["ERROR"],"traceId":null,"bodyAttributes":[]},"createdAt":1,"updatedAt":2}]}`)
		case route == "POST /api/v0/traces":
			var b struct{ Page int }
			_ = json.Unmarshal(reqBody, &b)
			if b.Page == 1 {
				writeJSON(w, 200, `{"results":[{"traceId":"t1","title":"GET /a"}],"hasNextPage":true}`)
			} else {
				writeJSON(w, 200, `{"results":[{"traceId":"t2","title":"GET /b"}],"hasNextPage":false}`)
			}
		case route == "GET /api/v0/traces/t1":
			writeJSON(w, 200, `{"spans":[{"traceId":"t1","spanId":"s1","name":"GET /a"}]}`)
		case route == "GET /api/v0/apm/http-server-stats":
			writeJSON(w, 200, `{"results":[{"method":"GET","route":"/a","requestCount":3}],"hasNextPage":false}`)
		case route == "POST /api/v0/services":
			writeJSON(w, 200, string(reqBody))
		case strings.HasPrefix(route, "GET /api/v0/hosts/"):
			writeJSON(w, 404, `{"error":{"message":"Host Not Found."}}`)
		case route == "GET /api/v0/alerts":
			if r.URL.Query().Get("nextId") == "" {
				writeJSON(w, 200, `{"alerts":[{"id":"a1","status":"CRITICAL","monitorId":"mon0000cpu1","hostId":"exampleHst1","type":"host"},{"id":"a2","status":"WARNING","monitorId":"mon0000svc1","type":"service"}],"nextId":"a2"}`)
			} else {
				writeJSON(w, 200, `{"alerts":[{"id":"a3","status":"CRITICAL","monitorId":"mon0000conn","hostId":"exampleHst2","type":"connectivity"}]}`)
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
