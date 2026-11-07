package extension

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
)

// Command builds the process for e. On Windows a managed extension without
// a PATHEXT suffix (a shell script with a shebang) runs with sh, which Git
// for Windows provides.
func (m *Manager) Command(e *Extension, args []string) (*exec.Cmd, error) {
	if runtime.GOOS == "windows" && !slices.Contains(m.pathExts(), strings.ToLower(filepath.Ext(e.Path))) {
		sh, err := exec.LookPath("sh")
		if err != nil {
			return nil, fmt.Errorf("extension %q is a script without a Windows suffix and no sh was found in PATH", e.Name)
		}
		return exec.Command(sh, append([]string{e.Path}, args...)...), nil
	}
	return exec.Command(e.Path, args...), nil
}

// Run starts cmd with the given standard streams, waits for it and returns
// its exit code. A child killed by a signal yields 128 + the signal number,
// as shells report it.
//
// Ctrl-C reaches the child directly, because the terminal sends SIGINT to
// the whole foreground process group; mcrl only has to survive it and wait.
// Forwarding SIGINT as well would deliver it twice. SIGTERM and SIGHUP are
// usually sent to mcrl alone (kill, a supervisor), so they are forwarded.
func Run(cmd *exec.Cmd, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-sigs:
				if s != os.Interrupt && runtime.GOOS != "windows" {
					_ = cmd.Process.Signal(s)
				}
			case <-done:
				return
			}
		}
	}()
	err := cmd.Wait()
	close(done)
	var ee *exec.ExitError
	if err != nil && !errors.As(err, &ee) {
		return 0, err
	}
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal()), nil
	}
	return cmd.ProcessState.ExitCode(), nil
}
