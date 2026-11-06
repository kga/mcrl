package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/mackerel"
)

func newMonitorCreateCmd(f *cmdutil.Factory) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:     "create -f <file>",
		Aliases: []string{"new"},
		Short:   "Create a monitor from a JSON or YAML file",
		Long: withAPI(`Create a monitor from a JSON or YAML file in the API's format.
Start from "mcrl monitor template <type>".`, "Read, Write", "POST /api/v0/monitors"),
		Example: `  mcrl monitor template host > cpu.yaml && vi cpu.yaml && mcrl monitor create -f cpu.yaml
  mcrl monitor get 'cpu high' --jq 'del(.id) | .name = "cpu high (staging)"' | mcrl monitor create -f -`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := readObject(f, file)
			if err != nil {
				return err
			}
			if _, ok := body["type"]; !ok {
				return cmdutil.Usagef(`%s has no "type"`, file)
			}
			delete(body, "id")
			return simpleSendUnwrap(cmd, f, post("/monitors", body), "monitor")
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", `JSON or YAML file ("-" for stdin) (required)`)
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newMonitorUpdateCmd(f *cmdutil.Factory) *cobra.Command {
	var (
		file    string
		replace bool
	)
	cmd := &cobra.Command{
		Use:   "update <monitor> -f <file>",
		Short: "Update a monitor from a JSON or YAML file",
		Long: withAPI(`Update a monitor.

The API replaces the whole monitor: omitted fields such as isMute, scopes and
excludeScopes are reset. To avoid that, the fields in the file are merged
into the current monitor before sending, so the file only needs the fields
to change. --replace sends the file as the complete monitor instead.`, "Read, Write", "GET /api/v0/monitors/<id>", "PUT /api/v0/monitors/<id>"),
		Example: `  echo 'warning: 85' | mcrl monitor update 'cpu high' -f -
  mcrl monitor update 2cSZzK3XfmG -f monitor.yaml --replace`,
		Args:              cmdutil.ExactArgs(1),
		ValidArgsFunction: completeKind(f, mackerel.KindMonitor),
		RunE: func(cmd *cobra.Command, args []string) error {
			patch, err := readObject(f, file)
			if err != nil {
				return err
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			cur, err := c.Resolve(cmd.Context(), mackerel.KindMonitor, args[0])
			if err != nil {
				return err
			}
			id := mackerel.StringField(cur, "id")
			body := patch
			if !replace {
				if body, err = decodeMap(cur); err != nil {
					return err
				}
				for k, v := range patch {
					body[k] = v
				}
			}
			delete(body, "id")
			if t, ok := patch["type"]; ok && t != mackerel.StringField(cur, "type") {
				return cmdutil.Usagef("the type of a monitor cannot be changed (%s -> %v)", mackerel.StringField(cur, "type"), t)
			}
			return simpleSendUnwrap(cmd, f, put("/monitors/"+url.PathEscape(id), body), "monitor")
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", `JSON or YAML file ("-" for stdin) (required)`)
	cmd.Flags().BoolVar(&replace, "replace", false, "send the file as the complete monitor without merging")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newMonitorDeleteCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:               "delete <monitor>",
		Aliases:           []string{"rm"},
		Short:             "Delete a monitor",
		Long:              withAPI("Delete a monitor. The output is the monitor as it was before deletion.", "Read, Write", "DELETE /api/v0/monitors/<id>"),
		Example:           "  mcrl monitor delete 'cpu high'\n  mcrl monitor delete 2cSZzK3XfmG --yes",
		Args:              cmdutil.ExactArgs(1),
		ValidArgsFunction: completeKind(f, mackerel.KindMonitor),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			cur, err := c.Resolve(cmd.Context(), mackerel.KindMonitor, args[0])
			if err != nil {
				return err
			}
			id := mackerel.StringField(cur, "id")
			if !f.DryRun {
				if err := confirm(f, fmt.Sprintf("Delete monitor %q (%s)?", mackerel.StringField(cur, "name"), id)); err != nil {
					return err
				}
			}
			return simpleSendUnwrap(cmd, f, del("/monitors/"+url.PathEscape(id)), "monitor")
		},
	}
}

func newMonitorMuteCmd(f *cmdutil.Factory, mute bool) *cobra.Command {
	verb := "mute"
	short := "Mute monitors"
	if !mute {
		verb, short = "unmute", "Unmute monitors"
	}
	return &cobra.Command{
		Use:   verb + " <monitor>...",
		Short: short,
		Long: withAPI(short+`. Only isMute is changed; the rest of each monitor is sent back as is.
The output is an array of the updated monitors.`, "Read, Write", "GET /api/v0/monitors/<id>", "PUT /api/v0/monitors/<id>"),
		Example:           fmt.Sprintf("  mcrl monitor %s 'cpu high'\n  mcrl monitor list --type host --jq '.[].id' | mcrl monitor %s -", verb, verb),
		Args:              cmdutil.MinimumArgs(1),
		ValidArgsFunction: completeKind(f, mackerel.KindMonitor),
		RunE: func(cmd *cobra.Command, args []string) error {
			refs, err := expandArgs(f, args)
			if err != nil {
				return err
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			var reqs []request
			for _, r := range refs {
				cur, err := c.Resolve(cmd.Context(), mackerel.KindMonitor, r)
				if err != nil {
					return err
				}
				body, err := decodeMap(cur)
				if err != nil {
					return err
				}
				id := mackerel.StringField(cur, "id")
				delete(body, "id")
				body["isMute"] = mute
				reqs = append(reqs, put("/monitors/"+url.PathEscape(id), body))
			}
			bodies, dry, err := sendAll(cmd, f, c, reqs)
			if dry {
				return err
			}
			items := make([]json.RawMessage, len(bodies))
			for i, b := range bodies {
				items[i] = mackerel.UnwrapIf(b, "monitor")
			}
			if perr := printItems(f, items); perr != nil && err == nil {
				err = perr
			}
			return err
		},
	}
}

// simpleSendUnwrap is simpleSend that strips a wrapper key from the response.
func simpleSendUnwrap(cmd *cobra.Command, f *cmdutil.Factory, r request, key string) error {
	c, err := apiClient(cmd.Context(), f)
	if err != nil {
		return err
	}
	bodies, dry, err := sendAll(cmd, f, c, []request{r})
	if dry || err != nil {
		return err
	}
	return printOne(f, mackerel.UnwrapIf(bodies[0], key))
}

func newMonitorTemplateCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "template <type>",
		Short: "Print a commented YAML template of a monitor",
		Long: `Print a commented YAML template of a monitor for "mcrl monitor create -f".
Types: ` + strings.Join(monitorTypes, ", ") + `.

This is the only command that prints YAML: it is input for you to edit, not
API output.`,
		Example:   "  mcrl monitor template host > cpu.yaml\n  mcrl monitor template external",
		Args:      cmdutil.ExactArgs(1),
		ValidArgs: monitorTypes,
		RunE: func(cmd *cobra.Command, args []string) error {
			t, ok := monitorTemplates[args[0]]
			if !ok {
				return cmdutil.Usagef("unknown monitor type %q (valid: %s)", args[0], strings.Join(monitorTypes, ", "))
			}
			_, err := io.WriteString(f.IO.Out, strings.TrimLeft(t, "\n"))
			return err
		},
	}
}

const commonTemplate = `memo: ""                      # optional, up to 2048 characters
notificationInterval: 60      # optional, minutes between re-notifications (10 or more)
isMute: false
`

var monitorTemplates = map[string]string{
	"host": `
type: host
name: cpu high
metric: cpu%                  # cpu%, memory%, disk%, swap%, container-cpu%, container-memory%, or a metric name
operator: ">"                 # ">" or "<"
warning: 80                   # optional
critical: 95                  # optional
duration: 5                   # minutes to average, 1-10
maxCheckAttempts: 1           # optional, 1-10
scopes: [web:app]             # optional, services or <service>:<role>
excludeScopes: []             # optional
` + commonTemplate,
	"connectivity": `
type: connectivity
name: connectivity            # optional
alertStatusOnGone: CRITICAL   # CRITICAL or WARNING
scopes: []                    # optional
excludeScopes: []             # optional
` + commonTemplate,
	"service": `
type: service
name: response time
service: web
metric: response_time
operator: ">"
warning: 1.0
critical: 3.0
duration: 3                   # the latest 1-10 points (not minutes)
maxCheckAttempts: 1           # optional, 1-10
missingDurationWarning: 30    # optional, minutes, multiple of 10
missingDurationCritical: 60   # optional
` + commonTemplate,
	"external": `
type: external
name: example.com
url: https://example.com/health
method: GET                   # GET, POST, PUT or DELETE
expectedStatusCode: 200       # optional; default accepts 2xx and 3xx
containsString: ok            # optional
service: web                  # needed to monitor the response time
responseTimeWarning: 3000     # optional, milliseconds
responseTimeCritical: 5000    # optional
responseTimeDuration: 3       # optional, minutes, 1-10
certificationExpirationWarning: 30   # optional, days
certificationExpirationCritical: 14  # optional
skipCertificateVerification: false
followRedirect: false
headers: []                   # optional, [{name: X-Foo, value: bar}]
requestBody: ""               # optional
maxCheckAttempts: 1           # optional, 1-10
` + commonTemplate,
	"expression": `
type: expression
name: average load of web:app
expression: avg(roleSlots("web:app","loadavg5"))
operator: ">"
warning: 5
critical: 10
evaluateBackwardMinutes: 2    # optional, 2-10
` + commonTemplate,
	"anomalyDetection": `
type: anomalyDetection
name: anomaly of web:app
scopes: [web:app]
warningSensitivity: normal    # insensitive, normal or sensitive
criticalSensitivity: insensitive
maxCheckAttempts: 3           # optional, 1-10
` + commonTemplate,
	"query": `
type: query
name: http error rate
query: sum(rate(http.server.request.count{http.response.status_code=~"5.."}[5m]))
legend: "{{service.name}}"    # optional
operator: ">"
warning: 1
critical: 5
evaluateBackwardMinutes: 0    # optional, 0-10
` + commonTemplate,
}
