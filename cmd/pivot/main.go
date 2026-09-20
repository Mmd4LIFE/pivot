// Command pivot is the Pivot business intelligence platform.
//
// Pivot ships as a single static binary: this command embeds the API server,
// the metadata store, the query engine, and (from Part 9) the web frontend.
//
// See https://github.com/Mmd4LIFE/pivot and docs/ for the design corpus.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Mmd4LIFE/pivot/internal/cli"
)

func main() {
	env := cli.Env{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Lookup: os.LookupEnv,
	}

	if err := cli.NewRootCmd(env).ExecuteContext(context.Background()); err != nil {
		// Cobra is configured with SilenceErrors so that this is the single
		// place an error reaches the user, in a consistent shape.
		fmt.Fprintf(os.Stderr, "pivot: %v\n", err)
		os.Exit(1)
	}
}
