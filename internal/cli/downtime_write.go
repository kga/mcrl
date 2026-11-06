package cli

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/mackerel"
	"github.com/kga/mcrl/internal/timeexpr"
)

var weekdayNames = map[string]string{
	"sun": "Sunday", "mon": "Monday", "tue": "Tuesday", "wed": "Wednesday",
	"thu": "Thursday", "fri": "Friday", "sat": "Saturday",
}

type downtimeFlags struct {
	file, name, memo, start, duration string
	recurrence, until                 string
	interval                          int
	services, roles, monitors         []string
	exServices, exRoles, exMonitors   []string
}

func (d *downtimeFlags) bind(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringVarP(&d.file, "file", "f", "", `JSON or YAML file in the API's format ("-" for stdin); flags override its fields`)
	fl.StringVar(&d.name, "name", "", "name")
	fl.StringVar(&d.memo, "memo", "", "memo")
	fl.StringVar(&d.start, "start", "", `start time (default "now" on create)`)
	fl.StringVar(&d.duration, "duration", "", `length, e.g. "2h", "90m", "1d"`)
	fl.StringVar(&d.recurrence, "recurrence", "", `repeat: hourly, daily, weekly, monthly or yearly; weekly takes days, e.g. "weekly:mon,thu"`)
	fl.IntVar(&d.interval, "interval", 1, "repeat every N hours/days/weeks/... (with --recurrence)")
	fl.StringVar(&d.until, "until", "", "end of the recurrence (with --recurrence)")
	fl.StringArrayVar(&d.services, "service", nil, "target service (repeatable)")
	fl.StringArrayVar(&d.roles, "role", nil, "target role as <service>:<role> (repeatable)")
	fl.StringArrayVar(&d.monitors, "monitor", nil, "target monitor, ID or name (repeatable)")
	fl.StringArrayVar(&d.exServices, "exclude-service", nil, "excluded service (repeatable)")
	fl.StringArrayVar(&d.exRoles, "exclude-role", nil, "excluded role (repeatable)")
	fl.StringArrayVar(&d.exMonitors, "exclude-monitor", nil, "excluded monitor (repeatable)")
}

// apply sets the changed flags on body.
func (d *downtimeFlags) apply(ctx context.Context, cmd *cobra.Command, c *mackerel.Client, body map[string]any) error {
	ch := cmd.Flags().Changed
	now := time.Now()
	if ch("name") {
		body["name"] = d.name
	}
	if ch("memo") {
		body["memo"] = d.memo
	}
	if ch("start") {
		t, err := timeexpr.Parse(d.start, now, nil)
		if err != nil {
			return &cmdutil.UsageError{Err: err}
		}
		body["start"] = t.Unix()
	}
	if ch("duration") {
		dur, err := timeexpr.ParseDuration(d.duration)
		if err != nil {
			return &cmdutil.UsageError{Err: err}
		}
		if dur < time.Minute || dur%time.Minute != 0 {
			return cmdutil.Usagef("--duration must be a whole number of minutes, got %s", d.duration)
		}
		body["duration"] = int64(dur / time.Minute)
	}
	if ch("recurrence") {
		r, err := parseRecurrence(d.recurrence, d.interval)
		if err != nil {
			return err
		}
		if ch("until") {
			t, err := timeexpr.Parse(d.until, now, nil)
			if err != nil {
				return &cmdutil.UsageError{Err: err}
			}
			r["until"] = t.Unix()
		}
		body["recurrence"] = r
	} else if ch("interval") || ch("until") {
		return cmdutil.Usagef("--interval and --until need --recurrence")
	}
	for flag, key := range map[string]string{"service": "serviceScopes", "exclude-service": "serviceExcludeScopes", "role": "roleScopes", "exclude-role": "roleExcludeScopes"} {
		if ch(flag) {
			v, _ := cmd.Flags().GetStringArray(flag)
			body[key] = v
		}
	}
	for flag, key := range map[string]string{"monitor": "monitorScopes", "exclude-monitor": "monitorExcludeScopes"} {
		if ch(flag) {
			refs, _ := cmd.Flags().GetStringArray(flag)
			ids := make([]string, len(refs))
			for i, r := range refs {
				id, err := c.ResolveID(ctx, mackerel.KindMonitor, r)
				if err != nil {
					return err
				}
				ids[i] = id
			}
			body[key] = ids
		}
	}
	return nil
}

func parseRecurrence(s string, interval int) (map[string]any, error) {
	typ, days, hasDays := strings.Cut(strings.ToLower(s), ":")
	switch typ {
	case "hourly", "daily", "weekly", "monthly", "yearly":
	default:
		return nil, cmdutil.Usagef("invalid --recurrence %q (hourly, daily, weekly[:mon,...], monthly, yearly)", s)
	}
	if interval < 1 {
		return nil, cmdutil.Usagef("--interval must be 1 or more")
	}
	r := map[string]any{"type": typ, "interval": interval}
	if hasDays {
		if typ != "weekly" {
			return nil, cmdutil.Usagef("days can be given only to weekly recurrence")
		}
		var wd []string
		for _, d := range strings.Split(days, ",") {
			key := strings.TrimSpace(d)
			if len(key) >= 3 {
				key = key[:3]
			}
			name, ok := weekdayNames[key]
			if !ok {
				return nil, cmdutil.Usagef("invalid weekday %q in --recurrence", d)
			}
			wd = append(wd, name)
		}
		r["weekdays"] = wd
	}
	return r, nil
}

func newDowntimeCreateCmd(f *cmdutil.Factory) *cobra.Command {
	var d downtimeFlags
	cmd := &cobra.Command{
		Use:     "create --name <name> --duration <duration> [flags]",
		Aliases: []string{"new"},
		Short:   "Create a downtime",
		Long: withAPI(`Create a downtime. Without a scope flag the downtime applies to every monitor.

--start accepts the same expressions as --since elsewhere (default: now).`, "Read, Write", "POST /api/v0/downtimes"),
		Example: `  # Silence the web service for the next 2 hours
  mcrl downtime create --name "emergency maintenance" --duration 2h --service web

  # Every Monday and Thursday 02:00-03:00 until the end of the year
  mcrl downtime create --name backup --start "2026-11-09 02:00" --duration 1h --recurrence weekly:mon,thu --until 2026-12-31`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := map[string]any{}
			if d.file != "" {
				var err error
				if body, err = readObject(f, d.file); err != nil {
					return err
				}
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			if err := d.apply(cmd.Context(), cmd, c, body); err != nil {
				return err
			}
			if _, ok := body["start"]; !ok {
				body["start"] = time.Now().Unix()
			}
			for _, k := range []string{"name", "duration"} {
				if _, ok := body[k]; !ok {
					return cmdutil.Usagef("--%s is required", k)
				}
			}
			return simpleSend(cmd, f, post("/downtimes", body))
		},
	}
	d.bind(cmd)
	return cmd
}

func newDowntimeUpdateCmd(f *cmdutil.Factory) *cobra.Command {
	var d downtimeFlags
	cmd := &cobra.Command{
		Use:   "update <downtime> [flags]",
		Short: "Update a downtime",
		Long: withAPI(`Update a downtime. The current downtime is fetched and the given flags
(and -f fields) are changed; everything else is kept.`, "Read, Write", "GET /api/v0/downtimes", "PUT /api/v0/downtimes/<id>"),
		Example:           "  mcrl downtime update backup --duration 90m\n  mcrl downtime update 3Ja1bAxcdef --service web --service batch",
		Args:              cmdutil.ExactArgs(1),
		ValidArgsFunction: completeKind(f, mackerel.KindDowntime),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			cur, err := c.Resolve(cmd.Context(), mackerel.KindDowntime, args[0])
			if err != nil {
				return err
			}
			body, err := decodeMap(cur)
			if err != nil {
				return err
			}
			if d.file != "" {
				patch, err := readObject(f, d.file)
				if err != nil {
					return err
				}
				for k, v := range patch {
					body[k] = v
				}
			}
			if err := d.apply(cmd.Context(), cmd, c, body); err != nil {
				return err
			}
			id := mackerel.StringField(cur, "id")
			delete(body, "id")
			return simpleSend(cmd, f, put("/downtimes/"+url.PathEscape(id), body))
		},
	}
	d.bind(cmd)
	return cmd
}

func newDowntimeDeleteCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:               "delete <downtime>",
		Aliases:           []string{"rm"},
		Short:             "Delete a downtime",
		Long:              withAPI("Delete a downtime. The output is the downtime as it was before deletion.", "Read, Write", "DELETE /api/v0/downtimes/<id>"),
		Example:           "  mcrl downtime delete backup\n  mcrl downtime delete 3Ja1bAxcdef --yes",
		Args:              cmdutil.ExactArgs(1),
		ValidArgsFunction: completeKind(f, mackerel.KindDowntime),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			id, err := c.ResolveID(cmd.Context(), mackerel.KindDowntime, args[0])
			if err != nil {
				return err
			}
			if !f.DryRun {
				if err := confirm(f, fmt.Sprintf("Delete downtime %q (%s)?", args[0], id)); err != nil {
					return err
				}
			}
			return simpleSend(cmd, f, del("/downtimes/"+url.PathEscape(id)))
		},
	}
}
