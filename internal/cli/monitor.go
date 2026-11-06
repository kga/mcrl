package cli

import (
	"encoding/json"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/mackerel"
)

var monitorTypes = []string{"host", "connectivity", "service", "external", "expression", "anomalyDetection", "query"}

func newMonitorCmd(f *cmdutil.Factory) *cobra.Command {
	return newGroup("monitor", "Manage monitors", []string{"monitors"},
		newMonitorListCmd(f),
		getCmd(f, mackerel.KindMonitor, "monitor", "  mcrl monitor get 'cpu high'\n  mcrl monitor get 2cSZzK3XfmG --jq '{name, isMute}'"),
		newMonitorCreateCmd(f), newMonitorUpdateCmd(f), newMonitorDeleteCmd(f),
		newMonitorMuteCmd(f, true), newMonitorMuteCmd(f, false), newMonitorTemplateCmd(f))
}

func newMonitorListCmd(f *cmdutil.Factory) *cobra.Command {
	var types []string
	var name string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List monitors",
		Long: withAPI(`List monitors, ordered by type and name.

--type and --name filter on the client side. --name matches a substring,
case-insensitively.`, "Read", "GET /api/v0/monitors"),
		Example: `  mcrl monitor list
  mcrl monitor list --type host --type service
  mcrl monitor list --name cpu --jq '.[] | {id, name, isMute}'`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, t := range types {
				if !contains(monitorTypes, t) {
					return cmdutil.Usagef("invalid --type %q (valid: %s)", t, strings.Join(monitorTypes, ", "))
				}
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			items, err := c.List(cmd.Context(), "/monitors", "monitors", nil)
			if err != nil {
				return err
			}
			out := []json.RawMessage{}
			for _, it := range items {
				if len(types) > 0 && !contains(types, mackerel.StringField(it, "type")) {
					continue
				}
				if name != "" && !strings.Contains(strings.ToLower(mackerel.StringField(it, "name")), strings.ToLower(name)) {
					continue
				}
				out = append(out, it)
			}
			return printItems(f, out)
		},
	}
	cmd.Flags().StringArrayVar(&types, "type", nil, "only monitors of this type (repeatable): "+strings.Join(monitorTypes, ", "))
	cmd.Flags().StringVar(&name, "name", "", "only monitors whose name contains this text")
	_ = cmd.RegisterFlagCompletionFunc("type", cobra.FixedCompletions(monitorTypes, cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
