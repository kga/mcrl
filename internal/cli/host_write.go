package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/mackerel"
)

// bulkLimit is the maximum number of IDs of the bulk host APIs.
const bulkLimit = 30

func readAllString(f *cmdutil.Factory) (string, error) {
	b, err := io.ReadAll(f.IO.In)
	return string(b), err
}

// resolvedHost is a host referenced on the command line.
type resolvedHost struct {
	ID   string
	Name string
	Raw  json.RawMessage
}

func resolveHosts(ctx context.Context, c *mackerel.Client, refs []string) ([]resolvedHost, error) {
	out := make([]resolvedHost, 0, len(refs))
	seen := map[string]bool{}
	for _, r := range refs {
		raw, err := c.ResolveHost(ctx, r)
		if err != nil {
			return nil, err
		}
		h := resolvedHost{ID: mackerel.StringField(raw, "id"), Name: mackerel.StringField(raw, "name"), Raw: raw}
		if !seen[h.ID] {
			seen[h.ID] = true
			out = append(out, h)
		}
	}
	return out, nil
}

func hostIDs(hs []resolvedHost) []string {
	ids := make([]string, len(hs))
	for i, h := range hs {
		ids[i] = h.ID
	}
	return ids
}

// refetchHosts prints the hosts after an update (docs/decisions.md D12).
func refetchHosts(cmd *cobra.Command, f *cmdutil.Factory, c *mackerel.Client, ids []string) error {
	items := make([]json.RawMessage, 0, len(ids))
	for _, id := range ids {
		b, err := c.Get(cmd.Context(), "/hosts/"+url.PathEscape(id), nil)
		if err != nil {
			f.IO.Warnf("the update succeeded, but fetching host %s failed: %v", id, err)
			continue
		}
		h, err := mackerel.Field(b, "host")
		if err != nil {
			return err
		}
		items = append(items, h)
	}
	return printItems(f, items)
}

func newHostStatusCmd(f *cmdutil.Factory) *cobra.Command {
	var noRefetch bool
	cmd := &cobra.Command{
		Use:   "status <status> <host>...",
		Short: "Change the status of hosts",
		Long: withAPI(`Change the status of hosts to working, standby, maintenance or poweroff.

Pass "-" to read hosts from stdin, one per line. Several hosts are updated
30 at a time with the bulk API. The output is an array of the updated hosts.

`+hostRefHelp, "Read, Write", "POST /api/v0/hosts/<id>/status", "POST /api/v0/hosts/bulk-update-statuses"),
		Example: `  mcrl host status maintenance web-1 web-2
  mcrl host list --service web --jq '.[].id' | mcrl host status standby -`,
		Args: cmdutil.MinimumArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, s string) ([]cobra.Completion, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return mackerel.AllHostStatuses, cobra.ShellCompDirectiveNoFileComp
			}
			return completeHosts(f)(cmd, args, s)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			status := args[0]
			if !validHostStatus(status) {
				return cmdutil.Usagef("invalid status %q (valid: %s)", status, strings.Join(mackerel.AllHostStatuses, ", "))
			}
			refs, err := expandArgs(f, args[1:])
			if err != nil {
				return err
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			hosts, err := resolveHosts(cmd.Context(), c, refs)
			if err != nil {
				return err
			}
			ids := hostIDs(hosts)
			var reqs []request
			if len(ids) == 1 {
				reqs = []request{post("/hosts/"+url.PathEscape(ids[0])+"/status", map[string]string{"status": status})}
			} else {
				for _, part := range chunk(ids, bulkLimit) {
					reqs = append(reqs, post("/hosts/bulk-update-statuses", map[string]any{"status": status, "ids": part}))
				}
			}
			bodies, dry, err := sendAll(cmd, f, c, reqs)
			if dry || err != nil {
				return err
			}
			if noRefetch {
				return printOne(f, bodies[len(bodies)-1])
			}
			return refetchHosts(cmd, f, c, ids)
		},
	}
	addNoRefetch(cmd, &noRefetch)
	return cmd
}

func newHostRetireCmd(f *cmdutil.Factory) *cobra.Command {
	var noRefetch bool
	cmd := &cobra.Command{
		Use:   "retire <host>...",
		Short: "Retire hosts",
		Long: withAPI(`Retire hosts. Retired hosts stop being monitored and cannot be restored.

Pass "-" to read hosts from stdin, one per line. Several hosts are retired
30 at a time with the bulk API. The output is an array of the retired hosts.

`+hostRefHelp, "Read, Write", "POST /api/v0/hosts/<id>/retire", "POST /api/v0/hosts/bulk-retire"),
		Example: `  mcrl host retire web-old-1
  mcrl host list --status poweroff --jq '.[].id' | mcrl host retire --yes -`,
		Args:              cmdutil.MinimumArgs(1),
		ValidArgsFunction: completeHosts(f),
		RunE: func(cmd *cobra.Command, args []string) error {
			refs, err := expandArgs(f, args)
			if err != nil {
				return err
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			hosts, err := resolveHosts(cmd.Context(), c, refs)
			if err != nil {
				return err
			}
			ids := hostIDs(hosts)
			var reqs []request
			if len(ids) == 1 {
				reqs = []request{post("/hosts/"+url.PathEscape(ids[0])+"/retire", map[string]any{})}
			} else {
				for _, part := range chunk(ids, bulkLimit) {
					reqs = append(reqs, post("/hosts/bulk-retire", map[string]any{"ids": part}))
				}
			}
			if !f.DryRun {
				names := make([]string, len(hosts))
				for i, h := range hosts {
					names[i] = fmt.Sprintf("%s (%s)", h.Name, h.ID)
				}
				if err := confirm(f, fmt.Sprintf("Retire %d host(s)? This cannot be undone: %s", len(hosts), strings.Join(names, ", "))); err != nil {
					return err
				}
			}
			bodies, dry, err := sendAll(cmd, f, c, reqs)
			if dry || err != nil {
				return err
			}
			if noRefetch {
				return printOne(f, bodies[len(bodies)-1])
			}
			return refetchHosts(cmd, f, c, ids)
		},
	}
	addNoRefetch(cmd, &noRefetch)
	return cmd
}

func newHostRoleCmd(f *cmdutil.Factory) *cobra.Command {
	mk := func(verb, short string, apply func(current, given []string) []string) *cobra.Command {
		var noRefetch bool
		cmd := &cobra.Command{
			Use:   verb + " <host> <service:role>...",
			Short: short,
			Long: withAPI(short+`.

Roles are given as "<service>:<role>". The API creates services and roles
that do not exist yet.

`+hostRefHelp, "Write", "PUT /api/v0/hosts/<id>/role-fullnames"),
			Example: fmt.Sprintf("  mcrl host role %s web-1 web:app\n  mcrl host role %s web-1 web:app web:batch", verb, verb),
			Args:    cmdutil.MinimumArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				for _, r := range args[1:] {
					if svc, role, ok := strings.Cut(r, ":"); !ok || svc == "" || role == "" {
						return cmdutil.Usagef("invalid role %q (want <service>:<role>)", r)
					}
				}
				c, err := apiClient(cmd.Context(), f)
				if err != nil {
					return err
				}
				raw, err := c.ResolveHost(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				id := mackerel.StringField(raw, "id")
				roles := apply(currentRoleFullnames(raw), args[1:])
				bodies, dry, err := sendAll(cmd, f, c, []request{put("/hosts/"+url.PathEscape(id)+"/role-fullnames", map[string]any{"roleFullnames": roles})})
				if dry || err != nil {
					return err
				}
				if noRefetch {
					return printOne(f, bodies[0])
				}
				return refetchHosts(cmd, f, c, []string{id})
			},
		}
		addNoRefetch(cmd, &noRefetch)
		return cmd
	}
	return newGroup("role", "Change the roles of a host", nil,
		mk("set", "Replace the roles of a host", func(_, given []string) []string { return uniqueSorted(given) }),
		mk("add", "Add roles to a host", func(cur, given []string) []string { return uniqueSorted(append(cur, given...)) }),
		mk("remove", "Remove roles from a host", func(cur, given []string) []string {
			drop := map[string]bool{}
			for _, g := range given {
				drop[g] = true
			}
			var out []string
			for _, r := range cur {
				if !drop[r] {
					out = append(out, r)
				}
			}
			return uniqueSorted(out)
		}),
	)
}

// currentRoleFullnames converts {"roles": {"svc": ["role"]}} to ["svc:role"].
func currentRoleFullnames(host json.RawMessage) []string {
	var h struct {
		Roles map[string][]string `json:"roles"`
	}
	_ = json.Unmarshal(host, &h)
	var out []string
	for svc, roles := range h.Roles {
		for _, r := range roles {
			out = append(out, svc+":"+r)
		}
	}
	return out
}

func uniqueSorted(s []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func newServiceCreateCmd(f *cmdutil.Factory) *cobra.Command {
	var memo string
	cmd := &cobra.Command{
		Use:     "create <name>",
		Aliases: []string{"new"},
		Short:   "Create a service",
		Long: withAPI(`Create a service. Names are 2 to 63 letters, digits, "-" and "_", and
cannot start with "-" or "_".`, "Read, Write", "POST /api/v0/services"),
		Example: "  mcrl service create web\n  mcrl service create batch --memo 'nightly jobs'",
		Args:    cmdutil.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !validServiceName(args[0]) {
				return cmdutil.Usagef("invalid service name %q: use 2 to 63 letters, digits, - and _, not starting with - or _", args[0])
			}
			return simpleSend(cmd, f, post("/services", map[string]string{"name": args[0], "memo": memo}))
		},
	}
	cmd.Flags().StringVar(&memo, "memo", "", "memo")
	return cmd
}

func newServiceDeleteCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"rm"},
		Short:   "Delete a service",
		Long: withAPI(`Delete a service. Its roles, service metrics, monitors and graph
annotations are deleted with it.`, "Read, Write", "DELETE /api/v0/services/<name>"),
		Example: "  mcrl service delete old-service\n  mcrl service delete old-service --yes",
		Args:    cmdutil.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !f.DryRun {
				if err := confirm(f, fmt.Sprintf("Delete service %q together with its roles, service metrics, monitors and graph annotations?", args[0])); err != nil {
					return err
				}
			}
			return simpleSend(cmd, f, del("/services/"+url.PathEscape(args[0])))
		},
	}
}

func newRoleCreateCmd(f *cmdutil.Factory) *cobra.Command {
	var memo string
	cmd := &cobra.Command{
		Use:     "create <service>:<role>",
		Aliases: []string{"new"},
		Short:   "Create a role",
		Long:    withAPI("Create a role in a service. Role names follow the same rules as service names.", "Read, Write", "POST /api/v0/services/<name>/roles"),
		Example: "  mcrl role create web:app\n  mcrl role create web:db --memo 'MySQL primary'",
		Args:    cmdutil.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, role, err := splitRole(args[0])
			if err != nil {
				return err
			}
			if !validServiceName(role) {
				return cmdutil.Usagef("invalid role name %q: use 2 to 63 letters, digits, - and _, not starting with - or _", role)
			}
			return simpleSend(cmd, f, post("/services/"+url.PathEscape(svc)+"/roles", map[string]string{"name": role, "memo": memo}))
		},
	}
	cmd.Flags().StringVar(&memo, "memo", "", "memo")
	return cmd
}

func newRoleDeleteCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:     "delete <service>:<role>",
		Aliases: []string{"rm"},
		Short:   "Delete a role",
		Long:    withAPI("Delete a role.", "Read, Write", "DELETE /api/v0/services/<name>/roles/<role>"),
		Example: "  mcrl role delete web:old\n  mcrl role delete web:old --yes",
		Args:    cmdutil.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, role, err := splitRole(args[0])
			if err != nil {
				return err
			}
			if !f.DryRun {
				if err := confirm(f, fmt.Sprintf("Delete role %q?", args[0])); err != nil {
					return err
				}
			}
			return simpleSend(cmd, f, del("/services/"+url.PathEscape(svc)+"/roles/"+url.PathEscape(role)))
		},
	}
}

func splitRole(s string) (string, string, error) {
	svc, role, ok := strings.Cut(s, ":")
	if !ok || svc == "" || role == "" {
		return "", "", cmdutil.Usagef("invalid role %q (want <service>:<role>)", s)
	}
	return svc, role, nil
}

func validServiceName(s string) bool {
	if len(s) < 2 || len(s) > 63 || s[0] == '-' || s[0] == '_' {
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

// simpleSend sends one request and prints its response.
func simpleSend(cmd *cobra.Command, f *cmdutil.Factory, r request) error {
	c, err := apiClient(cmd.Context(), f)
	if err != nil {
		return err
	}
	bodies, dry, err := sendAll(cmd, f, c, []request{r})
	if dry || err != nil {
		return err
	}
	return printOne(f, bodies[0])
}
