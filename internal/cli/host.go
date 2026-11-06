package cli

import (
	"encoding/json"
	"iter"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/mackerel"
)

// collect gathers up to limit items (0 = all) that pass keep (nil = all).
func collect(seq iter.Seq2[json.RawMessage, error], limit int, keep func(json.RawMessage) bool) ([]json.RawMessage, error) {
	if err := limitUsage(limit); err != nil {
		return nil, err
	}
	items := []json.RawMessage{}
	for it, err := range seq {
		if err != nil {
			return nil, err
		}
		if keep != nil && !keep(it) {
			continue
		}
		items = append(items, it)
		if limit > 0 && len(items) >= limit {
			break
		}
	}
	return items, nil
}

func newHostCmd(f *cmdutil.Factory) *cobra.Command {
	return newGroup("host", "Manage hosts", []string{"hosts"},
		newHostListCmd(f), newHostGetCmd(f), newHostMonitorStatusCmd(f),
		newHostStatusCmd(f), newHostRetireCmd(f), newHostRoleCmd(f))
}

func newHostListCmd(f *cmdutil.Factory) *cobra.Command {
	var (
		service, name, cid string
		roles, statuses    []string
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List hosts",
		Long: withAPI(`List hosts.

Without --status, the API returns working and standby hosts. Use
"--status all" for every status. Retired hosts are never listed.
--name matches the host name exactly and case-sensitively.`, "Read", "GET /api/v0/hosts"),
		Example: `  mcrl host list
  mcrl host list --service web --role app --status all
  mcrl host list --status poweroff --jq '.[].id'`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(roles) > 0 && service == "" {
				return cmdutil.Usagef("--role needs --service")
			}
			q := url.Values{}
			if service != "" {
				q.Set("service", service)
			}
			for _, r := range roles {
				q.Add("role", r)
			}
			if name != "" {
				q.Set("name", name)
			}
			if cid != "" {
				q.Set("customIdentifier", cid)
			}
			for _, s := range statuses {
				if s == "all" {
					q["status"] = mackerel.AllHostStatuses
					break
				}
				if !validHostStatus(s) {
					return cmdutil.Usagef("invalid --status %q (valid: working, standby, maintenance, poweroff, all)", s)
				}
				q.Add("status", s)
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			items, err := c.List(cmd.Context(), "/hosts", "hosts", q)
			if err != nil {
				return err
			}
			return printItems(f, items)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&service, "service", "", "only hosts in this service")
	fl.StringArrayVar(&roles, "role", nil, "only hosts with this role of --service (repeatable)")
	fl.StringVar(&name, "name", "", "only hosts with this exact name")
	fl.StringArrayVar(&statuses, "status", nil, "only hosts in this status: working, standby, maintenance, poweroff or all (repeatable)")
	fl.StringVar(&cid, "custom-identifier", "", "only the host with this custom identifier")
	_ = cmd.RegisterFlagCompletionFunc("status", cobra.FixedCompletions(
		[]cobra.Completion{"working", "standby", "maintenance", "poweroff", "all"}, cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

func validHostStatus(s string) bool {
	for _, v := range mackerel.AllHostStatuses {
		if s == v {
			return true
		}
	}
	return false
}

const hostRefHelp = `The host is given by ID, exact name, or "cid:<custom identifier>".
A name that looks like an ID can be forced with the "name:" prefix.`

func newHostGetCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:     "get <host>",
		Aliases: []string{"show"},
		Short:   "Show a host",
		Long:    withAPI("Show a host.\n\n"+hostRefHelp, "Read", "GET /api/v0/hosts/<id>", "GET /api/v0/hosts?name=", "GET /api/v0/hosts-by-custom-identifier/<cid>"),
		Example: `  mcrl host get web-1
  mcrl host get cid:i-0123456789abcdef0
  mcrl host get 5Vf7Be7guuy --jq '.roles'`,
		Args:              cmdutil.ExactArgs(1),
		ValidArgsFunction: completeHosts(f),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			h, err := c.ResolveHost(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printRaw(f, h)
		},
	}
}

func newHostMonitorStatusCmd(f *cmdutil.Factory) *cobra.Command {
	var expand bool
	cmd := &cobra.Command{
		Use:   "monitor-status <host>",
		Short: "Show the status of each monitor on a host",
		Long: withAPI(`Show the status of each monitor that targets a host.

--expand adds "_expanded": {"monitor": {"name", "type"}} to each entry.

`+hostRefHelp, "Read", "GET /api/v0/hosts/<id>/monitored-statuses", "GET /api/v0/monitors (--expand)"),
		Example: `  mcrl host monitor-status web-1
  mcrl host monitor-status web-1 --expand --jq '.[] | select(.status != "OK") | ._expanded.monitor.name'`,
		Args:              cmdutil.ExactArgs(1),
		ValidArgsFunction: completeHosts(f),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, err := apiClient(ctx, f)
			if err != nil {
				return err
			}
			id, err := c.ResolveHostID(ctx, args[0])
			if err != nil {
				return err
			}
			items, err := c.List(ctx, "/hosts/"+url.PathEscape(id)+"/monitored-statuses", "monitoredStatuses", nil)
			if err != nil {
				return err
			}
			if expand {
				monitors, err := monitorIndex(cmd, c)
				if err != nil {
					return err
				}
				for i, it := range items {
					extra := map[string]any{"monitor": monitors[mackerel.StringField(it, "monitorId")]}
					if items[i], err = mackerel.WithExpanded(it, extra); err != nil {
						return err
					}
				}
			}
			return printItems(f, items)
		},
	}
	cmd.Flags().BoolVar(&expand, "expand", false, "add monitor names under _expanded")
	return cmd
}

type monitorSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// monitorIndex maps monitor IDs to their names and types.
func monitorIndex(cmd *cobra.Command, c *mackerel.Client) (map[string]*monitorSummary, error) {
	items, err := c.List(cmd.Context(), "/monitors", "monitors", nil)
	if err != nil {
		return nil, err
	}
	idx := make(map[string]*monitorSummary, len(items))
	for _, it := range items {
		var m monitorSummary
		if json.Unmarshal(it, &m) == nil {
			idx[m.ID] = &m
		}
	}
	return idx, nil
}
