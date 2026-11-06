package cli

import (
	"encoding/json"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/mackerel"
)

const maxAlertMemo = 80 * 1024

func newAlertCloseCmd(f *cmdutil.Factory) *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "close <alert-id>... --reason <text>",
		Short: "Close alerts",
		Long: withAPI(`Close alerts. Pass "-" to read alert IDs from stdin, one per line.

The output is an array of the closed alerts.`, "Read, Write", "POST /api/v0/alerts/<id>/close"),
		Example: `  mcrl alert close 2bj4Jd2KFaP --reason "fixed by restarting nginx"
  mcrl alert list --monitor 'disk usage' --jq '.[].id' | mcrl alert close - --reason "disk cleaned up"`,
		Args: cmdutil.MinimumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := expandArgs(f, args)
			if err != nil {
				return err
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			reqs := make([]request, len(ids))
			for i, id := range ids {
				reqs[i] = post("/alerts/"+url.PathEscape(id)+"/close", map[string]string{"reason": reason})
			}
			bodies, dry, err := sendAll(cmd, f, c, reqs)
			if dry {
				return err
			}
			items := make([]json.RawMessage, len(bodies))
			for i, b := range bodies {
				items[i] = mackerel.UnwrapIf(b, "alert")
			}
			if err != nil {
				_ = printItems(f, items) // 途中で失敗しても、閉じたものは出す
				return mackerel.AsNotFound(err, "alert", ids[len(bodies)])
			}
			return printItems(f, items)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why the alerts are closed (required)")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

func newAlertMemoCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:     "memo <alert-id> <text>",
		Short:   "Set the memo of an alert",
		Long:    withAPI(`Set the memo of an alert. Pass "-" as the text to read it from stdin.`, "Read, Write", "PUT /api/v0/alerts/<id>"),
		Example: "  mcrl alert memo 2bj4Jd2KFaP 'investigating: https://example.com/incident/123'\n  journalctl -u app --since -10m | mcrl alert memo 2bj4Jd2KFaP -",
		Args:    cmdutil.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			memo := args[1]
			if memo == "-" {
				b, err := readAllString(f)
				if err != nil {
					return err
				}
				memo = b
			}
			if len(memo) > maxAlertMemo {
				return cmdutil.Usagef("the memo is %d bytes; the API accepts at most 80KB", len(memo))
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			bodies, dry, err := sendAll(cmd, f, c, []request{put("/alerts/"+url.PathEscape(args[0]), map[string]string{"memo": memo})})
			if dry {
				return err
			}
			if err != nil {
				return mackerel.AsNotFound(err, "alert", args[0])
			}
			return printOne(f, bodies[0])
		},
	}
}
