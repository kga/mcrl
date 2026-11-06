// Package config loads and saves the mcrl configuration file and resolves profiles.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/pelletier/go-toml/v2"
)

// DefaultBaseURL is the Mackerel API endpoint used when no profile overrides it.
const DefaultBaseURL = "https://api.mackerelio.com"

// DefaultProfileName is used when neither a flag, MCRL_PROFILE nor default_profile is set.
const DefaultProfileName = "default"

// Config is the content of config.toml.
type Config struct {
	DefaultProfile string              `toml:"default_profile,omitempty"`
	Profiles       map[string]*Profile `toml:"profiles,omitempty"`
}

// Profile is a connection setting for one Mackerel organization.
type Profile struct {
	BaseURL       string `toml:"base_url,omitempty"`
	APIKeyCommand string `toml:"api_key_command,omitempty"`
	// APIKey is a plaintext key. It is the last resort when no OS keychain is available.
	APIKey string `toml:"api_key,omitempty"`
}

// Env abstracts environment lookups so tests can inject values.
type Env func(key string) string

// Path returns the config file path: MCRL_CONFIG, then the XDG / OS default.
func Path(getenv Env) (string, error) {
	if p := getenv("MCRL_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := configDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mcrl", "config.toml"), nil
}

// CacheDir returns the cache directory: MCRL_CACHE_DIR, then the XDG / OS default.
func CacheDir(getenv Env) (string, error) {
	if p := getenv("MCRL_CACHE_DIR"); p != "" {
		return p, nil
	}
	if runtime.GOOS == "windows" {
		if d := getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "mcrl", "cache"), nil
		}
	}
	if d := getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "mcrl"), nil
	}
	home, err := homeDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "mcrl"), nil
}

func configDir(getenv Env) (string, error) {
	if runtime.GOOS == "windows" {
		if d := getenv("APPDATA"); d != "" {
			return d, nil
		}
	}
	// macOS でも ~/Library/Application Support ではなく XDG に従う（CLI 利用者の慣習）
	if d := getenv("XDG_CONFIG_HOME"); d != "" {
		return d, nil
	}
	home, err := homeDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

func homeDir(getenv Env) (string, error) {
	if h := getenv("HOME"); h != "" {
		return h, nil
	}
	return os.UserHomeDir()
}

// Load reads the config file. A missing file yields an empty config.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	dec := toml.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		var sm *toml.StrictMissingError
		if errors.As(err, &sm) {
			return nil, fmt.Errorf("%s: %s", path, sm.String())
		}
		var de *toml.DecodeError
		if errors.As(err, &de) {
			row, col := de.Position()
			return nil, fmt.Errorf("%s:%d:%d: %s", path, row, col, de.Error())
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

// Save writes the config file with 0600 permissions, creating parent directories.
func (c *Config) Save(path string) error {
	b, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Profile returns the named profile, or an empty one if it does not exist.
func (c *Config) Profile(name string) *Profile {
	if p, ok := c.Profiles[name]; ok && p != nil {
		return p
	}
	return &Profile{}
}

// HasProfile reports whether the named profile is defined in the file.
func (c *Config) HasProfile(name string) bool {
	_, ok := c.Profiles[name]
	return ok
}

// EnsureProfile returns the named profile, creating it if needed.
func (c *Config) EnsureProfile(name string) *Profile {
	if c.Profiles == nil {
		c.Profiles = map[string]*Profile{}
	}
	p, ok := c.Profiles[name]
	if !ok || p == nil {
		p = &Profile{}
		c.Profiles[name] = p
	}
	return p
}

// ProfileNames returns the defined profile names in sorted order.
func (c *Config) ProfileNames() []string {
	names := make([]string, 0, len(c.Profiles))
	for n := range c.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Source tells where a resolved value came from.
type Source string

const (
	SourceFlag    Source = "flag"
	SourceEnv     Source = "env"
	SourceFile    Source = "config"
	SourceDefault Source = "default"
)

// ResolveProfileName picks the profile: --profile, MCRL_PROFILE, default_profile, "default".
func (c *Config) ResolveProfileName(flag string, getenv Env) (string, Source) {
	if flag != "" {
		return flag, SourceFlag
	}
	if p := getenv("MCRL_PROFILE"); p != "" {
		return p, SourceEnv
	}
	if c.DefaultProfile != "" {
		return c.DefaultProfile, SourceFile
	}
	return DefaultProfileName, SourceDefault
}

// ResolveBaseURL picks the API base URL: MCRL_API_BASE, the profile, the default.
func ResolveBaseURL(p *Profile, getenv Env) (string, Source) {
	if u := getenv("MCRL_API_BASE"); u != "" {
		return u, SourceEnv
	}
	if p != nil && p.BaseURL != "" {
		return p.BaseURL, SourceFile
	}
	return DefaultBaseURL, SourceDefault
}
