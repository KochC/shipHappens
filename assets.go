// Package shiphappens is the module root. It exists to embed the authoring
// assets (the Pkl schema, reusable templates, and the DX guide) so tools like
// the MCP server can serve them to agents regardless of where the binary runs.
package shiphappens

import _ "embed"

// SchemaPkl is the Pkl schema (pkl/ship.pkl) — the authoritative field
// reference for authoring a pipeline.
//
//go:embed pkl/ship.pkl
var SchemaPkl string

// TemplatesPkl is the reusable job/step template library (pkl/templates.pkl).
//
//go:embed pkl/templates.pkl
var TemplatesPkl string

// DXGuide is the developer-experience guide (docs/DX.md).
//
//go:embed docs/DX.md
var DXGuide string
