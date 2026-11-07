// Command gendoc writes the command reference (docs/reference) generated
// from the command tree. Run it with "mise run docs" or "go generate ./internal/cli".
package main

import (
	"fmt"
	"os"

	"github.com/kga/mcrl/internal/cli"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: gendoc <dir>")
		os.Exit(2)
	}
	if err := cli.WriteReference(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "gendoc:", err)
		os.Exit(1)
	}
}
