package cli

import (
	"encoding/json"
	"net/url"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/mackerel"
)

// Read-only commands that map directly to list / get endpoints.

func newOrgCmd(f *cmdutil.Factory) *cobra.Command {
	return newGroup("org", "Show the organization and its settings", nil, newIPRestrictionCmd(f), &cobra.Command{
		Use:     "get",
		Short:   "Show the organization of the API key",
		Long:    withAPI("Show the organization of the API key.", "Read", "GET /api/v0/org"),
		Example: "  mcrl org get\n  mcrl org get --jq .name",
		Args:    cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			b, err := c.Get(cmd.Context(), "/org", nil)
			if err != nil {
				return err
			}
			return printRaw(f, b)
		},
	})
}

func newServiceCmd(f *cmdutil.Factory) *cobra.Command {
	return newGroup("service", "Manage services", []string{"services"},
		listCmd(f, "List services", "/services", "services",
			"  mcrl service list\n  mcrl service list --jq '.[].name'"),
		newServiceCreateCmd(f), newServiceDeleteCmd(f))
}

func newRoleCmd(f *cmdutil.Factory) *cobra.Command {
	var service string
	list := &cobra.Command{
		Use:     "list --service <name>",
		Aliases: []string{"ls"},
		Short:   "List roles of a service",
		Long: withAPI(`List roles of a service.

To see the roles of every service, use "mcrl service list", which includes
role names.`, "Read", "GET /api/v0/services/<name>/roles"),
		Example: "  mcrl role list --service web\n  mcrl role list --service web --jq '.[].name'",
		Args:    cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			items, err := c.List(cmd.Context(), "/services/"+url.PathEscape(service)+"/roles", "roles", nil)
			if err != nil {
				return err
			}
			return printItems(f, items)
		},
	}
	list.Flags().StringVar(&service, "service", "", "service name (required)")
	_ = list.MarkFlagRequired("service")
	_ = list.RegisterFlagCompletionFunc("service", func(cmd *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return completeFromList(cmd, f, "/services", "services", nil, "name", "memo"), cobra.ShellCompDirectiveNoFileComp
	})
	return newGroup("role", "Manage roles", []string{"roles"}, list, newRoleCreateCmd(f), newRoleDeleteCmd(f))
}

func newDowntimeCmd(f *cmdutil.Factory) *cobra.Command {
	var active bool
	dlist := listCmd(f, "List downtimes", "/downtimes", "downtimes",
		"  mcrl downtime list\n  mcrl downtime list --active --jq '.[].name'")
	dlist.Long = withAPI(`List downtimes.

--active keeps only the downtimes in effect now. Recurring downtimes are
evaluated in the local time zone.`, "Read", "GET /api/v0/downtimes")
	dlist.RunE = func(cmd *cobra.Command, _ []string) error {
		c, err := apiClient(cmd.Context(), f)
		if err != nil {
			return err
		}
		items, err := c.List(cmd.Context(), "/downtimes", "downtimes", nil)
		if err != nil {
			return err
		}
		if active {
			now := time.Now()
			kept := []json.RawMessage{}
			for _, it := range items {
				if downtimeActive(it, now) {
					kept = append(kept, it)
				}
			}
			items = kept
		}
		return printItems(f, items)
	}
	dlist.Flags().BoolVar(&active, "active", false, "only downtimes in effect now")
	return newGroup("downtime", "Manage downtimes", []string{"downtimes"},
		dlist,
		getCmd(f, mackerel.KindDowntime, "downtime", "  mcrl downtime get weekly-maintenance\n  mcrl downtime get 3Ja1bAxcdef"),
		newDowntimeCreateCmd(f), newDowntimeUpdateCmd(f), newEditCmd(f, downtimeEditable, "downtime"), newDowntimeDeleteCmd(f))
}

func newChannelCmd(f *cmdutil.Factory) *cobra.Command {
	return newGroup("channel", "Manage notification channels", []string{"channels"},
		listCmd(f, "List notification channels", "/channels", "channels",
			"  mcrl channel list\n  mcrl channel list --jq '.[] | select(.type == \"slack\") | .name'"),
		getCmd(f, mackerel.KindChannel, "channel", "  mcrl channel get ops-slack\n  mcrl channel get 2wvKpqFzgD9"),
		channelCRUD.createCmd(f), channelCRUD.updateCmd(f), newEditCmd(f, channelCRUD.editable, "channel"), channelCRUD.deleteCmd(f))
}

func newNotificationGroupCmd(f *cmdutil.Factory) *cobra.Command {
	return newGroup("notification-group", "Manage notification groups", []string{"notification-groups", "ng"},
		listCmd(f, "List notification groups", "/notification-groups", "notificationGroups",
			"  mcrl notification-group list\n  mcrl ng list --jq '.[].name'"),
		getCmd(f, mackerel.KindNotificationGroup, "notification-group", "  mcrl ng get Default\n  mcrl ng get 2wvKpqFzgD9"),
		notificationGroupCRUD.createCmd(f), notificationGroupCRUD.updateCmd(f), newEditCmd(f, notificationGroupCRUD.editable, "notification-group"), notificationGroupCRUD.deleteCmd(f))
}

func newAlertGroupCmd(f *cmdutil.Factory) *cobra.Command {
	return newGroup("alert-group", "Manage alert group settings", []string{"alert-groups"},
		listCmd(f, "List alert group settings", "/alert-group-settings", "alertGroupSettings",
			"  mcrl alert-group list\n  mcrl alert-group list --jq '.[].name'"),
		getCmd(f, mackerel.KindAlertGroup, "alert-group", "  mcrl alert-group get web\n  mcrl alert-group get 3Ja1bAxcdef"),
		alertGroupCRUD.createCmd(f), alertGroupCRUD.updateCmd(f), newEditCmd(f, alertGroupCRUD.editable, "alert-group"), alertGroupCRUD.deleteCmd(f))
}

func newDashboardCmd(f *cmdutil.Factory) *cobra.Command {
	return newGroup("dashboard", "Manage dashboards", []string{"dashboards"},
		listCmd(f, "List dashboards", "/dashboards", "dashboards",
			"  mcrl dashboard list\n  mcrl dashboard list --jq '.[] | {id, title, urlPath}'"),
		getCmd(f, mackerel.KindDashboard, "dashboard", "  mcrl dashboard get overview      # by URL path\n  mcrl dashboard get 'Web overview'  # by title"),
		dashboardCRUD.createCmd(f), dashboardCRUD.updateCmd(f), newEditCmd(f, dashboardCRUD.editable, "dashboard"), dashboardCRUD.deleteCmd(f))
}

func newUserCmd(f *cmdutil.Factory) *cobra.Command {
	return newGroup("user", "Manage organization members", []string{"users"},
		listCmd(f, "List organization members", "/users", "users",
			"  mcrl user list\n  mcrl user list --jq '.[] | select(.isMFAEnabled | not) | .email'"),
		newUserRemoveCmd(f))
}

func newAWSCmd(f *cmdutil.Factory) *cobra.Command {
	return newGroup("aws", "Manage AWS integrations", []string{"aws-integration", "aws-integrations"},
		listCmd(f, "List AWS integrations", "/aws-integrations", "aws_integrations",
			"  mcrl aws list\n  mcrl aws list --jq '.[] | {id, name, region}'"),
		getCmd(f, mackerel.KindAWSIntegration, "aws-integration", "  mcrl aws get production\n  mcrl aws get 3Ja1bAxcdef"),
		awsCRUD.createCmd(f), awsCRUD.updateCmd(f), newEditCmd(f, awsCRUD.editable, "aws-integration"), awsCRUD.deleteCmd(f),
		newAWSExternalIDCmd(f), newAWSExcludableMetricsCmd(f))
}

func newCheckCmd(f *cmdutil.Factory) *cobra.Command {
	var limit int
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List check monitors",
		Long:    withAPI("List check monitors (monitors created from check reports).", "Read", "GET /api/v0/monitoring/checks"),
		Example: "  mcrl check list\n  mcrl check list --jq '.[].name'",
		Args:    cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			items, err := collect(c.NextIDPages(cmd.Context(), "/monitoring/checks", "checks", nil, mackerel.PageSize), limit, nil)
			if err != nil {
				return err
			}
			return printItems(f, items)
		},
	}
	list.Flags().IntVar(&limit, "limit", 0, "maximum number of check monitors (0 = all)")
	return newGroup("check", "Manage check monitoring", []string{"checks"}, list,
		getCmd(f, mackerel.KindCheckMonitor, "check", "  mcrl check get backup\n  mcrl check get 3Ja1bAxcdef"),
		newCheckReportCmd(f), newCheckRunCmd(f))
}

func limitUsage(limit int) error {
	if limit < 0 {
		return cmdutil.Usagef("--limit must be 0 or more, got %s", strconv.Itoa(limit))
	}
	return nil
}
