package flow

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/chris/shiphappens/internal/changed"
	"github.com/chris/shiphappens/internal/compiler"
	"github.com/chris/shiphappens/internal/graph"
	"github.com/chris/shiphappens/internal/logs"
	"github.com/chris/shiphappens/internal/scheduler"
)

// writePlan serializes the compiled plan to a JSON artifact ("Terraform plan,
// but for CI").
func writePlan(p *compiler.RunPlan, path string) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Main is the entry point for a pipeline program. It parses CLI flags,
// compiles+validates the workflow, and runs (or prints) it, exiting the process
// with an appropriate status code.
//
// Flags:
//
//	--graph            print the DAG and exit
//	--job <id>         run only this job (and its dependencies); repeatable via comma
//	--no-cache         disable step caching
//	--changed[=<ref>]  only run jobs affected by git changes vs ref (default main)
func Main(w *Workflow) {
	var (
		graphOnly   bool
		jobFlag     string
		noCache     bool
		compileOnly string
		engine      string
	)
	changedVal := &optString{}
	fs := flag.NewFlagSet(w.Name, flag.ExitOnError)
	fs.BoolVar(&graphOnly, "graph", false, "print the execution graph and exit")
	fs.StringVar(&jobFlag, "job", "", "run only this job (and its dependencies)")
	fs.BoolVar(&noCache, "no-cache", false, "disable step caching")
	fs.StringVar(&compileOnly, "compile", "", "write the compiled plan as JSON to the given path and exit")
	fs.StringVar(&engine, "engine", "docker", "container engine for image jobs (docker|podman)")
	fs.Var(changedVal, "changed", "run only jobs affected by git changes vs ref (default main)")
	fs.Parse(os.Args[1:])
	changedSet := changedVal.set
	changedFl := changedVal.val

	raw := w.ToPlan()
	plan, err := compile(raw, w.Lines())
	if err != nil {
		logs.Failure("%s", err.Error())
		os.Exit(1)
	}

	logs.Success("✓ compiled: %s — %d jobs, %d steps, DAG valid", plan.Name, len(plan.Jobs), stepCount(plan))
	fmt.Println()

	dag := graph.Build(plan)

	if compileOnly != "" {
		if err := writePlan(plan, compileOnly); err != nil {
			logs.Failure("write plan: %v", err)
			os.Exit(1)
		}
		logs.Success("✓ wrote compiled plan → %s", compileOnly)
		return
	}

	if graphOnly {
		printGraph(plan, dag)
		return
	}

	// Determine job subset.
	var only map[string]bool
	if jobFlag != "" {
		if plan.Job(jobFlag) == nil {
			logs.Failure("unknown job %q", jobFlag)
			os.Exit(1)
		}
		only = dag.Subgraph(jobFlag)
	} else if changedSet {
		base := changedFl
		if base == "" || base == "true" {
			base = "main"
		}
		wd, _ := os.Getwd()
		files, ferr := changed.Files(wd, base)
		if ferr != nil {
			logs.Failure("git diff failed: %v", ferr)
			os.Exit(1)
		}
		if len(files) == 0 {
			logs.Info("no changes detected vs %s — nothing to run", base)
			return
		}
		only = changed.AffectedJobs(plan, dag, files)
		logs.Info("changed vs %s: %d file(s), %d job(s) affected\n", base, len(files), len(only))
	}

	printGraph(plan, dag)
	fmt.Println()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	wd, _ := os.Getwd()
	res := scheduler.Run(ctx, plan, scheduler.Options{
		Workdir: wd,
		NoCache: noCache,
		Only:    only,
		Engine:  engine,
	})

	fmt.Println()
	if res.Failed {
		logs.Failure("✗ %s failed in %s  (%d ran, %d cached)", plan.Name, res.Duration.Round(1e6), res.Ran, res.Cached)
		os.Exit(1)
	}
	logs.Success("✓ %s passed in %s  (%d ran, %d cached)", plan.Name, res.Duration.Round(1e6), res.Ran, res.Cached)
}

func stepCount(p *compiler.RunPlan) int {
	n := 0
	for _, j := range p.Jobs {
		n += len(j.Steps)
	}
	return n
}

func printGraph(p *compiler.RunPlan, dag *graph.DAG) {
	logs.Header("Execution graph:")
	order := dag.TopoOrder()
	for _, id := range order {
		needs := dag.Needs[id]
		if len(needs) == 0 {
			logs.Info("  %s", id)
		} else {
			logs.Info("  %s  ← %s", id, joinComma(needs))
		}
	}
}

func joinComma(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}

// optString is a flag.Value with an optional argument: "--changed" (bare) sets
// it with an empty value; "--changed=ref" sets a specific value.
type optString struct {
	set bool
	val string
}

func (o *optString) String() string     { return o.val }
func (o *optString) Set(s string) error  { o.set = true; o.val = s; return nil }
func (o *optString) IsBoolFlag() bool     { return true }
