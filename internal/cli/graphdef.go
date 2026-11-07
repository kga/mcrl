package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
)

// graphDefName is the format of a host graph definition name.
var graphDefName = regexp.MustCompile(`^custom(\.([-a-zA-Z0-9_]+|[*#]))+$`)

func newGraphDefCmd(f *cmdutil.Factory) *cobra.Command {
	return newGroup("graph-def", "Create and delete graph definitions", []string{"graph-defs"},
		newGraphDefCreateCmd(f), newGraphDefDeleteCmd(f))
}

func newGraphDefCreateCmd(f *cmdutil.Factory) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "create -f <file>",
		Short: "Create or replace graph definitions of custom host metrics",
		Long: withAPI(`Create or replace graph definitions of custom host metrics, from a JSON or
YAML file in the API's format: an array of graph definitions, or one.

A graph definition sets how the custom metrics whose names start with its
name ("custom.<...>", where "*" and "#" are wildcards) are drawn: the
graph's display name, unit, and each metric's display name and stacking.
The API has no way to read graph definitions back, so the output is the
API's response ({"success": true}).`, "Read, Write", "POST /api/v0/graph-defs/create"),
		Example: `  mcrl metric graph-def create -f graph-defs.yaml
  echo '{"name": "custom.queue", "unit": "integer", "metrics": [{"name": "custom.queue.length"}]}' | mcrl metric graph-def create -f -`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			raw, err := readDocument(f, file)
			if err != nil {
				return err
			}
			if bytes.HasPrefix(raw, []byte("{")) {
				raw = append(append([]byte("["), raw...), ']')
			}
			var defs []map[string]any
			if err := json.Unmarshal(raw, &defs); err != nil {
				return cmdutil.Usagef("%s must contain a graph definition or an array of them", file)
			}
			if len(defs) == 0 {
				return cmdutil.Usagef("%s has no graph definitions", file)
			}
			for i, d := range defs {
				name, _ := d["name"].(string)
				if !graphDefName.MatchString(name) {
					return cmdutil.Usagef("graph definition %d: invalid name %q: it must start with \"custom.\" (letters, digits, -, _, * and # separated by dots)", i+1, name)
				}
			}
			return simpleSend(cmd, f, post("/graph-defs/create", raw))
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", `JSON or YAML file ("-" for stdin) (required)`)
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newGraphDefDeleteCmd(f *cmdutil.Factory) *cobra.Command {
	var service string
	cmd := &cobra.Command{
		Use:     "delete [--service <service>] <name>",
		Aliases: []string{"rm"},
		Short:   "Delete a graph definition",
		Long: withAPI(`Delete the graph definition of custom host metrics with the name, or with
--service the graph definition of service metrics. The graph comes back
when metrics it covers are posted again. The output is the API's response
({"success": true}).`, "Read, Write", "DELETE /api/v0/graph-defs", "DELETE /api/v0/services/<name>/graph-defs/<graph>"),
		Example: `  mcrl metric graph-def delete custom.queue
  mcrl metric graph-def delete --service web 'custom.signups.*' --yes`,
		Args: cmdutil.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			r := request{Method: http.MethodDelete, Path: "/graph-defs", Body: map[string]string{"name": name}}
			what := fmt.Sprintf("the graph definition %q", name)
			if service != "" {
				// The API does not decode "%2A", so "*" goes into the path as is.
				r = del("/services/" + url.PathEscape(service) + "/graph-defs/" + strings.ReplaceAll(url.PathEscape(name), "%2A", "*"))
				what += fmt.Sprintf(" of service %q", service)
			}
			if !f.DryRun {
				if err := confirm(f, "Delete "+what+"?"); err != nil {
					return err
				}
			}
			return simpleSend(cmd, f, r)
		},
	}
	cmd.Flags().StringVar(&service, "service", "", "delete a graph definition of this service's metrics")
	_ = cmd.RegisterFlagCompletionFunc("service", func(cmd *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return completeFromList(cmd, f, "/services", "services", nil, "name", "memo"), cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}
