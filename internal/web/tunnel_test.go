package web

import (
	"testing"
	"time"
)

func TestNewTunnelManager(t *testing.T) {
	tm := NewTunnelManager("test-token", "example.com", 8080)
	if tm == nil {
		t.Fatal("expected non-nil tunnel manager")
	}
	if tm.token != "test-token" {
		t.Errorf("expected token 'test-token', got %q", tm.token)
	}
	if tm.hostname != "example.com" {
		t.Errorf("expected hostname 'example.com', got %q", tm.hostname)
	}
	if tm.localPort != 8080 {
		t.Errorf("expected port 8080, got %d", tm.localPort)
	}
}

func TestTunnelManagerStatusNotRunning(t *testing.T) {
	tm := NewTunnelManager("token", "example.com", 8080)
	status := tm.Status()
	if status.Running {
		t.Error("expected tunnel not running")
	}
	if status.Hostname != "example.com" {
		t.Errorf("expected hostname 'example.com', got %q", status.Hostname)
	}
	if status.Uptime != "" {
		t.Errorf("expected empty uptime, got %q", status.Uptime)
	}
}

func TestTunnelManagerHasToken(t *testing.T) {
	tm := NewTunnelManager("token", "example.com", 8080)
	if !tm.HasToken() {
		t.Error("expected HasToken() to return true")
	}

	tm2 := NewTunnelManager("", "example.com", 8080)
	if tm2.HasToken() {
		t.Error("expected HasToken() to return false for empty token")
	}
}

func TestTunnelManagerStartNoToken(t *testing.T) {
	tm := NewTunnelManager("", "example.com", 8080)
	err := tm.Start()
	if err == nil {
		t.Fatal("expected error starting tunnel without token")
	}
	if err.Error() != "no tunnel token configured (set --tunnel-token or CLOUDFLARE_TUNNEL_TOKEN)" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTunnelManagerStopWhenNotRunning(t *testing.T) {
	tm := NewTunnelManager("token", "example.com", 8080)
	err := tm.Stop()
	if err != nil {
		t.Errorf("expected no error stopping non-running tunnel, got: %v", err)
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		name     string
		seconds  int
		expected string
	}{
		{"seconds", 30, "30s"},
		{"minutes", 300, "5m"},
		{"hours", 7200, "2h0m"},
		{"hours and minutes", 5400, "1h30m"},
		{"days", 90000, "1d1h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := time.Duration(tt.seconds) * time.Second
			result := formatUptime(d)
			if result != tt.expected {
				t.Errorf("formatUptime(%v) = %q, want %q", d, result, tt.expected)
			}
		})
	}
}
