package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/mackerel"
	"github.com/kga/mcrl/internal/timeexpr"
)

// ---- metric post ----

type metricValue struct {
	HostID string  `json:"hostId,omitempty"`
	Name   string  `json:"name"`
	Time   int64   `json:"time"`
	Value  float64 `json:"value"`
}

// metricsPerRequest bounds the size of one POST /tsdb body.
const metricsPerRequest = 5000

func newMetricPostCmd(f *cmdutil.Factory) *cobra.Command {
	var t target
	cmd := &cobra.Command{
		Use:   "post (--host <host> | --service <service>) (<name>=<value>[@<time>]... | -)",
		Short: "Post metric values",
		Long: withAPI(`Post host or service metric values.

Values are given as name=value[@time] arguments, or with "-" read from stdin
in the plugin format "name<TAB>value<TAB>epoch" (one per line; any
whitespace separates the fields), or as a JSON array in the API's format.
The time defaults to now.

The API silently drops values older than 24 hours; mcrl warns about them.
Host metric names conventionally start with "custom.".`, "Read, Write", "POST /api/v0/tsdb", "POST /api/v0/services/<name>/tsdb"),
		Example: `  mcrl metric post --host web-1 custom.queue.length=42
  mcrl metric post --service web custom.signups=12@1762390800 custom.logins=340
  ./my-plugin.sh | mcrl metric post --host "$(hostname)" -`,
		Args: cmdutil.MinimumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			now := time.Now()
			var values []metricValue
			for _, a := range args {
				if a == "-" {
					vs, err := readMetrics(f.IO.In, now)
					if err != nil {
						return err
					}
					values = append(values, vs...)
					continue
				}
				v, err := parseMetricArg(a, now)
				if err != nil {
					return err
				}
				values = append(values, v)
			}
			if len(values) == 0 {
				return cmdutil.Usagef("no metric values given")
			}
			old := 0
			for _, v := range values {
				if now.Unix()-v.Time > 24*3600 {
					old++
				}
			}
			if old > 0 {
				f.IO.Warnf("%d value(s) are older than 24 hours; the API accepts but discards them", old)
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			path := "/tsdb"
			if t.service != "" {
				path = "/services/" + url.PathEscape(t.service) + "/tsdb"
			} else {
				id, err := c.ResolveHostID(cmd.Context(), t.host)
				if err != nil {
					return err
				}
				for i := range values {
					values[i].HostID = id
				}
			}
			var reqs []request
			for start := 0; start < len(values); start += metricsPerRequest {
				end := min(start+metricsPerRequest, len(values))
				r := post(path, values[start:end])
				r.Idempotent = true // 同じ時刻の値は上書きされるので再送してよい
				reqs = append(reqs, r)
			}
			bodies, dry, err := sendAll(cmd, f, c, reqs)
			if dry || err != nil {
				return err
			}
			return printOne(f, bodies[len(bodies)-1])
		},
	}
	t.bind(cmd, f)
	return cmd
}

func parseMetricArg(a string, now time.Time) (metricValue, error) {
	name, rest, ok := strings.Cut(a, "=")
	if !ok || name == "" {
		return metricValue{}, cmdutil.Usagef("invalid metric %q (want name=value[@time])", a)
	}
	val, at, hasTime := strings.Cut(rest, "@")
	v, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return metricValue{}, cmdutil.Usagef("invalid value in %q", a)
	}
	ts := now
	if hasTime {
		if ts, err = timeexpr.Parse(at, now, nil); err != nil {
			return metricValue{}, &cmdutil.UsageError{Err: err}
		}
	}
	return metricValue{Name: name, Value: v, Time: ts.Unix()}, nil
}

func readMetrics(r io.Reader, now time.Time) ([]metricValue, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var vs []metricValue
		if err := json.Unmarshal(trimmed, &vs); err != nil {
			return nil, cmdutil.Usagef("invalid JSON metrics on stdin: %v", err)
		}
		for i := range vs {
			if vs[i].Time == 0 {
				vs[i].Time = now.Unix()
			}
		}
		return vs, nil
	}
	var vs []metricValue
	sc := bufio.NewScanner(bytes.NewReader(b))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || len(fields) > 3 {
			return nil, cmdutil.Usagef("stdin line %d: want \"name value [epoch]\", got %q", n, line)
		}
		v, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return nil, cmdutil.Usagef("stdin line %d: invalid value %q", n, fields[1])
		}
		ts := now.Unix()
		if len(fields) == 3 {
			if ts, err = strconv.ParseInt(fields[2], 10, 64); err != nil {
				return nil, cmdutil.Usagef("stdin line %d: invalid epoch %q", n, fields[2])
			}
		}
		vs = append(vs, metricValue{Name: fields[0], Value: v, Time: ts})
	}
	return vs, sc.Err()
}

// ---- check report / run ----

const (
	checkReportsPerRequest = 100
	checkMessageLimit      = 1024
)

var checkStatuses = []string{"OK", "WARNING", "CRITICAL", "UNKNOWN"}

type checkFlags struct {
	host, name           string
	interval             string
	maxAttempts          int
	notificationInterval int64
}

func (cf *checkFlags) bind(cmd *cobra.Command, f *cmdutil.Factory, required bool) {
	cmd.Flags().StringVar(&cf.host, "host", "", "host the check belongs to (required)")
	cmd.Flags().StringVar(&cf.name, "name", "", "check monitor name: letters, digits, - and _ (required)")
	cmd.Flags().StringVar(&cf.interval, "notification-interval", "", `re-notify while not OK, e.g. "30m" (10m or more; default: no re-notification)`)
	cmd.Flags().IntVar(&cf.maxAttempts, "max-check-attempts", 1, "alert after this many consecutive non-OK reports (1-10)")
	if required {
		_ = cmd.MarkFlagRequired("host")
		_ = cmd.MarkFlagRequired("name")
	}
	_ = cmd.RegisterFlagCompletionFunc("host", completeHosts(f))
}

func (cf *checkFlags) validate() error {
	if !validCheckName(cf.name) {
		return cmdutil.Usagef("invalid --name %q: use letters, digits, - and _", cf.name)
	}
	if cf.maxAttempts < 1 || cf.maxAttempts > 10 {
		return cmdutil.Usagef("--max-check-attempts must be 1-10")
	}
	if cf.interval != "" {
		d, err := timeexpr.ParseDuration(cf.interval)
		if err != nil {
			return &cmdutil.UsageError{Err: err}
		}
		if d < 10*time.Minute {
			return cmdutil.Usagef("--notification-interval must be 10m or more")
		}
		cf.notificationInterval = int64(d / time.Minute)
	}
	return nil
}

func (cf *checkFlags) report(hostID, status, message string, at time.Time) map[string]any {
	if len(message) > checkMessageLimit-1 {
		message = message[:checkMessageLimit-4] + "..."
	}
	r := map[string]any{
		"source":           map[string]string{"type": "host", "hostId": hostID},
		"name":             cf.name,
		"status":           status,
		"message":          message,
		"occurredAt":       at.Unix(),
		"maxCheckAttempts": cf.maxAttempts,
	}
	if cf.notificationInterval > 0 {
		r["notificationInterval"] = cf.notificationInterval
	}
	return r
}

func validCheckName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-' || c == '_'
		if !ok {
			return false
		}
	}
	return true
}

func sendCheckReports(cmd *cobra.Command, f *cmdutil.Factory, c *mackerel.Client, reports []any) error {
	var reqs []request
	for start := 0; start < len(reports); start += checkReportsPerRequest {
		end := min(start+checkReportsPerRequest, len(reports))
		r := post("/monitoring/checks/report", map[string]any{"reports": reports[start:end]})
		r.Idempotent = true // 同じホスト・名前・時刻の報告は無視されるので再送してよい
		reqs = append(reqs, r)
	}
	bodies, dry, err := sendAll(cmd, f, c, reqs)
	if dry || err != nil {
		return err
	}
	return printOne(f, bodies[len(bodies)-1])
}

func newCheckReportCmd(f *cmdutil.Factory) *cobra.Command {
	var (
		cf              checkFlags
		status, message string
		file            string
	)
	cmd := &cobra.Command{
		Use:   "report (--host <host> --name <name> --status <status> --message <text> | -f <file>)",
		Short: "Report check monitoring results",
		Long: withAPI(`Report a check result for a host, creating the check monitor on first use.

-f reads a JSON or YAML array of reports in the API's format instead; they are
sent 100 at a time. The API ignores a report whose host, name and time were
already reported, or that is 6 hours or more in the future.`, "Read, Write", "POST /api/v0/monitoring/checks/report"),
		Example: `  mcrl check report --host web-1 --name backup --status OK --message "backup finished"
  mcrl check report --host web-1 --name disk --status WARNING --message "/var 85%" --notification-interval 1h`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if file != "" {
				raw, err := readDocument(f, file)
				if err != nil {
					return err
				}
				var reports []any
				if err := json.Unmarshal(raw, &reports); err != nil {
					return cmdutil.Usagef("%s must contain an array of reports", file)
				}
				c, err := apiClient(cmd.Context(), f)
				if err != nil {
					return err
				}
				return sendCheckReports(cmd, f, c, reports)
			}
			for _, flag := range []string{"host", "name", "status", "message"} {
				if !cmd.Flags().Changed(flag) {
					return cmdutil.Usagef("--%s is required (or use -f)", flag)
				}
			}
			status = strings.ToUpper(status)
			if !contains(checkStatuses, status) {
				return cmdutil.Usagef("invalid --status %q (valid: %s)", status, strings.Join(checkStatuses, ", "))
			}
			if err := cf.validate(); err != nil {
				return err
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			id, err := c.ResolveHostID(cmd.Context(), cf.host)
			if err != nil {
				return err
			}
			return sendCheckReports(cmd, f, c, []any{cf.report(id, status, message, time.Now())})
		},
	}
	cf.bind(cmd, f, false) // -f のときは不要
	cmd.Flags().StringVar(&status, "status", "", "OK, WARNING, CRITICAL or UNKNOWN")
	cmd.Flags().StringVar(&message, "message", "", "message (under 1024 characters)")
	cmd.Flags().StringVarP(&file, "file", "f", "", `JSON or YAML array of reports ("-" for stdin)`)
	_ = cmd.RegisterFlagCompletionFunc("status", cobra.FixedCompletions(checkStatuses, cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

func newCheckRunCmd(f *cmdutil.Factory) *cobra.Command {
	var cf checkFlags
	cmd := &cobra.Command{
		Use:   "run --host <host> --name <name> -- <command> [args...]",
		Short: "Run a command and report its result as a check",
		Long: withAPI(`Run a command and report its result as a check monitoring result.

The exit code maps to the status like Nagios plugins: 0 OK, 1 WARNING,
2 CRITICAL, anything else (or failing to start) UNKNOWN. The command's
output is passed through and its last 1024 characters become the message.

mcrl exits with the command's exit code, so it can replace the command in
cron jobs.`, "Read, Write", "POST /api/v0/monitoring/checks/report"),
		Example: `  # cron: report whether the backup succeeded
  mcrl check run --host "$(hostname)" --name backup -- /usr/local/bin/backup.sh

  # run an existing check plugin
  mcrl check run --host web-1 --name http -- check-http -u https://example.com/`,
		Args: cmdutil.MinimumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cf.validate(); err != nil {
				return err
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			id, err := c.ResolveHostID(cmd.Context(), cf.host)
			if err != nil {
				return err
			}
			start := time.Now()
			code, output := runPassthrough(f, args)
			status := checkStatus(code)
			report := cf.report(id, status, lastChars(output, checkMessageLimit-1), start)
			// Ctrl-C で親の context が取り消されても報告は送る
			ctx, cancel := context.WithTimeout(context.WithoutCancel(cmd.Context()), time.Minute)
			defer cancel()
			cmd.SetContext(ctx)
			if err := sendCheckReports(cmd, f, c, []any{report}); err != nil {
				return err
			}
			if code != 0 {
				return &cmdutil.ExitError{Code: code, Silent: true}
			}
			return nil
		},
	}
	cf.bind(cmd, f, true)
	cmd.Flags().SetInterspersed(false)
	return cmd
}

func checkStatus(code int) string {
	switch code {
	case 0:
		return "OK"
	case 1:
		return "WARNING"
	case 2:
		return "CRITICAL"
	}
	return "UNKNOWN"
}

// runPassthrough runs argv with stdin/stdout/stderr passed through and
// returns its exit code and combined output. A command that cannot start
// yields 127 or 126 like a shell, with the error as output.
func runPassthrough(f *cmdutil.Factory, argv []string) (int, string) {
	var buf bytes.Buffer
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec // 利用者が指定したコマンドを実行するのが目的
	cmd.Stdin = os.Stdin
	cmd.Stdout = io.MultiWriter(f.IO.Out, &buf)
	cmd.Stderr = io.MultiWriter(f.IO.ErrOut, &buf)
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, buf.String()
	case errors.As(err, &ee):
		if ee.ExitCode() < 0 { // シグナルで終了
			return 128 + 15, buf.String() + "\n" + err.Error()
		}
		return ee.ExitCode(), buf.String()
	case errors.Is(err, exec.ErrNotFound):
		return 127, err.Error()
	default:
		return 126, err.Error()
	}
}

func lastChars(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n+3:]
}

// ---- graph annotations ----

type annotationFlags struct {
	service, title, description string
	from, to                    string
	roles                       []string
	file                        string
}

func (a *annotationFlags) bind(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringVar(&a.service, "service", "", "service name")
	fl.StringVar(&a.title, "title", "", "title (up to 250 characters)")
	fl.StringVar(&a.description, "description", "", "description (up to 1024 characters)")
	fl.StringVar(&a.from, "from", "", `start time (default "now")`)
	fl.StringVar(&a.to, "to", "", "end time (default: same as --from)")
	fl.StringArrayVar(&a.roles, "role", nil, "attach to this role of the service instead of the whole service (repeatable)")
	fl.StringVarP(&a.file, "file", "f", "", `JSON or YAML file in the API's format ("-" for stdin); flags override its fields`)
}

func (a *annotationFlags) body(cmd *cobra.Command, f *cmdutil.Factory) (map[string]any, error) {
	body := map[string]any{}
	if a.file != "" {
		var err error
		if body, err = readObject(f, a.file); err != nil {
			return nil, err
		}
	}
	ch := cmd.Flags().Changed
	now := time.Now()
	if ch("service") {
		body["service"] = a.service
	}
	if ch("title") {
		body["title"] = a.title
	}
	if ch("description") {
		body["description"] = a.description
	}
	if ch("role") {
		body["roles"] = a.roles
	}
	if ch("from") || body["from"] == nil {
		expr := a.from
		if expr == "" {
			expr = "now"
		}
		t, err := timeexpr.Parse(expr, now, nil)
		if err != nil {
			return nil, &cmdutil.UsageError{Err: err}
		}
		body["from"] = t.Unix()
	}
	if ch("to") {
		t, err := timeexpr.Parse(a.to, now, nil)
		if err != nil {
			return nil, &cmdutil.UsageError{Err: err}
		}
		body["to"] = t.Unix()
	} else if body["to"] == nil {
		body["to"] = body["from"]
	}
	for _, k := range []string{"service", "title"} {
		if s, _ := body[k].(string); s == "" {
			return nil, cmdutil.Usagef("--%s is required", k)
		}
	}
	if s, _ := body["title"].(string); len([]rune(s)) > 250 {
		return nil, cmdutil.Usagef("--title is longer than 250 characters")
	}
	if s, _ := body["description"].(string); len([]rune(s)) > 1024 {
		return nil, cmdutil.Usagef("--description is longer than 1024 characters")
	}
	return body, nil
}

func newAnnotationCreateCmd(f *cmdutil.Factory) *cobra.Command {
	var a annotationFlags
	cmd := &cobra.Command{
		Use:     "create --service <service> --title <title> [flags]",
		Aliases: []string{"new"},
		Short:   "Create a graph annotation",
		Long:    withAPI("Create a graph annotation. --from defaults to now and --to to --from.", "Read, Write", "POST /api/v0/graph-annotations"),
		Example: `  mcrl annotation create --service web --title "deploy v1.2.3"
  mcrl annotation create --service web --role app --title "load test" --from 13:00 --to 14:30`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := a.body(cmd, f)
			if err != nil {
				return err
			}
			return simpleSend(cmd, f, post("/graph-annotations", body))
		},
	}
	a.bind(cmd)
	return cmd
}

func newAnnotationUpdateCmd(f *cmdutil.Factory) *cobra.Command {
	var a annotationFlags
	cmd := &cobra.Command{
		Use:   "update <annotation-id> --service <service> --title <title> [flags]",
		Short: "Replace a graph annotation",
		Long: withAPI(`Replace a graph annotation. The API has no way to fetch one annotation,
so give every field again (flags or -f), as for create.`, "Read, Write", "PUT /api/v0/graph-annotations/<id>"),
		Example: `  mcrl annotation update 3Ja1bAxcdef --service web --title "deploy v1.2.3 (rolled back)" --from 13:00 --to 13:20`,
		Args:    cmdutil.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := a.body(cmd, f)
			if err != nil {
				return err
			}
			delete(body, "id")
			return simpleSend(cmd, f, put("/graph-annotations/"+url.PathEscape(args[0]), body))
		},
	}
	a.bind(cmd)
	return cmd
}

func newAnnotationDeleteCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:     "delete <annotation-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a graph annotation",
		Long:    withAPI("Delete a graph annotation.", "Read, Write", "DELETE /api/v0/graph-annotations/<id>"),
		Example: "  mcrl annotation delete 3Ja1bAxcdef\n  mcrl annotation list --service web --jq '.[] | select(.title | startswith(\"test\")) | .id' | xargs -n1 mcrl annotation delete --yes",
		Args:    cmdutil.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !f.DryRun {
				if err := confirm(f, fmt.Sprintf("Delete graph annotation %s?", args[0])); err != nil {
					return err
				}
			}
			return simpleSend(cmd, f, del("/graph-annotations/"+url.PathEscape(args[0])))
		},
	}
}

func newAnnotationWrapCmd(f *cmdutil.Factory) *cobra.Command {
	var a annotationFlags
	cmd := &cobra.Command{
		Use:   "wrap --service <service> --title <title> -- <command> [args...]",
		Short: "Run a command and record it as a graph annotation",
		Long: withAPI(`Run a command and record its run time as a graph annotation.

The annotation is created when the command starts and updated with the end
time when it finishes. If the command fails, " (failed: exit N)" is added
to the title. The command's output is passed through, a note about the
annotation goes to stderr, and mcrl exits with the command's exit code.`,
			"Read, Write", "POST /api/v0/graph-annotations", "PUT /api/v0/graph-annotations/<id>"),
		Example: `  mcrl annotation wrap --service web --title "deploy $(git rev-parse --short HEAD)" -- ./deploy.sh
  mcrl annotation wrap --service db --role primary --title "VACUUM" -- psql -c 'VACUUM ANALYZE'`,
		Args: cmdutil.MinimumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("from") || cmd.Flags().Changed("to") {
				return cmdutil.Usagef("--from and --to are set from the command's run time")
			}
			body, err := a.body(cmd, f)
			if err != nil {
				return err
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			if f.DryRun {
				return dryRun(f, []request{post("/graph-annotations", body)})
			}
			created, err := post("/graph-annotations", body).send(cmd, c)
			if err != nil {
				return fmt.Errorf("creating the annotation (the command was not run): %w", err)
			}
			id := mackerel.StringField(created, "id")
			fmt.Fprintf(f.IO.ErrOut, "mcrl: created graph annotation %s\n", id)

			code, _ := runPassthrough(f, args)

			body["to"] = time.Now().Unix()
			if code != 0 {
				body["title"] = fmt.Sprintf("%s (failed: exit %d)", body["title"], code)
			}
			ctx, cancel := context.WithTimeout(context.WithoutCancel(cmd.Context()), time.Minute)
			defer cancel()
			cmd.SetContext(ctx)
			if _, err := put("/graph-annotations/"+url.PathEscape(id), body).send(cmd, c); err != nil {
				f.IO.Warnf("the command finished, but updating annotation %s failed: %v", id, err)
				if code == 0 {
					return err
				}
			}
			if code != 0 {
				return &cmdutil.ExitError{Code: code, Silent: true}
			}
			return nil
		},
	}
	a.bind(cmd)
	cmd.Flags().SetInterspersed(false)
	return cmd
}
