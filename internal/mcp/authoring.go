package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	assets "github.com/chris/shiphappens"
)

// ── MCP resources: authoring docs served to agents ───────────────────────────

type resource struct {
	uri, name, desc, mime string
	body                  func() string
}

// resources are the authoring assets an agent can read to learn the schema.
func resources() []resource {
	return []resource{
		{
			uri: "shiphappens://schema", name: "Ship Happens Pkl schema",
			desc: "pkl/ship.pkl — the authoritative field reference for authoring a pipeline.",
			mime: "text/x-pkl", body: func() string { return assets.SchemaPkl },
		},
		{
			uri: "shiphappens://templates", name: "Reusable Pkl templates",
			desc: "pkl/templates.pkl — pre-built job/step templates (goTest, goBuild, …).",
			mime: "text/x-pkl", body: func() string { return assets.TemplatesPkl },
		},
		{
			uri: "shiphappens://dx", name: "DX guide",
			desc: "docs/DX.md — the complete authoring & feature guide.",
			mime: "text/markdown", body: func() string { return assets.DXGuide },
		},
		{
			uri: "shiphappens://quickref", name: "Authoring quickref",
			desc: "A compact cheat sheet: schema fields + a minimal example.",
			mime: "text/markdown", body: func() string { return authoringQuickref },
		},
	}
}

func resourceList() []map[string]any {
	rs := resources()
	out := make([]map[string]any, 0, len(rs))
	for _, r := range rs {
		out = append(out, map[string]any{
			"uri": r.uri, "name": r.name, "description": r.desc, "mimeType": r.mime,
		})
	}
	return out
}

func (s *Server) readResource(req *rpcRequest, reply func(any) *rpcResponse, fail func(int, string) *rpcResponse) *rpcResponse {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return fail(-32602, "invalid params")
	}
	for _, r := range resources() {
		if r.uri == p.URI {
			return reply(map[string]any{"contents": []map[string]any{{
				"uri": r.uri, "mimeType": r.mime, "text": r.body(),
			}}})
		}
	}
	return fail(-32602, "unknown resource: "+p.URI)
}

// ── authoring helpers ────────────────────────────────────────────────────────

// SchemaPackage is the published Pkl package import scaffolded pipelines use, so
// they resolve from any repo without vendoring the schema. Kept in step with
// pkl/PklProject's version.
const SchemaPackage = "package://github.com/KochC/shipHappens/pkl/shiphappens@1.0.0#/ship.pkl"

// vendorDir is the directory ship_scaffold writes the schema into, relative to
// the scaffolded pipeline. A local amends keeps a scaffolded pipeline fully
// self-contained (no network, no auth) — the right default while the schema
// package is not publicly downloadable.
const vendorDir = ".ship"

// localAmends is the import a scaffolded pipeline uses: the vendored schema
// beside it. (When the repo/package is public, `amends "SchemaPackage"` is the
// zero-vendoring alternative — see ship_docs.)
const localAmends = vendorDir + "/ship.pkl"

// authoringQuickref is a compact, self-contained cheat sheet returned by
// ship_docs (topic=quickref). It gives an agent enough to author a correct
// pipeline without reading the full schema, and points at the deeper topics.
const authoringQuickref = `# Ship Happens — Pkl authoring quickref

A pipeline is a Pkl module that amends the Ship Happens schema and declares a
name and a map of jobs. Jobs form a DAG via ` + "`needs`" + `.

## Minimal pipeline
` + "```pkl" + `
amends "` + localAmends + `"   // vendored schema (ship_scaffold writes it for you)

name = "CI"

jobs {
  ["build"] {
    steps { new { id = "compile"; run = "go build ./..." } }
  }
  ["test"] {
    needs { "build" }
    steps { new { id = "unit"; run = "go test ./..."; retries = 2 } }
  }
}
` + "```" + `

> Use ship_scaffold to generate this plus the vendored ` + "`" + vendorDir + "/ship.pkl`" + ` so it
> validates immediately. (If the schema is published as a public Pkl package you
> can instead ` + "`amends \"" + SchemaPackage + "\"`" + ` with no vendoring.)

## Workflow fields
- name: String (required)          | vars: Mapping<String,String>
- toolchain: Mapping<String,String> (mise-backed native tool pins)
- security: { offlineByDefault: Boolean, defaultAllow: Listing<String> }
- notify: { desktop, webhook, exec, onStart, onJob }
- jobs: Mapping<String, Job> (required)

## Job fields
- needs: Listing<String>           | image: String (run in a container)
- env: Mapping<String,String>      | secrets: Listing<{name, fromEnv}>
- steps: Listing<Step> (required)  | outputs: Listing<String> (for --resume)
- toolchain, cleanAfter, network, allow (egress list, enforced), overlay
- timeoutSec, continueOnError
- services: Listing<{name,image,env,ports,health,timeout}>
- matrix: Mapping<String, Listing<String>>  (fan-out; e.g. os×go → N jobs,
  values injected as $OS/$GO, dependents rewired to all expansions)

## Step fields
- id: String (required)            | run: String (required, shell)
- cache: { inputs: Listing<String>, outputs: Listing<String> }
- env, workingDir, shell (sh|bash|python|node|…)
- timeoutSec, retries, retryBackoffSec, continueOnError
- needs (step sub-graph within a job), onFailure: Listing<Step>

## Run it
    ship run pipeline.pkl            # compile → validate → run (live TUI with --tui)
    ship validate pipeline.pkl       # compile + DAG check, no execution
    ship run pipeline.pkl --resume   # skip unchanged jobs, restore outputs

For the full schema call ship_docs topic=schema; for reusable templates
topic=templates; for the complete guide topic=dx. Validate anything you write
with the ship_validate tool.
`

// starterTemplate is the pipeline.pkl ship_scaffold writes. %[1]s is the local
// amends path (vendored schema); %[2]s is the workflow name.
const starterTemplate = `/// Ship Happens pipeline. Author, then:
///
///     ship run pipeline.pkl            # compile → validate → run
///     ship run pipeline.pkl --tui      # live dashboard
///     ship validate pipeline.pkl       # check without running
///
/// The schema is vendored beside this file (%[1]s). Schema reference: call the
/// MCP ship_docs tool (topic=schema|dx), or see https://github.com/KochC/shipHappens.
amends "%[1]s"

name = "%[2]s"

vars {
  ["GREETING"] = "hello"
}

jobs {
  ["build"] {
    steps {
      new {
        id = "compile"
        run = "echo \"$GREETING from build\""
        cache { inputs { "**/*" } }
      }
    }
    outputs { "bin/**" }
  }

  ["test"] {
    needs { "build" }
    steps {
      new { id = "unit"; run = "echo running tests"; retries = 2 }
    }
  }
}
`

// scaffold writes a starter pipeline.pkl into dir (default cwd) together with a
// vendored copy of the schema (and templates) in dir/.ship, so the pipeline
// validates immediately with no network or auth. Refuses to clobber the
// pipeline unless force.
func scaffold(dir, name string, force bool) (any, error) {
	if name == "" {
		name = "CI"
	}
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create dir: %w", err)
	}
	path := filepath.Join(dir, "pipeline.pkl")
	if _, err := os.Stat(path); err == nil && !force {
		return nil, fmt.Errorf("%s already exists (pass force=true to overwrite)", path)
	}

	// Vendor the schema + templates next to the pipeline so `amends` resolves
	// locally. These are embedded in the binary, so this works from any repo.
	vdir := filepath.Join(dir, vendorDir)
	if err := os.MkdirAll(vdir, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", vdir, err)
	}
	if err := os.WriteFile(filepath.Join(vdir, "ship.pkl"), []byte(assets.SchemaPkl), 0o644); err != nil {
		return nil, fmt.Errorf("vendor schema: %w", err)
	}
	if err := os.WriteFile(filepath.Join(vdir, "templates.pkl"), []byte(assets.TemplatesPkl), 0o644); err != nil {
		return nil, fmt.Errorf("vendor templates: %w", err)
	}

	content := fmt.Sprintf(starterTemplate, localAmends, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", path, err)
	}
	return toolJSON(map[string]any{
		"written":  path,
		"vendored": []string{filepath.Join(vdir, "ship.pkl"), filepath.Join(vdir, "templates.pkl")},
		"name":     name,
		"next":     fmt.Sprintf("edit %s, then validate with ship_validate {file: %q}", path, path),
		"content":  content,
	}), nil
}
