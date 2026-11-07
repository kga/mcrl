package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/mackerel"
)

// ---- metadata ----

const maxMetadataBytes = 100 * 1024

// metadataBases is the part of the metadata paths that names the target, for help texts.
const metadataBases = "{hosts/<id>,services/<name>,services/<name>/roles/<role>}"

// metadataTarget is the --host / --service / --role triple.
type metadataTarget struct{ host, service, role string }

func (t *metadataTarget) bind(cmd *cobra.Command, f *cmdutil.Factory) {
	cmd.Flags().StringVar(&t.host, "host", "", "host (ID, name or cid:<custom identifier>)")
	cmd.Flags().StringVar(&t.service, "service", "", "service name")
	cmd.Flags().StringVar(&t.role, "role", "", "role as <service>:<role>")
	cmd.MarkFlagsMutuallyExclusive("host", "service", "role")
	cmd.MarkFlagsOneRequired("host", "service", "role")
	_ = cmd.RegisterFlagCompletionFunc("host", completeHosts(f))
}

func (t *metadataTarget) base(ctx context.Context, c *mackerel.Client) (string, error) {
	switch {
	case t.host != "":
		id, err := c.ResolveHostID(ctx, t.host)
		if err != nil {
			return "", err
		}
		return "/hosts/" + url.PathEscape(id) + "/metadata", nil
	case t.service != "":
		return "/services/" + url.PathEscape(t.service) + "/metadata", nil
	}
	svc, role, err := splitRole(t.role)
	if err != nil {
		return "", err
	}
	return "/services/" + url.PathEscape(svc) + "/roles/" + url.PathEscape(role) + "/metadata", nil
}

func validNamespace(ns string) error {
	if ns == "" || strings.HasPrefix(ns, "mackerel") {
		return cmdutil.Usagef("invalid namespace %q: namespaces starting with \"mackerel\" are reserved", ns)
	}
	for i := 0; i < len(ns); i++ {
		c := ns[i]
		ok := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-' || c == '_'
		if !ok {
			return cmdutil.Usagef("invalid namespace %q: use letters, digits, - and _", ns)
		}
	}
	return nil
}

func newMetadataCmd(f *cmdutil.Factory) *cobra.Command {
	var lt, gt, st, dt metadataTarget
	var noRefetchMetadata bool
	list := &cobra.Command{
		Use:     "list (--host <host> | --service <service> | --role <service:role>)",
		Aliases: []string{"ls"},
		Short:   "List metadata namespaces",
		Long:    withAPI(`List the metadata namespaces of a host, service or role as [{"namespace": ...}].`, "Read", "GET /api/v0/"+metadataBases+"/metadata"),
		Example: "  mcrl metadata list --host web-1\n  mcrl metadata list --role web:app --jq '.[].namespace'",
		Args:    cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			base, err := lt.base(cmd.Context(), c)
			if err != nil {
				return err
			}
			items, err := c.List(cmd.Context(), base, "metadata", nil)
			if err != nil {
				return err
			}
			return printItems(f, items)
		},
	}
	lt.bind(list, f)

	get := &cobra.Command{
		Use:     "get <namespace> (--host <host> | --service <service> | --role <service:role>)",
		Short:   "Show a metadata value",
		Long:    withAPI("Show a metadata value: the JSON stored under the namespace, as is.", "Read", "GET /api/v0/"+metadataBases+"/metadata/<namespace>"),
		Example: "  mcrl metadata get inventory --host web-1\n  mcrl metadata get deploy --service web --jq .version",
		Args:    cmdutil.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			base, err := gt.base(cmd.Context(), c)
			if err != nil {
				return err
			}
			b, err := c.Get(cmd.Context(), base+"/"+url.PathEscape(args[0]), nil)
			if err != nil {
				return err
			}
			return printRaw(f, b)
		},
	}
	gt.bind(get, f)

	var file string
	set := &cobra.Command{
		Use:   "set <namespace> -f <file> (--host <host> | --service <service> | --role <service:role>)",
		Short: "Create or replace a metadata value",
		Long: withAPI(`Create or replace a metadata value with any JSON (or YAML) value up to 100KB.
Limits: 50 namespaces per host or service, 10 per role.`, "Read, Write", "PUT /api/v0/"+metadataBases+"/metadata/<namespace>"),
		Example: `  mcrl metadata set inventory --host web-1 -f inventory.json
  echo '{"version": "1.2.3"}' | mcrl metadata set deploy --service web -f -`,
		Args: cmdutil.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validNamespace(args[0]); err != nil {
				return err
			}
			raw, err := readDocument(f, file)
			if err != nil {
				return err
			}
			if len(raw) > maxMetadataBytes {
				return cmdutil.Usagef("the value is %d bytes; the API accepts at most 100KB", len(raw))
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			base, err := st.base(cmd.Context(), c)
			if err != nil {
				return err
			}
			path := base + "/" + url.PathEscape(args[0])
			bodies, dry, err := sendAll(cmd, f, c, []request{put(path, raw)})
			if dry || err != nil {
				return err
			}
			if noRefetchMetadata {
				return printOne(f, bodies[0])
			}
			b, err := c.Get(cmd.Context(), path, nil)
			if err != nil {
				f.IO.Warnf("the update succeeded, but fetching it back failed: %v", err)
				return printOne(f, bodies[0])
			}
			return printRaw(f, b)
		},
	}
	st.bind(set, f)
	set.Flags().StringVarP(&file, "file", "f", "", `JSON or YAML file ("-" for stdin) (required)`)
	_ = set.MarkFlagRequired("file")
	addNoRefetch(set, &noRefetchMetadata)

	delCmd := &cobra.Command{
		Use:     "delete <namespace> (--host <host> | --service <service> | --role <service:role>)",
		Aliases: []string{"rm"},
		Short:   "Delete a metadata value",
		Long:    withAPI("Delete a metadata value.", "Read, Write", "DELETE /api/v0/"+metadataBases+"/metadata/<namespace>"),
		Example: "  mcrl metadata delete inventory --host web-1\n  mcrl metadata delete deploy --service web --yes",
		Args:    cmdutil.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			base, err := dt.base(cmd.Context(), c)
			if err != nil {
				return err
			}
			if !f.DryRun {
				if err := confirm(f, fmt.Sprintf("Delete metadata %q?", args[0])); err != nil {
					return err
				}
			}
			return simpleSend(cmd, f, del(base+"/"+url.PathEscape(args[0])))
		},
	}
	dt.bind(delCmd, f)
	return newGroup("metadata", "Manage metadata of hosts, services and roles", nil, list, get, set, delCmd)
}

// ---- users and invitations ----

var authorities = []string{"manager", "collaborator", "viewer"}

func newUserRemoveCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:     "remove <user>",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a member from the organization",
		Long:    withAPI("Remove a member from the organization. The user is given by ID, email or screen name.", "Read, Write", "DELETE /api/v0/users/<id>"),
		Example: "  mcrl user remove alice@example.com\n  mcrl user remove alice --yes",
		Args:    cmdutil.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			u, err := c.Resolve(cmd.Context(), mackerel.KindUser, args[0])
			if err != nil {
				return err
			}
			id := mackerel.StringField(u, "id")
			if !f.DryRun {
				if err := confirm(f, fmt.Sprintf("Remove %s (%s) from the organization?", mackerel.StringField(u, "email"), id)); err != nil {
					return err
				}
			}
			return simpleSend(cmd, f, del("/users/"+url.PathEscape(id)))
		},
	}
}

func newInvitationCmd(f *cmdutil.Factory) *cobra.Command {
	var authority string
	create := &cobra.Command{
		Use:     "create <email> [--authority viewer]",
		Aliases: []string{"new"},
		Short:   "Invite someone to the organization",
		Long:    withAPI("Invite someone to the organization as a manager, collaborator or viewer.", "Read, Write", "POST /api/v0/invitations"),
		Example: "  mcrl invitation create alice@example.com\n  mcrl invitation create bob@example.com --authority collaborator",
		Args:    cmdutil.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !contains(authorities, authority) {
				return cmdutil.Usagef("invalid --authority %q (valid: %s)", authority, strings.Join(authorities, ", "))
			}
			return simpleSend(cmd, f, post("/invitations", map[string]string{"email": args[0], "authority": authority}))
		},
	}
	create.Flags().StringVar(&authority, "authority", "viewer", "manager, collaborator or viewer")
	_ = create.RegisterFlagCompletionFunc("authority", cobra.FixedCompletions(authorities, cobra.ShellCompDirectiveNoFileComp))

	revoke := &cobra.Command{
		Use:     "revoke <email>",
		Short:   "Cancel an invitation",
		Long:    withAPI("Cancel an invitation.", "Read, Write", "POST /api/v0/invitations/revoke"),
		Example: "  mcrl invitation revoke alice@example.com\n  mcrl invitation revoke alice@example.com --yes",
		Args:    cmdutil.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !f.DryRun {
				if err := confirm(f, fmt.Sprintf("Cancel the invitation of %s?", args[0])); err != nil {
					return err
				}
			}
			return simpleSend(cmd, f, post("/invitations/revoke", map[string]string{"email": args[0]}))
		},
	}
	return newGroup("invitation", "Manage invitations", []string{"invitations"},
		listCmd(f, "List pending invitations", "/invitations", "invitations",
			"  mcrl invitation list\n  mcrl invitation list --jq '.[].email'"),
		create, revoke)
}

// ---- IP restrictions ----

func newIPRestrictionCmd(f *cmdutil.Factory) *cobra.Command {
	var getTarget, setTarget string
	var cidrs []string
	var clear bool
	validTarget := func(t string) error {
		if t != "web" && t != "api" {
			return cmdutil.Usagef(`--target must be "web" (web console) or "api"`)
		}
		return nil
	}
	get := &cobra.Command{
		Use:     "get --target web|api",
		Short:   "Show the allowed IP ranges",
		Long:    withAPI(`Show the IP ranges allowed to access the web console ("web") or the API ("api").`, "Read", "GET /api/v0/settings/ip-restriction/{web,api}"),
		Example: "  mcrl org ip-restriction get --target api\n  mcrl org ip-restriction get --target web --jq '.cidrs[]'",
		Args:    cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validTarget(getTarget); err != nil {
				return err
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			b, err := c.Get(cmd.Context(), "/settings/ip-restriction/"+getTarget, nil)
			if err != nil {
				return err
			}
			return printRaw(f, b)
		},
	}
	get.Flags().StringVar(&getTarget, "target", "", "web or api (required)")
	_ = get.MarkFlagRequired("target")

	var noRefetch bool
	set := &cobra.Command{
		Use:   "set --target web|api (--cidr <cidr>... | --clear)",
		Short: "Replace the allowed IP ranges",
		Long: withAPI(`Replace the IP ranges allowed to access the web console or the API.
--clear removes the restriction.

Be careful: a list that excludes where you are locks you (and mcrl) out.
The command always asks for confirmation unless --yes.`, "Read, Write", "POST /api/v0/settings/ip-restriction/{web,api}"),
		Example: "  mcrl org ip-restriction set --target api --cidr 203.0.113.0/24 --cidr 198.51.100.10/32\n  mcrl org ip-restriction set --target web --clear",
		Args:    cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validTarget(setTarget); err != nil {
				return err
			}
			if clear == (len(cidrs) > 0) {
				return cmdutil.Usagef("give either --cidr or --clear")
			}
			list := []string{}
			for _, c := range cidrs {
				if _, _, err := net.ParseCIDR(c); err != nil {
					return cmdutil.Usagef("invalid --cidr %q", c)
				}
				list = append(list, c)
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			if !f.DryRun {
				q := fmt.Sprintf("Allow only %s to access the %s? Anything else, possibly including you, will be locked out.", strings.Join(list, ", "), setTarget)
				if clear {
					q = fmt.Sprintf("Remove the IP restriction of the %s?", setTarget)
				}
				if err := confirm(f, q); err != nil {
					return err
				}
			}
			path := "/settings/ip-restriction/" + setTarget
			bodies, dry, err := sendAll(cmd, f, c, []request{post(path, map[string]any{"cidrs": list})})
			if dry || err != nil {
				return err
			}
			if noRefetch {
				return printOne(f, bodies[0])
			}
			b, err := c.Get(cmd.Context(), path, nil)
			if err != nil {
				f.IO.Warnf("the update succeeded, but fetching it back failed: %v", err)
				return printOne(f, bodies[0])
			}
			return printRaw(f, b)
		},
	}
	set.Flags().StringVar(&setTarget, "target", "", "web or api (required)")
	set.Flags().StringArrayVar(&cidrs, "cidr", nil, "allowed IP range in CIDR notation (repeatable)")
	set.Flags().BoolVar(&clear, "clear", false, "remove the restriction")
	_ = set.MarkFlagRequired("target")
	addNoRefetch(set, &noRefetch)
	return newGroup("ip-restriction", "Manage IP address restrictions", nil, get, set)
}

// ---- downtime list --active ----

// downtimeActive reports whether a downtime covers now. Recurrences are
// evaluated in the local time zone.
func downtimeActive(raw json.RawMessage, now time.Time) bool {
	var d struct {
		Start      int64 `json:"start"`
		Duration   int64 `json:"duration"`
		Recurrence *struct {
			Type     string   `json:"type"`
			Interval int      `json:"interval"`
			Weekdays []string `json:"weekdays"`
			Until    *int64   `json:"until"`
		} `json:"recurrence"`
	}
	if json.Unmarshal(raw, &d) != nil {
		return false
	}
	length := time.Duration(d.Duration) * time.Minute
	start := time.Unix(d.Start, 0).In(now.Location())
	covers := func(s time.Time) bool { return !now.Before(s) && now.Before(s.Add(length)) }
	if d.Recurrence == nil {
		return covers(start)
	}
	r := d.Recurrence
	if r.Interval < 1 {
		r.Interval = 1
	}
	if r.Until != nil && now.After(time.Unix(*r.Until, 0).Add(length)) {
		return false
	}
	// 直近の開始時刻を探す。期間の長さより前に始まったものだけを見ればよい
	step := func(t time.Time, n int) time.Time {
		switch r.Type {
		case "hourly":
			return t.Add(time.Duration(n*r.Interval) * time.Hour)
		case "daily":
			return t.AddDate(0, 0, n*r.Interval)
		case "weekly":
			return t.AddDate(0, 0, 7*n*r.Interval)
		case "monthly":
			return t.AddDate(0, n*r.Interval, 0)
		case "yearly":
			return t.AddDate(n*r.Interval, 0, 0)
		}
		return t
	}
	candidates := func(t time.Time) []time.Time {
		if r.Type != "weekly" || len(r.Weekdays) == 0 {
			return []time.Time{t}
		}
		// その週（start と同じ曜日起点）の指定曜日すべて
		var out []time.Time
		for i := 0; i < 7; i++ {
			day := t.AddDate(0, 0, i)
			if contains(r.Weekdays, day.Weekday().String()) {
				out = append(out, day)
			}
		}
		return out
	}
	until := now.Add(time.Nanosecond)
	if r.Until != nil {
		until = time.Unix(*r.Until, 0)
	}
	for n, t := 0, start; !t.After(now) && n < 1_000_000; n, t = n+1, step(start, n+1) {
		if t.Add(7 * 24 * time.Hour).Before(now.Add(-length)) {
			continue // ずっと前の回は飛ばす
		}
		for _, s := range candidates(t) {
			if s.Before(start) || s.After(until) {
				continue
			}
			if covers(s) {
				return true
			}
		}
	}
	return false
}
