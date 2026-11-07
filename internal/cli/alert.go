package cli

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/mackerel"
)

var alertStatuses = []string{"OK", "WARNING", "CRITICAL", "UNKNOWN"}

func newAlertCmd(f *cmdutil.Factory) *cobra.Command {
	return newGroup("alert", "Manage alerts", []string{"alerts"},
		newAlertListCmd(f), newAlertGetCmd(f), newAlertLogsCmd(f), newAlertCloseCmd(f), newAlertMemoCmd(f))
}

func newAlertListCmd(f *cmdutil.Factory) *cobra.Command {
	var (
		all             bool
		limit           int
		statuses, types []string
		monitor, host   string
		expand          bool
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List alerts, newest first",
		Long: withAPI(`List alerts, newest first. Only open alerts are listed unless --all.

Pages are fetched until --limit alerts are collected. --status, --type,
--monitor and --host filter on the client side, so they may read more pages.

--expand adds "_expanded": {"monitor": {...}, "host": {...}} to each alert.`, "Read", "GET /api/v0/alerts"),
		Example: `  mcrl alert list
  mcrl alert list --status CRITICAL --expand --jq '.[] | [.id, ._expanded.monitor.name, ._expanded.host.name] | @tsv'
  mcrl alert list --all --limit 0 -o jsonl > alerts.jsonl`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			for i, s := range statuses {
				statuses[i] = strings.ToUpper(s)
				if !contains(alertStatuses, statuses[i]) {
					return cmdutil.Usagef("invalid --status %q (valid: %s)", s, strings.Join(alertStatuses, ", "))
				}
			}
			c, err := apiClient(ctx, f)
			if err != nil {
				return err
			}
			var monitorID, hostID string
			if monitor != "" {
				if monitorID, err = c.ResolveID(ctx, mackerel.KindMonitor, monitor); err != nil {
					return err
				}
			}
			if host != "" {
				if hostID, err = c.ResolveHostID(ctx, host); err != nil {
					return err
				}
			}
			filtered := len(statuses) > 0 || len(types) > 0 || monitorID != "" || hostID != ""
			keep := func(it json.RawMessage) bool {
				return (len(statuses) == 0 || contains(statuses, mackerel.StringField(it, "status"))) &&
					(len(types) == 0 || contains(types, mackerel.StringField(it, "type"))) &&
					(monitorID == "" || mackerel.StringField(it, "monitorId") == monitorID) &&
					(hostID == "" || mackerel.StringField(it, "hostId") == hostID)
			}
			if !filtered {
				keep = nil
			}
			q := url.Values{}
			if all {
				q.Set("withClosed", "true")
			}
			pageLimit := mackerel.PageSize
			if !filtered && limit > 0 && limit < pageLimit {
				pageLimit = limit
			}
			items, err := collect(c.NextIDPages(ctx, "/alerts", "alerts", q, pageLimit), limit, keep)
			if err != nil {
				return err
			}
			if expand {
				if items, err = expandAlerts(cmd, c, items); err != nil {
					return err
				}
			}
			return printItems(f, items)
		},
	}
	fl := cmd.Flags()
	fl.BoolVarP(&all, "all", "a", false, "include closed alerts")
	fl.IntVar(&limit, "limit", 100, "maximum number of alerts (0 = all)")
	fl.StringArrayVar(&statuses, "status", nil, "only alerts in this status (repeatable): OK, WARNING, CRITICAL, UNKNOWN")
	fl.StringArrayVar(&types, "type", nil, "only alerts of this monitor type (repeatable)")
	fl.StringVar(&monitor, "monitor", "", "only alerts of this monitor (ID or name)")
	fl.StringVar(&host, "host", "", "only alerts of this host (ID or name)")
	fl.BoolVar(&expand, "expand", false, "add monitor and host names under _expanded")
	_ = cmd.RegisterFlagCompletionFunc("status", cobra.FixedCompletions(alertStatuses, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("host", completeHosts(f))
	_ = cmd.RegisterFlagCompletionFunc("monitor", completeKind(f, mackerel.KindMonitor))
	return cmd
}

func newAlertGetCmd(f *cmdutil.Factory) *cobra.Command {
	var expand bool
	cmd := &cobra.Command{
		Use:     "get <alert-id>",
		Aliases: []string{"show"},
		Short:   "Show an alert",
		Long:    withAPI("Show an alert.\n\n--expand adds \"_expanded\": {\"monitor\": {...}, \"host\": {...}}.", "Read", "GET /api/v0/alerts/<id>"),
		Example: "  mcrl alert get 2bj4Jd2KFaP\n  mcrl alert get 2bj4Jd2KFaP --expand --jq '._expanded.monitor.name'",
		Args:    cmdutil.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			b, err := c.Get(cmd.Context(), "/alerts/"+url.PathEscape(args[0]), nil)
			if err != nil {
				return notFoundByID(err, "alert", args[0])
			}
			raw := mackerel.UnwrapIf(b, "alert")
			if expand {
				items, err := expandAlerts(cmd, c, []json.RawMessage{raw})
				if err != nil {
					return err
				}
				raw = items[0]
			}
			return printRaw(f, raw)
		},
	}
	cmd.Flags().BoolVar(&expand, "expand", false, "add monitor and host names under _expanded")
	return cmd
}

func newAlertLogsCmd(f *cmdutil.Factory) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:     "logs <alert-id>",
		Short:   "Show the status history of an alert",
		Long:    withAPI("Show the status history of an alert, newest first.", "Read", "GET /api/v0/alerts/<id>/logs"),
		Example: "  mcrl alert logs 2bj4Jd2KFaP\n  mcrl alert logs 2bj4Jd2KFaP --jq '.[] | [.status, .trigger, .createdAt] | @tsv'",
		Args:    cmdutil.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			items, err := collect(c.NextIDPages(cmd.Context(), "/alerts/"+url.PathEscape(args[0])+"/logs", "logs", nil, mackerel.PageSize), limit, nil)
			if err != nil {
				return notFoundByID(err, "alert", args[0])
			}
			return printItems(f, items)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 100, "maximum number of log entries (0 = all)")
	return cmd
}

type hostSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// expandAlerts adds monitor and host summaries to alerts.
func expandAlerts(cmd *cobra.Command, c *mackerel.Client, alerts []json.RawMessage) ([]json.RawMessage, error) {
	ctx := cmd.Context()
	monitors, err := monitorIndex(cmd, c)
	if err != nil {
		return nil, err
	}
	hosts := map[string]*hostSummary{}
	needHosts := false
	for _, a := range alerts {
		if mackerel.StringField(a, "hostId") != "" {
			needHosts = true
			break
		}
	}
	if needHosts {
		list, err := c.List(ctx, "/hosts", "hosts", url.Values{"status": mackerel.AllHostStatuses})
		if err != nil {
			return nil, err
		}
		for _, h := range list {
			var s hostSummary
			if json.Unmarshal(h, &s) == nil {
				hosts[s.ID] = &s
			}
		}
	}
	out := make([]json.RawMessage, len(alerts))
	for i, a := range alerts {
		extra := map[string]any{"monitor": monitors[mackerel.StringField(a, "monitorId")]}
		if hid := mackerel.StringField(a, "hostId"); hid != "" {
			h, ok := hosts[hid]
			if !ok { // 退役済みのホストは一覧に出ないので個別に取る
				if raw, err := hostByID(ctx, c, hid); err == nil && raw != nil {
					h = &hostSummary{ID: hid, Name: mackerel.StringField(raw, "name")}
				}
				hosts[hid] = h
			}
			extra["host"] = h
		}
		var err error
		if out[i], err = mackerel.WithExpanded(a, extra); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// notFoundByID turns the 400/404 of an ID lookup into a not-found error.
func notFoundByID(err error, kind, id string) error {
	return mackerel.AsNotFound(err, kind, id)
}
