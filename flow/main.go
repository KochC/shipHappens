package flow

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/chris/shiphappens/internal/changed"
	"github.com/chris/shiphappens/internal/compiler"
	"github.com/chris/shiphappens/internal/graph"
	"github.com/chris/shiphappens/internal/logs"
	"github.com/chris/shiphappens/internal/runner"
	"github.com/chris/shiphappens/internal/scheduler"
	"github.com/chris/shiphappens/internal/tui"
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
		noPreheat   bool
		useTUI      bool
		resume      bool
	)
	changedVal := &optString{}
	mounts := &sliceFlag{}
	fs := flag.NewFlagSet(w.Name, flag.ExitOnError)
	fs.BoolVar(&graphOnly, "graph", false, "print the execution graph and exit")
	fs.StringVar(&jobFlag, "job", "", "run only this job (and its dependencies)")
	fs.BoolVar(&noCache, "no-cache", false, "disable step caching")
	fs.StringVar(&compileOnly, "compile", "", "write the compiled plan as JSON to the given path and exit")
	fs.StringVar(&engine, "engine", "docker", "container engine for image jobs (docker|podman|apple)")
	fs.BoolVar(&noPreheat, "no-preheat", false, "skip image/cache preheating before the run")
	fs.BoolVar(&useTUI, "tui", false, "render a live status dashboard instead of streaming logs")
	fs.BoolVar(&resume, "resume", false, "skip jobs whose fingerprint matches a prior successful run (incremental)")
	fs.Var(mounts, "mount", "extra container volume spec for image jobs (repeatable), e.g. vol:/root/.platformio")
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

	// Preheat: pull images + prime shared caches concurrently before the DAG,
	// so jobs don't stall on cold pulls / empty toolchain volumes. Advisory —
	// failures warn but never block the build. Skipped with --no-preheat.
	if !noPreheat && len(w.preheat) > 0 {
		runPreheats(ctx, w.preheat, engine, wd, []string(*mounts))
		fmt.Println()
	}

	// Optional live TUI dashboard: suppress streaming logs and paint per-job
	// status instead. Build the job order (respecting --job/--changed subset).
	var ui *tui.Model
	var observer func(scheduler.Event)
	if useTUI {
		var order []string
		for _, id := range dag.TopoOrder() {
			if only == nil || only[id] {
				order = append(order, id)
			}
		}
		ui = tui.New(plan.Name, order)
		observer = ui.Observer()
		logs.SetQuiet(true)
		ui.Start()
	}

	res := scheduler.Run(ctx, plan, scheduler.Options{
		Workdir:  wd,
		NoCache:  noCache,
		Only:     only,
		Engine:   engine,
		Mounts:   []string(*mounts),
		Observer: observer,
		Resume:   resume,
	})

	if ui != nil {
		ui.Stop()
		logs.SetQuiet(false)
	}

	fmt.Println()
	if res.Failed {
		logs.Failure("✗ %s failed in %s  (%d ran, %d cached, %d resumed)", plan.Name, res.Duration.Round(1e6), res.Ran, res.Cached, res.Resumed)
		os.Exit(1)
	}
	logs.Success("✓ %s passed in %s  (%d ran, %d cached, %d resumed)", plan.Name, res.Duration.Round(1e6), res.Ran, res.Cached, res.Resumed)
}

// runPreheats warms images + caches concurrently. Advisory: logs failures but
// never affects exit status.
func runPreheats(ctx context.Context, specs []Preheat, engine, workdir string, mounts []string) {
	logs.Header("Preheating %d image(s)/cache(s)…", len(specs))
	var wg sync.WaitGroup
	for _, p := range specs {
		wg.Add(1)
		go func(p Preheat) {
			defer wg.Done()
			out := logs.Prefixed("preheat")
			err := runner.Preheat(ctx, runner.PreheatSpec{
				Image:   p.Image,
				Warm:    p.Warm,
				Mounts:  append(append([]string(nil), mounts...), p.Mounts...),
				Engine:  engine,
				Workdir: workdir,
			}, out)
			if err != nil {
				logs.Failure("preheat %s: %v (advisory — continuing)", p.Image, err)
			} else {
				logs.Success("✓ preheated %s", p.Image)
			}
		}(p)
	}
	wg.Wait()
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
func (o *optString) Set(s string) error { o.set = true; o.val = s; return nil }
func (o *optString) IsBoolFlag() bool   { return true }

// sliceFlag collects repeatable string flags.
type sliceFlag []string

func (s *sliceFlag) String() string { return strings.Join(*s, ",") }
func (s *sliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}
