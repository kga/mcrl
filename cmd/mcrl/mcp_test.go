package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kga/mcrl/internal/cli"
	"github.com/kga/mcrl/internal/cli/cmdutil"
)

// startMCP runs "mcrl mcp <args>" against the fake API and connects an MCP
// client to its stdin and stdout. It returns the session and the path of the
// request log.
func startMCP(t *testing.T, args ...string) (*mcp.ClientSession, string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "requests.log")
	srv := httptest.NewServer(newFakeAPI(logPath))
	t.Cleanup(srv.Close)
	t.Setenv("MCRL_API_BASE", srv.URL)
	t.Setenv("MACKEREL_APIKEY", "good-key")
	t.Setenv("MCRL_CONFIG", filepath.Join(dir, "config.toml"))
	t.Setenv("MCRL_CACHE_DIR", filepath.Join(dir, "cache"))
	t.Setenv("MCRL_PROFILE", "")
	t.Setenv("MCRL_OUTPUT", "jsonl") // must not leak into tool output
	t.Setenv("MCRL_DEBUG", "")

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	streams := &cmdutil.IOStreams{In: inR, Out: outW, ErrOut: io.Discard}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	done := make(chan int, 1)
	go func() {
		code := cli.Main(ctx, append([]string{"mcp"}, args...), streams, cli.WithKeyring(fileKeyring("")))
		outW.Close()
		done <- code
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "mcrl-test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, &mcp.IOTransport{Reader: outR, Writer: inW}, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cs.Close()
		inW.Close()
		select {
		case code := <-done:
			if code != 0 {
				t.Errorf("mcrl mcp exited with %d", code)
			}
		case <-time.After(10 * time.Second):
			t.Error("mcrl mcp did not stop after stdin was closed")
		}
		cancel()
	})
	return cs, logPath
}

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	var names []string
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tool.Name)
	}
	return names
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var texts []string
	for _, c := range res.Content {
		texts = append(texts, c.(*mcp.TextContent).Text)
	}
	return strings.Join(texts, "\n"), res.IsError
}

type mcpErrorBody struct {
	Error struct {
		Code     string `json:"code"`
		Message  string `json:"message"`
		Hint     string `json:"hint"`
		ExitCode int    `json:"exitCode"`
	} `json:"error"`
}

func parseMCPError(t *testing.T, s string) mcpErrorBody {
	t.Helper()
	var e mcpErrorBody
	if err := json.Unmarshal([]byte(strings.SplitN(s, "\n", 2)[0]), &e); err != nil {
		t.Fatalf("error result is not JSON: %q", s)
	}
	return e
}

func TestMCPReadOnly(t *testing.T) {
	cs, logPath := startMCP(t)

	names := toolNames(t, cs)
	for _, n := range []string{"host_get", "host_list", "alert_list", "log_search", "trace_get", "apm_http", "api"} {
		if !slices.Contains(names, n) {
			t.Errorf("missing tool %s", n)
		}
	}
	for _, n := range []string{"monitor_create", "alert_close", "host_retire", "monitor_delete", "host_edit", "log_tail", "auth_token", "check_run"} {
		if slices.Contains(names, n) {
			t.Errorf("tool %s must not be exposed by default", n)
		}
	}
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		if tool.Name == "host_list" {
			if !tool.Annotations.ReadOnlyHint {
				t.Error("host_list is not marked read-only")
			}
			if !strings.Contains(tool.Description, "API key permission: Read") || !strings.Contains(tool.Description, "CLI: mcrl host list") {
				t.Errorf("host_list description: %q", tool.Description)
			}
			if strings.Contains(tool.Description, "exit code 9") {
				t.Errorf("read tool host_list has the update note: %q", tool.Description)
			}
			status, ok := tool.InputSchema.(map[string]any)["properties"].(map[string]any)["status"].(map[string]any)
			items, _ := status["items"].(map[string]any)
			desc, _ := status["description"].(string)
			if !ok || status["type"] != "array" || items["type"] != "string" || !strings.Contains(desc, "array of strings; one value per element") {
				t.Errorf("host_list status schema: %v", status)
			}
		}
	}

	// The output is the command's JSON as-is.
	out, isErr := callTool(t, cs, "host_get", map[string]any{"host": "web-1"})
	if isErr {
		t.Fatalf("host_get: %s", out)
	}
	var host map[string]any
	if err := json.Unmarshal([]byte(out), &host); err != nil || host["id"] != "exampleHst1" {
		t.Errorf("host_get returned %q", out)
	}

	out, isErr = callTool(t, cs, "host_list", map[string]any{"status": []string{"working", "standby"}, "jq": `[.[].name] | join(",")`})
	if isErr || out != "web-1,db-1" {
		t.Errorf("host_list: %q (error %v)", out, isErr)
	}
	b, _ := os.ReadFile(logPath)
	if !strings.Contains(string(b), "GET /api/v0/hosts?status=working&status=standby") {
		t.Errorf("request log:\n%s", b)
	}

	out, isErr = callTool(t, cs, "alert_list", map[string]any{"limit": 0, "jq": "length"})
	if isErr || out != "3" {
		t.Errorf("alert_list: %q (error %v)", out, isErr)
	}

	out, isErr = callTool(t, cs, "api", map[string]any{"path": "/org", "jq": ".name"})
	if isErr || out != "test-org" {
		t.Errorf("api: %q (error %v)", out, isErr)
	}
}

func TestMCPErrors(t *testing.T) {
	cs, _ := startMCP(t)

	tests := []struct {
		name string
		tool string
		args map[string]any
		code string
		exit int
	}{
		{"not found", "host_get", map[string]any{"host": "nope"}, "not_found", 4},
		{"ambiguous name", "host_get", map[string]any{"host": "dup"}, "ambiguous", 5},
		{"missing argument", "host_get", map[string]any{}, "usage", 2},
		{"unknown argument", "host_get", map[string]any{"host": "web-1", "nope": true}, "usage", 2},
		{"wrong type", "alert_list", map[string]any{"limit": "ten"}, "usage", 2},
		{"command validation", "trace_list", map[string]any{"service": "web", "since": "1h"}, "usage", 2},
		{"bad time", "annotation_list", map[string]any{"service": "web", "since": "x"}, "usage", 2},
		{"API 400", "api", map[string]any{"path": "/graph-annotations"}, "invalid", 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, isErr := callTool(t, cs, tt.tool, tt.args)
			if !isErr {
				t.Fatalf("expected an error, got %q", out)
			}
			e := parseMCPError(t, out)
			if e.Error.Code != tt.code || e.Error.ExitCode != tt.exit || e.Error.Message == "" {
				t.Errorf("got %+v, want code %s exit %d", e.Error, tt.code, tt.exit)
			}
		})
	}

	// Write tools are not even listed without --allow-write.
	if _, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "monitor_create", Arguments: map[string]any{"body": map[string]any{}}}); err == nil {
		t.Error("monitor_create should be unknown without --allow-write")
	}
}

func TestMCPAuthError(t *testing.T) {
	cs, _ := startMCP(t)
	t.Setenv("MACKEREL_APIKEY", "bad-key")
	out, isErr := callTool(t, cs, "org_get", nil)
	e := parseMCPError(t, out)
	if !isErr || e.Error.Code != "unauthorized" || e.Error.ExitCode != 3 || e.Error.Hint == "" {
		t.Errorf("org_get with a bad key: %q", out)
	}
}

func TestMCPAllowWrite(t *testing.T) {
	cs, logPath := startMCP(t, "--allow-write")

	names := toolNames(t, cs)
	if !slices.Contains(names, "monitor_create") || !slices.Contains(names, "host_list") {
		t.Errorf("--allow-write lacks tools: %v", names)
	}
	for _, n := range []string{"host_retire", "monitor_delete", "service_delete", "user_remove", "org_ip_restriction_set"} {
		if slices.Contains(names, n) {
			t.Errorf("%s needs --allow-destructive", n)
		}
	}

	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		if tool.Name == "monitor_create" && (!strings.Contains(tool.Description, `Pass "jq" to keep it small`) || !strings.Contains(tool.Description, "exit code 9")) {
			t.Errorf("monitor_create description lacks the jq note: %q", tool.Description)
		}
	}

	// dry_run prints the request without sending it.
	out, isErr := callTool(t, cs, "monitor_create", map[string]any{"body": map[string]any{"type": "host", "name": "cpu"}, "dry_run": true})
	if isErr || !strings.Contains(out, `"method":"POST"`) {
		t.Errorf("dry run: %q", out)
	}
	if b, _ := os.ReadFile(logPath); strings.Contains(string(b), "POST /api/v0/monitors") {
		t.Error("dry run sent the request")
	}

	// body is given as stdin to --file -; YAML text works too.
	out, isErr = callTool(t, cs, "monitor_create", map[string]any{"body": "type: host\nname: cpu\n", "jq": ".id"})
	if isErr || out != "monNEW00001" {
		t.Errorf("monitor_create: %q (error %v)", out, isErr)
	}
	b, _ := os.ReadFile(logPath)
	if !strings.Contains(string(b), `BODY {"name":"cpu","type":"host"}`) {
		t.Errorf("request log:\n%s", b)
	}

	out, isErr = callTool(t, cs, "alert_close", map[string]any{"alert_id": []string{"a1"}, "reason": "done", "jq": ".[0].status // .status"})
	if isErr || out != "OK" {
		t.Errorf("alert_close: %q (error %v)", out, isErr)
	}
}

func TestMCPAllowDestructive(t *testing.T) {
	cs, logPath := startMCP(t, "--allow-destructive")

	names := toolNames(t, cs)
	if !slices.Contains(names, "monitor_delete") || !slices.Contains(names, "monitor_create") {
		t.Errorf("--allow-destructive lacks tools: %v", names)
	}
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		if tool.Name == "monitor_delete" && (tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint) {
			t.Error("monitor_delete is not marked destructive")
		}
	}

	// The CLI's confirmation prompt is skipped: the client approves the call.
	out, isErr := callTool(t, cs, "monitor_delete", map[string]any{"monitor": "mon0000cpu1", "jq": ".id"})
	if isErr || out != "mon0000cpu1" {
		t.Errorf("monitor_delete: %q (error %v)", out, isErr)
	}
	if b, _ := os.ReadFile(logPath); !strings.Contains(string(b), "DELETE /api/v0/monitors/mon0000cpu1") {
		t.Errorf("request log:\n%s", b)
	}
}

// Tool calls may run concurrently; each one builds its own command tree.
func TestMCPConcurrentCalls(t *testing.T) {
	cs, _ := startMCP(t)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tool, args, want := "host_get", map[string]any{"host": "web-1", "jq": ".id"}, "exampleHst1"
			if i%2 == 1 {
				tool, args, want = "dashboard_list", map[string]any{"jq": ".[0].title"}, "Overview"
			}
			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
			if err != nil {
				t.Error(err)
				return
			}
			if got := res.Content[0].(*mcp.TextContent).Text; res.IsError || got != want {
				t.Errorf("%s: %q", tool, got)
			}
		}()
	}
	wg.Wait()
}
