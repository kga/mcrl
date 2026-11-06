// Command mcrl is a command-line client for the Mackerel API.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/kga/mcrl/internal/cli"
	"github.com/kga/mcrl/internal/cli/cmdutil"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cli.Main(ctx, os.Args[1:], cmdutil.System())
}
