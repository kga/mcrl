package cli

import (
	"context"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/mackerel"
)

func newMetricCmd(f *cmdutil.Factory) *cobra.Command {
	return newGroup("metric", "Read and post metrics", []string{"metrics"},
		newMetricGetCmd(f), newMetricLatestCmd(f), newMetricNamesCmd(f), newMetricPostCmd(f), newGraphDefCmd(f))
}

// target is the --host / --service pair shared by metric commands.
type target struct{ host, service string }

func (t *target) bind(cmd *cobra.Command, f *cmdutil.Factory) {
	cmd.Flags().StringVar(&t.host, "host", "", "host (ID, name or cid:<custom identifier>)")
	cmd.Flags().StringVar(&t.service, "service", "", "service name")
	cmd.MarkFlagsMutuallyExclusive("host", "service")
	cmd.MarkFlagsOneRequired("host", "service")
	_ = cmd.RegisterFlagCompletionFunc("host", completeHosts(f))
	_ = cmd.RegisterFlagCompletionFunc("service", func(cmd *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return completeFromList(cmd, f, "/services", "services", nil, "name", "memo"), cobra.ShellCompDirectiveNoFileComp
	})
}

// path returns "/hosts/<id>" or "/services/<name>".
func (t *target) path(ctx context.Context, c *mackerel.Client) (string, error) {
	if t.service != "" {
		return "/services/" + url.PathEscape(t.service), nil
	}
	id, err := c.ResolveHostID(ctx, t.host)
	if err != nil {
		return "", err
	}
	return "/hosts/" + url.PathEscape(id), nil
}

func newMetricGetCmd(f *cmdutil.Factory) *cobra.Command {
	var (
		t            target
		name         string
		since, until string
	)
	cmd := &cobra.Command{
		Use:   "get (--host <host> | --service <service>) --name <metric>",
		Short: "Get a metric's time series",
		Long: withAPI(`Get a host or service metric's time series as [{"time", "value"}, ...].

Over a range of a day or longer, the API returns values aggregated over
coarser intervals instead of the raw one-minute points.

--since and --until accept "now", durations ago ("1h", "7d"), relative
times ("-30m"), RFC 3339, "2026-11-06 10:00" (local time) and epoch
seconds ("@1762390800").`, "Read", "GET /api/v0/hosts/<id>/metrics", "GET /api/v0/services/<name>/metrics"),
		Example: `  mcrl metric get --host web-1 --name loadavg5
  mcrl metric get --service web --name response_time --since 6h --jq 'map(.value) | add / length'`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			from, to, err := timeRange(since, until)
			if err != nil {
				return err
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			p, err := t.path(cmd.Context(), c)
			if err != nil {
				return err
			}
			q := url.Values{"name": {name}, "from": {strconv.FormatInt(from, 10)}, "to": {strconv.FormatInt(to, 10)}}
			items, err := c.List(cmd.Context(), p+"/metrics", "metrics", q)
			if err != nil {
				return err
			}
			return printItems(f, items)
		},
	}
	t.bind(cmd, f)
	cmd.Flags().StringVar(&name, "name", "", "metric name, e.g. loadavg5 (required)")
	cmd.Flags().StringVar(&since, "since", "1h", "start of the time range")
	cmd.Flags().StringVar(&until, "until", "now", "end of the time range")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newMetricLatestCmd(f *cmdutil.Factory) *cobra.Command {
	var hosts, names []string
	cmd := &cobra.Command{
		Use:   "latest --host <host>... --name <metric>...",
		Short: "Get the latest values of host metrics",
		Long: withAPI(`Get the latest values of host metrics.

The output is the API's {"<hostId>": {"<metric>": {"time", "value"}}} object.`,
			"Read", "GET /api/v0/tsdb/latest"),
		Example: `  mcrl metric latest --host web-1 --host web-2 --name loadavg5 --name memory.used
  mcrl metric latest --host web-1 --name loadavg5 --jq '.[].loadavg5.value'`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			q := url.Values{"name": names}
			for _, h := range hosts {
				id, err := c.ResolveHostID(cmd.Context(), h)
				if err != nil {
					return err
				}
				q.Add("hostId", id)
			}
			b, err := c.Get(cmd.Context(), "/tsdb/latest", q)
			if err != nil {
				return err
			}
			v, err := mackerel.Field(b, "tsdbLatest")
			if err != nil {
				return err
			}
			return printRaw(f, v)
		},
	}
	cmd.Flags().StringArrayVar(&hosts, "host", nil, "host (repeatable, required)")
	cmd.Flags().StringArrayVar(&names, "name", nil, "metric name (repeatable, required)")
	_ = cmd.MarkFlagRequired("host")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.RegisterFlagCompletionFunc("host", completeHosts(f))
	return cmd
}

func newMetricNamesCmd(f *cmdutil.Factory) *cobra.Command {
	var t target
	cmd := &cobra.Command{
		Use:   "names (--host <host> | --service <service>)",
		Short: "List the metric names of a host or service",
		Long: withAPI("List the metric names of a host or service.", "Read",
			"GET /api/v0/hosts/<id>/metric-names", "GET /api/v0/services/<name>/metric-names"),
		Example: "  mcrl metric names --host web-1\n  mcrl metric names --service web --jq '.[] | select(startswith(\"custom.\"))'",
		Args:    cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			p, err := t.path(cmd.Context(), c)
			if err != nil {
				return err
			}
			items, err := c.List(cmd.Context(), p+"/metric-names", "names", nil)
			if err != nil {
				return err
			}
			return printItems(f, items)
		},
	}
	t.bind(cmd, f)
	return cmd
}

func newAnnotationCmd(f *cmdutil.Factory) *cobra.Command {
	var service, since, until string
	list := &cobra.Command{
		Use:     "list --service <service>",
		Aliases: []string{"ls"},
		Short:   "List graph annotations of a service",
		Long: withAPI(`List graph annotations of a service that overlap the time range
(default: the last 7 days).`, "Read", "GET /api/v0/graph-annotations"),
		Example: "  mcrl annotation list --service web\n  mcrl annotation list --service web --since 30d --jq '.[].title'",
		Args:    cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			from, to, err := timeRange(since, until)
			if err != nil {
				return err
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			q := url.Values{"service": {service}, "from": {strconv.FormatInt(from, 10)}, "to": {strconv.FormatInt(to, 10)}}
			items, err := c.List(cmd.Context(), "/graph-annotations", "graphAnnotations", q)
			if err != nil {
				return err
			}
			return printItems(f, items)
		},
	}
	list.Flags().StringVar(&service, "service", "", "service name (required)")
	list.Flags().StringVar(&since, "since", "7d", "start of the time range")
	list.Flags().StringVar(&until, "until", "now", "end of the time range")
	_ = list.MarkFlagRequired("service")
	return newGroup("annotation", "Manage graph annotations", []string{"annotations"}, list,
		newAnnotationCreateCmd(f), newAnnotationUpdateCmd(f), newAnnotationDeleteCmd(f), newAnnotationWrapCmd(f))
}
