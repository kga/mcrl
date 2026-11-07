package cli

import (
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
)

// Every leaf command must be either exposed or excluded on purpose, so that
// a new command forces a decision about MCP.
func TestMCPCoversEveryCommand(t *testing.T) {
	root := NewRootCmd(&cmdutil.Factory{IO: &cmdutil.IOStreams{In: strings.NewReader(""), Out: io.Discard, ErrOut: io.Discard}})
	listed := map[string]bool{}
	for _, s := range mcpTools {
		if listed[s.path] {
			t.Errorf("%q is listed twice", s.path)
		}
		listed[s.path] = true
		if _, ok := mcpExcluded[s.path]; ok {
			t.Errorf("%q is both exposed and excluded", s.path)
		}
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Name() == "help" || c.Name() == "completion" {
			return
		}
		if c.HasSubCommands() {
			for _, s := range c.Commands() {
				walk(s)
			}
			return
		}
		path := strings.TrimPrefix(c.CommandPath(), "mcrl ")
		if !listed[path] && mcpExcluded[path] == "" {
			t.Errorf("command %q is neither in mcpTools nor in mcpExcluded", path)
		}
	}
	walk(root)
	for p := range mcpExcluded {
		if _, err := findCommand(root, p); err != nil {
			t.Errorf("excluded command %q does not exist", p)
		}
	}
}

func TestMCPToolSets(t *testing.T) {
	names := func(o *mcpOptions) []string {
		tools, err := buildMCPTools(o)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, tl := range tools {
			out = append(out, tl.tool.Name)
		}
		return out
	}
	read := names(&mcpOptions{})
	write := names(&mcpOptions{allowWrite: true})
	all := names(&mcpOptions{allowDestructive: true})
	if len(all) != len(mcpTools) {
		t.Errorf("--allow-destructive exposes %d tools, want %d", len(all), len(mcpTools))
	}
	for _, n := range []string{"host_get", "alert_list", "metric_latest", "org_ip_restriction_get", "api"} {
		if !slices.Contains(read, n) {
			t.Errorf("read-only set lacks %s", n)
		}
	}
	for _, n := range []string{"monitor_create", "alert_close", "host_retire", "service_delete"} {
		if slices.Contains(read, n) {
			t.Errorf("read-only set has %s", n)
		}
	}
	if !slices.Contains(write, "monitor_create") || slices.Contains(write, "host_retire") || slices.Contains(write, "monitor_delete") {
		t.Errorf("--allow-write set is wrong: %v", write)
	}
	if !slices.Contains(all, "host_retire") || !slices.Contains(all, "org_ip_restriction_set") {
		t.Errorf("--allow-destructive set is wrong: %v", all)
	}
}

func TestMCPCommandLine(t *testing.T) {
	tools, err := buildMCPTools(&mcpOptions{allowDestructive: true})
	if err != nil {
		t.Fatal(err)
	}
	get := func(name string) *mcpTool {
		for _, tl := range tools {
			if tl.tool.Name == name {
				return tl
			}
		}
		t.Fatalf("no tool %s", name)
		return nil
	}
	tests := []struct {
		tool  string
		in    map[string]any
		argv  string
		stdin string
	}{
		{"host_list", map[string]any{"status": []any{"working", "standby"}, "service": "web", "jq": ".[].id"},
			"host list --output=json --compact --color=never --no-input --jq=.[].id --service=web --status=working --status=standby --", ""},
		{"alert_close", map[string]any{"alert_id": []any{"a1", "-a2"}, "reason": "done", "dry_run": true},
			"alert close --output=json --compact --color=never --no-input --dry-run --reason=done -- a1 -a2", ""},
		{"alert_list", map[string]any{"limit": float64(5), "all": true},
			"alert list --output=json --compact --color=never --no-input --all=true --limit=5 --", ""},
		{"monitor_create", map[string]any{"body": map[string]any{"type": "host"}},
			"monitor create --output=json --compact --color=never --no-input --file=- --", `{"type":"host"}`},
		{"host_retire", map[string]any{"host": []any{"web-1"}},
			"host retire --output=json --compact --color=never --no-input --yes -- web-1", ""},
		{"metric_post", map[string]any{"host": "web-1", "values": []any{"x=1"}},
			"metric post --output=json --compact --color=never --no-input --host=web-1 -- x=1", ""},
		{"api", map[string]any{"path": "/org"},
			"api --output=json --compact --color=never --no-input --method=GET -- /org", ""},
		{"role_create", map[string]any{"service_role": "web:app"},
			"role create --output=json --compact --color=never --no-input -- web:app", ""},
	}
	for _, tt := range tests {
		tl := get(tt.tool)
		if err := tl.resolved.Validate(tt.in); err != nil {
			t.Errorf("%s: %v", tt.tool, err)
			continue
		}
		argv, stdin, err := tl.commandLine(tt.in)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(argv, " "); got != tt.argv {
			t.Errorf("%s:\n got %s\nwant %s", tt.tool, got, tt.argv)
		}
		if string(stdin) != tt.stdin {
			t.Errorf("%s: stdin %q, want %q", tt.tool, stdin, tt.stdin)
		}
	}
	if err := get("host_get").resolved.Validate(map[string]any{}); err == nil {
		t.Error("host_get without host should not validate")
	}
	if err := get("host_get").resolved.Validate(map[string]any{"host": "x", "nope": 1}); err == nil {
		t.Error("unknown property should not validate")
	}
	if _, ok := get("api").flags["method"]; ok {
		t.Error("api must not expose --method")
	}
}
