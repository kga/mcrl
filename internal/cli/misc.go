package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/build"
	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/config"
	"github.com/kga/mcrl/internal/httpx"
)

func newVersionCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Args:  cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := f.Printer()
			if err != nil {
				return err
			}
			return p.Print(build.Get())
		},
	}
}

func newConfigCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config <command>",
		Short: "Inspect the configuration",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Show the files and directories mcrl reads and writes",
		Args:  cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := f.ConfigPath()
			if err != nil {
				return err
			}
			cache, err := config.CacheDir(f.Getenv)
			if err != nil {
				return err
			}
			p, err := f.Printer()
			if err != nil {
				return err
			}
			return p.Print(map[string]string{"config": cfg, "cache": cache})
		},
	})
	return cmd
}

type check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok, warn, fail
	Detail string `json:"detail"`
}

func newDoctorCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the configuration, API key and connectivity",
		Long: `Check the config file, the active profile, where the API key comes from,
connectivity to the API, the key's validity and the local clock.

Exits with 1 when any check fails.`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			var checks []check
			add := func(name, status, format string, args ...any) {
				checks = append(checks, check{name, status, fmt.Sprintf(format, args...)})
			}

			path, err := f.ConfigPath()
			cfgOK := false
			switch {
			case err != nil:
				add("config", "fail", "%v", err)
			default:
				if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
					add("config", "ok", "%s (not created yet)", path)
					cfgOK = true
				} else if _, err := f.Config(); err != nil {
					add("config", "fail", "%v", err)
				} else {
					add("config", "ok", "%s", path)
					cfgOK = true
				}
			}

			var conn *cmdutil.Connection
			if cfgOK {
				var connErr error
				conn, connErr = f.Connection(ctx)
				cfg, _ := f.Config()
				switch {
				case conn.ProfileSource != config.SourceDefault && !cfg.HasProfile(conn.Profile):
					add("profile", "warn", "%q (from %s) is not defined in the config file", conn.Profile, conn.ProfileSource)
				default:
					add("profile", "ok", "%q (from %s)", conn.Profile, conn.ProfileSource)
				}
				add("baseUrl", "ok", "%s (from %s)", conn.BaseURL, conn.BaseURLSource)
				if connErr != nil {
					add("apiKey", "fail", "%v", connErr)
					conn = nil
				} else {
					add("apiKey", "ok", "found in %s", conn.Key.Source)
				}
			}

			for _, v := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
				if p := f.Getenv(v); p != "" {
					add("proxy", "ok", "%s is set", v)
					break
				}
			}

			if conn != nil {
				client, err := f.NewClient(conn.BaseURL, conn.Key.Key)
				if err != nil {
					add("api", "fail", "%v", err)
				} else {
					client.Retries = 0
					start := time.Now()
					resp, err := client.Do(ctx, &httpx.Request{Path: "/org"})
					elapsed := time.Since(start).Round(time.Millisecond)
					if err != nil {
						add("api", "fail", "GET /api/v0/org: %v", err)
					} else {
						org, _ := decodeOrg(resp.Body)
						add("api", "ok", "GET /api/v0/org succeeded in %s (organization %q)", elapsed, org)
						checkClock(resp, start, add)
					}
				}
			}

			p, err := f.Printer()
			if err != nil {
				return err
			}
			if err := p.Print(map[string]any{"checks": checks}); err != nil {
				return err
			}
			for _, c := range checks {
				if c.Status == "fail" {
					return &cmdutil.ExitError{Code: ExitError, Silent: true}
				}
			}
			return nil
		},
	}
}

func decodeOrg(b []byte) (string, error) {
	var org orgInfo
	if err := jsonUnmarshal(b, &org); err != nil {
		return "", err
	}
	return org.Name, nil
}

func checkClock(resp *httpx.Response, sent time.Time, add func(name, status, format string, args ...any)) {
	d := resp.Header.Get("Date")
	if d == "" {
		return
	}
	server, err := http.ParseTime(d)
	if err != nil {
		return
	}
	skew := time.Since(sent) // おおよその往復時間を許容する
	diff := time.Since(server)
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Minute+skew {
		add("clock", "warn", "local clock differs from the API server by about %s; time-based options (--since, metric timestamps) may be off", diff.Round(time.Second))
		return
	}
	add("clock", "ok", "within %s of the API server", (time.Minute + skew).Round(time.Second))
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
