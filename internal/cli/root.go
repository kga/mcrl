// Package cli defines the mcrl command tree.
package cli

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/build"
	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/credential"
)

// Option customizes Main for tests.
type Option func(*cmdutil.Factory)

// WithKeyring replaces the OS keychain.
func WithKeyring(k credential.Keyring) Option { return func(f *cmdutil.Factory) { f.Keyring = k } }

// WithTransport replaces the HTTP transport.
func WithTransport(t http.RoundTripper) Option {
	return func(f *cmdutil.Factory) { f.Transport = t }
}

// Main runs mcrl and returns the exit code.
func Main(ctx context.Context, args []string, io *cmdutil.IOStreams, opts ...Option) int {
	f := &cmdutil.Factory{
		IO:      io,
		Getenv:  os.Getenv,
		Keyring: credential.OSKeyring(),
	}
	for _, o := range opts {
		o(f)
	}
	root := NewRootCmd(f)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err != nil && ctx.Err() != nil {
		err = context.Canceled
	}
	if err != nil {
		PrintError(io, err)
	}
	return ExitCode(err)
}

// NewRootCmd builds the command tree.
func NewRootCmd(f *cmdutil.Factory) *cobra.Command {
	root := &cobra.Command{
		Use:   "mcrl <command> <subcommand> [flags]",
		Short: "A command-line client for the Mackerel API",
		Long: `mcrl is a command-line client for the Mackerel API.

Output is always JSON: indented and colored on a terminal, compact when piped.
Use --jq to filter it. Errors go to stderr and are reflected in the exit code
(see "mcrl help exit-codes").`,
		Version:       build.Get().Version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetIn(f.IO.In)
	root.SetOut(f.IO.Out)
	root.SetErr(f.IO.ErrOut)
	root.SetVersionTemplate("mcrl version {{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &cmdutil.UsageError{Err: err}
	})

	pf := root.PersistentFlags()
	pf.StringVar(&f.Profile, "profile", "", "profile to use (env: MCRL_PROFILE)")
	pf.StringVarP(&f.Output, "output", "o", "", "output format: json or jsonl (env: MCRL_OUTPUT)")
	pf.StringVar(&f.JQ, "jq", "", "filter the JSON output with a jq expression; strings are printed raw")
	pf.BoolVar(&f.Compact, "compact", false, "print compact JSON even on a terminal")
	pf.StringVar(&f.Color, "color", "", "colorize JSON: auto, always or never (env: MCRL_COLOR, NO_COLOR)")
	pf.BoolVar(&f.Debug, "debug", false, "log HTTP requests and responses to stderr (env: MCRL_DEBUG)")
	pf.BoolVar(&f.DryRun, "dry-run", false, "print the request that would be sent and exit")
	pf.BoolVarP(&f.Yes, "yes", "y", false, "skip confirmation prompts")
	pf.BoolVar(&f.NoInput, "no-input", os.Getenv("MCRL_NO_INPUT") != "", "never prompt (env: MCRL_NO_INPUT)")
	pf.DurationVar(&f.Timeout, "timeout", 30*time.Second, "timeout for each HTTP request")

	root.AddGroup(
		&cobra.Group{ID: "core", Title: "Commands:"},
		&cobra.Group{ID: "setup", Title: "Setup commands:"},
	)
	for _, c := range []*cobra.Command{
		newOrgCmd(f), newServiceCmd(f), newRoleCmd(f), newHostCmd(f), newMetricCmd(f),
		newMonitorCmd(f), newCheckCmd(f), newAlertCmd(f), newDowntimeCmd(f), newAlertGroupCmd(f),
		newChannelCmd(f), newNotificationGroupCmd(f), newDashboardCmd(f), newAnnotationCmd(f),
		newLogCmd(f), newTraceCmd(f), newAPMCmd(f),
		newUserCmd(f), newInvitationCmd(f), newMetadataCmd(f), newAWSCmd(f), newAPICmd(f), newMCPCmd(f),
	} {
		c.GroupID = "core"
		root.AddCommand(c)
	}
	for _, c := range []*cobra.Command{newAuthCmd(f), newConfigCmd(f), newDoctorCmd(f), newVersionCmd(f)} {
		c.GroupID = "setup"
		root.AddCommand(c)
	}
	root.SetHelpCommandGroupID("setup")
	root.SetCompletionCommandGroupID("setup")
	root.AddCommand(newHelpTopics()...)
	return root
}
