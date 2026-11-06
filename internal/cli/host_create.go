package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/mackerel"
)

// hostInputFields are the fields of POST/PUT /hosts. Everything else in a
// host object (id, status, roles, size, createdAt, ...) is read-only.
var hostInputFields = []string{"name", "displayName", "customIdentifier", "memo", "meta", "interfaces", "roleFullnames", "checks"}

type hostFlags struct {
	file                         string
	name, displayName, memo, cid string
	roles, checks                []string
}

func (h *hostFlags) bind(cmd *cobra.Command, create bool) {
	fl := cmd.Flags()
	fl.StringVarP(&h.file, "file", "f", "", `JSON or YAML file in the API's format ("-" for stdin); flags override its fields`)
	fl.StringVar(&h.name, "name", "", "host name")
	fl.StringVar(&h.displayName, "display-name", "", "display name")
	fl.StringVar(&h.memo, "memo", "", "memo")
	fl.StringVar(&h.cid, "custom-identifier", "", `custom identifier ("" clears it)`)
	if create {
		fl.StringArrayVar(&h.roles, "role", nil, "role as <service>:<role> (repeatable; missing services and roles are created)")
	}
	fl.StringArrayVar(&h.checks, "check", nil, "check monitor name the host reports, as name or name=memo (repeatable; replaces the list)")
}

func (h *hostFlags) apply(cmd *cobra.Command, body map[string]any) error {
	setString(cmd, body, "name", "name")
	setString(cmd, body, "display-name", "displayName")
	setString(cmd, body, "memo", "memo")
	setString(cmd, body, "custom-identifier", "customIdentifier")
	if cmd.Flags().Changed("role") {
		for _, r := range h.roles {
			if _, _, err := splitRole(r); err != nil {
				return err
			}
		}
		body["roleFullnames"] = h.roles
	}
	if cmd.Flags().Changed("check") {
		list := make([]map[string]string, 0, len(h.checks))
		for _, c := range h.checks {
			name, memo, _ := strings.Cut(c, "=")
			if !validCheckName(name) {
				return cmdutil.Usagef("invalid --check name %q: use letters, digits, - and _", name)
			}
			item := map[string]string{"name": name}
			if memo != "" {
				item["memo"] = memo
			}
			list = append(list, item)
		}
		body["checks"] = list
	}
	return nil
}

func newHostCreateCmd(f *cmdutil.Factory) *cobra.Command {
	var (
		h         hostFlags
		noRefetch bool
	)
	cmd := &cobra.Command{
		Use:     "create --name <name> [flags]",
		Aliases: []string{"new"},
		Short:   "Register a host",
		Long: withAPI(`Register a host without mackerel-agent, for example to post metrics or check
results for something that cannot run the agent. The output is the
registered host (fetched again after registration).

"meta" defaults to {}. Interfaces and the full meta can be given with -f.`, "Read, Write", "POST /api/v0/hosts"),
		Example: `  mcrl host create --name router-1 --role network:router --custom-identifier router-1.example.com
  mcrl host create --name batch-1 --check backup --check 'nightly=daily batch' --memo 'no agent'`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := map[string]any{}
			if h.file != "" {
				var err error
				if body, err = readObject(f, h.file); err != nil {
					return err
				}
			}
			if err := h.apply(cmd, body); err != nil {
				return err
			}
			onlyInputFields(body)
			if s, _ := body["name"].(string); s == "" {
				return cmdutil.Usagef("--name is required")
			}
			if _, ok := body["meta"]; !ok {
				body["meta"] = map[string]any{}
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			bodies, dry, err := sendAll(cmd, f, c, []request{post("/hosts", body)})
			if dry || err != nil {
				return err
			}
			if noRefetch {
				return printOne(f, bodies[0])
			}
			return refetchHost(cmd, f, c, mackerel.StringField(bodies[0], "id"), bodies[0])
		},
	}
	h.bind(cmd, true)
	addNoRefetch(cmd, &noRefetch)
	return cmd
}

func newHostUpdateCmd(f *cmdutil.Factory) *cobra.Command {
	var (
		h         hostFlags
		noRefetch bool
	)
	cmd := &cobra.Command{
		Use:   "update <host> [flags]",
		Short: "Update a host's name, display name, memo, custom identifier, meta or checks",
		Long: withAPI(`Update a host.

The API replaces the host's information as a whole, so the current values are
fetched and the flags (and -f fields) are applied on top; fields that are not
given keep their values. Roles are changed with "mcrl host role".
mackerel-agent overwrites meta and interfaces of hosts it runs on.

`+hostRefHelp, "Read, Write", "GET /api/v0/hosts/<id>", "PUT /api/v0/hosts/<id>"),
		Example: `  mcrl host update web-1 --display-name 'Web #1' --memo 'replaced disk 2026-11'
  mcrl host update router-1 --custom-identifier ''`,
		Args:              cmdutil.ExactArgs(1),
		ValidArgsFunction: completeHosts(f),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			cur, err := c.ResolveHost(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			body, err := hostInput(cur)
			if err != nil {
				return err
			}
			if h.file != "" {
				patch, err := readObject(f, h.file)
				if err != nil {
					return err
				}
				for k, v := range patch {
					body[k] = v
				}
			}
			if err := h.apply(cmd, body); err != nil {
				return err
			}
			onlyInputFields(body)
			id := mackerel.StringField(cur, "id")
			bodies, dry, err := sendAll(cmd, f, c, []request{put("/hosts/"+url.PathEscape(id), body)})
			if dry || err != nil {
				return err
			}
			if noRefetch {
				return printOne(f, bodies[0])
			}
			return refetchHost(cmd, f, c, id, bodies[0])
		},
	}
	h.bind(cmd, false)
	addNoRefetch(cmd, &noRefetch)
	return cmd
}

func newHostEditCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "edit <host>",
		Short: "Edit a host's information in $EDITOR",
		Long: withAPI(`Open the host's updatable fields (name, displayName, customIdentifier, memo,
meta, interfaces) as YAML in $VISUAL or $EDITOR, then show the changes and
send them after confirmation. Roles are changed with "mcrl host role".

`+hostRefHelp, "Read, Write", "GET /api/v0/hosts/<id>", "PUT /api/v0/hosts/<id>"),
		Example:           "  mcrl host edit web-1\n  EDITOR='code --wait' mcrl host edit cid:router-1.example.com",
		Args:              cmdutil.ExactArgs(1),
		ValidArgsFunction: completeHosts(f),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			cur, err := c.ResolveHost(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			id := mackerel.StringField(cur, "id")
			var drop []string
			for _, k := range orderedKeys(cur) {
				if !contains(hostInputFields, k) {
					drop = append(drop, k)
				}
			}
			before, err := toEditableYAML(cur, drop)
			if err != nil {
				return err
			}
			edited, after, changed, err := editLoop(f, before, "host "+id, nil)
			if err != nil || !changed {
				return err
			}
			diff := lineDiff(string(before), after)
			if diff == "" {
				fmt.Fprintln(f.IO.ErrOut, "no changes")
				return nil
			}
			fmt.Fprint(f.IO.ErrOut, colorDiff(f, diff))
			onlyInputFields(edited)
			if _, ok := edited["meta"]; !ok {
				edited["meta"] = map[string]any{}
			}
			if !f.DryRun {
				if err := confirm(f, fmt.Sprintf("Update host %s with these changes?", id)); err != nil {
					return err
				}
			}
			bodies, dry, err := sendAll(cmd, f, c, []request{put("/hosts/"+url.PathEscape(id), edited)})
			if dry || err != nil {
				return err
			}
			return refetchHost(cmd, f, c, id, bodies[0])
		},
	}
}

// hostInput converts a host object into a PUT body that keeps its values.
func hostInput(host json.RawMessage) (map[string]any, error) {
	m, err := decodeMap(host)
	if err != nil {
		return nil, err
	}
	onlyInputFields(m)
	if _, ok := m["meta"]; !ok {
		m["meta"] = map[string]any{}
	}
	return m, nil
}

func onlyInputFields(m map[string]any) {
	for k := range m {
		if !contains(hostInputFields, k) {
			delete(m, k)
		}
	}
}

// refetchHost prints the host after create/update; on failure it falls back
// to the API's own response.
func refetchHost(cmd *cobra.Command, f *cmdutil.Factory, c *mackerel.Client, id string, fallback []byte) error {
	b, err := c.Get(cmd.Context(), "/hosts/"+url.PathEscape(id), nil)
	if err != nil {
		f.IO.Warnf("the request succeeded, but fetching host %s failed: %v", id, err)
		return printOne(f, fallback)
	}
	h, err := mackerel.Field(b, "host")
	if err != nil {
		return err
	}
	return printRaw(f, h)
}

// orderedKeys returns the top-level keys of a JSON object in order.
func orderedKeys(raw json.RawMessage) []string {
	var keys []string
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return keys
		}
		keys = append(keys, t.(string))
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return keys
		}
	}
	return keys
}
