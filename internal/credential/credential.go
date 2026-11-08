// Package credential resolves, stores and deletes Mackerel API keys.
//
// Resolution order (first match wins):
//  1. the MACKEREL_APIKEY environment variable
//  2. the profile's api_key_command (its stdout)
//  3. the OS keychain (service "mcrl", account = profile name)
//  4. the profile's plaintext api_key
package credential

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/kga/mcrl/internal/config"
)

// EnvVar is the only environment variable mcrl reads an API key from.
const EnvVar = "MACKEREL_APIKEY"

// KeyringService is the service name used in the OS keychain.
const KeyringService = "mcrl"

// Source tells where an API key came from.
type Source string

const (
	SourceNone    Source = ""
	SourceEnv     Source = "env:" + EnvVar
	SourceCommand Source = "api_key_command"
	SourceKeyring Source = "keyring"
	SourceFile    Source = "config:api_key"
)

// ErrNotFound means no API key was found anywhere.
var ErrNotFound = errors.New("no API key found")

// MaxStderrLines is how many lines of a failing api_key_command's stderr are
// kept in the error message.
const MaxStderrLines = 3

// CommandError means api_key_command could not be run or exited unsuccessfully.
// Its message keeps the exit status.
type CommandError struct{ Err error }

func (e *CommandError) Error() string { return e.Err.Error() }
func (e *CommandError) Unwrap() error { return e.Err }

// TrimLines keeps the first max lines of s and notes how many were cut.
func TrimLines(s string, max int) string {
	s = strings.TrimSpace(s)
	lines := strings.Split(s, "\n")
	if len(lines) <= max {
		return s
	}
	return fmt.Sprintf("%s\n… (%d more lines)", strings.Join(lines[:max], "\n"), len(lines)-max)
}

// Keyring is the subset of the OS keychain used by mcrl. Tests swap it out.
type Keyring interface {
	Get(service, user string) (string, error)
	Set(service, user, secret string) error
	Delete(service, user string) error
}

type osKeyring struct{}

func (osKeyring) Get(s, u string) (string, error) { return keyring.Get(s, u) }
func (osKeyring) Set(s, u, p string) error        { return keyring.Set(s, u, p) }
func (osKeyring) Delete(s, u string) error        { return keyring.Delete(s, u) }

// OSKeyring returns the platform keychain.
func OSKeyring() Keyring { return osKeyring{} }

// Resolver finds the API key for a profile.
type Resolver struct {
	Getenv  config.Env
	Keyring Keyring
	// RunCommand executes api_key_command. nil means exec without a shell.
	RunCommand func(ctx context.Context, argv []string) ([]byte, error)
}

// Result is a resolved API key.
type Result struct {
	Key    string
	Source Source
}

// Resolve returns the API key for the given profile.
func (r *Resolver) Resolve(ctx context.Context, profileName string, p *config.Profile) (Result, error) {
	if k := strings.TrimSpace(r.Getenv(EnvVar)); k != "" {
		return Result{Key: k, Source: SourceEnv}, nil
	}
	if p != nil && p.APIKeyCommand != "" {
		k, err := r.runCommand(ctx, p.APIKeyCommand)
		if err != nil {
			return Result{}, fmt.Errorf("api_key_command of profile %q failed: %w", profileName, err)
		}
		return Result{Key: k, Source: SourceCommand}, nil
	}
	if r.Keyring != nil {
		k, err := r.Keyring.Get(KeyringService, profileName)
		switch {
		case err == nil && k != "":
			return Result{Key: k, Source: SourceKeyring}, nil
		case err == nil, errors.Is(err, keyring.ErrNotFound):
			// fall through
		default:
			// キーチェーンが使えない環境（ヘッドレス Linux など）では平文設定へ進む
		}
	}
	if p != nil && p.APIKey != "" {
		return Result{Key: p.APIKey, Source: SourceFile}, nil
	}
	return Result{}, ErrNotFound
}

func (r *Resolver) runCommand(ctx context.Context, cmdline string) (string, error) {
	argv, err := SplitArgs(cmdline)
	if err != nil {
		return "", err
	}
	if len(argv) == 0 {
		return "", errors.New("empty command")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	run := r.RunCommand
	if run == nil {
		run = func(ctx context.Context, argv []string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				if msg := strings.TrimSpace(stderr.String()); msg != "" {
					return nil, fmt.Errorf("%w: %s", err, TrimLines(msg, MaxStderrLines))
				}
				return nil, err
			}
			return out, nil
		}
	}
	out, err := run(ctx, argv)
	if err != nil {
		return "", &CommandError{Err: err}
	}
	k := strings.TrimSpace(string(out))
	if k == "" {
		return "", errors.New("command printed nothing")
	}
	return k, nil
}

// SplitArgs splits a command line into arguments without invoking a shell.
// It understands single quotes, double quotes and backslash escapes.
func SplitArgs(s string) ([]string, error) {
	var (
		args    []string
		cur     strings.Builder
		inArg   bool
		quote   rune
		escaped bool
	)
	for _, c := range s {
		switch {
		case escaped:
			cur.WriteRune(c)
			escaped = false
		case c == '\\' && quote != '\'':
			escaped = true
			inArg = true
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteRune(c)
			}
		case c == '\'' || c == '"':
			quote = c
			inArg = true
		case c == ' ' || c == '\t' || c == '\n':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(c)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote in command")
	}
	if escaped {
		return nil, errors.New("trailing backslash in command")
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}
