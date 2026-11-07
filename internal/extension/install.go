package extension

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// UsageError means the argument does not name an installable extension.
type UsageError struct{ Err error }

func (e *UsageError) Error() string { return e.Err.Error() }
func (e *UsageError) Unwrap() error { return e.Err }

func usagef(format string, args ...any) error {
	return &UsageError{Err: fmt.Errorf(format, args...)}
}

var ownerRepoRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Install installs from a local directory (linked, so edits take effect at
// once), a GitHub "owner/repo", or a git URL (cloned at the default
// branch's latest commit). An existing directory wins over "owner/repo".
func (m *Manager) Install(ctx context.Context, spec string) (*Extension, error) {
	if st, err := os.Stat(spec); err == nil && st.IsDir() {
		return m.installLocal(spec)
	}
	url := spec
	switch {
	case strings.Contains(spec, "://") || strings.HasPrefix(spec, "git@"):
	case ownerRepoRE.MatchString(spec):
		url = "https://github.com/" + spec + ".git"
	default:
		return nil, usagef("%q is neither a directory, OWNER/REPO nor a git URL", spec)
	}
	base := strings.TrimSuffix(path.Base(strings.TrimRight(url, "/")), ".git")
	name, err := m.checkNew(base)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(m.Dir, ".install-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	clone := filepath.Join(tmp, base)
	if _, err := git(ctx, "", "clone", "--depth", "1", "--quiet", "--", url, clone); err != nil {
		return nil, err
	}
	if m.executable(clone, base, true) == "" {
		return nil, conflictf("the repository has no executable %s at its root", base)
	}
	dest := filepath.Join(m.Dir, base)
	if err := os.Rename(clone, dest); err != nil {
		return nil, err
	}
	return m.Describe(ctx, name)
}

func (m *Manager) installLocal(dir string) (*Extension, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	base := filepath.Base(abs)
	name, err := m.checkNew(base)
	if err != nil {
		return nil, err
	}
	if m.executable(abs, base, true) == "" {
		return nil, conflictf("%s has no executable %s", abs, base)
	}
	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.Symlink(abs, filepath.Join(m.Dir, base)); err != nil {
		return nil, fmt.Errorf("link the directory: %w", err)
	}
	return m.managed(name)
}

// checkNew validates the directory or repository name mcrl-<name> and makes
// sure nothing is installed under it yet.
func (m *Manager) checkNew(base string) (string, error) {
	name, ok := strings.CutPrefix(base, Prefix)
	if !ok || !ValidName(name) {
		return "", usagef("an extension's directory or repository must be named %s<name> with lowercase letters, digits and hyphens, got %q", Prefix, base)
	}
	if m.Reserved != nil && m.Reserved(name) {
		return "", conflictf("%q is a built-in command; an extension with this name would never run", name)
	}
	if e, err := m.managed(name); err != nil {
		return "", err
	} else if e != nil {
		return "", conflictf("extension %q is already installed from %s; remove it first", name, e.Dir)
	}
	return name, nil
}

// Describe returns an installed extension with its git remote and commit.
func (m *Manager) Describe(ctx context.Context, name string) (*Extension, error) {
	e, err := m.managed(name)
	if err != nil || e == nil {
		return e, err
	}
	m.addGitInfo(ctx, e)
	return e, nil
}

// addGitInfo fills URL and Commit for a cloned extension. Failures leave
// them empty: listing should still work without git.
func (m *Manager) addGitInfo(ctx context.Context, e *Extension) {
	if e.Source != SourceGit {
		return
	}
	if out, err := git(ctx, e.Dir, "rev-parse", "HEAD"); err == nil {
		e.Commit = out
	}
	if out, err := git(ctx, e.Dir, "remote", "get-url", "origin"); err == nil {
		e.URL = out
	}
}

// ListDetailed is List with git remotes and commits.
func (m *Manager) ListDetailed(ctx context.Context) ([]Extension, error) {
	exts, err := m.List()
	if err != nil {
		return nil, err
	}
	for i := range exts {
		m.addGitInfo(ctx, &exts[i])
	}
	return exts, nil
}

// Upgrade is the result of upgrading one extension.
type Upgrade struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	From     string `json:"from"`
	To       string `json:"to"`
	Upgraded bool   `json:"upgraded"`
	DryRun   bool   `json:"dryRun,omitempty"`
}

// Upgrade moves a cloned extension to the latest commit of the remote's
// default branch. With dryRun it only fetches and reports that commit.
func (m *Manager) Upgrade(ctx context.Context, name string, dryRun bool) (*Upgrade, error) {
	e, err := m.Describe(ctx, name)
	if err != nil {
		return nil, err
	}
	if e == nil {
		if p, _ := m.Find(name); p != nil {
			return nil, conflictf("extension %q is %s, not installed by mcrl; update it the way you installed it", name, p.Path)
		}
		return nil, &NotFoundError{Name: name}
	}
	if e.Source != SourceGit {
		return nil, conflictf("extension %q is linked from %s; it already runs the files there", name, e.Dir)
	}
	if e.Commit == "" {
		return nil, conflictf("cannot read the installed commit of %q in %s", name, e.Dir)
	}
	if _, err := git(ctx, e.Dir, "fetch", "--depth", "1", "--quiet", "origin", "HEAD"); err != nil {
		return nil, err
	}
	to, err := git(ctx, e.Dir, "rev-parse", "FETCH_HEAD")
	if err != nil {
		return nil, err
	}
	u := &Upgrade{Name: name, URL: e.URL, From: e.Commit, To: to, DryRun: dryRun}
	if dryRun || to == e.Commit {
		return u, nil
	}
	// The clone is mcrl's: discard local changes rather than merge them.
	if _, err := git(ctx, e.Dir, "reset", "--hard", "--quiet", "FETCH_HEAD"); err != nil {
		return nil, err
	}
	u.Upgraded = true
	return u, nil
}

// Remove deletes a cloned extension, or only the link of a local one.
func (m *Manager) Remove(name string) (*Extension, error) {
	e, err := m.managed(name)
	if err != nil {
		return nil, err
	}
	if e == nil {
		if p, _ := m.Find(name); p != nil {
			return nil, conflictf("extension %q is %s, not installed by mcrl; delete it yourself", name, p.Path)
		}
		return nil, &NotFoundError{Name: name}
	}
	link := filepath.Join(m.Dir, Prefix+name)
	if e.Source == SourceGit {
		err = os.RemoveAll(link)
	} else {
		err = os.Remove(link)
	}
	return e, err
}

// GitNotFoundError means git is needed but not installed.
type GitNotFoundError struct{ Err error }

func (e *GitNotFoundError) Error() string {
	return "git is required to install extensions from a repository: " + e.Err.Error()
}
func (e *GitNotFoundError) Unwrap() error { return e.Err }

func git(ctx context.Context, dir string, args ...string) (string, error) {
	sub := args[0]
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", &GitNotFoundError{Err: err}
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", sub, msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}
