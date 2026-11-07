// Package extension finds, installs and runs mcrl extensions: executables
// named mcrl-<name> that "mcrl <name>" runs when <name> is not a built-in
// command. See docs/extensions.md.
package extension

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
)

// Prefix is the file name prefix of every extension executable.
const Prefix = "mcrl-"

// DirEnv overrides the directory managed extensions are installed into.
const DirEnv = "MCRL_EXTENSIONS_DIR"

// Where an extension comes from.
const (
	SourceGit   = "git"   // cloned by "mcrl extension install"
	SourceLocal = "local" // a directory linked by "mcrl extension install <path>"
	SourcePath  = "path"  // an executable found in PATH
)

// Extension is one installed or discovered extension.
type Extension struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	// Path is the executable that runs.
	Path string `json:"path"`
	// Dir is the clone (git) or the linked directory (local).
	Dir    string `json:"dir,omitempty"`
	URL    string `json:"url,omitempty"`
	Commit string `json:"commit,omitempty"`
	// Shadowed is true when a built-in command has the same name, so the
	// extension never runs. Set by the caller, which knows the commands.
	Shadowed bool `json:"shadowed,omitempty"`
}

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidName reports whether name can be an extension name. It also keeps
// "mcrl ../x" from turning into a path.
func ValidName(name string) bool { return nameRE.MatchString(name) }

// Dir returns the directory for managed extensions: MCRL_EXTENSIONS_DIR,
// then the XDG data directory (or %LOCALAPPDATA% on Windows).
func Dir(getenv func(string) string) (string, error) {
	if d := getenv(DirEnv); d != "" {
		return d, nil
	}
	if runtime.GOOS == "windows" {
		if d := getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "mcrl", "extensions"), nil
		}
	}
	if d := getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "mcrl", "extensions"), nil
	}
	home := getenv("HOME")
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return "", err
		}
	}
	return filepath.Join(home, ".local", "share", "mcrl", "extensions"), nil
}

// Manager looks up extensions in Dir, then in the directories of PATH.
type Manager struct {
	Dir     string
	PATH    string // the PATH value to search
	PATHEXT string // Windows executable suffixes; empty means the default
	// Reserved reports names taken by built-in commands. Install refuses them.
	Reserved func(name string) bool
}

// NotFoundError means no extension has the name.
type NotFoundError struct{ Name string }

func (e *NotFoundError) Error() string { return fmt.Sprintf("extension %q not found", e.Name) }

// ConflictError means the operation does not fit the extension's state.
type ConflictError struct{ Err error }

func (e *ConflictError) Error() string { return e.Err.Error() }
func (e *ConflictError) Unwrap() error { return e.Err }

func conflictf(format string, args ...any) error {
	return &ConflictError{Err: fmt.Errorf(format, args...)}
}

// Find returns the extension called name, or nil when there is none. It does
// not run git, so it is cheap enough to call on every invocation.
func (m *Manager) Find(name string) (*Extension, error) {
	if !ValidName(name) {
		return nil, nil
	}
	if e, err := m.managed(name); e != nil || err != nil {
		return e, err
	}
	for _, dir := range m.pathDirs() {
		if p := m.executable(dir, Prefix+name, false); p != "" {
			return &Extension{Name: name, Source: SourcePath, Path: p}, nil
		}
	}
	return nil, nil
}

// List returns every extension sorted by name. When the same name is both
// installed and in PATH, only the one that runs is listed.
func (m *Manager) List() ([]Extension, error) {
	seen := map[string]bool{}
	var out []Extension
	entries, err := os.ReadDir(m.Dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, ent := range entries {
		name, ok := strings.CutPrefix(ent.Name(), Prefix)
		if !ok || !ValidName(name) {
			continue
		}
		e, err := m.managed(name)
		if err != nil {
			return nil, err
		}
		if e != nil {
			out = append(out, *e)
			seen[name] = true
		}
	}
	for _, dir := range m.pathDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, ent := range entries {
			base := m.trimExecSuffix(ent.Name())
			name, ok := strings.CutPrefix(base, Prefix)
			if !ok || !ValidName(name) || seen[name] {
				continue
			}
			if p := m.executable(dir, base, false); p != "" {
				out = append(out, Extension{Name: name, Source: SourcePath, Path: p})
				seen[name] = true
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// managed returns the extension installed in m.Dir, or nil.
func (m *Manager) managed(name string) (*Extension, error) {
	link := filepath.Join(m.Dir, Prefix+name)
	st, err := os.Lstat(link)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e := &Extension{Name: name, Source: SourceLocal, Dir: link}
	switch {
	case st.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(link)
		if err != nil {
			return nil, err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(m.Dir, target)
		}
		e.Dir = target
	case st.IsDir():
		if _, err := os.Stat(filepath.Join(link, ".git")); err == nil {
			e.Source = SourceGit
		}
	default:
		return nil, nil
	}
	e.Path = m.executable(e.Dir, Prefix+name, true)
	if e.Path == "" {
		// Installed but broken (the linked directory moved, or the
		// repository lost its executable). List it so it can be removed.
		e.Path = filepath.Join(e.Dir, Prefix+name)
	}
	return e, nil
}

// Runnable reports whether the extension's executable exists.
func (m *Manager) Runnable(e *Extension) bool {
	return m.executable(filepath.Dir(e.Path), filepath.Base(e.Path), true) != ""
}

func (m *Manager) pathDirs() []string {
	var dirs []string
	for _, d := range filepath.SplitList(m.PATH) {
		// Relative entries ("." or "") would make the result depend on the
		// current directory, which is how planted binaries get run.
		if d != "" && filepath.IsAbs(d) {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

func (m *Manager) pathExts() []string {
	v := m.PATHEXT
	if v == "" {
		v = ".com;.exe;.bat;.cmd"
	}
	var exts []string
	for _, e := range strings.Split(strings.ToLower(v), ";") {
		if e != "" {
			exts = append(exts, e)
		}
	}
	return exts
}

func (m *Manager) trimExecSuffix(file string) string {
	if runtime.GOOS != "windows" {
		return file
	}
	ext := strings.ToLower(filepath.Ext(file))
	for _, e := range m.pathExts() {
		if ext == e {
			return strings.TrimSuffix(file, filepath.Ext(file))
		}
	}
	return file
}

// executable returns the path of the executable base in dir, or "". On
// Windows it tries the PATHEXT suffixes; in a managed directory it also
// accepts a file without a suffix, which Command runs with sh.
func (m *Manager) executable(dir, base string, managed bool) string {
	isFile := func(p string) (os.FileInfo, bool) {
		st, err := os.Stat(p)
		return st, err == nil && st.Mode().IsRegular()
	}
	p := filepath.Join(dir, base)
	if runtime.GOOS != "windows" {
		if st, ok := isFile(p); ok && st.Mode().Perm()&0o111 != 0 {
			return p
		}
		return ""
	}
	if ext := strings.ToLower(filepath.Ext(base)); ext != "" && slices.Contains(m.pathExts(), ext) {
		if _, ok := isFile(p); ok {
			return p
		}
		return ""
	}
	for _, ext := range m.pathExts() {
		if _, ok := isFile(p + ext); ok {
			return p + ext
		}
	}
	if managed {
		if _, ok := isFile(p); ok {
			return p
		}
	}
	return ""
}
