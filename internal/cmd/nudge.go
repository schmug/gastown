package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/tmux"
)

func init() {
	rootCmd.AddCommand(nudgeCmd)
}

var nudgeCmd = &cobra.Command{
	Use:   "nudge <session> <message>",
	Short: "Send a message to a Claude session reliably",
	Long: `Sends a message to a tmux session running Claude Code.

Uses a reliable delivery pattern:
1. Sends text in literal mode (-l flag)
2. Waits 500ms for paste to complete
3. Sends Enter as a separate command

This is the ONLY way to send messages to Claude sessions.
Do not use raw tmux send-keys elsewhere.`,
	Args: cobra.ExactArgs(2),
	RunE: runNudge,
}

func runNudge(cmd *cobra.Command, args []string) error {
	session := args[0]
	message := args[1]

	t := tmux.NewTmux()

	// Verify session exists
	exists, err := t.HasSession(session)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !exists {
		return fmt.Errorf("session %q not found", session)
	}

	// Send message with reliable pattern
	if err := t.NudgeSession(session, message); err != nil {
		return fmt.Errorf("nudging session: %w", err)
	}

	fmt.Printf("✓ Nudged %s\n", session)
	return nil
}
