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
	// RunPlan always marshals cleanly, so the error is intentionally not checked.
	b, _ := json.MarshalIndent(p, "", "  ")
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
// runOpts holds resolved run configuration (parsed from flags).
type runOpts struct {
	graphOnly   bool
	jobFlag     string
	noCache     bool
	compileOnly string
	engine      string
	noPreheat   bool
	useTUI      bool
	noTUI       bool
	resume      bool
	changedSet  bool
	changedRef  string
	mounts      []string
	vars        map[string]string
}

// getwd is the injection point for the working directory (overridable in tests).
var getwd = os.Getwd

// parseFlags parses argv into runOpts.
func parseFlags(name string, argv []string) runOpts {
	var o runOpts
	changedVal := &optString{}
	mounts := &sliceFlag{}
	vars := &sliceFlag{}
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fs.BoolVar(&o.graphOnly, "graph", false, "print the execution graph and exit")
	fs.StringVar(&o.jobFlag, "job", "", "run only this job (and its dependencies)")
	fs.BoolVar(&o.noCache, "no-cache", false, "disable step caching")
	fs.StringVar(&o.compileOnly, "compile", "", "write the compiled plan as JSON to the given path and exit")
	fs.StringVar(&o.engine, "engine", "docker", "container engine for image jobs (docker|podman|apple)")
	fs.BoolVar(&o.noPreheat, "no-preheat", false, "skip image/cache preheating before the run")
	fs.BoolVar(&o.useTUI, "tui", false, "render a live status dashboard instead of streaming logs")
	fs.BoolVar(&o.noTUI, "no-tui", false, "force streaming logs even if the program defaults to the TUI")
	fs.BoolVar(&o.resume, "resume", false, "skip jobs whose fingerprint matches a prior successful run (incremental)")
	fs.Var(mounts, "mount", "extra container volume spec for image jobs (repeatable), e.g. vol:/root/.platformio")
	fs.Var(vars, "var", "set/override a workflow variable (repeatable), e.g. --var REGION=eu")
	fs.Var(changedVal, "changed", "run only jobs affected by git changes vs ref (default main)")
	fs.Parse(argv)
	o.changedSet = changedVal.set
	o.changedRef = changedVal.val
	o.mounts = []string(*mounts)
	o.vars = parseKV([]string(*vars))
	return o
}

// parseKV parses "K=V" pairs into a map (last value wins; malformed entries
// without '=' are ignored).
func parseKV(pairs []string) map[string]string {
	if len(pairs) == 0 {
		return nil
	}
	m := map[string]string{}
	for _, p := range pairs {
		if i := indexByte(p, '='); i > 0 {
			m[p[:i]] = p[i+1:]
		}
	}
	return m
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// osExit is the process-exit indirection (overridable in tests).
var osExit = os.Exit

// Main is the entry point for a pipeline program. It parses CLI flags,
// compiles+validates the workflow, and runs (or prints) it, exiting the process
// with an appropriate status code.
func Main(w *Workflow) {
	osExit(run(w, parseFlags(w.Name, os.Args[1:])))
}

// RunWithTUI is like Main but defaults the live TUI dashboard on (as if --tui
// were passed). Users can still force streaming logs with --no-tui. Other CLI
// flags are honored. Handy for demos and programs that want the dashboard.
func RunWithTUI(w *Workflow) {
	o := parseFlags(w.Name, os.Args[1:])
	o.useTUI = !o.noTUI
	osExit(run(w, o))
}

// RunWithTUIResume defaults both the live TUI and resume/incremental mode on
// (as if --tui --resume). --no-tui still disables the dashboard. Other CLI
// flags are honored.
func RunWithTUIResume(w *Workflow) {
	o := parseFlags(w.Name, os.Args[1:])
	o.useTUI = !o.noTUI
	o.resume = true
	osExit(run(w, o))
}

// run executes the pipeline per opts and returns the process exit code. It never
// calls os.Exit, so it is fully testable.
func run(w *Workflow, o runOpts) int {
	raw := w.ToPlan()
	// CLI --var overrides merge into (and override) workflow vars.
	if len(o.vars) > 0 {
		if raw.Vars == nil {
			raw.Vars = map[string]string{}
		}
		for k, v := range o.vars {
			raw.Vars[k] = v
		}
	}
	plan, err := compile(raw, w.Lines())
	if err != nil {
		logs.Failure("%s", err.Error())
		return 1
	}
	return runCompiled(plan, o)
}

// runCompiled executes an already-validated plan. Shared by the Go DSL
// front-end (run) and file-based front-ends (Pkl/JSON via cmd/ship).
func runCompiled(plan *compiler.RunPlan, o runOpts) int {
	// CLI --var overrides (for file-loaded plans, applied here too).
	if len(o.vars) > 0 {
		if plan.Vars == nil {
			plan.Vars = map[string]string{}
		}
		for k, v := range o.vars {
			plan.Vars[k] = v
		}
	}

	logs.Success("✓ compiled: %s — %d jobs, %d steps, DAG valid", plan.Name, len(plan.Jobs), stepCount(plan))
	fmt.Println()

	dag := graph.Build(plan)

	if o.compileOnly != "" {
		if err := writePlan(plan, o.compileOnly); err != nil {
			logs.Failure("write plan: %v", err)
			return 1
		}
		logs.Success("✓ wrote compiled plan → %s", o.compileOnly)
		return 0
	}

	if o.graphOnly {
		printGraph(plan, dag)
		return 0
	}

	// Determine job subset.
	var only map[string]bool
	if o.jobFlag != "" {
		if plan.Job(o.jobFlag) == nil {
			logs.Failure("unknown job %q", o.jobFlag)
			return 1
		}
		only = dag.Subgraph(o.jobFlag)
	} else if o.changedSet {
		base := o.changedRef
		if base == "" || base == "true" {
			base = "main"
		}
		wd, _ := getwd()
		files, ferr := changed.Files(wd, base)
		if ferr != nil {
			logs.Failure("git diff failed: %v", ferr)
			return 1
		}
		if len(files) == 0 {
			logs.Info("no changes detected vs %s — nothing to run", base)
			return 0
		}
		only = changed.AffectedJobs(plan, dag, files)
		logs.Info("changed vs %s: %d file(s), %d job(s) affected\n", base, len(files), len(only))
	}

	printGraph(plan, dag)
	fmt.Println()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	wd, _ := getwd()

	// Preheat: pull images + prime shared caches concurrently before the DAG.
	if !o.noPreheat && len(plan.Preheat) > 0 {
		runPreheats(ctx, plan.Preheat, o.engine, wd, o.mounts)
		fmt.Println()
	}

	// Optional live TUI dashboard.
	var ui *tui.Model
	var observer func(scheduler.Event)
	if o.useTUI {
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
		NoCache:  o.noCache,
		Only:     only,
		Engine:   o.engine,
		Mounts:   o.mounts,
		Observer: observer,
		Resume:   o.resume,
	})

	if ui != nil {
		ui.Stop()
		logs.SetQuiet(false)
	}

	fmt.Println()
	if res.Failed {
		logs.Failure("✗ %s failed in %s  (%d ran, %d cached, %d resumed)", plan.Name, res.Duration.Round(1e6), res.Ran, res.Cached, res.Resumed)
		return 1
	}
	logs.Success("✓ %s passed in %s  (%d ran, %d cached, %d resumed)", plan.Name, res.Duration.Round(1e6), res.Ran, res.Cached, res.Resumed)
	return 0
}

// preheatFn is the indirection point for warming (overridable in tests).
var preheatFn = runner.Preheat

// runPreheats warms images + caches concurrently. Advisory: logs failures but
// never affects exit status.
func runPreheats(ctx context.Context, specs []compiler.PreheatSpec, engine, workdir string, mounts []string) {
	logs.Header("Preheating %d image(s)/cache(s)…", len(specs))
	var wg sync.WaitGroup
	for _, p := range specs {
		wg.Add(1)
		go func(p compiler.PreheatSpec) {
			defer wg.Done()
			out := logs.Prefixed("preheat")
			err := preheatFn(ctx, runner.PreheatSpec{
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
