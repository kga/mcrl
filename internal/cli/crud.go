package cli

import (
	"context"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/mackerel"
)

// flagSetter applies resource-specific flags to a request body. It returns
// a usage error for invalid values.
type flagSetter func(ctx context.Context, cmd *cobra.Command, c *mackerel.Client, body map[string]any) error

// crud describes a resource with POST create, PUT update and DELETE.
type crud struct {
	editable
	CreatePath string
	// Bind registers resource-specific flags; Apply sets them on the body.
	Bind     func(cmd *cobra.Command)
	Apply    flagSetter
	Required []string // body fields required on create
	Examples struct{ Create, Update, Delete string }
}

func (r *crud) createCmd(f *cmdutil.Factory) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:     "create [flags]",
		Aliases: []string{"new"},
		Short:   "Create a " + r.Kind.Name,
		Long: withAPI(fmt.Sprintf(`Create a %s from flags, or from a JSON or YAML file in the API's format
with -f. Flags override the file's fields.`, r.Kind.Name), "Read, Write", "POST /api/v0"+r.CreatePath),
		Example: r.Examples.Create,
		Args:    cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := map[string]any{}
			if file != "" {
				var err error
				if body, err = readObject(f, file); err != nil {
					return err
				}
			}
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			if r.Apply != nil {
				if err := r.Apply(cmd.Context(), cmd, c, body); err != nil {
					return err
				}
			}
			for _, k := range r.ReadOnly {
				delete(body, k)
			}
			for _, k := range r.Required {
				if _, ok := body[k]; !ok {
					return cmdutil.Usagef("%q is required (flag or -f)", k)
				}
			}
			if r.Validate != nil {
				if err := r.Validate(body); err != nil {
					return &cmdutil.UsageError{Err: err}
				}
			}
			return simpleSendUnwrap(cmd, f, post(r.CreatePath, body), r.ResponseKey)
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", `JSON or YAML file in the API's format ("-" for stdin)`)
	if r.Bind != nil {
		r.Bind(cmd)
	}
	return cmd
}

func (r *crud) updateCmd(f *cmdutil.Factory) *cobra.Command {
	var (
		file    string
		replace bool
	)
	cmd := &cobra.Command{
		Use:   "update <" + cmdName(r.Kind) + "> [flags]",
		Short: "Update a " + r.Kind.Name,
		Long: withAPI(fmt.Sprintf(`Update a %s. The current %s is fetched and the flags (and -f fields) are
applied on top, so only the fields to change need to be given. --replace
sends the -f file as the complete object instead.`, r.Kind.Name, r.Kind.Name), "Read, Write", "PUT /api/v0"+fmt.Sprintf(r.ItemPath, "<id>")),
		Example:           r.Examples.Update,
		Args:              cmdutil.ExactArgs(1),
		ValidArgsFunction: completeKind(f, r.Kind),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			cur, err := c.Resolve(cmd.Context(), r.Kind, args[0])
			if err != nil {
				return err
			}
			id := mackerel.StringField(cur, "id")
			body, err := decodeMap(cur)
			if err != nil {
				return err
			}
			if file != "" {
				patch, err := readObject(f, file)
				if err != nil {
					return err
				}
				if replace {
					body = patch
				} else {
					for k, v := range patch {
						body[k] = v
					}
				}
			} else if replace {
				return cmdutil.Usagef("--replace needs -f")
			}
			if r.Apply != nil {
				if err := r.Apply(cmd.Context(), cmd, c, body); err != nil {
					return err
				}
			}
			for _, k := range r.ReadOnly {
				delete(body, k)
			}
			if r.Finalize != nil {
				curMap, err := decodeMap(cur)
				if err != nil {
					return err
				}
				r.Finalize(curMap, body)
			}
			if r.Validate != nil {
				if err := r.Validate(body); err != nil {
					return &cmdutil.UsageError{Err: err}
				}
			}
			return simpleSendUnwrap(cmd, f, put(fmt.Sprintf(r.ItemPath, url.PathEscape(id)), body), r.ResponseKey)
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", `JSON or YAML file with the fields to change ("-" for stdin)`)
	cmd.Flags().BoolVar(&replace, "replace", false, "send the -f file as the complete object without merging")
	if r.Bind != nil {
		r.Bind(cmd)
	}
	return cmd
}

func (r *crud) deleteCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:               "delete <" + cmdName(r.Kind) + ">",
		Aliases:           []string{"rm"},
		Short:             "Delete a " + r.Kind.Name,
		Long:              withAPI(fmt.Sprintf("Delete a %s. The output is the deleted object.", r.Kind.Name), "Read, Write", "DELETE /api/v0"+fmt.Sprintf(r.ItemPath, "<id>")),
		Example:           r.Examples.Delete,
		Args:              cmdutil.ExactArgs(1),
		ValidArgsFunction: completeKind(f, r.Kind),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			cur, err := c.Resolve(cmd.Context(), r.Kind, args[0])
			if err != nil {
				return err
			}
			id := mackerel.StringField(cur, "id")
			if !f.DryRun {
				name := mackerel.StringField(cur, r.Kind.NameFields[0])
				if err := confirm(f, fmt.Sprintf("Delete %s %q (%s)?", r.Kind.Name, name, id)); err != nil {
					return err
				}
			}
			return simpleSendUnwrap(cmd, f, del(fmt.Sprintf(r.ItemPath, url.PathEscape(id))), r.ResponseKey)
		},
	}
}

// Common flag helpers.

func setString(cmd *cobra.Command, body map[string]any, flag, key string) {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetString(flag)
		body[key] = v
	}
}

func setStrings(cmd *cobra.Command, body map[string]any, flag, key string) {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetStringArray(flag)
		body[key] = v
	}
}

func setBool(cmd *cobra.Command, body map[string]any, flag, key string) {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetBool(flag)
		body[key] = v
	}
}

// resolveIDs resolves the repeated flag's references to IDs.
func resolveIDs(ctx context.Context, cmd *cobra.Command, c *mackerel.Client, k mackerel.Kind, flag string) ([]string, bool, error) {
	if !cmd.Flags().Changed(flag) {
		return nil, false, nil
	}
	refs, _ := cmd.Flags().GetStringArray(flag)
	ids := make([]string, len(refs))
	for i, r := range refs {
		id, err := c.ResolveID(ctx, k, r)
		if err != nil {
			return nil, true, err
		}
		ids[i] = id
	}
	return ids, true, nil
}
