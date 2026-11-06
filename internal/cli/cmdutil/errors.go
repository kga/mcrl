package cmdutil

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// UsageError is a mistake in how a command was invoked (exit code 2).
type UsageError struct{ Err error }

func (e *UsageError) Error() string { return e.Err.Error() }
func (e *UsageError) Unwrap() error { return e.Err }

// Usagef creates a UsageError.
func Usagef(format string, args ...any) error {
	return &UsageError{Err: fmt.Errorf(format, args...)}
}

// HintError attaches a suggestion for what to do next.
type HintError struct {
	Err  error
	Hint string
}

func (e *HintError) Error() string { return e.Err.Error() }
func (e *HintError) Unwrap() error { return e.Err }

// WithHint attaches a hint to err.
func WithHint(err error, hint string) error {
	if err == nil {
		return nil
	}
	return &HintError{Err: err, Hint: hint}
}

// ExitError ends the command with a specific exit code. When Silent is true,
// the command already reported the problem and nothing more is printed.
type ExitError struct {
	Code   int
	Err    error
	Silent bool
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit status %d", e.Code)
	}
	return e.Err.Error()
}
func (e *ExitError) Unwrap() error { return e.Err }

// ErrNoAPIKey means no API key could be found for the profile.
var ErrNoAPIKey = errors.New("no API key configured")

// AuthError marks failures that should exit with the authentication code (3).
type AuthError struct{ Err error }

func (e *AuthError) Error() string { return e.Err.Error() }
func (e *AuthError) Unwrap() error { return e.Err }

// ExactArgs is cobra.ExactArgs returning a UsageError.
func ExactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return Usagef("%s requires exactly %d argument(s), got %d", cmd.CommandPath(), n, len(args))
		}
		return nil
	}
}

// NoArgs is cobra.NoArgs returning a UsageError.
func NoArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return Usagef("unexpected argument %q for %s", args[0], cmd.CommandPath())
	}
	return nil
}

// MinimumArgs is cobra.MinimumNArgs returning a UsageError.
func MinimumArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < n {
			return Usagef("%s requires at least %d argument(s), got %d", cmd.CommandPath(), n, len(args))
		}
		return nil
	}
}
