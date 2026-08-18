// Command ship-mcp is a Model Context Protocol server for Ship Happens over
// stdio, so agents and IDEs can validate, inspect, run, and monitor pipelines.
//
// Register it with an MCP client (e.g. in a client config):
//
//	{ "command": "ship-mcp" }
//
// Tools exposed: ship_validate, ship_graph, ship_run (async), ship_status
// (poll — never re-triggers work), ship_cancel, ship_runs, ship_cache_du.
package main

import (
	"fmt"
	"os"

	"github.com/chris/shiphappens/internal/mcp"
)

// version is set at build time via -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-v", "--version", "version":
			fmt.Printf("ship-mcp %s\n", version)
			return
		case "-h", "--help", "help":
			fmt.Fprintln(os.Stderr, "ship-mcp — MCP server for Ship Happens (speaks JSON-RPC over stdio)")
			return
		}
	}
	mcp.Version = version
	srv := mcp.NewServer()
	if err := srv.Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "ship-mcp: %v\n", err)
		os.Exit(1)
	}
}
