package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zalando/go-keyring"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/config"
	"github.com/kga/mcrl/internal/credential"
	"github.com/kga/mcrl/internal/httpx"
)

func newAuthCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth <command>",
		Short: "Manage API keys and profiles",
		Long: `Manage API keys and profiles.

A profile is the connection setting for one Mackerel organization. The API
key is looked up in this order:

  1. the MACKEREL_APIKEY environment variable
  2. the profile's api_key_command in the config file
  3. the OS keychain (stored by "mcrl auth login")
  4. the profile's plaintext api_key in the config file`,
	}
	cmd.AddCommand(newAuthLoginCmd(f), newAuthLogoutCmd(f), newAuthStatusCmd(f),
		newAuthListCmd(f), newAuthSwitchCmd(f), newAuthTokenCmd(f))
	groupArgs(cmd)
	return cmd
}

type orgInfo struct {
	Name        string  `json:"name"`
	DisplayName *string `json:"displayName"`
}

func fetchOrg(ctx context.Context, c *httpx.Client) (*orgInfo, error) {
	resp, err := c.Do(ctx, &httpx.Request{Path: "/org"})
	if err != nil {
		return nil, err
	}
	var org orgInfo
	if err := json.Unmarshal(resp.Body, &org); err != nil {
		return nil, fmt.Errorf("unexpected /org response: %w", err)
	}
	return &org, nil
}

func newAuthLoginCmd(f *cmdutil.Factory) *cobra.Command {
	var (
		withToken bool
		baseURL   string
		insecure  bool
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store an API key for a profile",
		Long: `Store an API key for a profile.

The key is verified with GET /api/v0/org and saved in the OS keychain. Where
no keychain is available, --insecure-storage saves it in plaintext in the
config file instead (file mode 0600).`,
		Example: `  # Paste the key at the prompt
  mcrl auth login

  # Read the key from stdin, for a second organization on Mackerel for KCPS
  op read op://Infra/mackerel/credential | mcrl auth login --profile kcps --with-token --base-url https://kcps-mackerel.io`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			name, _, err := f.ProfileName()
			if err != nil {
				return err
			}

			key, err := readAPIKey(f, withToken)
			if err != nil {
				return err
			}

			p := cfg.Profile(name)
			base := baseURL
			if base == "" {
				base, _ = config.ResolveBaseURL(p, f.Getenv)
			}
			client, err := f.NewClient(base, key)
			if err != nil {
				return &cmdutil.UsageError{Err: err}
			}
			org, err := fetchOrg(ctx, client)
			if err != nil {
				return fmt.Errorf("verifying the API key: %w", err)
			}

			p = cfg.EnsureProfile(name)
			if baseURL != "" {
				p.BaseURL = baseURL
			}
			// キーチェーンの元の状態。設定ファイルの保存に失敗したら戻し、
			// 平文で保存したら古いキー（平文より優先される）を消すのに使う
			prev, prevErr := f.Keyring.Get(credential.KeyringService, name)
			storage := string(credential.SourceKeyring)
			inKeyring := false
			if insecure {
				p.APIKey = key
				storage = string(credential.SourceFile)
			} else if err := f.Keyring.Set(credential.KeyringService, name, key); err != nil {
				ok, perr := f.IO.Confirm(f.NoInput || f.Yes,
					fmt.Sprintf("Cannot use the OS keychain (%v). Store the key in plaintext in the config file?", err))
				if perr != nil || !ok {
					return cmdutil.WithHint(fmt.Errorf("storing the API key in the OS keychain: %w", err),
						"use --insecure-storage to store it in the config file instead")
				}
				p.APIKey = key
				storage = string(credential.SourceFile)
			} else {
				p.APIKey = "" // キーチェーンに入れたら平文の古いキーは消す
				inKeyring = true
			}
			if cfg.DefaultProfile == "" {
				cfg.DefaultProfile = name
			}
			if err := f.SaveConfig(); err != nil {
				err = fmt.Errorf("saving the config file: %w", err)
				if inKeyring {
					if rerr := restoreKeyring(f.Keyring, name, prev, prevErr); rerr != nil {
						return cmdutil.WithHint(err, fmt.Sprintf(
							"the new key is left in the OS keychain (restoring the previous state failed: %v); run `mcrl auth logout --profile %s` to remove it", rerr, name))
					}
				}
				return err
			}
			if !inKeyring && prevErr == nil {
				// キーチェーンのキーは平文より優先されるので、古いものが残ると新しいキーが使われない
				if err := f.Keyring.Delete(credential.KeyringService, name); err != nil && !errors.Is(err, keyring.ErrNotFound) {
					return cmdutil.WithHint(fmt.Errorf("removing the previous key from the OS keychain: %w", err),
						"the new key is saved in the config file, but the previous key in the OS keychain takes precedence over it")
				}
			}
			if f.Getenv(credential.EnvVar) != "" {
				f.IO.Warnf("%s is set and takes precedence over the stored key", credential.EnvVar)
			}
			printer, err := f.Printer()
			if err != nil {
				return err
			}
			return printer.Print(map[string]any{
				"profile":      name,
				"baseUrl":      base,
				"storage":      storage,
				"organization": org,
			})
		},
	}
	cmd.Flags().BoolVar(&withToken, "with-token", false, "read the API key from stdin")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "API base URL to save in the profile (e.g. https://kcps-mackerel.io)")
	cmd.Flags().BoolVar(&insecure, "insecure-storage", false, "store the key in plaintext in the config file")
	return cmd
}

// restoreKeyring puts the keychain entry of a profile back to what Get returned
// before it was overwritten: the previous key, or no entry.
func restoreKeyring(kr credential.Keyring, name, prev string, prevErr error) error {
	switch {
	case prevErr == nil:
		return kr.Set(credential.KeyringService, name, prev)
	case errors.Is(prevErr, keyring.ErrNotFound):
		if err := kr.Delete(credential.KeyringService, name); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return err
		}
		return nil
	default:
		return fmt.Errorf("the previous state is unknown: %w", prevErr)
	}
}

func readAPIKey(f *cmdutil.Factory, fromStdin bool) (string, error) {
	var key string
	switch {
	case fromStdin:
		b, err := io.ReadAll(f.IO.In)
		if err != nil {
			return "", err
		}
		key = string(b)
	case f.IO.CanPrompt(f.NoInput) && f.IO.ReadSecret != nil:
		fmt.Fprint(f.IO.ErrOut, "? Paste your Mackerel API key: ")
		s, err := f.IO.ReadSecret()
		fmt.Fprintln(f.IO.ErrOut)
		if err != nil {
			return "", err
		}
		key = s
	default:
		return "", cmdutil.Usagef("cannot prompt for the API key; pipe it to `mcrl auth login --with-token`")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", cmdutil.Usagef("empty API key")
	}
	return key, nil
}

func newAuthLogoutCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored API key of a profile",
		Args:  cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			name, _, err := f.ProfileName()
			if err != nil {
				return err
			}
			// キーチェーンにキーがあるかを先に確かめる。読めないとき（キーチェーンが
			// 使えない環境）は、そこには保存されていないとみなす
			_, getErr := f.Keyring.Get(credential.KeyringService, name)
			if getErr != nil && !errors.Is(getErr, keyring.ErrNotFound) {
				f.IO.Warnf("could not read the OS keychain: %v", getErr)
			}
			removed := []string{}
			// 平文のキーを先に消す。保存に失敗したら何も変わっていない
			if cfg.HasProfile(name) && cfg.Profile(name).APIKey != "" {
				cfg.Profile(name).APIKey = ""
				if err := f.SaveConfig(); err != nil {
					return fmt.Errorf("removing the key from the config file: %w", err)
				}
				removed = append(removed, string(credential.SourceFile))
			}
			if getErr == nil {
				if err := f.Keyring.Delete(credential.KeyringService, name); err != nil && !errors.Is(err, keyring.ErrNotFound) {
					hint := "the key is still in the OS keychain; run `mcrl auth logout` again"
					if len(removed) > 0 {
						hint = "the key was removed from the config file but is still in the OS keychain; run `mcrl auth logout` again"
					}
					return cmdutil.WithHint(fmt.Errorf("removing the key from the OS keychain: %w", err), hint)
				}
				removed = append(removed, string(credential.SourceKeyring))
			}
			if f.Getenv(credential.EnvVar) != "" {
				f.IO.Warnf("%s is still set in the environment", credential.EnvVar)
			}
			printer, err := f.Printer()
			if err != nil {
				return err
			}
			return printer.Print(map[string]any{"profile": name, "removed": removed})
		},
	}
}

func newAuthStatusCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the active profile, where its API key comes from, and its organization",
		Args:  cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfgPath, err := f.ConfigPath()
			if err != nil {
				return err
			}
			conn, connErr := f.Connection(ctx)
			if conn == nil {
				return connErr
			}
			status := map[string]any{
				"profile":    map[string]any{"name": conn.Profile, "source": conn.ProfileSource},
				"configFile": cfgPath,
				"baseUrl":    map[string]any{"url": conn.BaseURL, "source": conn.BaseURLSource},
				"apiKey":     nil,
			}
			var orgErr error
			if connErr == nil {
				status["apiKey"] = map[string]any{"source": conn.Key.Source}
				client, err := f.NewClient(conn.BaseURL, conn.Key.Key)
				if err != nil {
					return err
				}
				org, err := fetchOrg(ctx, client)
				if err != nil {
					orgErr = err
				} else {
					status["organization"] = org
				}
			}
			printer, err := f.Printer()
			if err != nil {
				return err
			}
			if err := printer.Print(status); err != nil {
				return err
			}
			if connErr != nil {
				return connErr
			}
			return orgErr
		},
	}
}

func newAuthListCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List profiles in the config file",
		Args:  cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			active, _, err := f.ProfileName()
			if err != nil {
				return err
			}
			list := []map[string]any{}
			for _, n := range cfg.ProfileNames() {
				base, _ := config.ResolveBaseURL(cfg.Profile(n), func(string) string { return "" })
				list = append(list, map[string]any{
					"name":    n,
					"default": n == cfg.DefaultProfile,
					"active":  n == active,
					"baseUrl": base,
				})
			}
			printer, err := f.Printer()
			if err != nil {
				return err
			}
			return printer.Print(list)
		},
	}
}

func newAuthSwitchCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "switch <profile>",
		Short: "Set the default profile",
		Args:  cmdutil.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			cfg, err := f.Config()
			if err != nil || len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return cfg.ProfileNames(), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			name := args[0]
			if !cfg.HasProfile(name) {
				return cmdutil.WithHint(fmt.Errorf("profile %q is not defined", name),
					fmt.Sprintf("run `mcrl auth login --profile %s` first", name))
			}
			cfg.DefaultProfile = name
			if err := f.SaveConfig(); err != nil {
				return err
			}
			printer, err := f.Printer()
			if err != nil {
				return err
			}
			return printer.Print(map[string]any{"defaultProfile": name})
		},
	}
}

func newAuthTokenCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "token",
		Short: "Print the API key of the active profile",
		Long: `Print the API key of the active profile as plain text, for passing it to
other tools. Avoid running it where the output is visible to others.`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			conn, err := f.Connection(cmd.Context())
			if err != nil {
				return err
			}
			if f.IO.OutTTY {
				f.IO.Warnf("printing a secret to the terminal")
			}
			_, err = fmt.Fprintln(f.IO.Out, conn.Key.Key)
			return err
		},
	}
}
