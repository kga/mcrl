package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/kga/mcrl/internal/build"
	"github.com/kga/mcrl/internal/cli/cmdutil"
)

// mcpKind decides when a command is exposed as an MCP tool (docs/decisions.md D20).
type mcpKind int

const (
	// mcpRead commands only read. Always exposed.
	mcpRead mcpKind = iota
	// mcpWrite commands change something without asking for confirmation in
	// the CLI. Exposed with --allow-write.
	mcpWrite
	// mcpDestructive commands ask for confirmation in the CLI (delete,
	// retire, ...). Exposed only with --allow-destructive.
	mcpDestructive
)

// mcpToolSpec maps a command to an MCP tool.
type mcpToolSpec struct {
	path     string // command path without "mcrl", e.g. "host get"
	kind     mcpKind
	additive bool     // mcpWrite only: creates or appends without overwriting (destructiveHint: false)
	fixed    []string // arguments always passed
	hide     []string // flags not exposed
	args     *mcpArg  // positional arguments when Use cannot describe them
	note     string   // added to the description
}

// mcpArg describes one positional argument.
type mcpArg struct {
	name     string // property name
	use      string // as written in Use, e.g. "<host>"
	variadic bool
}

// mcpTools lists every command exposed over MCP. Commands that are not here
// are listed in mcpExcluded with the reason; a test makes sure each leaf
// command is in one of the two.
var mcpTools = []mcpToolSpec{
	{path: "org get"},
	{path: "org ip-restriction get"},
	{path: "service list"},
	{path: "role list"},
	{path: "host list"},
	{path: "host get"},
	{path: "host monitor-status"},
	{path: "metric get"},
	{path: "metric latest"},
	{path: "metric names"},
	{path: "monitor list"},
	{path: "monitor get"},
	{path: "monitor template"},
	{path: "check list"},
	{path: "check get"},
	{path: "alert list"},
	{path: "alert get"},
	{path: "alert logs"},
	{path: "downtime list"},
	{path: "downtime get"},
	{path: "alert-group list"},
	{path: "alert-group get"},
	{path: "channel list"},
	{path: "channel get"},
	{path: "notification-group list"},
	{path: "notification-group get"},
	{path: "dashboard list"},
	{path: "dashboard get"},
	{path: "annotation list"},
	{path: "log search"},
	{path: "log saved"},
	{path: "trace list"},
	{path: "trace get"},
	{path: "apm http"},
	{path: "apm db"},
	{path: "user list"},
	{path: "invitation list"},
	{path: "metadata list"},
	{path: "metadata get"},
	{path: "aws list"},
	{path: "aws get"},
	{path: "aws excludable-metrics"},
	{path: "audit", hide: []string{"fail-on", "now"}},
	{
		path: "api", fixed: []string{"--method=GET"}, hide: []string{"method", "file", "header", "include"},
		note: "Over MCP, only GET requests can be sent.",
	},

	{path: "alert close", kind: mcpWrite},
	{path: "alert memo", kind: mcpWrite},
	{path: "service create", kind: mcpWrite, additive: true},
	{path: "role create", kind: mcpWrite, additive: true},
	{path: "host create", kind: mcpWrite, additive: true},
	{path: "host update", kind: mcpWrite},
	{path: "host status", kind: mcpWrite},
	{path: "host role add", kind: mcpWrite, additive: true},
	{path: "host role set", kind: mcpWrite},
	{path: "host role remove", kind: mcpWrite},
	{path: "monitor create", kind: mcpWrite, additive: true},
	{path: "monitor update", kind: mcpWrite},
	{path: "monitor mute", kind: mcpWrite},
	{path: "monitor unmute", kind: mcpWrite},
	{path: "downtime create", kind: mcpWrite, additive: true},
	{path: "downtime update", kind: mcpWrite},
	{
		path: "metric post", kind: mcpWrite, additive: true,
		args: &mcpArg{name: "values", use: "<name>=<value>[@<time>]", variadic: true},
	},
	{path: "metric graph-def create", kind: mcpWrite},
	{path: "check report", kind: mcpWrite, additive: true},
	{path: "annotation create", kind: mcpWrite, additive: true},
	{path: "annotation update", kind: mcpWrite},
	{path: "channel create", kind: mcpWrite, additive: true},
	{path: "channel update", kind: mcpWrite},
	{path: "notification-group create", kind: mcpWrite, additive: true},
	{path: "notification-group update", kind: mcpWrite},
	{path: "alert-group create", kind: mcpWrite, additive: true},
	{path: "alert-group update", kind: mcpWrite},
	{path: "dashboard create", kind: mcpWrite, additive: true},
	{path: "dashboard update", kind: mcpWrite},
	{path: "aws create", kind: mcpWrite, additive: true},
	{path: "aws update", kind: mcpWrite},
	{path: "aws external-id", kind: mcpWrite, additive: true},
	{path: "metadata set", kind: mcpWrite},
	{path: "invitation create", kind: mcpWrite, additive: true},

	{path: "service delete", kind: mcpDestructive},
	{path: "role delete", kind: mcpDestructive},
	{path: "host retire", kind: mcpDestructive},
	{path: "monitor delete", kind: mcpDestructive},
	{path: "downtime delete", kind: mcpDestructive},
	{path: "annotation delete", kind: mcpDestructive},
	{path: "channel delete", kind: mcpDestructive},
	{path: "notification-group delete", kind: mcpDestructive},
	{path: "alert-group delete", kind: mcpDestructive},
	{path: "dashboard delete", kind: mcpDestructive},
	{path: "aws delete", kind: mcpDestructive},
	{path: "metadata delete", kind: mcpDestructive},
	{path: "metric graph-def delete", kind: mcpDestructive},
	{path: "user remove", kind: mcpDestructive},
	{path: "invitation revoke", kind: mcpDestructive},
	{path: "org ip-restriction set", kind: mcpDestructive},
}

// mcpExcluded lists leaf commands that are never exposed, with the reason.
var mcpExcluded = map[string]string{
	"alert-group edit":        "opens $EDITOR",
	"aws edit":                "opens $EDITOR",
	"channel edit":            "opens $EDITOR",
	"dashboard edit":          "opens $EDITOR",
	"downtime edit":           "opens $EDITOR",
	"host edit":               "opens $EDITOR",
	"monitor edit":            "opens $EDITOR",
	"notification-group edit": "opens $EDITOR",
	"log tail":                "never ends",
	"annotation wrap":         "runs a local command",
	"check run":               "runs a local command",
	"auth list":               "manages local credentials",
	"auth login":              "manages local credentials",
	"auth logout":             "manages local credentials",
	"auth status":             "manages local credentials",
	"auth switch":             "manages local credentials",
	"auth token":              "prints the API key",
	"config clear-cache":      "local configuration",
	"config path":             "local configuration",
	"doctor":                  "local diagnostics",
	"version":                 "local information",
	"mcp":                     "the server itself",
	"exit-codes":              "help topic",
	"environment":             "help topic",
}

// MCP property names shared by all tools.
const (
	mcpPropJQ     = "jq"
	mcpPropDryRun = "dry_run"
	mcpPropBody   = "body"
)

type mcpOptions struct {
	allowWrite       bool
	allowDestructive bool
}

func newMCPCmd(f *cmdutil.Factory) *cobra.Command {
	o := &mcpOptions{}
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run an MCP server on stdio",
		Long: `Run a Model Context Protocol (MCP) server on stdin and stdout, exposing mcrl
commands as tools to AI agents such as Claude Code and Claude Desktop.

Each tool runs the mcrl command of the same name (e.g. "host_get" runs
"mcrl host get") and returns its JSON output. Tool arguments correspond to
the command's flags and positional arguments, plus "jq" for --jq. Errors are
returned as a tool error with the same JSON as stderr ({"error": {"code",
"message", ...}}) and the exit code in "exitCode" (see "mcrl help exit-codes").

By default only read-only commands are exposed. --allow-write adds commands
that create or update resources, with a "dry_run" argument for --dry-run.
Commands that ask for confirmation in the CLI (delete, host retire, user
remove, invitation revoke, org ip-restriction set, ...) are exposed only with
--allow-destructive, and then run without asking: the MCP client's approval
of the tool call takes the place of the prompt. Commands that open an editor,
run a local command, never end (log tail) or manage credentials are never
exposed.

Global flags given to "mcrl mcp" (--profile, --timeout, --debug) apply to
every tool call. With --debug, HTTP logs go to the server's stderr.`,
		Example: `  claude mcp add mcrl -- mcrl mcp
  claude mcp add mcrl-prod -- mcrl --profile prod mcp
  claude mcp add mcrl-write -- mcrl mcp --allow-write`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := newMCPServer(f, o)
			if err != nil {
				return err
			}
			t := &mcp.IOTransport{Reader: io.NopCloser(f.IO.In), Writer: nopWriteCloser{f.IO.Out}}
			return s.Run(cmd.Context(), t)
		},
	}
	cmd.Flags().BoolVar(&o.allowWrite, "allow-write", false, "also expose commands that create or update resources")
	cmd.Flags().BoolVar(&o.allowDestructive, "allow-destructive", false, "also expose commands that delete or retire resources (implies --allow-write)")
	return cmd
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

const mcpInstructions = `mcrl exposes the Mackerel API. Each tool runs the mcrl command of the same name and returns its JSON output unchanged.
Resources can usually be given by ID or by name. Use the "jq" argument to keep large outputs small (e.g. ".[] | {id, name}"), and "limit" where available.
A failed call returns {"error": {"code", "message", "hint", "exitCode"}}; exitCode follows mcrl's exit codes (2 usage, 3 auth, 4 not found, 5 invalid, 6 rate limited, 7 network).`

// newMCPServer builds the server with the tools allowed by o.
func newMCPServer(f *cmdutil.Factory, o *mcpOptions) (*mcp.Server, error) {
	tools, err := buildMCPTools(o)
	if err != nil {
		return nil, err
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "mcrl", Version: build.Get().Version}, &mcp.ServerOptions{
		Instructions: mcpInstructions,
		Capabilities: &mcp.ServerCapabilities{},
	})
	r := &mcpRunner{f: f}
	for _, t := range tools {
		s.AddTool(t.tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return r.call(ctx, t, req.Params.Arguments), nil
		})
	}
	return s, nil
}

// mcpTool is a built tool: its definition and how to turn arguments into a
// command line.
type mcpTool struct {
	spec     mcpToolSpec
	tool     *mcp.Tool
	resolved *jsonschema.Resolved
	args     []mcpArg
	flags    map[string]*pflag.Flag // property name -> flag
}

// buildMCPTools derives the tools from the command tree.
func buildMCPTools(o *mcpOptions) ([]*mcpTool, error) {
	root := NewRootCmd(&cmdutil.Factory{IO: &cmdutil.IOStreams{In: strings.NewReader(""), Out: io.Discard, ErrOut: io.Discard}})
	var out []*mcpTool
	for _, spec := range mcpTools {
		switch spec.kind {
		case mcpWrite:
			if !o.allowWrite && !o.allowDestructive {
				continue
			}
		case mcpDestructive:
			if !o.allowDestructive {
				continue
			}
		}
		t, err := buildMCPTool(root, spec)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func mcpToolName(path string) string {
	return strings.NewReplacer(" ", "_", "-", "_").Replace(path)
}

var mcpNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func mcpPropName(s string) string {
	s = mcpNonAlnum.ReplaceAllString(strings.ToLower(s), "_")
	return strings.Trim(s, "_")
}

func findCommand(root *cobra.Command, path string) (*cobra.Command, error) {
	c, rest, err := root.Find(strings.Fields(path))
	if err != nil || len(rest) > 0 || c.CommandPath() != "mcrl "+path {
		return nil, fmt.Errorf("mcp: command %q not found", path)
	}
	return c, nil
}

// parseUseArgs reads the positional arguments from a Use line such as
// "close <alert-id>... --reason <text>". It stops at the first flag or
// group ("--x", "(", "[").
func parseUseArgs(use string) []mcpArg {
	var args []mcpArg
	for _, tok := range strings.Fields(use)[1:] {
		if !strings.HasPrefix(tok, "<") {
			break
		}
		a := mcpArg{use: tok}
		if strings.HasSuffix(tok, "...") {
			a.variadic = true
			a.use = strings.TrimSuffix(tok, "...")
		}
		a.name = mcpPropName(strings.NewReplacer("<", "", ">", "").Replace(a.use))
		args = append(args, a)
	}
	return args
}

func buildMCPTool(root *cobra.Command, spec mcpToolSpec) (*mcpTool, error) {
	cmd, err := findCommand(root, spec.path)
	if err != nil {
		return nil, err
	}
	t := &mcpTool{spec: spec, flags: map[string]*pflag.Flag{}}
	schema := &jsonschema.Schema{
		Type:                 "object",
		Properties:           map[string]*jsonschema.Schema{},
		AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
	}
	add := func(name string, s *jsonschema.Schema) error {
		if _, dup := schema.Properties[name]; dup {
			return fmt.Errorf("mcp: %s: duplicate property %q", spec.path, name)
		}
		schema.Properties[name] = s
		return nil
	}

	if spec.args != nil {
		t.args = []mcpArg{*spec.args}
	} else {
		t.args = parseUseArgs(cmd.Use)
	}
	for _, a := range t.args {
		s := &jsonschema.Schema{Type: "string", Description: "positional argument " + a.use}
		if a.variadic {
			s = &jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Type: "string"}, MinItems: jsonschema.Ptr(1),
				Description: "positional arguments " + a.use + "... (one or more)"}
		}
		if err := add(a.name, s); err != nil {
			return nil, err
		}
		schema.Required = append(schema.Required, a.name)
	}

	var flagErr error
	cmd.LocalNonPersistentFlags().VisitAll(func(fl *pflag.Flag) {
		if flagErr != nil || fl.Hidden || slices.Contains(spec.hide, fl.Name) {
			return
		}
		name := mcpPropName(fl.Name)
		var s *jsonschema.Schema
		switch typ := fl.Value.Type(); {
		case fl.Name == "file":
			// Files are read on the server's machine; pass the content instead.
			name = mcpPropBody
			s = &jsonschema.Schema{Description: `content of --file, sent on stdin as "--file -": a JSON value, or a string of JSON or YAML. --file: ` + fl.Usage}
		case typ == "bool":
			s = &jsonschema.Schema{Type: "boolean", Description: fl.Usage}
		case typ == "int":
			s = &jsonschema.Schema{Type: "integer", Description: fl.Usage}
		case typ == "stringArray" || typ == "stringSlice":
			s = &jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Type: "string"}, Description: fl.Usage}
		case typ == "string" || typ == "duration":
			s = &jsonschema.Schema{Type: "string", Description: fl.Usage}
		default:
			flagErr = fmt.Errorf("mcp: %s: unsupported type %s of --%s", spec.path, typ, fl.Name)
			return
		}
		if fl.DefValue != "" && fl.DefValue != "false" && fl.DefValue != "[]" && fl.DefValue != "0s" {
			s.Description += " (default " + fl.DefValue + ")"
		}
		s.Description = "--" + fl.Name + ": " + s.Description
		if err := add(name, s); err != nil {
			flagErr = err
			return
		}
		t.flags[name] = fl
		if ann := fl.Annotations[cobra.BashCompOneRequiredFlag]; len(ann) > 0 && ann[0] == "true" {
			schema.Required = append(schema.Required, name)
		}
	})
	if flagErr != nil {
		return nil, flagErr
	}

	jq := root.PersistentFlags().Lookup("jq")
	if err := add(mcpPropJQ, &jsonschema.Schema{Type: "string", Description: "--jq: " + jq.Usage}); err != nil {
		return nil, err
	}
	if spec.kind != mcpRead {
		dr := root.PersistentFlags().Lookup("dry-run")
		if err := add(mcpPropDryRun, &jsonschema.Schema{Type: "boolean", Description: "--dry-run: " + dr.Usage}); err != nil {
			return nil, err
		}
	}

	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: %s: %w", spec.path, err)
	}
	t.resolved = resolved
	t.tool = &mcp.Tool{
		Name:        mcpToolName(spec.path),
		Title:       cmd.Short,
		Description: mcpDescription(cmd, spec),
		InputSchema: schema,
		Annotations: mcpAnnotations(spec),
	}
	return t, nil
}

func mcpDescription(cmd *cobra.Command, spec mcpToolSpec) string {
	d := cmd.Long
	if d == "" {
		d = cmd.Short + "."
	}
	if spec.note != "" {
		d += "\n\n" + spec.note
	}
	return d + "\n\nCLI: " + cmd.UseLine()
}

func mcpAnnotations(spec mcpToolSpec) *mcp.ToolAnnotations {
	no, yes := false, true
	switch spec.kind {
	case mcpRead:
		return &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: &no}
	case mcpWrite:
		if spec.additive {
			return &mcp.ToolAnnotations{DestructiveHint: &no}
		}
		return &mcp.ToolAnnotations{DestructiveHint: &yes}
	default:
		return &mcp.ToolAnnotations{DestructiveHint: &yes}
	}
}

// commandLine turns validated tool arguments into mcrl arguments and stdin.
func (t *mcpTool) commandLine(in map[string]any) (argv []string, stdin []byte, err error) {
	argv = append(strings.Fields(t.spec.path), "--output=json", "--compact", "--color=never", "--no-input")
	argv = append(argv, t.spec.fixed...)
	if t.spec.kind == mcpDestructive {
		argv = append(argv, "--yes")
	}
	if jq, ok := in[mcpPropJQ].(string); ok && jq != "" {
		argv = append(argv, "--jq="+jq)
	}
	if dr, ok := in[mcpPropDryRun].(bool); ok && dr {
		argv = append(argv, "--dry-run")
	}
	for _, name := range slices.Sorted(maps.Keys(t.flags)) {
		v, ok := in[name]
		if !ok || v == nil {
			continue
		}
		fl := t.flags[name]
		if name == mcpPropBody {
			if s, ok := v.(string); ok {
				stdin = []byte(s)
			} else if stdin, err = json.Marshal(v); err != nil {
				return nil, nil, err
			}
			argv = append(argv, "--"+fl.Name+"=-")
			continue
		}
		switch v := v.(type) {
		case []any:
			for _, e := range v {
				argv = append(argv, "--"+fl.Name+"="+mcpScalar(e))
			}
		default:
			argv = append(argv, "--"+fl.Name+"="+mcpScalar(v))
		}
	}
	// Positional arguments come after "--" so that values starting with "-"
	// are not read as flags.
	argv = append(argv, "--")
	for _, a := range t.args {
		switch v := in[a.name].(type) {
		case []any:
			for _, e := range v {
				argv = append(argv, mcpScalar(e))
			}
		default:
			argv = append(argv, mcpScalar(v))
		}
	}
	return argv, stdin, nil
}

func mcpScalar(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

// mcpRunner runs tool calls as mcrl commands.
type mcpRunner struct {
	f        *cmdutil.Factory
	stderrMu sync.Mutex // serializes writes to the server's stderr
}

type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (r *mcpRunner) call(ctx context.Context, t *mcpTool, raw json.RawMessage) *mcp.CallToolResult {
	in := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
		if err := json.Unmarshal(raw, &in); err != nil {
			return mcpError(&cmdutil.UsageError{Err: fmt.Errorf("invalid arguments: %w", err)}, "", "")
		}
	}
	if err := t.resolved.Validate(in); err != nil {
		return mcpError(&cmdutil.UsageError{Err: fmt.Errorf("invalid arguments: %w", err)}, "", "")
	}
	argv, stdin, err := t.commandLine(in)
	if err != nil {
		return mcpError(err, "", "")
	}

	var stdout, stderr bytes.Buffer
	streams := &cmdutil.IOStreams{In: bytes.NewReader(stdin), Out: &stdout, ErrOut: &stderr}
	debug := r.f.DebugEnabled()
	if debug {
		streams.ErrOut = lockedWriter{&r.stderrMu, r.f.IO.ErrOut}
	}
	f := &cmdutil.Factory{IO: streams, Getenv: r.f.Getenv, Keyring: r.f.Keyring, Transport: r.f.Transport}
	root := NewRootCmd(f)
	// Global flags of "mcrl mcp" apply to every call. Flag defaults are
	// already set, so these survive parsing unless argv overrides them.
	f.Profile, f.Timeout, f.Debug = r.f.Profile, r.f.Timeout, r.f.Debug
	root.SetArgs(argv)
	err = root.ExecuteContext(ctx)
	if err != nil && ctx.Err() != nil {
		err = context.Canceled
	}
	if err != nil {
		return mcpError(err, stdout.String(), stderr.String())
	}
	res := &mcp.CallToolResult{}
	out := strings.TrimRight(stdout.String(), "\n")
	if out == "" {
		out = "null"
	}
	res.Content = append(res.Content, &mcp.TextContent{Text: out})
	if s := strings.TrimSpace(stderr.String()); s != "" {
		res.Content = append(res.Content, &mcp.TextContent{Text: "stderr: " + s})
	}
	return res
}

// mcpError reports err as a tool error in the same JSON as the CLI's
// stderr, plus the exit code.
func mcpError(err error, stdout, stderr string) *mcp.CallToolResult {
	c := classify(err)
	msg := err.Error()
	if s := strings.TrimSpace(stderr); s != "" {
		var ee *cmdutil.ExitError
		if errors.As(err, &ee) && ee.Silent {
			msg = s
		}
	}
	type payload struct {
		Code     string `json:"code"`
		Message  string `json:"message"`
		Status   int    `json:"status,omitempty"`
		Hint     string `json:"hint,omitempty"`
		ExitCode int    `json:"exitCode"`
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(struct {
		Error payload `json:"error"`
	}{payload{Code: c.code, Message: msg, Status: c.status, Hint: c.hint, ExitCode: c.exit}})
	res := &mcp.CallToolResult{IsError: true}
	res.Content = append(res.Content, &mcp.TextContent{Text: strings.TrimRight(buf.String(), "\n")})
	if s := strings.TrimSpace(stdout); s != "" {
		res.Content = append(res.Content, &mcp.TextContent{Text: "stdout: " + s})
	}
	return res
}
