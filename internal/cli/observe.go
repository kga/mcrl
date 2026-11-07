package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/mackerel"
	"github.com/kga/mcrl/internal/output"
	"github.com/kga/mcrl/internal/timeexpr"
)

// emitItems prints up to limit items (0 = all). With -o jsonl and no --jq,
// each item is printed as soon as it arrives, so long searches stream.
func emitItems(f *cmdutil.Factory, seq iter.Seq2[json.RawMessage, error], limit int) error {
	if err := limitUsage(limit); err != nil {
		return err
	}
	p, err := f.Printer()
	if err != nil {
		return err
	}
	if p.Format == output.FormatJSONL {
		n := 0
		for it, err := range seq {
			if err != nil {
				return err
			}
			if err := p.PrintRaw(it); err != nil {
				return err
			}
			if n++; limit > 0 && n >= limit {
				break
			}
		}
		return nil
	}
	items, err := collect(seq, limit, nil)
	if err != nil {
		return err
	}
	return printItems(f, items)
}

// ---- attribute conditions (--where / --attr) ----

var whereRe = regexp.MustCompile(`^(.+?)(>=|<=|!=|\^=|=|>|<)(.*)$`)

var whereOps = map[string]string{"=": "EQ", "!=": "NEQ", ">": "GT", ">=": "GTE", "<": "LT", "<=": "LTE", "^=": "STARTS_WITH"}

// parseWhere parses "a.b>=500", "level=error", "path^=/api" or
// "code=200:string" into {keyPath|key, operator, value, type}. The type is
// inferred from the value unless given with a ":string", ":int", ":double"
// or ":bool" suffix.
func parseWhere(s string, keyPath bool) (map[string]any, error) {
	m := whereRe.FindStringSubmatch(s)
	if m == nil || strings.TrimSpace(m[1]) == "" {
		return nil, cmdutil.Usagef("invalid condition %q (want key<op>value with op =, !=, >, >=, <, <=, ^=)", s)
	}
	key, opSym, val := strings.TrimSpace(m[1]), m[2], m[3]
	typ := ""
	if i := strings.LastIndex(val, ":"); i >= 0 {
		switch t := val[i+1:]; t {
		case "string", "int", "double", "bool":
			typ, val = t, val[:i]
		}
	}
	if typ == "" {
		switch {
		case val == "true" || val == "false":
			typ = "bool"
		case isInt(val):
			typ = "int"
		case isFloat(val):
			typ = "double"
		default:
			typ = "string"
		}
		// 大小比較は数値にしか使えないので、数値でなければ文字列として送らない
		if opSym != "=" && opSym != "^=" && typ != "int" && typ != "double" {
			return nil, cmdutil.Usagef("condition %q: %s works only with numbers", s, opSym)
		}
		if opSym == "^=" {
			typ = "string"
		}
	}
	op := whereOps[opSym]
	switch {
	case op == "STARTS_WITH" && typ != "string":
		return nil, cmdutil.Usagef("condition %q: ^= works only with strings", s)
	case op != "EQ" && op != "STARTS_WITH" && typ != "int" && typ != "double":
		return nil, cmdutil.Usagef("condition %q: %s works only with :int or :double", s, opSym)
	}
	cond := map[string]any{"operator": op, "value": val, "type": typ}
	if keyPath {
		cond["keyPath"] = splitKeyPath(key)
	} else {
		cond["key"] = key
	}
	return cond, nil
}

// splitKeyPath splits "a.b.c" at dots; "\." keeps a literal dot.
func splitKeyPath(s string) []string {
	var parts []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == '.':
			cur.WriteByte('.')
			i++
		case s[i] == '.':
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(s[i])
		}
	}
	return append(parts, cur.String())
}

func isInt(s string) bool { _, err := strconv.ParseInt(s, 10, 64); return err == nil }
func isFloat(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil && !strings.ContainsAny(s, "xXnN")
}

// ---- logs ----

var logSeverities = []string{"UNSPECIFIED", "TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL"}

// parseSeverities accepts names and "warn+" (that level and above).
func parseSeverities(in []string) ([]string, error) {
	var out []string
	for _, s := range in {
		for _, part := range strings.Split(s, ",") {
			p := strings.ToUpper(strings.TrimSpace(part))
			if p == "" {
				continue
			}
			if p == "WARNING" {
				p = "WARN"
			}
			if base, ok := strings.CutSuffix(p, "+"); ok {
				i := indexOf(logSeverities, base)
				if i < 0 {
					return nil, cmdutil.Usagef("invalid severity %q", s)
				}
				out = append(out, logSeverities[i:]...)
				continue
			}
			if indexOf(logSeverities, p) < 0 {
				return nil, cmdutil.Usagef("invalid severity %q (valid: %s, or e.g. warn+)", s, strings.Join(logSeverities, ", "))
			}
			out = append(out, p)
		}
	}
	return uniqueSorted(out), nil
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

type logFlags struct {
	service, namespace   string
	since, until         string
	keywords, severities []string
	traceID, saved       string
	where                []string
	limit                int
	asc                  bool
	interval             time.Duration
}

func (l *logFlags) bind(cmd *cobra.Command, f *cmdutil.Factory, tail bool) {
	fl := cmd.Flags()
	fl.StringVar(&l.service, "service", "", "service name (the service.name resource attribute)")
	fl.StringVar(&l.namespace, "namespace", "", "service namespace (service.namespace)")
	fl.StringArrayVar(&l.keywords, "keyword", nil, "text the body contains (repeatable; all must match)")
	fl.StringArrayVar(&l.severities, "severity", nil, `severity, comma-separated or repeated; "warn+" means WARN and above`)
	fl.StringVar(&l.traceID, "trace-id", "", "trace ID (32 hex characters)")
	fl.StringArrayVar(&l.where, "where", nil, `condition on a JSON body field, e.g. "res.status>=500", "req.method=GET", "path^=/api" (repeatable)`)
	fl.StringVar(&l.saved, "saved", "", "start from a saved log search (ID or name); flags override its conditions")
	if tail {
		fl.StringVar(&l.since, "since", "1m", "show logs from this time before following")
		fl.DurationVar(&l.interval, "interval", 2*time.Second, "polling interval (1s or more)")
	} else {
		fl.StringVar(&l.since, "since", "15m", "start of the time range (at most 30 days before --until)")
		fl.StringVar(&l.until, "until", "now", "end of the time range (exclusive)")
		fl.IntVar(&l.limit, "limit", 100, "maximum number of log entries (0 = all)")
		fl.BoolVar(&l.asc, "asc", false, "oldest first (default: newest first)")
	}
	_ = cmd.RegisterFlagCompletionFunc("service", func(cmd *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return completeFromList(cmd, f, "/services", "services", nil, "name", "memo"), cobra.ShellCompDirectiveNoFileComp
	})
	_ = cmd.RegisterFlagCompletionFunc("severity", cobra.FixedCompletions([]cobra.Completion{"trace+", "debug+", "info+", "warn+", "error+", "fatal"}, cobra.ShellCompDirectiveNoFileComp))
}

// body builds the POST /logs conditions (without the time range). changed
// tells which flags were given; the others are left to --saved or the API.
func (l *logFlags) body(ctx context.Context, changed func(string) bool, c *mackerel.Client) (map[string]any, error) {
	body := map[string]any{}
	if l.saved != "" {
		def, err := savedSearchDefinition(ctx, c, l.saved)
		if err != nil {
			return nil, err
		}
		for k, v := range def {
			if v != nil {
				body[k] = v
			}
		}
	}
	ch := changed
	if ch("service") {
		body["serviceName"] = l.service
	}
	if ch("namespace") {
		body["serviceNamespace"] = l.namespace
	}
	if s, _ := body["serviceName"].(string); s == "" {
		return nil, cmdutil.Usagef("--service is required (or --saved)")
	}
	if ch("keyword") {
		for _, k := range l.keywords {
			if k == "" {
				return nil, cmdutil.Usagef("--keyword cannot be empty")
			}
		}
		body["keywords"] = l.keywords
	}
	if ch("severity") {
		sev, err := parseSeverities(l.severities)
		if err != nil {
			return nil, err
		}
		body["severities"] = sev
	}
	if ch("trace-id") {
		body["traceId"] = l.traceID
	}
	if ch("where") {
		var conds []map[string]any
		for _, w := range l.where {
			cond, err := parseWhere(w, true)
			if err != nil {
				return nil, err
			}
			conds = append(conds, cond)
		}
		body["bodyAttributes"] = conds
	}
	for _, k := range []string{"keywords", "severities", "bodyAttributes"} {
		if v, ok := body[k].([]any); ok && len(v) == 0 {
			delete(body, k) // 空の配列は API が受け付けない
		}
	}
	return body, nil
}

// setRange adds the time range, order and page size of a log search.
func (l *logFlags) setRange(body map[string]any, from, to int64) {
	body["from"], body["to"] = from, to
	dir := "DESC"
	if l.asc {
		dir = "ASC"
	}
	body["order"] = map[string]string{"column": "TIMESTAMP", "direction": dir}
	body["perPage"] = perPage(l.limit, 1000)
}

func savedSearchDefinition(ctx context.Context, c *mackerel.Client, ref string) (map[string]any, error) {
	items, err := c.List(ctx, "/saved-log-searches", "results", nil)
	if err != nil {
		return nil, err
	}
	var hits []json.RawMessage
	for _, it := range items {
		if mackerel.StringField(it, "id") == ref || mackerel.StringField(it, "name") == ref {
			hits = append(hits, it)
		}
	}
	switch len(hits) {
	case 0:
		return nil, &mackerel.NotFoundError{Kind: "saved log search", Ref: ref}
	case 1:
	default:
		return nil, &mackerel.AmbiguousError{Kind: "saved log search", Ref: ref}
	}
	var s struct {
		Definition map[string]any `json:"definition"`
	}
	if err := json.Unmarshal(hits[0], &s); err != nil {
		return nil, err
	}
	return s.Definition, nil
}

const maxLogRange = 30 * 24 * time.Hour

func newLogCmd(f *cmdutil.Factory) *cobra.Command {
	saved := listCmd(f, "List saved log searches", "/saved-log-searches", "results",
		"  mcrl log saved\n  mcrl log saved --jq '.[] | {name, definition}'")
	saved.Use, saved.Aliases = "saved", nil
	return newGroup("log", "Search logs", []string{"logs"}, newLogSearchCmd(f), newLogTailCmd(f), saved)
}

func newLogSearchCmd(f *cmdutil.Factory) *cobra.Command {
	var l logFlags
	cmd := &cobra.Command{
		Use:   "search --service <service> [flags]",
		Short: "Search logs",
		Long: withAPI(`Search logs of a service, newest first.

The range is --since (default 15 minutes ago) to --until (exclusive), at most
30 days. --keyword matches text in the body; --where matches fields of JSON
bodies, with the type inferred from the value. With -o jsonl the results are
printed as each page arrives. The API allows 1 request per second, so large
--limit values take a while.`, "Read", "POST /api/v0/logs", "GET /api/v0/saved-log-searches (--saved)"),
		Example: `  mcrl log search --service web --severity error+
  mcrl log search --service web --since 1h --where 'res.status>=500' --where 'req.method=POST'
  mcrl log search --saved 'checkout errors' --since 7d --limit 0 -o jsonl > errors.jsonl`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			from, to, err := timeRange(l.since, l.until)
			if err != nil {
				return err
			}
			if time.Duration(to-from)*time.Second > maxLogRange {
				return cmdutil.Usagef("the time range is longer than 30 days")
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			body, err := l.body(cmd.Context(), cmd.Flags().Changed, c)
			if err != nil {
				return err
			}
			l.setRange(body, from, to)
			if f.DryRun {
				return dryRun(f, []request{post("/logs", body)})
			}
			return emitItems(f, c.CursorPages(cmd.Context(), "/logs", body), l.limit)
		},
	}
	l.bind(cmd, f, false)
	return cmd
}

// perPage picks the page size for a limit (0 = all) and an API maximum.
func perPage(limit, maxPerPage int) int {
	if limit > 0 && limit < maxPerPage {
		return limit
	}
	return maxPerPage
}

func newLogTailCmd(f *cmdutil.Factory) *cobra.Command {
	var l logFlags
	cmd := &cobra.Command{
		Use:   "tail --service <service> [flags]",
		Short: "Follow new logs",
		Long: withAPI(`Print recent logs, then keep printing new ones as they arrive, oldest first,
one JSON object per line. Stop with Ctrl-C.

The API is polled every --interval (at least 1 second; the API allows 1
request per second). Logs that arrive late, with a timestamp older than ones
already printed, may be missed.`, "Read", "POST /api/v0/logs"),
		Example: `  mcrl log tail --service web
  mcrl log tail --service web --severity warn+ --jq '[.timestamp, .severity, .body] | @tsv'`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if l.interval < time.Second {
				return cmdutil.Usagef("--interval must be 1s or more")
			}
			start, err := timeexpr.Parse(l.since, time.Now(), nil)
			if err != nil {
				return &cmdutil.UsageError{Err: err}
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			base, err := l.body(cmd.Context(), cmd.Flags().Changed, c)
			if err != nil {
				return err
			}
			p, err := f.Printer()
			if err != nil {
				return err
			}
			p.Format, p.Pretty = output.FormatJSONL, false // 終わりのないコマンドは常に JSONL
			ctx := cmd.Context()
			from := start.Unix()
			seen := map[string]bool{} // 直近の秒の cursor（同じ秒の重複を避ける）
			for {
				body := map[string]any{}
				for k, v := range base {
					body[k] = v
				}
				now := time.Now().Unix()
				body["from"], body["to"] = from, now+1
				body["order"] = map[string]string{"column": "TIMESTAMP", "direction": "ASC"}
				body["perPage"] = 1000
				latest := from
				nextSeen := map[string]bool{}
				for it, err := range c.CursorPages(ctx, "/logs", body) {
					if err != nil {
						if ctx.Err() != nil {
							return ctx.Err()
						}
						return err
					}
					cur := mackerel.StringField(it, "cursor")
					ts, _ := time.Parse(time.RFC3339Nano, mackerel.StringField(it, "timestamp"))
					sec := ts.Unix()
					if sec == from && seen[cur] {
						continue
					}
					if err := p.PrintRaw(it); err != nil {
						return err
					}
					if sec > latest {
						latest, nextSeen = sec, map[string]bool{}
					}
					if sec == latest {
						nextSeen[cur] = true
					}
				}
				if latest == from {
					for k := range nextSeen {
						seen[k] = true
					}
				} else {
					from, seen = latest, nextSeen
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(l.interval):
				}
			}
		},
	}
	l.bind(cmd, f, true)
	return cmd
}

// ---- traces ----

// traceFlags are the conditions of "trace list".
type traceFlags struct {
	service, namespace, env, traceID, spanName, version, status, issue string
	since, until, minLatency, maxLatency, sort                         string
	attrs, resAttrs                                                    []string
	limit                                                              int
	asc                                                                bool
}

// body builds the POST /traces request for the first page. changed tells
// which optional flags were given.
func (t *traceFlags) body(changed func(string) bool, from, to int64) (map[string]any, error) {
	// ドキュメントには書かれていないが、API は 30 分を超える範囲を 400 にする（実測）
	if time.Duration(to-from)*time.Second > maxAPMRange {
		return nil, cmdutil.Usagef("the time range is longer than 30 minutes, the API's limit")
	}
	body := map[string]any{"serviceName": t.service, "from": from, "to": to}
	for flag, kv := range map[string]struct {
		key string
		val *string
	}{"namespace": {"serviceNamespace", &t.namespace}, "env": {"environment", &t.env}, "trace-id": {"traceId", &t.traceID},
		"span-name": {"spanName", &t.spanName}, "version": {"version", &t.version}, "issue": {"issueFingerprint", &t.issue}} {
		if changed(flag) {
			body[kv.key] = *kv.val
		}
	}
	if changed("status") {
		s := strings.ToUpper(t.status)
		if s != "OK" && s != "ERROR" {
			return nil, cmdutil.Usagef("--status must be ok or error")
		}
		body["statusCode"] = s
	}
	for flag, kv := range map[string]struct {
		key string
		val *string
	}{"min-latency": {"minLatencyMillis", &t.minLatency}, "max-latency": {"maxLatencyMillis", &t.maxLatency}} {
		if changed(flag) {
			d, err := timeexpr.ParseDuration(*kv.val)
			if err != nil {
				return nil, &cmdutil.UsageError{Err: err}
			}
			body[kv.key] = d.Milliseconds()
		}
	}
	for key, vals := range map[string][]string{"attributes": t.attrs, "resourceAttributes": t.resAttrs} {
		var conds []map[string]any
		for _, a := range vals {
			cond, err := parseWhere(a, false)
			if err != nil {
				return nil, err
			}
			conds = append(conds, cond)
		}
		if len(conds) > 0 {
			body[key] = conds
		}
	}
	col := map[string]string{"start": "START_AT", "latency": "LATENCY"}[strings.ToLower(t.sort)]
	if col == "" {
		return nil, cmdutil.Usagef("--sort must be start or latency")
	}
	dir := "DESC"
	if t.asc {
		dir = "ASC"
	}
	body["order"] = map[string]string{"column": col, "direction": dir}
	body["perPage"] = perPage(t.limit, 100)
	body["page"] = 1
	return body, nil
}

// tracePages fetches the pages of a trace search, starting from body.
func tracePages(ctx context.Context, c *mackerel.Client, body map[string]any) iter.Seq2[json.RawMessage, error] {
	return mackerel.NumberedPages(func(page int) ([]byte, error) {
		body["page"] = page
		return c.PostJSON(ctx, "/traces", body)
	})
}

func newTraceCmd(f *cmdutil.Factory) *cobra.Command {
	var t traceFlags
	list := &cobra.Command{
		Use:     "list --service <service> [flags]",
		Aliases: []string{"ls"},
		Short:   "Search traces",
		Long: withAPI(`Search traces of a service, newest first (or by latency with --sort latency).
The time range is at most 30 minutes (the API's limit).

--attr and --resource-attr take conditions like "http.route=/users/{id}" or
"http.response.status_code>=500", with the type inferred from the value.`, "Read", "POST /api/v0/traces"),
		Example: `  mcrl trace list --service web --status error
  mcrl trace list --service web --since 1h --min-latency 1s --sort latency --jq '.[] | [.traceId, .traceLatencyMillis, .title] | @tsv'`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			from, to, err := timeRange(t.since, t.until)
			if err != nil {
				return err
			}
			body, err := t.body(cmd.Flags().Changed, from, to)
			if err != nil {
				return err
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			if f.DryRun {
				return dryRun(f, []request{post("/traces", body)})
			}
			return emitItems(f, tracePages(cmd.Context(), c, body), t.limit)
		},
	}
	fl := list.Flags()
	fl.StringVar(&t.service, "service", "", "service name (required)")
	fl.StringVar(&t.namespace, "namespace", "", "service namespace")
	fl.StringVar(&t.env, "env", "", "deployment environment")
	fl.StringVar(&t.traceID, "trace-id", "", "trace ID")
	fl.StringVar(&t.spanName, "span-name", "", "span name")
	fl.StringVar(&t.version, "version", "", "service version")
	fl.StringVar(&t.issue, "issue", "", "issue fingerprint")
	fl.StringVar(&t.status, "status", "", "ok or error")
	fl.StringVar(&t.minLatency, "min-latency", "", `minimum latency, e.g. "500ms", "2s"`)
	fl.StringVar(&t.maxLatency, "max-latency", "", "maximum latency")
	fl.StringArrayVar(&t.attrs, "attr", nil, "span attribute condition (repeatable)")
	fl.StringArrayVar(&t.resAttrs, "resource-attr", nil, "resource attribute condition (repeatable)")
	fl.StringVar(&t.since, "since", "15m", "start of the time range")
	fl.StringVar(&t.until, "until", "now", "end of the time range")
	fl.StringVar(&t.sort, "sort", "start", "start or latency")
	fl.BoolVar(&t.asc, "asc", false, "ascending order")
	fl.IntVar(&t.limit, "limit", 20, "maximum number of traces (0 = all)")
	_ = list.MarkFlagRequired("service")
	_ = list.RegisterFlagCompletionFunc("status", cobra.FixedCompletions([]cobra.Completion{"ok", "error"}, cobra.ShellCompDirectiveNoFileComp))
	_ = list.RegisterFlagCompletionFunc("sort", cobra.FixedCompletions([]cobra.Completion{"start", "latency"}, cobra.ShellCompDirectiveNoFileComp))

	get := &cobra.Command{
		Use:     "get <trace-id>",
		Aliases: []string{"show"},
		Short:   "Show the spans of a trace",
		Long:    withAPI(`Show the spans of a trace as an array, in the API's format.`, "Read", "GET /api/v0/traces/<id>"),
		Example: "  mcrl trace get 5b8efff798038103d269b633813fc60c\n  mcrl trace get 5b8efff798038103d269b633813fc60c --jq '.[] | [.name, .startTime, .endTime] | @tsv'",
		Args:    cmdutil.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			items, err := c.List(cmd.Context(), "/traces/"+url.PathEscape(args[0]), "spans", nil)
			if err != nil {
				return mackerel.AsNotFound(err, "trace", args[0])
			}
			return printItems(f, items)
		},
	}
	return newGroup("trace", "Search traces", []string{"traces"}, list, get)
}

// ---- APM ----

const maxAPMRange = 30 * time.Minute

func newAPMCmd(f *cmdutil.Factory) *cobra.Command {
	mk := func(name, short, path, filterFlag, filterKey, filterHelp string, sorts map[string]string, defaultSort string) *cobra.Command {
		var (
			service, namespace, env, version, filter, method, since, until, sort string
			asc                                                                  bool
			limit                                                                int
		)
		keys := make([]string, 0, len(sorts))
		for k := range sorts {
			keys = append(keys, k)
		}
		keys = uniqueSorted(keys)
		cmd := &cobra.Command{
			Use:   name + " --service <service> [flags]",
			Short: short,
			Long: withAPI(short+`, for at most the last 30 minutes of the range (the API's limit).
Sorted by --sort, descending unless --asc.`, "Read", "GET /api/v0"+path),
			Example: fmt.Sprintf("  mcrl apm %s --service web\n  mcrl apm %s --service web --since 10m --sort %s --limit 5", name, name, keys[0]),
			Args:    cmdutil.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				from, to, err := timeRange(since, until)
				if err != nil {
					return err
				}
				if time.Duration(to-from)*time.Second > maxAPMRange {
					return cmdutil.Usagef("the time range is longer than 30 minutes, the API's limit")
				}
				col, ok := sorts[strings.ToLower(sort)]
				if !ok {
					return cmdutil.Usagef("--sort must be one of %s", strings.Join(keys, ", "))
				}
				q := url.Values{"serviceName": {service}, "from": {strconv.FormatInt(from, 10)}, "to": {strconv.FormatInt(to, 10)},
					"orderColumn": {col}, "perPage": {strconv.Itoa(perPage(limit, 100))}}
				dir := "DESC"
				if asc {
					dir = "ASC"
				}
				q.Set("orderDirection", dir)
				ch := cmd.Flags().Changed
				for flag, kv := range map[string]struct {
					key string
					val *string
				}{"namespace": {"serviceNamespace", &namespace}, "env": {"environment", &env}, "version": {"version", &version}, filterFlag: {filterKey, &filter}, "method": {"method", &method}} {
					if ch(flag) {
						q.Set(kv.key, *kv.val)
					}
				}
				c, err := apiClient(cmd.Context(), f)
				if err != nil {
					return err
				}
				if f.DryRun {
					q.Set("page", "1")
					return dryRun(f, []request{{Method: "GET", Path: path, Query: q}})
				}
				seq := mackerel.NumberedPages(func(page int) ([]byte, error) {
					q.Set("page", strconv.Itoa(page))
					return c.Get(cmd.Context(), path, q)
				})
				return emitItems(f, seq, limit)
			},
		}
		fl := cmd.Flags()
		fl.StringVar(&service, "service", "", "service name (required)")
		fl.StringVar(&namespace, "namespace", "", "service namespace")
		fl.StringVar(&env, "env", "", "deployment environment")
		fl.StringVar(&version, "version", "", "service version")
		fl.StringVar(&filter, filterFlag, "", filterHelp)
		if name == "http" {
			fl.StringVar(&method, "method", "", "HTTP method")
		}
		fl.StringVar(&since, "since", "30m", "start of the time range (at most 30 minutes before --until)")
		fl.StringVar(&until, "until", "now", "end of the time range")
		fl.StringVar(&sort, "sort", defaultSort, "sort column: "+strings.Join(keys, ", "))
		fl.BoolVar(&asc, "asc", false, "ascending order")
		fl.IntVar(&limit, "limit", 20, "maximum number of rows (0 = all)")
		_ = cmd.MarkFlagRequired("service")
		_ = cmd.RegisterFlagCompletionFunc("sort", cobra.FixedCompletions(keys, cobra.ShellCompDirectiveNoFileComp))
		return cmd
	}
	return newGroup("apm", "Show APM statistics", nil,
		mk("http", "Show HTTP server statistics per route", "/apm/http-server-stats", "route", "route", "route (partial match)",
			map[string]string{"p95": "P95", "sum": "SUM", "average": "AVERAGE", "error-rate": "ERROR_RATE", "requests": "REQUEST_COUNT"}, "p95"),
		mk("db", "Show database query statistics", "/apm/db-query-stats", "query", "query", "SQL (partial match)",
			map[string]string{"p95": "P95", "sum": "SUM", "average": "AVERAGE", "executions": "EXECUTION_COUNT"}, "p95"),
	)
}
