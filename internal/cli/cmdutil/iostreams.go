// Package cmdutil holds pieces shared by all mcrl commands.
package cmdutil

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

// IOStreams are the process's standard streams plus TTY information.
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer

	InTTY  bool
	OutTTY bool
	ErrTTY bool

	// ReadSecret reads a line from the terminal without echo.
	ReadSecret func() (string, error)
}

// System returns the real standard streams.
func System() *IOStreams {
	isTTY := func(f *os.File) bool {
		return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
	}
	return &IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
		InTTY:  isTTY(os.Stdin),
		OutTTY: isTTY(os.Stdout),
		ErrTTY: isTTY(os.Stderr),
		ReadSecret: func() (string, error) {
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			return string(b), err
		},
	}
}

// CanPrompt reports whether interactive prompts are allowed.
func (s *IOStreams) CanPrompt(noInput bool) bool {
	return !noInput && s.InTTY && s.ErrTTY
}

// ErrNoPrompt is returned when a prompt is needed but not possible.
var ErrNoPrompt = errors.New("cannot prompt: not a terminal or --no-input is set")

// Confirm asks a yes/no question on stderr. The default answer is no.
func (s *IOStreams) Confirm(noInput bool, question string) (bool, error) {
	if !s.CanPrompt(noInput) {
		return false, ErrNoPrompt
	}
	fmt.Fprintf(s.ErrOut, "? %s [y/N] ", question)
	line, err := bufio.NewReader(s.In).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}

// Warnf writes a warning line to stderr.
func (s *IOStreams) Warnf(format string, args ...any) {
	fmt.Fprintf(s.ErrOut, "warning: "+format+"\n", args...)
}
