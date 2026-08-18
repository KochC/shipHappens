package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/chris/shiphappens/internal/planfile"
	"github.com/chris/shiphappens/internal/validator"
)

// protocolVersion is the MCP revision this server implements.
const protocolVersion = "2024-11-05"

// rpcRequest / rpcResponse are minimal JSON-RPC 2.0 envelopes.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Server is a stdio MCP server for Ship Happens.
type Server struct {
	mgr  *Manager
	tools []toolDef
}

// NewServer builds a server with the standard tool set.
func NewServer() *Server {
	s := &Server{mgr: NewManager()}
	s.tools = s.buildTools()
	return s
}

// Serve reads JSON-RPC requests (one JSON object per line) from in and writes
// responses to out. It returns when in is exhausted.
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = enc.Encode(rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		resp := s.handle(&req)
		// Notifications (no id) get no response.
		if resp == nil {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *Server) handle(req *rpcRequest) *rpcResponse {
	reply := func(result any) *rpcResponse {
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	}
	fail := func(code int, msg string) *rpcResponse {
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: code, Message: msg}}
	}

	switch req.Method {
	case "initialize":
		return reply(map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "shiphappens", "version": Version},
		})
	case "notifications/initialized", "initialized":
		return nil // notification
	case "ping":
		return reply(map[string]any{})
	case "tools/list":
		return reply(map[string]any{"tools": s.toolSchemas()})
	case "tools/call":
		return s.callTool(req, reply, fail)
	default:
		if len(req.ID) == 0 {
			return nil // unknown notification
		}
		return fail(-32601, "method not found: "+req.Method)
	}
}

// Version is stamped by the CLI; default keeps tests deterministic.
var Version = "dev"

// ── tools ────────────────────────────────────────────────────────────────────

type toolDef struct {
	name    string
	desc    string
	schema  map[string]any
	handler func(args map[string]any) (any, error)
}

func (s *Server) toolSchemas() []map[string]any {
	out := make([]map[string]any, 0, len(s.tools))
	for _, t := range s.tools {
		out = append(out, map[string]any{
			"name":        t.name,
			"description": t.desc,
			"inputSchema": t.schema,
		})
	}
	return out
}

func (s *Server) callTool(req *rpcRequest, reply func(any) *rpcResponse, fail func(int, string) *rpcResponse) *rpcResponse {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return fail(-32602, "invalid params")
	}
	for _, t := range s.tools {
		if t.name == p.Name {
			res, err := t.handler(p.Arguments)
			if err != nil {
				return reply(toolText(fmt.Sprintf("error: %v", err), true))
			}
			return reply(res)
		}
	}
	return fail(-32602, "unknown tool: "+p.Name)
}

// toolText wraps a string as an MCP tool result (content array).
func toolText(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

// toolJSON wraps a value as pretty-printed JSON text content.
func toolJSON(v any) map[string]any {
	b, _ := json.MarshalIndent(v, "", "  ")
	return toolText(string(b), false)
}

func strArg(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func (s *Server) buildTools() []toolDef {
	fileSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file": map[string]any{"type": "string", "description": "path to a .pkl or .json pipeline"},
		},
		"required": []string{"file"},
	}

	return []toolDef{
		{
			name: "ship_validate",
			desc: "Compile and validate a Ship Happens pipeline (no execution). Returns diagnostics.",
			schema: fileSchema,
			handler: func(args map[string]any) (any, error) {
				file := strArg(args, "file")
				plan, err := planfile.Load(file)
				if err != nil {
					return toolText("invalid: "+err.Error(), false), nil
				}
				diags := validator.Validate(plan, nil)
				if len(diags) == 0 {
					return toolJSON(map[string]any{"valid": true, "name": plan.Name, "jobs": len(plan.Jobs)}), nil
				}
				msgs := make([]string, len(diags))
				for i, d := range diags {
					msgs[i] = d.String()
				}
				return toolJSON(map[string]any{"valid": false, "diagnostics": msgs}), nil
			},
		},
		{
			name: "ship_graph",
			desc: "Return the pipeline's job dependency graph (ids and needs).",
			schema: fileSchema,
			handler: func(args map[string]any) (any, error) {
				plan, err := planfile.Load(strArg(args, "file"))
				if err != nil {
					return nil, err
				}
				jobs := make([]map[string]any, 0, len(plan.Jobs))
				for _, j := range plan.Jobs {
					jobs = append(jobs, map[string]any{"id": j.ID, "needs": j.Needs})
				}
				return toolJSON(map[string]any{"name": plan.Name, "jobs": jobs}), nil
			},
		},
		{
			name: "ship_run",
			desc: "Start a pipeline run in the BACKGROUND and return a runId immediately. Poll with ship_status — polling never re-triggers work (no cold caches).",
			schema: fileSchema,
			handler: func(args map[string]any) (any, error) {
				file := strArg(args, "file")
				plan, err := planfile.Load(file)
				if err != nil {
					return nil, err
				}
				ids := make([]string, 0, len(plan.Jobs))
				for _, j := range plan.Jobs {
					ids = append(ids, j.ID)
				}
				run := s.mgr.Start(file, ids)
				return toolJSON(map[string]any{"runId": run.ID, "state": "running", "jobs": len(ids)}), nil
			},
		},
		{
			name: "ship_status",
			desc: "Get the current status of a background run (job/step progress, summary). Read-only — never re-runs anything.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"runId": map[string]any{"type": "string"},
				},
				"required": []string{"runId"},
			},
			handler: func(args map[string]any) (any, error) {
				run, ok := s.mgr.Get(strArg(args, "runId"))
				if !ok {
					return nil, fmt.Errorf("unknown runId")
				}
				return toolJSON(run.snapshot()), nil
			},
		},
		{
			name: "ship_cancel",
			desc: "Cancel a running background run.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{"runId": map[string]any{"type": "string"}},
				"required":   []string{"runId"},
			},
			handler: func(args map[string]any) (any, error) {
				if s.mgr.Cancel(strArg(args, "runId")) {
					return toolText("canceled", false), nil
				}
				return nil, fmt.Errorf("unknown runId")
			},
		},
		{
			name:   "ship_runs",
			desc:   "List all runs started this session.",
			schema: map[string]any{"type": "object", "properties": map[string]any{}},
			handler: func(map[string]any) (any, error) {
				return toolJSON(map[string]any{"runs": s.mgr.List()}), nil
			},
		},
		{
			name:   "ship_cache_du",
			desc:   "Report cache disk usage (via the ship CLI).",
			schema: map[string]any{"type": "object", "properties": map[string]any{}},
			handler: func(map[string]any) (any, error) {
				out, err := shipCLI("cache", "du")
				if err != nil {
					return nil, err
				}
				return toolText(out, false), nil
			},
		},
	}
}

// shipCLI runs the ship binary if it is on PATH (best-effort helper for cache
// inspection). Overridable in tests.
var shipCLI = func(args ...string) (string, error) {
	bin, err := exec.LookPath("ship")
	if err != nil {
		return "", fmt.Errorf("ship CLI not found on PATH")
	}
	out, err := exec.Command(bin, args...).CombinedOutput()
	return string(out), err
}
