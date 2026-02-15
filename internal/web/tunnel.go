package web

import (
	"fmt"
	"log"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// TunnelStatus represents the current state of the Cloudflare tunnel.
type TunnelStatus struct {
	Running   bool   `json:"running"`
	Hostname  string `json:"hostname"`
	Uptime    string `json:"uptime,omitempty"`
	LocalPort int    `json:"local_port"`
}

// TunnelManager manages a cloudflared tunnel subprocess.
type TunnelManager struct {
	cmd       *exec.Cmd
	running   bool
	token     string
	hostname  string
	localPort int
	startTime time.Time
	mu        sync.Mutex
}

// NewTunnelManager creates a new tunnel manager with the given config.
func NewTunnelManager(token, hostname string, localPort int) *TunnelManager {
	return &TunnelManager{
		token:     token,
		hostname:  hostname,
		localPort: localPort,
	}
}

// Start launches the cloudflared tunnel process.
func (tm *TunnelManager) Start() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.running {
		return fmt.Errorf("tunnel already running")
	}

	if tm.token == "" {
		return fmt.Errorf("no tunnel token configured (set --tunnel-token or CLOUDFLARE_TUNNEL_TOKEN)")
	}

	tm.cmd = exec.Command("cloudflared", "tunnel", "run", "--token", tm.token)

	// Pipe output to the dashboard log
	tm.cmd.Stdout = log.Writer()
	tm.cmd.Stderr = log.Writer()

	if err := tm.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start cloudflared: %w", err)
	}

	tm.running = true
	tm.startTime = time.Now()

	// Monitor the process in the background
	go func() {
		err := tm.cmd.Wait()
		tm.mu.Lock()
		tm.running = false
		tm.mu.Unlock()
		if err != nil {
			log.Printf("cloudflared exited: %v", err)
		} else {
			log.Printf("cloudflared exited cleanly")
		}
	}()

	log.Printf("tunnel started: https://%s -> localhost:%d", tm.hostname, tm.localPort)
	return nil
}

// Stop gracefully shuts down the tunnel process.
func (tm *TunnelManager) Stop() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.running || tm.cmd == nil || tm.cmd.Process == nil {
		return nil
	}

	log.Printf("stopping tunnel...")
	if err := tm.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Process may have already exited
		return nil
	}

	// Wait briefly for graceful shutdown
	done := make(chan struct{})
	go func() {
		_ = tm.cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = tm.cmd.Process.Kill()
	}

	tm.running = false
	log.Printf("tunnel stopped")
	return nil
}

// Status returns the current tunnel status.
func (tm *TunnelManager) Status() *TunnelStatus {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	status := &TunnelStatus{
		Running:   tm.running,
		Hostname:  tm.hostname,
		LocalPort: tm.localPort,
	}

	if tm.running {
		status.Uptime = formatUptime(time.Since(tm.startTime))
	}

	return status
}

// HasToken returns whether a tunnel token is configured.
func (tm *TunnelManager) HasToken() bool {
	return tm.token != ""
}

// formatUptime formats a duration as a human-readable uptime string.
func formatUptime(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh%dm", h, m)
	default:
		days := int(d.Hours()) / 24
		h := int(d.Hours()) % 24
		return fmt.Sprintf("%dd%dh", days, h)
	}
}
