package extension

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix paths")
	}
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	tests := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{DirEnv: "/x", "XDG_DATA_HOME": "/xdg", "HOME": "/h"}, "/x"},
		{map[string]string{"XDG_DATA_HOME": "/xdg", "HOME": "/h"}, "/xdg/mcrl/extensions"},
		{map[string]string{"HOME": "/h"}, "/h/.local/share/mcrl/extensions"},
	}
	for _, tt := range tests {
		if got, err := Dir(env(tt.env)); err != nil || got != tt.want {
			t.Errorf("Dir(%v) = %q, %v; want %q", tt.env, got, err, tt.want)
		}
	}
}

func TestValidName(t *testing.T) {
	for _, n := range []string{"drill", "a", "back-test", "x2"} {
		if !ValidName(n) {
			t.Errorf("%q should be valid", n)
		}
	}
	for _, n := range []string{"", "-x", "../x", "a/b", "Drill", "a b", `a\b`} {
		if ValidName(n) {
			t.Errorf("%q should be invalid", n)
		}
	}
}

func TestFindPrefersManagedAndSkipsRelativePATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses the executable bit")
	}
	root := t.TempDir()
	write := func(p string, mode os.FileMode) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	managed := filepath.Join(root, "ext")
	bin := filepath.Join(root, "bin")
	write(filepath.Join(managed, "mcrl-a", "mcrl-a"), 0o755)
	write(filepath.Join(bin, "mcrl-a"), 0o755)
	write(filepath.Join(bin, "mcrl-b"), 0o755)
	write(filepath.Join(bin, "mcrl-noexec"), 0o644)

	m := &Manager{Dir: managed, PATH: "." + string(os.PathListSeparator) + bin}
	if e, _ := m.Find("a"); e == nil || e.Source != SourceLocal {
		t.Errorf("a = %+v, want the managed one", e)
	}
	if e, _ := m.Find("b"); e == nil || e.Source != SourcePath {
		t.Errorf("b = %+v", e)
	}
	if e, _ := m.Find("noexec"); e != nil {
		t.Errorf("noexec = %+v, want nil", e)
	}
	t.Chdir(bin)
	m.PATH = "."
	if e, _ := m.Find("b"); e != nil {
		t.Errorf("relative PATH entry was searched: %+v", e)
	}
	list, err := (&Manager{Dir: managed, PATH: bin}).List()
	if err != nil || len(list) != 2 || list[0].Name != "a" || list[0].Source != SourceLocal || list[1].Name != "b" {
		t.Errorf("List = %+v, %v", list, err)
	}
}
