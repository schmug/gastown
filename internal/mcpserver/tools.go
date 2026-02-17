package mcpserver

// registerTools wires all gastown tool handlers into the server.
func (s *Server) registerTools() {
	s.tools["status"] = s.handleStatus
	s.tools["session_list"] = s.handleSessionList
	s.tools["session_start"] = s.handleSessionStart
	s.tools["session_stop"] = s.handleSessionStop
	s.tools["session_status"] = s.handleSessionStatus
	s.tools["session_capture"] = s.handleSessionCapture
	s.tools["nudge"] = s.handleNudge
	s.tools["mail_send"] = s.handleMailSend
	s.tools["mail_inbox"] = s.handleMailInbox
	s.tools["crew_list"] = s.handleCrewList
	s.tools["crew_start"] = s.handleCrewStart
	s.tools["crew_stop"] = s.handleCrewStop
}

// toolDefs returns the MCP tool definitions for tools/list.
func (s *Server) toolDefs() []ToolDef {
	return []ToolDef{
		{
			Name:        "status",
			Description: "Show overall Gas Town status: rigs, agents, sessions, mail counts, merge queue.",
			InputSchema: obj(
				"type", "object",
				"properties", obj(
					"fast", obj("type", "boolean", "description", "Skip mail lookups for faster execution"),
				),
			),
		},
		{
			Name:        "session_list",
			Description: "List all running polecat sessions across rigs.",
			InputSchema: obj(
				"type", "object",
				"properties", obj(
					"rig", obj("type", "string", "description", "Filter by rig name"),
				),
			),
		},
		{
			Name:        "session_start",
			Description: "Start a polecat session. Creates a tmux session and launches the agent.",
			InputSchema: obj(
				"type", "object",
				"properties", obj(
					"address", obj("type", "string", "description", "Rig/polecat address (e.g. greenplace/Toast)"),
					"issue", obj("type", "string", "description", "Issue ID to work on"),
				),
				"required", []string{"address"},
			),
		},
		{
			Name:        "session_stop",
			Description: "Stop a running polecat session.",
			InputSchema: obj(
				"type", "object",
				"properties", obj(
					"address", obj("type", "string", "description", "Rig/polecat address (e.g. greenplace/Toast)"),
					"force", obj("type", "boolean", "description", "Skip graceful shutdown"),
				),
				"required", []string{"address"},
			),
		},
		{
			Name:        "session_status",
			Description: "Show detailed status for a specific polecat session.",
			InputSchema: obj(
				"type", "object",
				"properties", obj(
					"address", obj("type", "string", "description", "Rig/polecat address (e.g. greenplace/Toast)"),
				),
				"required", []string{"address"},
			),
		},
		{
			Name:        "session_capture",
			Description: "Capture recent terminal output from a polecat session.",
			InputSchema: obj(
				"type", "object",
				"properties", obj(
					"address", obj("type", "string", "description", "Rig/polecat address (e.g. greenplace/Toast)"),
					"lines", obj("type", "integer", "description", "Number of lines to capture (default 100)"),
				),
				"required", []string{"address"},
			),
		},
		{
			Name:        "nudge",
			Description: "Send a message to any Gas Town agent session (polecat, crew, witness, mayor, deacon).",
			InputSchema: obj(
				"type", "object",
				"properties", obj(
					"target", obj("type", "string", "description", "Target address: rig/polecat, rig/crew/name, mayor, deacon, witness, refinery"),
					"message", obj("type", "string", "description", "Message to send"),
					"mode", obj("type", "string", "enum", []string{"immediate", "queue", "wait-idle"}, "description", "Delivery mode (default: immediate)"),
					"sender", obj("type", "string", "description", "Sender identity (default: companion)"),
				),
				"required", []string{"target", "message"},
			),
		},
		{
			Name:        "mail_send",
			Description: "Send a mail message to an agent's beads mailbox.",
			InputSchema: obj(
				"type", "object",
				"properties", obj(
					"to", obj("type", "string", "description", "Recipient address (e.g. mayor/, greenplace/Toast, greenplace/crew/max)"),
					"subject", obj("type", "string", "description", "Message subject"),
					"body", obj("type", "string", "description", "Message body"),
					"from", obj("type", "string", "description", "Sender address (default: companion)"),
					"priority", obj("type", "integer", "description", "Priority 0-4 (0=urgent, 2=normal, 4=backlog)"),
					"notify", obj("type", "boolean", "description", "Also nudge the recipient"),
				),
				"required", []string{"to", "subject", "body"},
			),
		},
		{
			Name:        "mail_inbox",
			Description: "Check an agent's mail inbox. Returns message list with subjects and read status.",
			InputSchema: obj(
				"type", "object",
				"properties", obj(
					"address", obj("type", "string", "description", "Mailbox address (e.g. mayor/, greenplace/Toast)"),
					"unread_only", obj("type", "boolean", "description", "Only show unread messages"),
				),
				"required", []string{"address"},
			),
		},
		{
			Name:        "crew_list",
			Description: "List crew workspaces with session and git status.",
			InputSchema: obj(
				"type", "object",
				"properties", obj(
					"rig", obj("type", "string", "description", "Rig name (auto-detected if omitted)"),
				),
			),
		},
		{
			Name:        "crew_start",
			Description: "Start a crew session for a human developer workspace.",
			InputSchema: obj(
				"type", "object",
				"properties", obj(
					"name", obj("type", "string", "description", "Crew worker name"),
					"rig", obj("type", "string", "description", "Rig name (auto-detected if omitted)"),
				),
				"required", []string{"name"},
			),
		},
		{
			Name:        "crew_stop",
			Description: "Stop a crew session.",
			InputSchema: obj(
				"type", "object",
				"properties", obj(
					"name", obj("type", "string", "description", "Crew worker name"),
					"rig", obj("type", "string", "description", "Rig name (auto-detected if omitted)"),
				),
				"required", []string{"name"},
			),
		},
	}
}

// obj is a helper to build map[string]any for JSON schema definitions.
func obj(pairs ...any) map[string]any {
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs)-1; i += 2 {
		m[pairs[i].(string)] = pairs[i+1]
	}
	return m
}
