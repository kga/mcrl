package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func env(m map[string]string) Env { return func(k string) string { return m[k] } }

func TestPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix paths")
	}
	tests := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"MCRL_CONFIG": "/x/c.toml", "HOME": "/h"}, "/x/c.toml"},
		{map[string]string{"XDG_CONFIG_HOME": "/xdg", "HOME": "/h"}, "/xdg/mcrl/config.toml"},
		{map[string]string{"HOME": "/h"}, "/h/.config/mcrl/config.toml"},
	}
	for _, tt := range tests {
		got, err := Path(env(tt.env))
		if err != nil || got != tt.want {
			t.Errorf("Path(%v) = %q, %v; want %q", tt.env, got, err, tt.want)
		}
	}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml")
	c, err := Load(path)
	if err != nil || len(c.Profiles) != 0 {
		t.Fatalf("missing file: %+v, %v", c, err)
	}
	c.DefaultProfile = "work"
	c.EnsureProfile("work").BaseURL = "https://kcps-mackerel.io"
	c.EnsureProfile("ci").APIKeyCommand = "op read op://x"
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		st, _ := os.Stat(path)
		if st.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v", st.Mode().Perm())
		}
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultProfile != "work" || got.Profile("work").BaseURL != "https://kcps-mackerel.io" ||
		got.Profile("ci").APIKeyCommand != "op read op://x" {
		t.Errorf("round trip lost data: %+v", got)
	}
	if names := got.ProfileNames(); strings.Join(names, ",") != "ci,work" {
		t.Errorf("names = %v", names)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[profiles.x]\napikey = \"typo\"\n"), 0o600)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "apikey") {
		t.Errorf("err = %v", err)
	}
}

func TestResolve(t *testing.T) {
	c := &Config{DefaultProfile: "work", Profiles: map[string]*Profile{"work": {BaseURL: "https://kcps-mackerel.io"}}}
	if n, s := c.ResolveProfileName("flag", env(map[string]string{"MCRL_PROFILE": "env"})); n != "flag" || s != SourceFlag {
		t.Errorf("flag: %s %s", n, s)
	}
	if n, s := c.ResolveProfileName("", env(map[string]string{"MCRL_PROFILE": "env"})); n != "env" || s != SourceEnv {
		t.Errorf("env: %s %s", n, s)
	}
	if n, s := c.ResolveProfileName("", env(nil)); n != "work" || s != SourceFile {
		t.Errorf("file: %s %s", n, s)
	}
	if n, s := (&Config{}).ResolveProfileName("", env(nil)); n != DefaultProfileName || s != SourceDefault {
		t.Errorf("default: %s %s", n, s)
	}
	if u, s := ResolveBaseURL(c.Profile("work"), env(nil)); u != "https://kcps-mackerel.io" || s != SourceFile {
		t.Errorf("base: %s %s", u, s)
	}
	if u, s := ResolveBaseURL(c.Profile("none"), env(nil)); u != DefaultBaseURL || s != SourceDefault {
		t.Errorf("base default: %s %s", u, s)
	}
}
