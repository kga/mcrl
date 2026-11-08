package cmdutil

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kga/mcrl/internal/build"
	"github.com/kga/mcrl/internal/config"
	"github.com/kga/mcrl/internal/credential"
	"github.com/kga/mcrl/internal/httpx"
	"github.com/kga/mcrl/internal/output"
)

// Factory gives commands access to configuration, credentials, the API
// client and the output printer. Global flags are bound to its fields.
type Factory struct {
	IO        *IOStreams
	Getenv    config.Env
	Keyring   credential.Keyring
	Transport http.RoundTripper
	// Executable is the mcrl binary passed to extensions as MCRL_BIN.
	// Empty means os.Executable.
	Executable string

	// Global flags.
	Profile string
	Output  string
	JQ      string
	Compact bool
	Color   string
	Debug   bool
	DryRun  bool
	Yes     bool
	NoInput bool
	Timeout time.Duration

	// Applied is set once a request that changes resources has succeeded.
	// From then on a failing --jq must not look like a failed command.
	Applied bool

	cfg     *config.Config
	cfgPath string
}

// CheckJQ compiles the --jq expression, if any, so that a syntax error is
// reported as a usage error before any request is sent.
func (f *Factory) CheckJQ() error {
	if f.JQ == "" {
		return nil
	}
	if _, err := output.CompileJQ(f.JQ); err != nil {
		return &UsageError{Err: err}
	}
	return nil
}

// ConfigPath returns the path of config.toml.
func (f *Factory) ConfigPath() (string, error) {
	if f.cfgPath != "" {
		return f.cfgPath, nil
	}
	p, err := config.Path(f.Getenv)
	if err != nil {
		return "", err
	}
	f.cfgPath = p
	return p, nil
}

// Config loads config.toml once.
func (f *Factory) Config() (*config.Config, error) {
	if f.cfg != nil {
		return f.cfg, nil
	}
	p, err := f.ConfigPath()
	if err != nil {
		return nil, err
	}
	c, err := config.Load(p)
	if err != nil {
		return nil, err
	}
	f.cfg = c
	return c, nil
}

// SaveConfig writes the loaded config back to disk.
func (f *Factory) SaveConfig() error {
	p, err := f.ConfigPath()
	if err != nil {
		return err
	}
	if f.cfg == nil {
		return errors.New("config not loaded")
	}
	return f.cfg.Save(p)
}

// ProfileName resolves the active profile name and where it came from.
func (f *Factory) ProfileName() (string, config.Source, error) {
	c, err := f.Config()
	if err != nil {
		return "", "", err
	}
	name, src := c.ResolveProfileName(f.Profile, f.Getenv)
	return name, src, nil
}

// Resolver returns the credential resolver.
func (f *Factory) Resolver() *credential.Resolver {
	return &credential.Resolver{Getenv: f.Getenv, Keyring: f.Keyring}
}

// Connection is the resolved target of API calls.
type Connection struct {
	Profile       string
	ProfileSource config.Source
	BaseURL       string
	BaseURLSource config.Source
	Key           credential.Result
}

// Connection resolves the profile, base URL and API key.
func (f *Factory) Connection(ctx context.Context) (*Connection, error) {
	c, err := f.Config()
	if err != nil {
		return nil, err
	}
	name, src := c.ResolveProfileName(f.Profile, f.Getenv)
	p := c.Profile(name)
	base, baseSrc := config.ResolveBaseURL(p, f.Getenv)
	conn := &Connection{Profile: name, ProfileSource: src, BaseURL: base, BaseURLSource: baseSrc}
	key, err := f.Resolver().Resolve(ctx, name, p)
	if errors.Is(err, credential.ErrNotFound) {
		return conn, &AuthError{Err: WithHint(
			fmt.Errorf("%w for profile %q", ErrNoAPIKey, name),
			"run `mcrl auth login` or set the MACKEREL_APIKEY environment variable",
		)}
	}
	if err != nil {
		return conn, &AuthError{Err: err}
	}
	conn.Key = key
	if src == config.SourceFlag && key.Source == credential.SourceEnv {
		f.IO.Warnf("using the API key from %s, not the one stored for profile %q", credential.EnvVar, name)
	}
	return conn, nil
}

// APIClient builds an HTTP client for the active connection.
func (f *Factory) APIClient(ctx context.Context) (*httpx.Client, *Connection, error) {
	conn, err := f.Connection(ctx)
	if err != nil {
		return nil, conn, err
	}
	cl, err := f.NewClient(conn.BaseURL, conn.Key.Key)
	return cl, conn, err
}

// NewClient builds an HTTP client for an explicit base URL and key.
func (f *Factory) NewClient(baseURL, key string) (*httpx.Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid API base URL %q", baseURL)
	}
	cl := &httpx.Client{
		BaseURL:   u,
		APIKey:    key,
		UserAgent: UserAgent(),
		HTTP:      &http.Client{Timeout: f.Timeout, Transport: f.Transport},
		Retries:   3,
	}
	if f.DebugEnabled() {
		cl.Debug = f.IO.ErrOut
	}
	return cl, nil
}

// UserAgent is sent with every request.
func UserAgent() string {
	b := build.Get()
	return fmt.Sprintf("mcrl/%s (%s; %s)", b.Version, b.OS, b.Arch)
}

// DebugEnabled reports whether --debug or MCRL_DEBUG is on.
func (f *Factory) DebugEnabled() bool {
	if f.Debug {
		return true
	}
	return truthy(f.Getenv("MCRL_DEBUG"))
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

// Printer builds the output printer from the global flags.
func (f *Factory) Printer() (*output.Printer, error) {
	o := f.Output
	if o == "" {
		o = f.Getenv("MCRL_OUTPUT")
	}
	format, err := output.ParseFormat(o)
	if err != nil {
		return nil, &UsageError{Err: err}
	}
	color, err := f.colorEnabled()
	if err != nil {
		return nil, err
	}
	p := &output.Printer{
		Out:    f.IO.Out,
		Format: format,
		Pretty: f.IO.OutTTY && !f.Compact,
		Color:  color,
	}
	if f.JQ != "" {
		code, err := output.CompileJQ(f.JQ)
		if err != nil {
			return nil, &UsageError{Err: err}
		}
		p.Query = code
	}
	return p, nil
}

func (f *Factory) colorEnabled() (bool, error) {
	when := f.Color
	if when == "" {
		when = f.Getenv("MCRL_COLOR")
	}
	switch strings.ToLower(when) {
	case "always":
		return true, nil
	case "never":
		return false, nil
	case "", "auto":
		return f.IO.OutTTY && f.Getenv("NO_COLOR") == "" && f.Getenv("TERM") != "dumb", nil
	}
	return false, Usagef("invalid --color value %q (valid: auto, always, never)", when)
}

// BaseURL resolves the API base URL without needing an API key.
func (f *Factory) BaseURL() (string, error) {
	c, err := f.Config()
	if err != nil {
		return "", err
	}
	name, _ := c.ResolveProfileName(f.Profile, f.Getenv)
	u, _ := config.ResolveBaseURL(c.Profile(name), f.Getenv)
	return u, nil
}
