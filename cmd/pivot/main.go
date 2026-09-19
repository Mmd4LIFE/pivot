// Command pivot is the Pivot business intelligence platform.
//
// Pivot ships as a single static binary: this command embeds the API server,
// the metadata store, the query engine, and (from Part 9) the web frontend.
//
// See https://github.com/Mmd4LIFE/pivot and docs/ for the design corpus.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/Mmd4LIFE/pivot/internal/version"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "pivot: %v\n", err)
		os.Exit(1)
	}
}

// run holds the real entrypoint so it stays testable: no os.Exit, and output
// goes to an injected writer rather than straight to stdout.
//
// Part 2 replaces this with a cobra command tree (serve, version, config,
// migrate). For now it exists to prove the build works end to end.
func run(args []string, stdout io.Writer) error {
	info := version.Get()

	if len(args) > 0 {
		switch args[0] {
		case "version", "--version", "-v":
			_, err := fmt.Fprintln(stdout, info.String())
			return err
		case "help", "--help", "-h":
			_, err := fmt.Fprint(stdout, usage)
			return err
		default:
			return fmt.Errorf("unknown command %q (try `pivot help`)", args[0])
		}
	}

	_, err := fmt.Fprintln(stdout, info.String())
	return err
}

const usage = `Pivot — the open business intelligence platform for the AI era.

Usage:
  pivot [command]

Available commands:
  version     Print build information
  help        Show this help

Not yet implemented (see CHECKLIST.md):
  serve       Start the Pivot server                        [Part 2]
  migrate     Run metadata database migrations              [Part 3]
  admin       Administrative commands                       [Part 6]
  doctor      Diagnose an installation                      [Part 15]
`
