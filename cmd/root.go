package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/orsi-bit/openclawder/internal/telemetry"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "openclawder",
	Short: "AI agent harness for persistent memory and instance communication",
	Long: `OpenClawder is an MCP server that provides Claude Code with:
- Persistent memory (facts, decisions, context) across sessions
- Multi-instance discovery and messaging across directories
- Automatic context injection based on working directory`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		fmt.Fprintf(os.Stderr, "[openclawder] PersistentPreRun starting...\n")
		telemetry.SetVersion(Version)
		fmt.Fprintf(os.Stderr, "[openclawder] Initializing telemetry...\n")
		telemetry.Init()
		fmt.Fprintf(os.Stderr, "[openclawder] Telemetry initialized\n")
		// Track command usage (skip root command itself)
		if cmd.Name() != "openclawder" {
			telemetry.TrackCommand(cmd.Name())
		}
		fmt.Fprintf(os.Stderr, "[openclawder] PersistentPreRun complete\n")
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		telemetry.Close()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(rememberCmd)
	rootCmd.AddCommand(recallCmd)
	rootCmd.AddCommand(instancesCmd)
	rootCmd.AddCommand(sendCmd)
	rootCmd.AddCommand(messagesCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(uiCmd)
	rootCmd.AddCommand(wrapCmd)
}

func getDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting home directory: %v\n", err)
		os.Exit(1)
	}
	return home + "/.clauder"
}

// generateDirectoryID creates a stable directory ID based on path
// This is used for grouping instances in the same directory
func generateDirectoryID(directory string) string {
	hash := sha256.Sum256([]byte(directory))
	// Use first 16 bytes (32 hex chars) for a readable ID
	return hex.EncodeToString(hash[:16])
}
