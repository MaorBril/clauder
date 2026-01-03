package cmd

import (
	"fmt"
	"os"

	"github.com/maorbril/clauder/internal/telemetry"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "clauder",
	Short: "Claude Code harness for persistent memory and instance communication",
	Long: `Clauder is an MCP server that provides Claude Code with:
- Persistent memory (facts, decisions, context) across sessions
- Multi-instance discovery and messaging across directories
- Automatic context injection based on working directory`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		telemetry.SetVersion(Version)
		telemetry.Init()
		// Track command usage (skip root command itself)
		if cmd.Name() != "clauder" {
			telemetry.TrackCommand(cmd.Name())
		}
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
}

func getDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting home directory: %v\n", err)
		os.Exit(1)
	}
	return home + "/.clauder"
}
