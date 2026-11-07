package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/extension"
	"github.com/kga/mcrl/internal/mackerel"
)

// Extensions are executables named mcrl-<name>; see docs/design/extensions.md.
// Everything about them lives in this file: the dispatch that runs before
// cobra parses the arguments, the "extension" commands, and the entries
// added to the help and completion.

const extensionAnnotation = "mcrl-extension"

const extensionLong = `Extensions add commands to mcrl. "mcrl <name>" runs the executable
mcrl-<name> when <name> is not a built-in command. mcrl looks for it in the
extensions directory first, then in PATH, and passes the remaining arguments,
the standard streams and the exit code through.

An extension runs with your permissions and can call mcrl, which uses your
API key. Install only extensions you trust, and read their code first.
"mcrl extension install" shows the commit it installed and never updates an
extension on its own.

Extensions receive these environment variables:

  MCRL_BIN       absolute path of mcrl, so that they can run "$MCRL_BIN" api ...
  MCRL_PROFILE   the profile mcrl resolved
  MCRL_API_BASE  the API base URL of that profile

The API key itself is never passed: an extension calls mcrl, which resolves it.

Of the global flags, --profile, --output, --color, --debug and --no-input may
come before the extension name; they reach the extension as MCRL_PROFILE,
MCRL_OUTPUT, MCRL_COLOR, MCRL_DEBUG and MCRL_NO_INPUT. Other flags go after
the name, for the extension to interpret.

Extensions directory: $MCRL_EXTENSIONS_DIR, else $XDG_DATA_HOME/mcrl/extensions
(~/.local/share/mcrl/extensions; %LOCALAPPDATA%\mcrl\extensions on Windows).`

func newExtensionCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "extension <command>",
		Aliases: []string{"ext"},
		Short:   "Manage extensions",
		Long:    extensionLong,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List extensions",
			Long: `List the installed extensions and the mcrl-* executables in PATH.

"source" is git (cloned by install), local (a linked directory) or path (found
in PATH). Cloned extensions show their remote "url" and the "commit" checked
out. "shadowed" marks an extension that never runs because a built-in command
has its name.`,
			Example: `  mcrl extension list
  mcrl extension list --jq '.[] | "\(.name) \(.commit)"'`,
			Args: cmdutil.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				m, err := extensionManager(f, cmd.Root())
				if err != nil {
					return err
				}
				exts, err := m.ListDetailed(cmd.Context())
				if err != nil {
					return err
				}
				items := make([]json.RawMessage, 0, len(exts))
				for _, e := range exts {
					e.Shadowed = isBuiltinName(cmd.Root(), e.Name)
					b, err := json.Marshal(e)
					if err != nil {
						return err
					}
					items = append(items, b)
				}
				return printItems(f, items)
			},
		},
		&cobra.Command{
			Use:   "install <owner/repo | url | path>",
			Short: "Install an extension",
			Long: `Install an extension from GitHub, a git URL or a local directory.

OWNER/REPO clones https://github.com/OWNER/REPO; any other git URL is cloned
as is. The repository must be named mcrl-<name> and have an executable
mcrl-<name> at its root. The latest commit of the default branch is checked
out and shown in the output; it changes only with "mcrl extension upgrade".
Cloning needs git in PATH; for a private repository, set up git credentials
(for example with "gh auth setup-git").

A local directory named mcrl-<name> is linked rather than copied, so changes
to an extension under development take effect at once. A directory wins over
OWNER/REPO with the same spelling; use a URL to mean GitHub.

The extension runs with your permissions and can use your Mackerel API key
through mcrl. Read its code before installing it.`,
			Example: `  mcrl extension install someone/mcrl-drill
  mcrl extension install https://git.example.com/ops/mcrl-drill.git
  mcrl extension install ./mcrl-hello`,
			Args: cmdutil.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				m, err := extensionManager(f, cmd.Root())
				if err != nil {
					return err
				}
				e, err := m.Install(cmd.Context(), args[0])
				if err != nil {
					return extensionError(err)
				}
				return printExtension(f, e)
			},
		},
		&cobra.Command{
			Use:   "upgrade <name>",
			Short: "Upgrade a cloned extension to the latest commit",
			Long: `Upgrade an extension installed from a repository to the latest commit of
the remote's default branch. Local changes in the clone are discarded.
With --dry-run, fetch and show the new commit without checking it out.

Linked directories and extensions found in PATH are not managed by mcrl and
cannot be upgraded here.`,
			Example: `  mcrl extension upgrade drill --dry-run
  mcrl extension upgrade drill`,
			Args: cmdutil.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				m, err := extensionManager(f, cmd.Root())
				if err != nil {
					return err
				}
				u, err := m.Upgrade(cmd.Context(), args[0], f.DryRun)
				if err != nil {
					return extensionError(err)
				}
				p, err := f.Printer()
				if err != nil {
					return err
				}
				return p.Print(u)
			},
		},
		&cobra.Command{
			Use:   "remove <name>",
			Short: "Remove an extension",
			Long: `Remove an installed extension. A cloned extension is deleted; for a linked
directory only the link is removed. Prints the removed extension.`,
			Example: `  mcrl extension remove drill`,
			Args:    cmdutil.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				m, err := extensionManager(f, cmd.Root())
				if err != nil {
					return err
				}
				e, err := m.Remove(args[0])
				if err != nil {
					return extensionError(err)
				}
				return printExtension(f, e)
			},
		},
	)
	for _, c := range cmd.Commands() {
		switch c.Name() {
		case "upgrade", "remove":
			c.ValidArgsFunction = completeInstalledExtensions(f)
		case "list":
			c.ValidArgsFunction = cobra.NoFileCompletions
		}
	}
	groupArgs(cmd)
	return cmd
}

func completeInstalledExtensions(f *cmdutil.Factory) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		m, err := extensionManager(f, cmd.Root())
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		exts, _ := m.List()
		var names []cobra.Completion
		for _, e := range exts {
			if e.Source != extension.SourcePath {
				names = append(names, e.Name)
			}
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}

func printExtension(f *cmdutil.Factory, e *extension.Extension) error {
	p, err := f.Printer()
	if err != nil {
		return err
	}
	return p.Print(e)
}

func extensionManager(f *cmdutil.Factory, root *cobra.Command) (*extension.Manager, error) {
	dir, err := extension.Dir(f.Getenv)
	if err != nil {
		return nil, err
	}
	return &extension.Manager{
		Dir:      dir,
		PATH:     f.Getenv("PATH"),
		PATHEXT:  f.Getenv("PATHEXT"),
		Reserved: func(name string) bool { return isBuiltinName(root, name) },
	}, nil
}

// extensionError maps the extension package's errors to mcrl's exit codes.
func extensionError(err error) error {
	var (
		nf  *extension.NotFoundError
		ce  *extension.ConflictError
		ue  *extension.UsageError
		gnf *extension.GitNotFoundError
	)
	switch {
	case errors.As(err, &nf):
		return &mackerel.NotFoundError{Kind: "extension", Ref: nf.Name}
	case errors.As(err, &ce):
		return &cmdutil.ExitError{Code: ExitInvalid, Err: err}
	case errors.As(err, &ue):
		return &cmdutil.UsageError{Err: err}
	case errors.As(err, &gnf):
		return cmdutil.WithHint(err, "install git, or install the extension from a local directory")
	}
	return err
}

// isBuiltinName reports whether name is a command or alias of mcrl itself,
// including the ones cobra adds while executing.
func isBuiltinName(root *cobra.Command, name string) bool {
	switch name {
	case "help", "completion", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return true
	}
	for _, c := range root.Commands() {
		if _, ext := c.Annotations[extensionAnnotation]; !ext && (c.Name() == name || c.HasAlias(name)) {
			return true
		}
	}
	return false
}

// Global flags that may precede an extension name, and the environment
// variable that carries each to the extension. --profile is always passed
// (resolved) as MCRL_PROFILE.
var extensionFlagEnv = map[string]string{
	"profile":  "",
	"output":   "MCRL_OUTPUT",
	"color":    "MCRL_COLOR",
	"debug":    "MCRL_DEBUG",
	"no-input": "MCRL_NO_INPUT",
}

// splitArgs finds the first positional argument, skipping the global flags
// (and their values) before it. It returns -1 when there is none, and false
// when an unknown flag or "--" makes the command line cobra's business.
func splitArgs(root *cobra.Command, args []string) (int, bool) {
	pf := root.PersistentFlags()
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return -1, false
		case a == "-h" || a == "--help" || a == "--version":
			continue
		case !strings.HasPrefix(a, "-") || a == "-":
			return i, true
		}
		var fl *pflag.Flag
		takesNext := false
		if long, ok := strings.CutPrefix(a, "--"); ok {
			name, _, hasValue := strings.Cut(long, "=")
			fl = pf.Lookup(name)
			takesNext = fl != nil && !hasValue && fl.NoOptDefVal == ""
		} else {
			fl = pf.ShorthandLookup(a[1:2])
			if fl != nil && len(a) > 2 && fl.NoOptDefVal != "" {
				return -1, false // combined shorthands like -yo
			}
			takesNext = fl != nil && len(a) == 2 && fl.NoOptDefVal == ""
		}
		if fl == nil {
			return -1, false
		}
		if takesNext {
			i++
		}
	}
	return -1, true
}

// runExtension runs "mcrl [global flags] <name> args..." as the extension
// mcrl-<name> when <name> is not a built-in command and such an extension
// exists. When ok is false, mcrl handles the arguments itself.
func runExtension(f *cmdutil.Factory, root *cobra.Command, args []string) (code int, ok bool) {
	i, ok := splitArgs(root, args)
	if !ok || i < 0 || isBuiltinName(root, args[i]) || !extension.ValidName(args[i]) {
		return 0, false
	}
	m, err := extensionManager(f, root)
	if err != nil {
		return 0, false
	}
	e, err := m.Find(args[i])
	if err != nil || e == nil {
		return 0, false
	}
	code, err = execExtension(f, root, m, e, args[:i], args[i+1:])
	if err != nil {
		PrintError(f.IO, err)
		return ExitCode(err), true
	}
	return code, true
}

func execExtension(f *cmdutil.Factory, root *cobra.Command, m *extension.Manager, e *extension.Extension, globals, args []string) (int, error) {
	fs := pflag.NewFlagSet("mcrl", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.AddFlagSet(root.PersistentFlags())
	if err := fs.Parse(globals); err != nil {
		return 0, &cmdutil.UsageError{Err: err}
	}
	env := os.Environ()
	var bad error
	fs.Visit(func(fl *pflag.Flag) {
		name, allowed := extensionFlagEnv[fl.Name]
		switch {
		case !allowed:
			if bad == nil {
				bad = cmdutil.Usagef("--%s cannot come before an extension name; put it after %q if the extension accepts it", fl.Name, e.Name)
			}
		case name != "":
			env = setEnv(env, name, fl.Value.String())
		}
	})
	if bad != nil {
		return 0, bad
	}
	profile, _, err := f.ProfileName()
	if err != nil {
		return 0, err
	}
	base, err := f.BaseURL()
	if err != nil {
		return 0, err
	}
	bin := f.Executable
	if bin == "" {
		if bin, err = os.Executable(); err != nil {
			return 0, err
		}
	}
	env = setEnv(env, "MCRL_BIN", bin)
	env = setEnv(env, "MCRL_PROFILE", profile)
	env = setEnv(env, "MCRL_API_BASE", base)

	if !m.Runnable(e) {
		return 0, cmdutil.WithHint(&cmdutil.ExitError{Code: ExitError, Err: errors.New("extension " + e.Name + ": " + e.Path + " is missing or not executable")},
			"reinstall it, or run `mcrl extension remove "+e.Name+"`")
	}
	cmd, err := m.Command(e, args)
	if err != nil {
		return 0, err
	}
	cmd.Env = env
	return extension.Run(cmd, f.IO.In, f.IO.Out, f.IO.ErrOut)
}

func setEnv(env []string, key, value string) []string {
	out := env[:0:0]
	for _, kv := range env {
		if k, _, _ := strings.Cut(kv, "="); !strings.EqualFold(k, key) {
			out = append(out, kv)
		}
	}
	return append(out, key+"="+value)
}

// addExtensionCommands lists the extensions in "mcrl --help" and in the
// completion of the first argument. It scans the extension directory and
// PATH, so it runs only for those invocations.
func addExtensionCommands(f *cmdutil.Factory, root *cobra.Command, args []string) {
	i, ok := splitArgs(root, args)
	if !ok || (i >= 0 && args[i] != "help" && !strings.HasPrefix(args[i], cobra.ShellCompRequestCmd)) {
		return
	}
	m, err := extensionManager(f, root)
	if err != nil {
		return
	}
	exts, err := m.List()
	if err != nil || len(exts) == 0 {
		return
	}
	root.AddGroup(&cobra.Group{ID: "extension", Title: "Extension commands:"})
	for _, e := range exts {
		if isBuiltinName(root, e.Name) {
			continue
		}
		short := "Extension at " + e.Path
		switch e.Source {
		case extension.SourceGit:
			short = "Extension installed from a repository"
		case extension.SourceLocal:
			short = "Extension linked from " + e.Dir
		}
		root.AddCommand(&cobra.Command{
			Use:                e.Name,
			Short:              short,
			GroupID:            "extension",
			Annotations:        map[string]string{extensionAnnotation: e.Path},
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				code, err := execExtension(f, root, m, &e, nil, args)
				if err != nil {
					return err
				}
				if code != 0 {
					return &cmdutil.ExitError{Code: code, Silent: true}
				}
				return nil
			},
		})
	}
}
