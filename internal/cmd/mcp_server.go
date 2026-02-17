package cmd

import (
	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/mcpserver"
	"github.com/steveyegge/gastown/internal/workspace"
)

var mcpServerCmd = &cobra.Command{
	Use:     "mcp-server",
	GroupID: GroupServices,
	Short:   "Run the MCP (Model Context Protocol) server",
	Long: `Run a JSON-RPC MCP server over stdio.

This exposes gastown's CLI surface as MCP tools that can be called by
AI agents (e.g., Claude's Companion) via the standard MCP protocol.

Configure in .mcp.json:
  {"mcpServers": {"gastown": {"command": "gt", "args": ["mcp-server"]}}}

The server speaks newline-delimited JSON-RPC 2.0 over stdin/stdout.`,
	RunE: runMCPServer,
}

func init() {
	rootCmd.AddCommand(mcpServerCmd)
}

func runMCPServer(_ *cobra.Command, _ []string) error {
	// Auto-detect town root from cwd (best-effort).
	townRoot, _ := workspace.FindFromCwd()

	srv := mcpserver.NewServer(townRoot)
	return srv.Run()
}
