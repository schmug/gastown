package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/web"
	"github.com/steveyegge/gastown/internal/workspace"
)

var (
	dashboardPort       int
	dashboardOpen       bool
	dashboardTunnel     bool
	dashboardTunnelTok  string
	dashboardTunnelHost string
)

var dashboardCmd = &cobra.Command{
	Use:     "dashboard",
	GroupID: GroupDiag,
	Short:   "Start the convoy tracking web dashboard",
	Long: `Start a web server that displays the convoy tracking dashboard.

The dashboard shows real-time convoy status with:
- Convoy list with status indicators
- Progress tracking for each convoy
- Last activity indicator (green/yellow/red)
- Auto-refresh every 30 seconds via htmx
- Optional Cloudflare Tunnel for remote access

Example:
  gt dashboard              # Start on default port 8080
  gt dashboard --port 3000  # Start on port 3000
  gt dashboard --open       # Start and open browser
  gt dashboard --tunnel     # Start with Cloudflare Tunnel`,
	RunE: runDashboard,
}

func init() {
	dashboardCmd.Flags().IntVar(&dashboardPort, "port", 8080, "HTTP port to listen on")
	dashboardCmd.Flags().BoolVar(&dashboardOpen, "open", false, "Open browser automatically")
	dashboardCmd.Flags().BoolVar(&dashboardTunnel, "tunnel", false, "Auto-start Cloudflare Tunnel for remote access")
	dashboardCmd.Flags().StringVar(&dashboardTunnelTok, "tunnel-token", "", "Cloudflare Tunnel token (or set CLOUDFLARE_TUNNEL_TOKEN)")
	dashboardCmd.Flags().StringVar(&dashboardTunnelHost, "tunnel-hostname", "gt.coryrank.in", "Public hostname for the tunnel")
	rootCmd.AddCommand(dashboardCmd)
}

func runDashboard(cmd *cobra.Command, args []string) error {
	// Check if we're in a workspace - if not, run in setup mode
	var handler http.Handler
	var err error
	var tunnelMgr *web.TunnelManager

	// Resolve tunnel token from flag or env
	tunnelToken := dashboardTunnelTok
	if tunnelToken == "" {
		tunnelToken = os.Getenv("CLOUDFLARE_TUNNEL_TOKEN")
	}

	// Create tunnel manager if token is available (even without --tunnel flag,
	// so the UI can still show the toggle button for manual start/stop)
	if tunnelToken != "" {
		tunnelMgr = web.NewTunnelManager(tunnelToken, dashboardTunnelHost, dashboardPort)
	}

	townRoot, wsErr := workspace.FindFromCwdOrError()
	if wsErr != nil {
		// No workspace - run in setup mode
		handler, err = web.NewSetupMux()
		if err != nil {
			return fmt.Errorf("creating setup handler: %w", err)
		}
	} else {
		// In a workspace - run normal dashboard
		fetcher, fetchErr := web.NewLiveConvoyFetcher()
		if fetchErr != nil {
			return fmt.Errorf("creating convoy fetcher: %w", fetchErr)
		}

		// Load web timeouts config (nil-safe: NewDashboardMux applies defaults)
		var webCfg *config.WebTimeoutsConfig
		if ts, loadErr := config.LoadOrCreateTownSettings(config.TownSettingsPath(townRoot)); loadErr == nil {
			webCfg = ts.WebTimeouts
		} else {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: loading town settings: %v (using defaults)\n", loadErr)
		}

		handler, err = web.NewDashboardMux(fetcher, webCfg, tunnelMgr)
		if err != nil {
			return fmt.Errorf("creating dashboard handler: %w", err)
		}
	}

	// Auto-start tunnel if requested
	if dashboardTunnel {
		if tunnelMgr == nil {
			return fmt.Errorf("--tunnel requires a token (set --tunnel-token or CLOUDFLARE_TUNNEL_TOKEN)")
		}
		if startErr := tunnelMgr.Start(); startErr != nil {
			return fmt.Errorf("starting tunnel: %w", startErr)
		}
	}

	// Build the URL
	url := fmt.Sprintf("http://localhost:%d", dashboardPort)

	// Open browser if requested
	if dashboardOpen {
		go openBrowser(url)
	}

	// Start the server with timeouts
	fmt.Print(`
 __       __  ________  __        ______    ______   __       __  ________
|  \  _  |  \|        \|  \      /      \  /      \ |  \     /  \|        \
| $$ / \ | $$| $$$$$$$$| $$     |  $$$$$$\|  $$$$$$\| $$\   /  $$| $$$$$$$$
| $$/  $\| $$| $$__    | $$     | $$   \$$| $$  | $$| $$$\ /  $$$| $$__
| $$  $$$\ $$| $$  \   | $$     | $$      | $$  | $$| $$$$\  $$$$| $$  \
| $$ $$\$$\$$| $$$$$   | $$     | $$   __ | $$  | $$| $$\$$ $$ $$| $$$$$
| $$$$  \$$$$| $$_____ | $$_____| $$__/  \| $$__/ $$| $$ \$$$| $$| $$_____
| $$$    \$$$| $$     \| $$     \\$$    $$ \$$    $$| $$  \$ | $$| $$     \
 \$$      \$$ \$$$$$$$$ \$$$$$$$$ \$$$$$$   \$$$$$$  \$$      \$$ \$$$$$$$$

 ________   ______          ______    ______    ______   ________   ______   __       __  __    __
|        \ /      \        /      \  /      \  /      \ |        \ /      \ |  \  _  |  \|  \  |  \
 \$$$$$$$$|  $$$$$$\      |  $$$$$$\|  $$$$$$\|  $$$$$$\ \$$$$$$$$|  $$$$$$\| $$ / \ | $$| $$\ | $$
   | $$   | $$  | $$      | $$ __\$$| $$__| $$| $$___\$$   | $$   | $$  | $$| $$/  $\| $$| $$$\| $$
   | $$   | $$  | $$      | $$|    \| $$    $$ \$$    \    | $$   | $$  | $$| $$  $$$\ $$| $$$$\ $$
   | $$   | $$  | $$      | $$ \$$$$| $$$$$$$$ _\$$$$$$\   | $$   | $$  | $$| $$ $$\$$\$$| $$\$$ $$
   | $$   | $$__/ $$      | $$__| $$| $$  | $$|  \__| $$   | $$   | $$__/ $$| $$$$  \$$$$| $$ \$$$$
   | $$    \$$    $$       \$$    $$| $$  | $$ \$$    $$   | $$    \$$    $$| $$$    \$$$| $$  \$$$
    \$$     \$$$$$$         \$$$$$$  \$$   \$$  \$$$$$$     \$$     \$$$$$$  \$$      \$$ \$$   \$$

`)
	fmt.Printf("  launching dashboard at %s  •  api: %s/api/  •  ctrl+c to stop\n", url, url)
	if tunnelMgr != nil && tunnelMgr.Status().Running {
		fmt.Printf("  tunnel active: https://%s\n", dashboardTunnelHost)
	}

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", dashboardPort),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case sig := <-sigCh:
		fmt.Printf("\n  received %v, shutting down...\n", sig)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
	}

	// Stop tunnel first (if running)
	if tunnelMgr != nil {
		_ = tunnelMgr.Stop()
	}

	// Gracefully shut down the HTTP server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}

	fmt.Println("  dashboard stopped")
	return nil
}

// openBrowser opens the specified URL in the default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return
	}
	_ = cmd.Start()
}
