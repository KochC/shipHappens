package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitializeAdvertisesResources(t *testing.T) {
	s := NewServer()
	r := call(t, s, "initialize", map[string]any{})
	caps := r.Result.(map[string]any)["capabilities"].(map[string]any)
	if _, ok := caps["resources"]; !ok {
		t.Error("initialize should advertise a resources capability")
	}
}

func TestResourcesList(t *testing.T) {
	s := NewServer()
	r := call(t, s, "resources/list", map[string]any{})
	res := r.Result.(map[string]any)["resources"].([]any)
	got := map[string]bool{}
	for _, x := range res {
		got[x.(map[string]any)["uri"].(string)] = true
	}
	for _, want := range []string{"shiphappens://schema", "shiphappens://templates", "shiphappens://dx", "shiphappens://quickref"} {
		if !got[want] {
			t.Errorf("missing resource %s (got %v)", want, got)
		}
	}
}

func TestResourcesReadEach(t *testing.T) {
	s := NewServer()
	for _, uri := range []string{"shiphappens://schema", "shiphappens://templates", "shiphappens://dx", "shiphappens://quickref"} {
		r := call(t, s, "resources/read", map[string]any{"uri": uri})
		contents := r.Result.(map[string]any)["contents"].([]any)
		if len(contents) != 1 || contents[0].(map[string]any)["text"].(string) == "" {
			t.Errorf("resource %s returned empty body", uri)
		}
	}
	// schema must contain the Job class.
	schema := readResourceText(t, s, "shiphappens://schema")
	if !strings.Contains(schema, "class Job") {
		t.Error("schema resource should be the Pkl schema")
	}
}

func readResourceText(t *testing.T, s *Server, uri string) string {
	t.Helper()
	r := call(t, s, "resources/read", map[string]any{"uri": uri})
	contents := r.Result.(map[string]any)["contents"].([]any)
	return contents[0].(map[string]any)["text"].(string)
}

func TestResourcesReadUnknown(t *testing.T) {
	s := NewServer()
	r := call(t, s, "resources/read", map[string]any{"uri": "shiphappens://nope"})
	if r.Error == nil {
		t.Error("unknown resource should error")
	}
}

func TestResourcesReadBadParams(t *testing.T) {
	s := NewServer()
	// params that don't unmarshal into {uri}
	resp := s.handle(&rpcRequest{JSONRPC: "2.0", ID: rawID(), Method: "resources/read", Params: []byte(`123`)})
	if resp.Error == nil {
		t.Error("bad params should error")
	}
}

func rawID() []byte { return []byte(`1`) }

func TestShipDocsTopics(t *testing.T) {
	s := NewServer()
	cases := map[string]string{
		"":          "authoring quickref",
		"quickref":  "authoring quickref",
		"schema":    "class Job",
		"templates": "", // just non-empty
		"dx":        "",
	}
	for topic, want := range cases {
		r := call(t, s, "tools/call", map[string]any{"name": "ship_docs", "arguments": map[string]any{"topic": topic}})
		txt := toolResultText(t, r)
		if txt == "" {
			t.Errorf("ship_docs topic=%q empty", topic)
		}
		if want != "" && !strings.Contains(strings.ToLower(txt), strings.ToLower(want)) {
			t.Errorf("ship_docs topic=%q missing %q", topic, want)
		}
	}
}

func TestShipScaffoldWritesValidStarter(t *testing.T) {
	s := NewServer()
	dir := t.TempDir()
	r := call(t, s, "tools/call", map[string]any{
		"name": "ship_scaffold", "arguments": map[string]any{"dir": dir, "name": "MyApp"},
	})
	txt := toolResultText(t, r)
	if !strings.Contains(txt, "pipeline.pkl") {
		t.Fatalf("scaffold result: %s", txt)
	}
	path := filepath.Join(dir, "pipeline.pkl")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("scaffold did not write file: %v", err)
	}
	content := string(body)
	if !strings.Contains(content, `name = "MyApp"`) {
		t.Error("workflow name not applied")
	}
	// Default is a LOCAL vendored amends (no unreachable package URL).
	if !strings.Contains(content, `amends "`+localAmends+`"`) {
		t.Errorf("scaffold should amend the vendored schema, got:\n%s", content)
	}
	if strings.Contains(content, "package://") {
		t.Error("scaffold should not reference the (private) Pkl package by default")
	}
	// The schema + templates must be vendored beside the pipeline.
	for _, f := range []string{".ship/ship.pkl", ".ship/templates.pkl"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("expected vendored %s: %v", f, err)
		}
	}
}

func TestShipScaffoldDefaults(t *testing.T) {
	s := NewServer()
	dir := t.TempDir()
	// no name → defaults to CI; dir provided.
	call(t, s, "tools/call", map[string]any{"name": "ship_scaffold", "arguments": map[string]any{"dir": dir}})
	body, _ := os.ReadFile(filepath.Join(dir, "pipeline.pkl"))
	if !strings.Contains(string(body), `name = "CI"`) {
		t.Error("default name should be CI")
	}
}

func TestShipScaffoldNoClobber(t *testing.T) {
	s := NewServer()
	dir := t.TempDir()
	args := map[string]any{"name": "ship_scaffold", "arguments": map[string]any{"dir": dir}}
	call(t, s, "tools/call", args)
	// Second call without force must error.
	r := call(t, s, "tools/call", args)
	if !strings.Contains(strings.ToLower(toolResultText(t, r)), "already exists") {
		t.Errorf("expected already-exists error, got: %s", toolResultText(t, r))
	}
	// With force it overwrites.
	r = call(t, s, "tools/call", map[string]any{"name": "ship_scaffold", "arguments": map[string]any{"dir": dir, "force": true}})
	if strings.Contains(strings.ToLower(toolResultText(t, r)), "already exists") {
		t.Error("force=true should overwrite")
	}
}

func TestShipScaffoldErrors(t *testing.T) {
	s := NewServer()

	// dir path is actually a file → MkdirAll fails.
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "afile")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := call(t, s, "tools/call", map[string]any{
		"name": "ship_scaffold", "arguments": map[string]any{"dir": filePath},
	})
	if !strings.Contains(strings.ToLower(toolResultText(t, r)), "create dir") {
		t.Errorf("expected create-dir error, got: %s", toolResultText(t, r))
	}

	// pipeline.pkl exists as a directory → WriteFile(pipeline) fails (force skips
	// the clobber guard).
	tmp2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp2, "pipeline.pkl"), 0o755); err != nil {
		t.Fatal(err)
	}
	r = call(t, s, "tools/call", map[string]any{
		"name": "ship_scaffold", "arguments": map[string]any{"dir": tmp2, "force": true},
	})
	if !strings.Contains(strings.ToLower(toolResultText(t, r)), "write") {
		t.Errorf("expected write error, got: %s", toolResultText(t, r))
	}
}

func TestShipDocsAndScaffoldInToolsList(t *testing.T) {
	s := NewServer()
	r := call(t, s, "tools/list", nil)
	tools := r.Result.(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"ship_docs", "ship_scaffold"} {
		if !names[want] {
			t.Errorf("tool %s not listed", want)
		}
	}
}
