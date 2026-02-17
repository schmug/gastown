package mcpserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

const (
	mcpProtocolVersion = "2024-11-05"
	serverName         = "gastown"
	serverVersion      = "0.1.0"
)

// Server is an MCP server that reads JSON-RPC from stdin and writes to stdout.
type Server struct {
	townRoot string
	tools    map[string]ToolHandler
	reader   *bufio.Reader
	writer   io.Writer
}

// ToolHandler is a function that handles a tool call.
type ToolHandler func(args json.RawMessage) *ToolCallResult

// NewServer creates a new MCP server.
// If townRoot is empty, it will be auto-detected from cwd.
func NewServer(townRoot string) *Server {
	s := &Server{
		townRoot: townRoot,
		tools:    make(map[string]ToolHandler),
		reader:   bufio.NewReader(os.Stdin),
		writer:   os.Stdout,
	}
	s.registerTools()
	return s
}

// Run starts the MCP stdio loop. It blocks until stdin closes or an error occurs.
func (s *Server) Run() error {
	for {
		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("reading stdin: %w", err)
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			s.sendError(nil, -32700, "parse error: "+err.Error())
			continue
		}

		s.handleRequest(&req)
	}
}

func (s *Server) handleRequest(req *Request) {
	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "initialized":
		// Client acknowledgment - nothing to do
	case "ping":
		s.sendResult(req.ID, map[string]any{})
	case "tools/list":
		s.handleToolsList(req)
	case "tools/call":
		s.handleToolsCall(req)
	default:
		// Unknown method. If it has an ID, it's a request that needs an error.
		// If no ID, it's a notification we can silently ignore.
		if req.ID != nil {
			s.sendError(req.ID, -32601, "method not found: "+req.Method)
		}
	}
}

func (s *Server) handleInitialize(req *Request) {
	result := InitializeResult{
		ProtocolVersion: mcpProtocolVersion,
		Capabilities: ServerCapability{
			Tools: &ToolsCapability{},
		},
		ServerInfo: ServerInfo{
			Name:    serverName,
			Version: serverVersion,
		},
	}
	s.sendResult(req.ID, result)
}

func (s *Server) handleToolsList(req *Request) {
	result := ToolsListResult{
		Tools: s.toolDefs(),
	}
	s.sendResult(req.ID, result)
}

func (s *Server) handleToolsCall(req *Request) {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendError(req.ID, -32602, "invalid params: "+err.Error())
		return
	}

	handler, ok := s.tools[params.Name]
	if !ok {
		s.sendResult(req.ID, errorResult("unknown tool: "+params.Name))
		return
	}

	result := handler(params.Arguments)
	s.sendResult(req.ID, result)
}

func (s *Server) sendResult(id json.RawMessage, result any) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	s.send(resp)
}

func (s *Server) sendError(id json.RawMessage, code int, message string) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
	s.send(resp)
}

func (s *Server) send(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcpserver: marshal error: %v\n", err)
		return
	}
	data = append(data, '\n')
	_, _ = s.writer.Write(data)
}
