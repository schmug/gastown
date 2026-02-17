package mcpserver

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/crew"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/mail"
	"github.com/steveyegge/gastown/internal/nudge"
	"github.com/steveyegge/gastown/internal/polecat"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/tmux"
	"github.com/steveyegge/gastown/internal/workspace"
)

// getTownRoot returns the town root, using the cached value or auto-detecting.
func (s *Server) getTownRoot() (string, error) {
	if s.townRoot != "" {
		return s.townRoot, nil
	}
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return "", fmt.Errorf("not in a Gas Town workspace: %w", err)
	}
	s.townRoot = townRoot
	return townRoot, nil
}

// getRig resolves a rig by name.
func (s *Server) getRig(rigName string) (string, *rig.Rig, error) {
	townRoot, err := s.getTownRoot()
	if err != nil {
		return "", nil, err
	}

	rigsConfigPath := filepath.Join(townRoot, "mayor", "rigs.json")
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Rigs: make(map[string]config.RigEntry)}
	}

	g := git.NewGit(townRoot)
	mgr := rig.NewManager(townRoot, rigsConfig, g)
	r, err := mgr.GetRig(rigName)
	if err != nil {
		return "", nil, fmt.Errorf("rig %q not found", rigName)
	}
	return townRoot, r, nil
}

// discoverRigs returns all rigs.
func (s *Server) discoverRigs() (string, []*rig.Rig, *config.RigsConfig, error) {
	townRoot, err := s.getTownRoot()
	if err != nil {
		return "", nil, nil, err
	}

	rigsConfigPath := filepath.Join(townRoot, "mayor", "rigs.json")
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Rigs: make(map[string]config.RigEntry)}
	}

	g := git.NewGit(townRoot)
	mgr := rig.NewManager(townRoot, rigsConfig, g)
	rigs, err := mgr.DiscoverRigs()
	if err != nil {
		return "", nil, nil, fmt.Errorf("discovering rigs: %w", err)
	}
	return townRoot, rigs, rigsConfig, nil
}

// parseAddress splits "rig/polecat" into parts.
func parseAddress(addr string) (rigName, name string, err error) {
	parts := strings.SplitN(addr, "/", 2)
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0], parts[1], nil
	}
	return "", "", fmt.Errorf("invalid address format: expected 'rig/name', got %q", addr)
}

// --- Status ---

type statusArgs struct {
	Fast bool `json:"fast"`
}

// statusResult mirrors TownStatus from cmd/status.go for JSON output.
type statusResult struct {
	Name     string            `json:"name"`
	Location string            `json:"location"`
	Agents   []agentRuntime    `json:"agents"`
	Rigs     []rigStatusResult `json:"rigs"`
	Summary  statusSummary     `json:"summary"`
}

type agentRuntime struct {
	Name       string `json:"name"`
	Address    string `json:"address"`
	Session    string `json:"session"`
	Role       string `json:"role"`
	Running    bool   `json:"running"`
	HasWork    bool   `json:"has_work"`
	WorkTitle  string `json:"work_title,omitempty"`
	State      string `json:"state,omitempty"`
	UnreadMail int    `json:"unread_mail"`
}

type rigStatusResult struct {
	Name         string         `json:"name"`
	Polecats     []string       `json:"polecats"`
	PolecatCount int            `json:"polecat_count"`
	Crews        []string       `json:"crews"`
	CrewCount    int            `json:"crew_count"`
	HasWitness   bool           `json:"has_witness"`
	HasRefinery  bool           `json:"has_refinery"`
	Agents       []agentRuntime `json:"agents,omitempty"`
}

type statusSummary struct {
	RigCount      int `json:"rig_count"`
	PolecatCount  int `json:"polecat_count"`
	CrewCount     int `json:"crew_count"`
	WitnessCount  int `json:"witness_count"`
	RefineryCount int `json:"refinery_count"`
}

func (s *Server) handleStatus(raw json.RawMessage) *ToolCallResult {
	var args statusArgs
	_ = json.Unmarshal(raw, &args)

	townRoot, rigs, _, err := s.discoverRigs()
	if err != nil {
		return errorResult(err.Error())
	}

	townConfigPath := filepath.Join(townRoot, "mayor", "town.json")
	townConfig, err := config.LoadTownConfig(townConfigPath)
	if err != nil {
		townConfig = &config.TownConfig{Name: filepath.Base(townRoot)}
	}

	t := tmux.NewTmux()

	// Pre-fetch all tmux sessions and verify agent liveness.
	allSessions := make(map[string]bool)
	if sessions, err := t.ListSessions(); err == nil {
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, sess := range sessions {
			if session.IsKnownSession(sess) {
				wg.Add(1)
				go func(name string) {
					defer wg.Done()
					alive := t.IsAgentAlive(name)
					mu.Lock()
					allSessions[name] = alive
					mu.Unlock()
				}(sess)
			} else {
				allSessions[sess] = true
			}
		}
		wg.Wait()
	}

	// Pre-fetch agent beads.
	allAgentBeads := make(map[string]*beads.Issue)
	allHookBeads := make(map[string]*beads.Issue)
	var beadsMu sync.Mutex

	var beadsWg sync.WaitGroup

	// Town-level beads
	townBeadsPath := beads.GetTownBeadsPath(townRoot)
	beadsWg.Add(1)
	go func() {
		defer beadsWg.Done()
		bc := beads.New(townBeadsPath)
		ab, _ := bc.ListAgentBeads()
		beadsMu.Lock()
		for id, issue := range ab {
			allAgentBeads[id] = issue
		}
		beadsMu.Unlock()

		var hookIDs []string
		for _, issue := range ab {
			hookID := issue.HookBead
			if hookID == "" {
				fields := beads.ParseAgentFields(issue.Description)
				if fields != nil {
					hookID = fields.HookBead
				}
			}
			if hookID != "" {
				hookIDs = append(hookIDs, hookID)
			}
		}
		if len(hookIDs) > 0 {
			hb, _ := bc.ShowMultiple(hookIDs)
			beadsMu.Lock()
			for id, issue := range hb {
				allHookBeads[id] = issue
			}
			beadsMu.Unlock()
		}
	}()

	// Rig-level beads
	for _, r := range rigs {
		beadsWg.Add(1)
		go func(r *rig.Rig) {
			defer beadsWg.Done()
			rigBeadsPath := filepath.Join(r.Path, "mayor", "rig")
			bc := beads.New(rigBeadsPath)
			ab, _ := bc.ListAgentBeads()
			if ab == nil {
				return
			}
			beadsMu.Lock()
			for id, issue := range ab {
				allAgentBeads[id] = issue
			}
			beadsMu.Unlock()

			var hookIDs []string
			for _, issue := range ab {
				hookID := issue.HookBead
				if hookID == "" {
					fields := beads.ParseAgentFields(issue.Description)
					if fields != nil {
						hookID = fields.HookBead
					}
				}
				if hookID != "" {
					hookIDs = append(hookIDs, hookID)
				}
			}
			if len(hookIDs) > 0 {
				hb, _ := bc.ShowMultiple(hookIDs)
				beadsMu.Lock()
				for id, issue := range hb {
					allHookBeads[id] = issue
				}
				beadsMu.Unlock()
			}
		}(r)
	}
	beadsWg.Wait()

	// Create mail router for inbox lookups.
	mailRouter := mail.NewRouter(townRoot)

	// Build global agents.
	globalAgents := buildGlobalAgents(allSessions, allAgentBeads, allHookBeads, mailRouter, args.Fast)

	// Build rig statuses.
	rigStatuses := make([]rigStatusResult, len(rigs))
	var rigWg sync.WaitGroup
	for i, r := range rigs {
		rigWg.Add(1)
		go func(idx int, r *rig.Rig) {
			defer rigWg.Done()
			rs := rigStatusResult{
				Name:         r.Name,
				Polecats:     r.Polecats,
				PolecatCount: len(r.Polecats),
				HasWitness:   r.HasWitness,
				HasRefinery:  r.HasRefinery,
			}

			crewGit := git.NewGit(r.Path)
			crewMgr := crew.NewManager(r, crewGit)
			if workers, err := crewMgr.List(); err == nil {
				for _, w := range workers {
					rs.Crews = append(rs.Crews, w.Name)
				}
				rs.CrewCount = len(workers)
			}

			rs.Agents = buildRigAgents(allSessions, r, rs.Crews, allAgentBeads, allHookBeads, mailRouter, args.Fast, townRoot)

			rigStatuses[idx] = rs
		}(i, r)
	}
	rigWg.Wait()

	// Aggregate summary.
	var summary statusSummary
	summary.RigCount = len(rigs)
	for _, rs := range rigStatuses {
		summary.PolecatCount += rs.PolecatCount
		summary.CrewCount += rs.CrewCount
		if rs.HasWitness {
			summary.WitnessCount++
		}
		if rs.HasRefinery {
			summary.RefineryCount++
		}
	}

	result := statusResult{
		Name:     townConfig.Name,
		Location: townRoot,
		Agents:   globalAgents,
		Rigs:     rigStatuses,
		Summary:  summary,
	}
	return jsonResult(result)
}

// buildGlobalAgents discovers Mayor and Deacon runtime state.
func buildGlobalAgents(allSessions map[string]bool, agentBeads, hookBeads map[string]*beads.Issue, mailRouter *mail.Router, fast bool) []agentRuntime {
	defs := []struct {
		name, address, sess, role, beadID string
	}{
		{"mayor", "mayor/", session.MayorSessionName(), "coordinator", beads.MayorBeadIDTown()},
		{"deacon", "deacon/", session.DeaconSessionName(), "health-check", beads.DeaconBeadIDTown()},
	}

	agents := make([]agentRuntime, len(defs))
	var wg sync.WaitGroup
	for i, d := range defs {
		wg.Add(1)
		go func(idx int, d struct {
			name, address, sess, role, beadID string
		}) {
			defer wg.Done()
			agents[idx] = buildAgent(d.name, d.address, d.sess, d.role, d.beadID, allSessions, agentBeads, hookBeads, mailRouter, fast)
		}(i, d)
	}
	wg.Wait()
	return agents
}

// buildRigAgents discovers all agents in a rig.
func buildRigAgents(allSessions map[string]bool, r *rig.Rig, crews []string, agentBeads, hookBeads map[string]*beads.Issue, mailRouter *mail.Router, fast bool, townRoot string) []agentRuntime {
	type agentDef struct {
		name, address, sess, role, beadID string
	}

	prefix := beads.GetPrefixForRig(townRoot, r.Name)
	var defs []agentDef

	if r.HasWitness {
		defs = append(defs, agentDef{
			name:    "witness",
			address: r.Name + "/witness",
			sess:    session.WitnessSessionName(session.PrefixFor(r.Name)),
			role:    "witness",
			beadID:  beads.WitnessBeadIDWithPrefix(prefix, r.Name),
		})
	}
	if r.HasRefinery {
		defs = append(defs, agentDef{
			name:    "refinery",
			address: r.Name + "/refinery",
			sess:    session.RefinerySessionName(session.PrefixFor(r.Name)),
			role:    "refinery",
			beadID:  beads.RefineryBeadIDWithPrefix(prefix, r.Name),
		})
	}
	for _, name := range r.Polecats {
		defs = append(defs, agentDef{
			name:    name,
			address: r.Name + "/" + name,
			sess:    session.PolecatSessionName(session.PrefixFor(r.Name), name),
			role:    "polecat",
			beadID:  beads.PolecatBeadIDWithPrefix(prefix, r.Name, name),
		})
	}
	for _, name := range crews {
		defs = append(defs, agentDef{
			name:    name,
			address: r.Name + "/crew/" + name,
			sess:    session.CrewSessionName(session.PrefixFor(r.Name), name),
			role:    "crew",
			beadID:  beads.CrewBeadIDWithPrefix(prefix, r.Name, name),
		})
	}

	agents := make([]agentRuntime, len(defs))
	var wg sync.WaitGroup
	for i, d := range defs {
		wg.Add(1)
		go func(idx int, d agentDef) {
			defer wg.Done()
			agents[idx] = buildAgent(d.name, d.address, d.sess, d.role, d.beadID, allSessions, agentBeads, hookBeads, mailRouter, fast)
		}(i, d)
	}
	wg.Wait()
	return agents
}

// buildAgent constructs an agentRuntime from preloaded data.
func buildAgent(name, address, sess, role, beadID string, allSessions map[string]bool, agentBeads, hookBeads map[string]*beads.Issue, mailRouter *mail.Router, fast bool) agentRuntime {
	a := agentRuntime{
		Name:    name,
		Address: address,
		Session: sess,
		Role:    role,
		Running: allSessions[sess],
	}

	if issue, ok := agentBeads[beadID]; ok {
		a.State = issue.AgentState
		if issue.HookBead != "" {
			a.HasWork = true
			a.WorkTitle = ""
			if pinnedIssue, ok := hookBeads[issue.HookBead]; ok {
				a.WorkTitle = pinnedIssue.Title
			}
		}
		if a.State == "" {
			fields := beads.ParseAgentFields(issue.Description)
			if fields != nil {
				a.State = fields.AgentState
			}
		}
	}

	if !fast && mailRouter != nil {
		if mailbox, err := mailRouter.GetMailbox(address); err == nil {
			_, unread, _ := mailbox.Count()
			a.UnreadMail = unread
		}
	}

	return a
}

// --- Session List ---

type sessionListArgs struct {
	Rig string `json:"rig"`
}

type sessionListItem struct {
	Rig       string `json:"rig"`
	Polecat   string `json:"polecat"`
	SessionID string `json:"session_id"`
	Running   bool   `json:"running"`
}

func (s *Server) handleSessionList(raw json.RawMessage) *ToolCallResult {
	var args sessionListArgs
	_ = json.Unmarshal(raw, &args)

	_, rigs, _, err := s.discoverRigs()
	if err != nil {
		return errorResult(err.Error())
	}

	if args.Rig != "" {
		var filtered []*rig.Rig
		for _, r := range rigs {
			if r.Name == args.Rig {
				filtered = append(filtered, r)
			}
		}
		rigs = filtered
	}

	t := tmux.NewTmux()
	all := make([]sessionListItem, 0)

	for _, r := range rigs {
		polecatMgr := polecat.NewSessionManager(t, r)
		infos, err := polecatMgr.List()
		if err != nil {
			continue
		}
		for _, info := range infos {
			all = append(all, sessionListItem{
				Rig:       r.Name,
				Polecat:   info.Polecat,
				SessionID: info.SessionID,
				Running:   info.Running,
			})
		}
	}

	return jsonResult(all)
}

// --- Session Start ---

type sessionStartArgs struct {
	Address string `json:"address"`
	Issue   string `json:"issue"`
}

func (s *Server) handleSessionStart(raw json.RawMessage) *ToolCallResult {
	var args sessionStartArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return errorResult("invalid arguments: " + err.Error())
	}
	if args.Address == "" {
		return errorResult("address is required")
	}

	rigName, polecatName, err := parseAddress(args.Address)
	if err != nil {
		return errorResult(err.Error())
	}

	_, r, err := s.getRig(rigName)
	if err != nil {
		return errorResult(err.Error())
	}

	t := tmux.NewTmux()
	polecatMgr := polecat.NewSessionManager(t, r)

	// Check polecat exists.
	found := false
	for _, p := range r.Polecats {
		if p == polecatName {
			found = true
			break
		}
	}
	if !found {
		return errorResult(fmt.Sprintf("polecat %q not found in rig %q", polecatName, rigName))
	}

	opts := polecat.SessionStartOptions{
		Issue: args.Issue,
	}
	if err := polecatMgr.Start(polecatName, opts); err != nil {
		return errorResult(fmt.Sprintf("starting session: %v", err))
	}

	return textResult(fmt.Sprintf("Session started for %s/%s", rigName, polecatName))
}

// --- Session Stop ---

type sessionStopArgs struct {
	Address string `json:"address"`
	Force   bool   `json:"force"`
}

func (s *Server) handleSessionStop(raw json.RawMessage) *ToolCallResult {
	var args sessionStopArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return errorResult("invalid arguments: " + err.Error())
	}
	if args.Address == "" {
		return errorResult("address is required")
	}

	rigName, polecatName, err := parseAddress(args.Address)
	if err != nil {
		return errorResult(err.Error())
	}

	_, r, err := s.getRig(rigName)
	if err != nil {
		return errorResult(err.Error())
	}

	t := tmux.NewTmux()
	polecatMgr := polecat.NewSessionManager(t, r)

	if err := polecatMgr.Stop(polecatName, args.Force); err != nil {
		return errorResult(fmt.Sprintf("stopping session: %v", err))
	}

	return textResult(fmt.Sprintf("Session stopped for %s/%s", rigName, polecatName))
}

// --- Session Status ---

type sessionStatusArgs struct {
	Address string `json:"address"`
}

func (s *Server) handleSessionStatus(raw json.RawMessage) *ToolCallResult {
	var args sessionStatusArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return errorResult("invalid arguments: " + err.Error())
	}
	if args.Address == "" {
		return errorResult("address is required")
	}

	rigName, polecatName, err := parseAddress(args.Address)
	if err != nil {
		return errorResult(err.Error())
	}

	_, r, err := s.getRig(rigName)
	if err != nil {
		return errorResult(err.Error())
	}

	t := tmux.NewTmux()
	polecatMgr := polecat.NewSessionManager(t, r)

	info, err := polecatMgr.Status(polecatName)
	if err != nil {
		return errorResult(fmt.Sprintf("getting status: %v", err))
	}

	return jsonResult(info)
}

// --- Session Capture ---

type sessionCaptureArgs struct {
	Address string `json:"address"`
	Lines   int    `json:"lines"`
}

func (s *Server) handleSessionCapture(raw json.RawMessage) *ToolCallResult {
	var args sessionCaptureArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return errorResult("invalid arguments: " + err.Error())
	}
	if args.Address == "" {
		return errorResult("address is required")
	}
	if args.Lines <= 0 {
		args.Lines = 100
	}

	rigName, polecatName, err := parseAddress(args.Address)
	if err != nil {
		return errorResult(err.Error())
	}

	_, r, err := s.getRig(rigName)
	if err != nil {
		return errorResult(err.Error())
	}

	t := tmux.NewTmux()
	polecatMgr := polecat.NewSessionManager(t, r)

	output, err := polecatMgr.Capture(polecatName, args.Lines)
	if err != nil {
		return errorResult(fmt.Sprintf("capturing output: %v", err))
	}

	return textResult(output)
}

// --- Nudge ---

type nudgeArgs struct {
	Target  string `json:"target"`
	Message string `json:"message"`
	Mode    string `json:"mode"`
	Sender  string `json:"sender"`
}

func (s *Server) handleNudge(raw json.RawMessage) *ToolCallResult {
	var args nudgeArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return errorResult("invalid arguments: " + err.Error())
	}
	if args.Target == "" || args.Message == "" {
		return errorResult("target and message are required")
	}
	if args.Mode == "" {
		args.Mode = "immediate"
	}
	if args.Sender == "" {
		args.Sender = "companion"
	}

	townRoot, err := s.getTownRoot()
	if err != nil {
		return errorResult(err.Error())
	}

	t := tmux.NewTmux()
	target := args.Target

	// Expand role shortcuts.
	switch target {
	case "mayor":
		target = session.MayorSessionName()
	case "deacon":
		target = session.DeaconSessionName()
	}

	// If it contains "/", resolve rig/polecat to session name.
	if strings.Contains(target, "/") {
		rigName, polecatName, err := parseAddress(target)
		if err != nil {
			return errorResult(err.Error())
		}

		// Check crew vs polecat.
		if strings.HasPrefix(polecatName, "crew/") {
			crewName := strings.TrimPrefix(polecatName, "crew/")
			target = session.CrewSessionName(session.PrefixFor(rigName), crewName)
		} else if polecatName == "witness" {
			target = session.WitnessSessionName(session.PrefixFor(rigName))
		} else if polecatName == "refinery" {
			target = session.RefinerySessionName(session.PrefixFor(rigName))
		} else {
			// Try crew first, fall back to polecat.
			crewSession := session.CrewSessionName(session.PrefixFor(rigName), polecatName)
			if exists, _ := t.HasSession(crewSession); exists {
				target = crewSession
			} else {
				_, r, err := s.getRig(rigName)
				if err != nil {
					return errorResult(err.Error())
				}
				mgr := polecat.NewSessionManager(t, r)
				target = mgr.SessionName(polecatName)
			}
		}
	}

	prefixedMessage := fmt.Sprintf("[from %s] %s", args.Sender, args.Message)

	switch args.Mode {
	case "queue":
		if err := nudge.Enqueue(townRoot, target, nudge.QueuedNudge{
			Sender:   args.Sender,
			Message:  args.Message,
			Priority: nudge.PriorityNormal,
		}); err != nil {
			return errorResult(fmt.Sprintf("queueing nudge: %v", err))
		}
	case "wait-idle":
		if err := t.WaitForIdle(target, 15*time.Second); err == nil {
			if err := t.NudgeSession(target, prefixedMessage); err != nil {
				return errorResult(fmt.Sprintf("nudging: %v", err))
			}
		} else {
			// Fall back to queue.
			if err := nudge.Enqueue(townRoot, target, nudge.QueuedNudge{
				Sender:   args.Sender,
				Message:  args.Message,
				Priority: nudge.PriorityNormal,
			}); err != nil {
				// Last resort: immediate.
				if err := t.NudgeSession(target, prefixedMessage); err != nil {
					return errorResult(fmt.Sprintf("nudging (fallback): %v", err))
				}
			}
		}
	default: // immediate
		if err := t.NudgeSession(target, prefixedMessage); err != nil {
			return errorResult(fmt.Sprintf("nudging: %v", err))
		}
	}

	return textResult(fmt.Sprintf("Nudged %s (%s)", args.Target, args.Mode))
}

// --- Mail Send ---

type mailSendArgs struct {
	To       string `json:"to"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	From     string `json:"from"`
	Priority int    `json:"priority"`
	Notify   bool   `json:"notify"`
}

func (s *Server) handleMailSend(raw json.RawMessage) *ToolCallResult {
	var args mailSendArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return errorResult("invalid arguments: " + err.Error())
	}
	if args.To == "" || args.Subject == "" || args.Body == "" {
		return errorResult("to, subject, and body are required")
	}
	if args.From == "" {
		args.From = "companion"
	}

	townRoot, err := s.getTownRoot()
	if err != nil {
		return errorResult(err.Error())
	}

	router := mail.NewRouter(townRoot)

	msg := mail.NewMessage(args.From, args.To, args.Subject, args.Body)
	msg.Priority = mail.PriorityFromInt(args.Priority)

	if err := router.Send(msg); err != nil {
		return errorResult(fmt.Sprintf("sending mail: %v", err))
	}

	result := fmt.Sprintf("Mail sent to %s: %s", args.To, args.Subject)

	// Optionally nudge the recipient.
	if args.Notify {
		t := tmux.NewTmux()
		// Try to resolve the address to a session and nudge.
		nudgeTarget := args.To
		// Reuse nudge logic via a synthetic call.
		nudgeRaw, _ := json.Marshal(nudgeArgs{
			Target:  nudgeTarget,
			Message: fmt.Sprintf("You have new mail: %s", args.Subject),
			Mode:    "queue",
			Sender:  args.From,
		})
		_ = t // suppress unused; handled via handleNudge
		nudgeResult := s.handleNudge(nudgeRaw)
		if nudgeResult.IsError {
			result += " (nudge failed: " + nudgeResult.Content[0].Text + ")"
		} else {
			result += " (notified)"
		}
	}

	return textResult(result)
}

// --- Mail Inbox ---

type mailInboxArgs struct {
	Address    string `json:"address"`
	UnreadOnly bool   `json:"unread_only"`
}

type mailInboxItem struct {
	ID        string `json:"id"`
	From      string `json:"from"`
	Subject   string `json:"subject"`
	Read      bool   `json:"read"`
	Priority  string `json:"priority"`
	Timestamp string `json:"timestamp"`
}

func (s *Server) handleMailInbox(raw json.RawMessage) *ToolCallResult {
	var args mailInboxArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return errorResult("invalid arguments: " + err.Error())
	}
	if args.Address == "" {
		return errorResult("address is required")
	}

	townRoot, err := s.getTownRoot()
	if err != nil {
		return errorResult(err.Error())
	}

	router := mail.NewRouter(townRoot)
	mailbox, err := router.GetMailbox(args.Address)
	if err != nil {
		return errorResult(fmt.Sprintf("getting mailbox: %v", err))
	}

	var messages []*mail.Message
	if args.UnreadOnly {
		messages, err = mailbox.ListUnread()
	} else {
		messages, err = mailbox.List()
	}
	if err != nil {
		return errorResult(fmt.Sprintf("listing messages: %v", err))
	}

	items := make([]mailInboxItem, 0)
	for _, msg := range messages {
		items = append(items, mailInboxItem{
			ID:        msg.ID,
			From:      msg.From,
			Subject:   msg.Subject,
			Read:      msg.Read,
			Priority:  string(msg.Priority),
			Timestamp: msg.Timestamp.Format("2006-01-02T15:04:05Z"),
		})
	}

	return jsonResult(items)
}

// --- Crew List ---

type crewListArgs struct {
	Rig string `json:"rig"`
}

type crewListItem struct {
	Name      string `json:"name"`
	Rig       string `json:"rig"`
	Branch    string `json:"branch"`
	ClonePath string `json:"clone_path"`
	Running   bool   `json:"running"`
}

func (s *Server) handleCrewList(raw json.RawMessage) *ToolCallResult {
	var args crewListArgs
	_ = json.Unmarshal(raw, &args)

	_, rigs, _, err := s.discoverRigs()
	if err != nil {
		return errorResult(err.Error())
	}

	if args.Rig != "" {
		var filtered []*rig.Rig
		for _, r := range rigs {
			if r.Name == args.Rig {
				filtered = append(filtered, r)
			}
		}
		rigs = filtered
	}

	t := tmux.NewTmux()
	all := make([]crewListItem, 0)

	for _, r := range rigs {
		crewGit := git.NewGit(r.Path)
		crewMgr := crew.NewManager(r, crewGit)
		workers, err := crewMgr.List()
		if err != nil {
			continue
		}
		for _, w := range workers {
			sessionName := session.CrewSessionName(session.PrefixFor(r.Name), w.Name)
			running, _ := t.HasSession(sessionName)
			all = append(all, crewListItem{
				Name:      w.Name,
				Rig:       r.Name,
				Branch:    w.Branch,
				ClonePath: w.ClonePath,
				Running:   running,
			})
		}
	}

	return jsonResult(all)
}

// --- Crew Start ---

type crewStartArgs struct {
	Name string `json:"name"`
	Rig  string `json:"rig"`
}

func (s *Server) handleCrewStart(raw json.RawMessage) *ToolCallResult {
	var args crewStartArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return errorResult("invalid arguments: " + err.Error())
	}
	if args.Name == "" {
		return errorResult("name is required")
	}

	rigName := args.Rig
	if rigName == "" {
		// Try to infer rig.
		_, rigs, _, err := s.discoverRigs()
		if err != nil {
			return errorResult(err.Error())
		}
		if len(rigs) == 1 {
			rigName = rigs[0].Name
		} else {
			return errorResult("rig is required when multiple rigs exist")
		}
	}

	_, r, err := s.getRig(rigName)
	if err != nil {
		return errorResult(err.Error())
	}

	crewGit := git.NewGit(r.Path)
	crewMgr := crew.NewManager(r, crewGit)

	if err := crewMgr.Start(args.Name, crew.StartOptions{}); err != nil {
		return errorResult(fmt.Sprintf("starting crew session: %v", err))
	}

	return textResult(fmt.Sprintf("Crew session started for %s/%s", rigName, args.Name))
}

// --- Crew Stop ---

type crewStopArgs struct {
	Name string `json:"name"`
	Rig  string `json:"rig"`
}

func (s *Server) handleCrewStop(raw json.RawMessage) *ToolCallResult {
	var args crewStopArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return errorResult("invalid arguments: " + err.Error())
	}
	if args.Name == "" {
		return errorResult("name is required")
	}

	rigName := args.Rig
	if rigName == "" {
		_, rigs, _, err := s.discoverRigs()
		if err != nil {
			return errorResult(err.Error())
		}
		if len(rigs) == 1 {
			rigName = rigs[0].Name
		} else {
			return errorResult("rig is required when multiple rigs exist")
		}
	}

	_, r, err := s.getRig(rigName)
	if err != nil {
		return errorResult(err.Error())
	}

	crewGit := git.NewGit(r.Path)
	crewMgr := crew.NewManager(r, crewGit)

	if err := crewMgr.Stop(args.Name); err != nil {
		return errorResult(fmt.Sprintf("stopping crew session: %v", err))
	}

	return textResult(fmt.Sprintf("Crew session stopped for %s/%s", rigName, args.Name))
}
