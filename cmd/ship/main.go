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
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/chris/shiphappens/flow"
	"github.com/chris/shiphappens/internal/cache"
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
	case "cache":
		os.Exit(cacheCmd(args[1:]))
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
  ship cache <du|prune> [flags]           inspect / garbage-collect the cache
  ship version                            print the version

flags: --job <id>  --resume  --changed[=ref]  --engine <docker|podman|apple>
       --mount <vol:/path>  --var <K=V>  --tui / --no-tui  --graph
       --compile <out.json>  --no-cache  --no-preheat  --max-parallel <N>

Pkl pipelines require the `+"`pkl`"+` CLI (https://pkl-lang.org).
`)
}

// cacheCmd implements `ship cache du` and `ship cache prune`.
func cacheCmd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ship cache <du|prune> [flags]")
		return 2
	}
	store, err := cache.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cache: %v\n", err)
		return 1
	}
	switch args[0] {
	case "du":
		st, err := store.Stat()
		if err != nil {
			fmt.Fprintf(os.Stderr, "cache du: %v\n", err)
			return 1
		}
		fmt.Printf("cache: %s\n", store.Root())
		fmt.Printf("objects: %d\n", st.Objects)
		fmt.Printf("size:    %s\n", humanBytes(st.Bytes))
		if st.Objects > 0 {
			fmt.Printf("oldest:  %s\n", st.Oldest.Format("2006-01-02 15:04"))
			fmt.Printf("newest:  %s\n", st.Newest.Format("2006-01-02 15:04"))
		}
		return 0
	case "prune":
		fs := flag.NewFlagSet("cache prune", flag.ExitOnError)
		days := fs.Int("older-than-days", 0, "remove objects older than N days")
		maxGB := fs.Float64("max-size-gb", 0, "cap total cache size to N gigabytes (LRU eviction)")
		all := fs.Bool("all", false, "remove the entire cache")
		fs.Parse(args[1:])

		var res cache.PruneResult
		if *all {
			res, err = store.PruneAll()
		} else if *days == 0 && *maxGB == 0 {
			fmt.Fprintln(os.Stderr, "cache prune: specify --older-than-days, --max-size-gb, or --all")
			return 2
		} else {
			res, err = store.Prune(
				time.Duration(*days)*24*time.Hour,
				int64(*maxGB*1e9),
			)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "cache prune: %v\n", err)
			return 1
		}
		fmt.Printf("pruned %d object(s), freed %s\n", res.Removed, humanBytes(res.Bytes))
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown cache subcommand %q (du|prune)\n", args[0])
		return 2
	}
}

// humanBytes formats a byte count as a human-readable string.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
