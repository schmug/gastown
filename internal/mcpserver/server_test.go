package mcpserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestInitialize(t *testing.T) {
	var out bytes.Buffer
	s := &Server{
		tools:  make(map[string]ToolHandler),
		reader: bufio.NewReader(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}` + "\n")),
		writer: &out,
	}
	s.registerTools()

	_ = s.Run()

	var resp Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("result is not a map")
	}
	if result["protocolVersion"] != mcpProtocolVersion {
		t.Errorf("protocolVersion = %v, want %v", result["protocolVersion"], mcpProtocolVersion)
	}

	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatal("serverInfo is not a map")
	}
	if serverInfo["name"] != "gastown" {
		t.Errorf("serverInfo.name = %v, want gastown", serverInfo["name"])
	}
}

func TestToolsList(t *testing.T) {
	var out bytes.Buffer
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n"
	s := &Server{
		tools:  make(map[string]ToolHandler),
		reader: bufio.NewReader(strings.NewReader(input)),
		writer: &out,
	}
	s.registerTools()

	_ = s.Run()

	var resp Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	// Verify tools are present.
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("result is not a map")
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatal("tools is not a list")
	}
	if len(tools) < 10 {
		t.Errorf("expected at least 10 tools, got %d", len(tools))
	}

	// Check that status tool is present.
	found := false
	for _, tool := range tools {
		if td, ok := tool.(map[string]any); ok {
			if td["name"] == "status" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("status tool not found in tools/list")
	}
}

func TestUnknownTool(t *testing.T) {
	var out bytes.Buffer
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nonexistent","arguments":{}}}` + "\n"
	s := &Server{
		tools:  make(map[string]ToolHandler),
		reader: bufio.NewReader(strings.NewReader(input)),
		writer: &out,
	}
	s.registerTools()

	_ = s.Run()

	var resp Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	// Should return a result with isError, not a JSON-RPC error.
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("result is not a map")
	}
	if result["isError"] != true {
		t.Error("expected isError=true for unknown tool")
	}
}

func TestPing(t *testing.T) {
	var out bytes.Buffer
	input := `{"jsonrpc":"2.0","id":42,"method":"ping"}` + "\n"
	s := &Server{
		tools:  make(map[string]ToolHandler),
		reader: bufio.NewReader(strings.NewReader(input)),
		writer: &out,
	}

	_ = s.Run()

	var resp Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}

func TestUnknownMethod(t *testing.T) {
	var out bytes.Buffer
	input := `{"jsonrpc":"2.0","id":1,"method":"resources/list","params":{}}` + "\n"
	s := &Server{
		tools:  make(map[string]ToolHandler),
		reader: bufio.NewReader(strings.NewReader(input)),
		writer: &out,
	}

	_ = s.Run()

	var resp Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
}
