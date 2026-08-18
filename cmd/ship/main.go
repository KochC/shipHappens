// Command ship is the standalone Ship Happens CLI for file-based pipelines.
//
//	ship run <pipeline.pkl|.json> [flags]   # run a pipeline (Pkl or JSON plan)
//	ship validate <pipeline.pkl|.json>      # validate only
//	ship <pipeline.pkl|.json> [flags]       # shorthand for `ship run`
//
// Pipelines authored in Pkl require the `pkl` CLI on PATH (https://pkl-lang.org).
// JSON plans are the RunPlan artifacts produced by `--compile`.
//
// All Ship Happens run flags apply: --job, --resume, --changed, --engine,
// --mount, --var, --tui/--no-tui, --graph, --compile, --no-cache, --no-preheat.
package main

import (
	"fmt"
	"os"

	"github.com/chris/shiphappens/flow"
)

// version is set at build time via -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	switch args[0] {
	case "-h", "--help", "help":
		usage()
		os.Exit(0)
	case "-v", "--version", "version":
		fmt.Printf("ship %s\n", version)
		os.Exit(0)
	case "run":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "ship run: missing pipeline file")
			os.Exit(2)
		}
		flow.MainFile(args[1], args[2:])
	case "validate":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "ship validate: missing pipeline file")
			os.Exit(2)
		}
		// Validation is a run with --graph (compiles + validates, doesn't execute).
		os.Exit(flow.RunFile(args[1], append([]string{"--graph"}, args[2:]...)))
	default:
		// Shorthand: `ship pipeline.pkl [flags]` == `ship run pipeline.pkl …`.
		flow.MainFile(args[0], args[1:])
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `ship — local-first CI runner (file-based pipelines)

usage:
  ship run <pipeline.pkl|.json> [flags]   run a pipeline
  ship validate <pipeline.pkl|.json>      compile + validate only
  ship <pipeline.pkl|.json> [flags]       shorthand for `+"`ship run`"+`
  ship version                            print the version

flags: --job <id>  --resume  --changed[=ref]  --engine <docker|podman|apple>
       --mount <vol:/path>  --var <K=V>  --tui / --no-tui  --graph
       --compile <out.json>  --no-cache  --no-preheat  --max-parallel <N>

Pkl pipelines require the `+"`pkl`"+` CLI (https://pkl-lang.org).
`)
}
