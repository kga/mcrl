package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/mackerel"
	"github.com/kga/mcrl/internal/timeexpr"
)

// ---- edit for resources whose create/update are defined elsewhere ----

var (
	monitorEditable  = editable{Kind: mackerel.KindMonitor, ItemPath: "/monitors/%s", ReadOnly: []string{"id"}, ResponseKey: "monitor"}
	downtimeEditable = editable{Kind: mackerel.KindDowntime, ItemPath: "/downtimes/%s", ReadOnly: []string{"id"}}
)

// ---- notification channels ----

var channelEvents = []string{"alert", "alertGroup", "hostStatus", "hostRegister", "hostRetire", "monitor"}

var channelCRUD = &crud{
	editable: editable{
		Kind:     mackerel.KindChannel,
		ItemPath: "/channels/%s",
		ReadOnly: []string{"id", "suspendedAt"},
		Validate: func(m map[string]any) error {
			switch t, _ := m["type"].(string); t {
			case "email", "slack", "webhook":
				return nil
			default:
				return fmt.Errorf("the API can create and update only email, slack and webhook channels, not %q", t)
			}
		},
	},
	CreatePath: "/channels",
	Required:   []string{"type", "name"},
	Bind: func(cmd *cobra.Command) {
		fl := cmd.Flags()
		fl.String("type", "", "email, slack or webhook (create only)")
		fl.String("name", "", "name")
		fl.StringArray("email", nil, "email address (email channels, repeatable)")
		fl.StringArray("user", nil, "organization member ID (email channels, repeatable)")
		fl.String("url", "", "Slack incoming webhook URL or webhook URL")
		fl.StringArray("mention", nil, `Slack mention per status as status=text, e.g. "critical=<!channel>" (repeatable)`)
		fl.Bool("graph-image", false, "attach graph images (slack, webhook)")
		fl.StringArray("event", nil, "events to notify (repeatable): "+strings.Join(channelEvents, ", "))
		fl.Bool("add-to-default-group", false, "add to the default notification group (create only)")
		_ = cmd.RegisterFlagCompletionFunc("type", cobra.FixedCompletions([]string{"email", "slack", "webhook"}, cobra.ShellCompDirectiveNoFileComp))
		_ = cmd.RegisterFlagCompletionFunc("event", cobra.FixedCompletions(channelEvents, cobra.ShellCompDirectiveNoFileComp))
	},
	Apply: func(_ context.Context, cmd *cobra.Command, _ *mackerel.Client, body map[string]any) error {
		if cmd.Flags().Changed("type") {
			if t, _ := cmd.Flags().GetString("type"); body["type"] != nil && body["type"] != t && cmd.Name() == "update" {
				return cmdutil.Usagef("the type of a channel cannot be changed")
			}
		}
		setString(cmd, body, "type", "type")
		setString(cmd, body, "name", "name")
		setStrings(cmd, body, "email", "emails")
		setStrings(cmd, body, "user", "userIds")
		setString(cmd, body, "url", "url")
		setBool(cmd, body, "graph-image", "enabledGraphImage")
		if cmd.Flags().Changed("event") {
			evs, _ := cmd.Flags().GetStringArray("event")
			for _, e := range evs {
				if !contains(channelEvents, e) {
					return cmdutil.Usagef("invalid --event %q (valid: %s)", e, strings.Join(channelEvents, ", "))
				}
			}
			body["events"] = evs
		}
		if cmd.Flags().Changed("mention") {
			ms, _ := cmd.Flags().GetStringArray("mention")
			mentions := map[string]any{}
			for _, m := range ms {
				k, v, ok := strings.Cut(m, "=")
				if !ok || !contains([]string{"ok", "warning", "critical"}, strings.ToLower(k)) {
					return cmdutil.Usagef("invalid --mention %q (want ok=..., warning=... or critical=...)", m)
				}
				mentions[strings.ToLower(k)] = v
			}
			body["mentions"] = mentions
		}
		if cmd.Name() == "update" {
			delete(body, "addToDefaultNotificationGroup") // 更新時は指定できない
		} else {
			setBool(cmd, body, "add-to-default-group", "addToDefaultNotificationGroup")
		}
		if t, _ := body["type"].(string); t != "" {
			if (t == "slack" || t == "webhook") && body["url"] == nil {
				return cmdutil.Usagef("--url is required for %s channels", t)
			}
		}
		return nil
	},
}

func init() {
	channelCRUD.Examples.Create = `  mcrl channel create --type slack --name ops --url https://hooks.slack.com/services/... --mention critical='<!channel>' --event alert
  mcrl channel create --type email --name oncall --email oncall@example.com --event alert --event hostRetire`
	channelCRUD.Examples.Update = `  mcrl channel update ops --event alert --event alertGroup
  mcrl channel update ops --graph-image`
	channelCRUD.Examples.Delete = "  mcrl channel delete ops\n  mcrl channel delete 2wvKpqFzgD9 --yes"
}

// ---- notification groups ----

var notificationGroupCRUD = &crud{
	editable: editable{
		Kind:     mackerel.KindNotificationGroup,
		ItemPath: "/notification-groups/%s",
		ReadOnly: []string{"id", "type"},
	},
	CreatePath: "/notification-groups",
	Required:   []string{"name"},
	Bind: func(cmd *cobra.Command) {
		fl := cmd.Flags()
		fl.String("name", "", "name")
		fl.String("level", "", "notification level: all or critical")
		fl.StringArray("channel", nil, "notification channel, ID or name (repeatable; replaces the list)")
		fl.StringArray("child-group", nil, "child notification group, ID or name (repeatable; replaces the list)")
		fl.StringArray("monitor", nil, "monitor, ID or name (repeatable; replaces the list)")
		fl.StringArray("monitor-only", nil, "monitor whose alerts go only to this group (skipDefault), ID or name (repeatable)")
		fl.StringArray("service", nil, "service (repeatable; replaces the list)")
		_ = cmd.RegisterFlagCompletionFunc("level", cobra.FixedCompletions([]string{"all", "critical"}, cobra.ShellCompDirectiveNoFileComp))
	},
	Apply: func(ctx context.Context, cmd *cobra.Command, c *mackerel.Client, body map[string]any) error {
		setString(cmd, body, "name", "name")
		if cmd.Flags().Changed("level") {
			l, _ := cmd.Flags().GetString("level")
			if l != "all" && l != "critical" {
				return cmdutil.Usagef("invalid --level %q (valid: all, critical)", l)
			}
			body["notificationLevel"] = l
		}
		for flag, spec := range map[string]struct {
			key  string
			kind mackerel.Kind
		}{"channel": {"childChannelIds", mackerel.KindChannel}, "child-group": {"childNotificationGroupIds", mackerel.KindNotificationGroup}} {
			ids, ok, err := resolveIDs(ctx, cmd, c, spec.kind, flag)
			if err != nil {
				return err
			}
			if ok {
				body[spec.key] = ids
			}
		}
		normal, okN, err := resolveIDs(ctx, cmd, c, mackerel.KindMonitor, "monitor")
		if err != nil {
			return err
		}
		only, okO, err := resolveIDs(ctx, cmd, c, mackerel.KindMonitor, "monitor-only")
		if err != nil {
			return err
		}
		if okN || okO {
			var ms []map[string]any
			for _, id := range normal {
				ms = append(ms, map[string]any{"id": id, "skipDefault": false})
			}
			for _, id := range only {
				ms = append(ms, map[string]any{"id": id, "skipDefault": true})
			}
			body["monitors"] = ms
		}
		if cmd.Flags().Changed("service") {
			svcs, _ := cmd.Flags().GetStringArray("service")
			list := make([]map[string]string, len(svcs))
			for i, s := range svcs {
				list[i] = map[string]string{"name": s}
			}
			body["services"] = list
		}
		if cmd.Name() == "create" {
			// API は子グループ・子チャンネルの配列を必須として扱う（ドキュメントには記載なし、実測）
			for k, v := range map[string]any{"notificationLevel": "all", "childNotificationGroupIds": []string{}, "childChannelIds": []string{}} {
				if _, ok := body[k]; !ok {
					body[k] = v
				}
			}
		}
		return nil
	},
}

func init() {
	notificationGroupCRUD.Examples.Create = `  mcrl ng create --name web-oncall --level critical --channel ops-slack --service web
  mcrl ng create -f group.yaml`
	notificationGroupCRUD.Examples.Update = "  mcrl ng update web-oncall --channel ops-slack --channel oncall-email\n  mcrl ng update web-oncall --monitor-only 'cpu high'"
	notificationGroupCRUD.Examples.Delete = "  mcrl ng delete web-oncall\n  mcrl ng delete 2wvKpqFzgD9 --yes"
}

// ---- alert group settings ----

var alertGroupCRUD = &crud{
	editable: editable{
		Kind:     mackerel.KindAlertGroup,
		ItemPath: "/alert-group-settings/%s",
		ReadOnly: []string{"id"},
	},
	CreatePath: "/alert-group-settings",
	Required:   []string{"name"},
	Bind: func(cmd *cobra.Command) {
		fl := cmd.Flags()
		fl.String("name", "", "name")
		fl.String("memo", "", "memo")
		fl.StringArray("service", nil, "service scope (repeatable; replaces the list)")
		fl.StringArray("role", nil, "role scope as <service>:<role> (repeatable; replaces the list)")
		fl.StringArray("monitor", nil, "monitor scope, ID or name (repeatable; replaces the list)")
		fl.String("notification-interval", "", `re-notify interval, e.g. "1h"`)
	},
	Apply: func(ctx context.Context, cmd *cobra.Command, c *mackerel.Client, body map[string]any) error {
		setString(cmd, body, "name", "name")
		setString(cmd, body, "memo", "memo")
		setStrings(cmd, body, "service", "serviceScopes")
		setStrings(cmd, body, "role", "roleScopes")
		ids, ok, err := resolveIDs(ctx, cmd, c, mackerel.KindMonitor, "monitor")
		if err != nil {
			return err
		}
		if ok {
			body["monitorScopes"] = ids
		}
		if cmd.Flags().Changed("notification-interval") {
			s, _ := cmd.Flags().GetString("notification-interval")
			d, err := timeexpr.ParseDuration(s)
			if err != nil || d < time.Minute || d%time.Minute != 0 {
				return cmdutil.Usagef("invalid --notification-interval %q (whole minutes, e.g. 30m)", s)
			}
			body["notificationInterval"] = int64(d / time.Minute)
		}
		return nil
	},
}

func init() {
	alertGroupCRUD.Examples.Create = "  mcrl alert-group create --name web --service web --notification-interval 1h\n  mcrl alert-group create -f alert-group.yaml"
	alertGroupCRUD.Examples.Update = "  mcrl alert-group update web --role web:app --role web:db\n  mcrl alert-group update web --memo 'grouped by service'"
	alertGroupCRUD.Examples.Delete = "  mcrl alert-group delete web\n  mcrl alert-group delete 3Ja1bAxcdef --yes"
}

// ---- dashboards ----

var dashboardCRUD = &crud{
	editable: editable{
		Kind:     mackerel.KindDashboard,
		ItemPath: "/dashboards/%s",
		ReadOnly: []string{"id", "createdAt", "updatedAt"},
		Validate: validateDashboard,
	},
	CreatePath: "/dashboards",
	Required:   []string{"title", "urlPath"},
	Bind: func(cmd *cobra.Command) {
		fl := cmd.Flags()
		fl.String("title", "", "title")
		fl.String("url-path", "", `URL path, e.g. "web/overview"`)
		fl.String("memo", "", "memo")
	},
	Apply: func(_ context.Context, cmd *cobra.Command, _ *mackerel.Client, body map[string]any) error {
		setString(cmd, body, "title", "title")
		setString(cmd, body, "url-path", "urlPath")
		setString(cmd, body, "memo", "memo")
		if _, ok := body["widgets"]; !ok {
			body["widgets"] = []any{}
		}
		if _, ok := body["memo"]; !ok {
			body["memo"] = ""
		}
		return nil
	},
}

func init() {
	dashboardCRUD.Examples.Create = "  mcrl dashboard create -f dashboard.yaml\n  mcrl dashboard get overview --jq 'del(.id) | .urlPath = \"overview-copy\"' | mcrl dashboard create -f -"
	dashboardCRUD.Examples.Update = "  mcrl dashboard update overview --title 'Web overview'\n  mcrl dashboard update overview -f widgets.yaml"
	dashboardCRUD.Examples.Delete = "  mcrl dashboard delete overview\n  mcrl dashboard delete 3Ja1bAxcdef --yes"
}

// widgetLimits are {minWidth, minHeight, maxWidth, maxHeight} per widget type.
var widgetLimits = map[string][4]int{
	"graph":       {6, 6, 24, 32},
	"value":       {4, 4, 24, 32},
	"markdown":    {4, 2, 24, 80},
	"alertStatus": {4, 3, 24, 32},
}

// validateDashboard checks the widget layout rules that the API does not
// reject but the dashboard cannot display: 24 columns, sizes, no overlaps.
func validateDashboard(m map[string]any) error {
	widgets, _ := m["widgets"].([]any)
	type rect struct{ x, y, w, h int }
	var rects []rect
	var errs []string
	for i, w := range widgets {
		obj, _ := w.(map[string]any)
		typ, _ := obj["type"].(string)
		layout, _ := obj["layout"].(map[string]any)
		get := func(k string) int {
			n, _ := toInt(layout[k])
			return n
		}
		r := rect{get("x"), get("y"), get("width"), get("height")}
		title, _ := obj["title"].(string)
		where := fmt.Sprintf("widgets[%d] (%s %q)", i, typ, title)
		if r.x < 0 || r.y < 0 {
			errs = append(errs, where+": x and y must be 0 or more")
		}
		if r.x+r.w > 24 {
			errs = append(errs, fmt.Sprintf("%s: x + width = %d exceeds the 24 columns", where, r.x+r.w))
		}
		if lim, ok := widgetLimits[typ]; ok {
			if r.w < lim[0] || r.h < lim[1] || r.w > lim[2] || r.h > lim[3] {
				errs = append(errs, fmt.Sprintf("%s: size %dx%d is outside %dx%d..%dx%d", where, r.w, r.h, lim[0], lim[1], lim[2], lim[3]))
			}
		}
		for j, o := range rects {
			if r.x < o.x+o.w && o.x < r.x+r.w && r.y < o.y+o.h && o.y < r.y+r.h {
				errs = append(errs, fmt.Sprintf("%s overlaps widgets[%d]", where, j))
			}
		}
		rects = append(rects, r)
	}
	if len(errs) > 0 {
		return errors.New("invalid dashboard layout:\n  " + strings.Join(errs, "\n  "))
	}
	return nil
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case interface{ Int64() (int64, error) }:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

// ---- AWS integrations ----

var awsCRUD = &crud{
	editable: editable{
		Kind:     mackerel.KindAWSIntegration,
		ItemPath: "/aws-integrations/%s",
		ReadOnly: []string{"id"},
		// key / secretKey / roleArn / externalId は省略すると変更されない。secretKey は API が
		// 返さないので、今の値と同じ認証情報は送らず、認証方式を意図せず変えないようにする
		Finalize: func(cur, body map[string]any) {
			for _, k := range []string{"key", "secretKey", "roleArn", "externalId"} {
				if v, ok := body[k]; ok && fmt.Sprint(v) == fmt.Sprint(cur[k]) {
					delete(body, k)
				}
			}
		},
	},
	CreatePath: "/aws-integrations",
	Required:   []string{"name"},
	Bind: func(cmd *cobra.Command) {
		cmd.Flags().String("name", "", "name")
		cmd.Flags().String("memo", "", "memo")
		cmd.Flags().String("region", "", "AWS region, e.g. ap-northeast-1")
		cmd.Flags().String("role-arn", "", "IAM role ARN (with an external ID from `mcrl aws external-id`)")
		cmd.Flags().String("external-id", "", "external ID for the IAM role")
	},
	Apply: func(_ context.Context, cmd *cobra.Command, _ *mackerel.Client, body map[string]any) error {
		setString(cmd, body, "name", "name")
		setString(cmd, body, "memo", "memo")
		setString(cmd, body, "region", "region")
		setString(cmd, body, "role-arn", "roleArn")
		setString(cmd, body, "external-id", "externalId")
		return nil
	},
}

func init() {
	awsCRUD.Examples.Create = "  mcrl aws create -f aws.yaml\n  mcrl aws create --name prod --region ap-northeast-1 --role-arn arn:aws:iam::123456789012:role/mackerel --external-id \"$(mcrl aws external-id --jq .externalId)\""
	awsCRUD.Examples.Update = "  mcrl aws update prod --memo 'production account'\n  mcrl aws update prod -f services.yaml"
	awsCRUD.Examples.Delete = "  mcrl aws delete prod\n  mcrl aws delete 3Ja1bAxcdef --yes"
}

func newAWSExternalIDCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:     "external-id",
		Short:   "Generate an external ID for an IAM role integration",
		Long:    withAPI("Generate an external ID for the IAM role trust policy. It can be reused until every integration using it is deleted.", "Read, Write", "POST /api/v0/aws-integrations-external-id"),
		Example: "  mcrl aws external-id\n  mcrl aws external-id --jq .externalId",
		Args:    cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return simpleSend(cmd, f, request{Method: "POST", Path: "/aws-integrations-external-id"})
		},
	}
}

func newAWSExcludableMetricsCmd(f *cmdutil.Factory) *cobra.Command {
	var service string
	cmd := &cobra.Command{
		Use:     "excludable-metrics [--service <aws-service>]",
		Short:   "List the metrics that can be excluded per AWS service",
		Long:    withAPI(`List the metrics that can be excluded, as {"<AWS service>": [metric, ...]}.`, "Read", "GET /api/v0/aws-integrations-excludable-metrics"),
		Example: "  mcrl aws excludable-metrics\n  mcrl aws excludable-metrics --service EC2",
		Args:    cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			b, err := c.Get(cmd.Context(), "/aws-integrations-excludable-metrics", nil)
			if err != nil {
				return err
			}
			if service != "" {
				v, err := mackerel.Field(b, service)
				if err != nil {
					return cmdutil.Usagef("unknown AWS service %q", service)
				}
				return printRaw(f, v)
			}
			return printRaw(f, b)
		},
	}
	cmd.Flags().StringVar(&service, "service", "", "only this AWS service, e.g. EC2, RDS, ALB")
	return cmd
}
