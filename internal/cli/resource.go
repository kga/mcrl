package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/config"
	"github.com/kga/mcrl/internal/mackerel"
	"github.com/kga/mcrl/internal/output"
	"github.com/kga/mcrl/internal/timeexpr"
)

// apiClient returns the Mackerel client for the active profile.
func apiClient(ctx context.Context, f *cmdutil.Factory) (*mackerel.Client, error) {
	h, _, err := f.APIClient(ctx)
	if err != nil {
		return nil, err
	}
	return mackerel.New(h), nil
}

// printItems prints a list of raw JSON values as one array. With -o jsonl
// each item is a line, and --jq runs on each item rather than on the array.
func printItems(f *cmdutil.Factory, items []json.RawMessage) error {
	if items == nil {
		items = []json.RawMessage{}
	}
	if p, err := f.Printer(); err == nil && p.Format == output.FormatJSONL {
		for _, it := range items {
			if err := p.PrintRaw(it); err != nil {
				return err
			}
		}
		return nil
	}
	b, err := json.Marshal(items)
	if err != nil {
		return err
	}
	return printRaw(f, b)
}

func printRaw(f *cmdutil.Factory, b []byte) error {
	p, err := f.Printer()
	if err != nil {
		return err
	}
	return p.PrintRaw(b)
}

// withAPI appends the endpoints and the API key permission to a help text.
func withAPI(long, permission string, endpoints ...string) string {
	return strings.TrimSpace(long) + "\n\nAPI: " + strings.Join(endpoints, ", ") + "\nAPI key permission: " + permission
}

// listCmd builds "<resource> list" for an endpoint that returns {key: [...]}.
func listCmd(f *cmdutil.Factory, short, path, key, example string) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   short,
		Long:    withAPI(short+".", "Read", "GET /api/v0"+path),
		Example: example,
		Args:    cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			items, err := c.List(cmd.Context(), path, key, nil)
			if err != nil {
				return err
			}
			return printItems(f, items)
		},
	}
}

// getCmd builds "<resource> get <ref>" that resolves an ID or name.
func getCmd(f *cmdutil.Factory, k mackerel.Kind, refName, example string) *cobra.Command {
	endpoints := []string{"GET /api/v0" + k.ListPath}
	if k.GetPath != "" {
		endpoints = append([]string{"GET /api/v0" + strings.ReplaceAll(k.GetPath, "%s", "<id>")}, endpoints...)
	}
	return &cobra.Command{
		Use:     "get <" + refName + ">",
		Aliases: []string{"show"},
		Short:   "Show a " + k.Name,
		Long: withAPI(fmt.Sprintf(`Show a %s by ID or %s.

A reference that looks like an ID (11 alphanumeric characters) is looked up
as an ID first. Prefix it with "id:" or "name:" to force either.`, k.Name, strings.Join(k.NameFields, " or ")), "Read", endpoints...),
		Example:           example,
		Args:              cmdutil.ExactArgs(1),
		ValidArgsFunction: completeKind(f, k),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			raw, err := c.Resolve(cmd.Context(), k, args[0])
			if err != nil {
				return err
			}
			return printRaw(f, raw)
		},
	}
}

// completeKind completes names of a resource from the API.
func completeKind(f *cmdutil.Factory, k mackerel.Kind) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completeFromList(cmd, f, k.ListPath, k.ListKey, nil, k.NameFields[0], "id"), cobra.ShellCompDirectiveNoFileComp
	}
}

func completeHosts(f *cmdutil.Factory) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return completeFromList(cmd, f, "/hosts", "hosts", nil, "name", "id"), cobra.ShellCompDirectiveNoFileComp
	}
}

// completionTTL is how long completion candidates are cached. The cache is
// used only for completion, never to resolve names for commands.
const completionTTL = 5 * time.Minute

// completeFromList lists a resource and returns "name\tid" completions,
// cached per profile for completionTTL. Errors yield no completions:
// completion must never print errors.
func completeFromList(cmd *cobra.Command, f *cmdutil.Factory, path, key string, q url.Values, nameField, descField string) []cobra.Completion {
	cachePath := completionCachePath(f, path+"?"+q.Encode()+"#"+nameField+","+descField)
	if cachePath != "" {
		if st, err := os.Stat(cachePath); err == nil && time.Since(st.ModTime()) < completionTTL {
			var cached []cobra.Completion
			if b, err := os.ReadFile(cachePath); err == nil && json.Unmarshal(b, &cached) == nil {
				return cached
			}
		}
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
	defer cancel()
	c, err := apiClient(ctx, f)
	if err != nil {
		return nil
	}
	c.HTTP.Retries = 0
	items, err := c.List(ctx, path, key, q)
	if err != nil {
		return nil
	}
	out := make([]cobra.Completion, 0, len(items))
	for _, it := range items {
		name := mackerel.StringField(it, nameField)
		if name == "" {
			continue
		}
		out = append(out, cobra.CompletionWithDesc(name, mackerel.StringField(it, descField)))
	}
	if cachePath != "" {
		if b, err := json.Marshal(out); err == nil && os.MkdirAll(filepath.Dir(cachePath), 0o700) == nil {
			_ = os.WriteFile(cachePath, b, 0o600)
		}
	}
	return out
}

func completionCachePath(f *cmdutil.Factory, key string) string {
	dir, err := config.CacheDir(f.Getenv)
	if err != nil {
		return ""
	}
	profile, _, err := f.ProfileName()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(profile + "\x00" + key))
	return filepath.Join(dir, "completion", hex.EncodeToString(sum[:8])+".json")
}

// timeRange parses --since/--until style flags into epoch seconds.
func timeRange(since, until string) (int64, int64, error) {
	now := time.Now()
	from, err := timeexpr.Parse(since, now, nil)
	if err != nil {
		return 0, 0, &cmdutil.UsageError{Err: err}
	}
	to, err := timeexpr.Parse(until, now, nil)
	if err != nil {
		return 0, 0, &cmdutil.UsageError{Err: err}
	}
	if to.Before(from) {
		return 0, 0, cmdutil.Usagef("the end of the time range (%s) is before the start (%s)", to.Format(time.RFC3339), from.Format(time.RFC3339))
	}
	return from.Unix(), to.Unix(), nil
}

// newGroup builds a resource command with subcommands.
func newGroup(use, short string, aliases []string, subs ...*cobra.Command) *cobra.Command {
	cmd := &cobra.Command{Use: use, Short: short, Aliases: aliases}
	cmd.AddCommand(subs...)
	groupArgs(cmd)
	return cmd
}

// groupArgs makes a command that only has subcommands print its help when
// called alone and fail with a usage error for an unknown subcommand.
func groupArgs(cmd *cobra.Command) {
	cmd.Args = cobra.ArbitraryArgs
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return cmdutil.Usagef("unknown command %q for %q", args[0], cmd.CommandPath())
		}
		return cmd.Help()
	}
}
