package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	setupGlobal  bool
	setupProject bool
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Add clauder to Claude Code MCP configuration",
	Long: `Adds clauder as an MCP server to Claude Code configuration.

By default, adds to the global config (~/.claude.json).
Use --project to add to .mcp.json in current directory instead.`,
	RunE: runSetup,
}

func init() {
	setupCmd.Flags().BoolVarP(&setupGlobal, "global", "g", false, "Add to global Claude config (~/.claude.json)")
	setupCmd.Flags().BoolVarP(&setupProject, "project", "p", false, "Add to project config (.mcp.json)")
}

type MCPConfig struct {
	McpServers map[string]MCPServer `json:"mcpServers"`
}

type MCPServer struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type ClaudeConfig struct {
	McpServers map[string]MCPServer `json:"mcpServers,omitempty"`
	// Preserve other fields
	Other map[string]json.RawMessage `json:"-"`
}

func runSetup(cmd *cobra.Command, args []string) error {
	// Find the clauder binary path
	binaryPath, err := getBinaryPath()
	if err != nil {
		return fmt.Errorf("failed to find clauder binary: %w", err)
	}

	// Determine which config file to use
	if !setupGlobal && !setupProject {
		// Default to global
		setupGlobal = true
	}

	if setupProject {
		return setupProjectConfig(binaryPath)
	}
	return setupGlobalConfig(binaryPath)
}

func getBinaryPath() (string, error) {
	// First try to find in PATH
	path, err := exec.LookPath("clauder")
	if err == nil {
		return filepath.Abs(path)
	}

	// Fall back to current executable
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Abs(exe)
}

func setupGlobalConfig(binaryPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configPath := filepath.Join(home, ".claude.json")

	// Read existing config or create new one
	config := make(map[string]interface{})

	data, err := os.ReadFile(configPath)
	if err == nil {
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("failed to parse existing config: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read config: %w", err)
	}

	// Get or create mcpServers
	mcpServers, ok := config["mcpServers"].(map[string]interface{})
	if !ok {
		mcpServers = make(map[string]interface{})
	}

	// Add clauder
	mcpServers["clauder"] = map[string]interface{}{
		"command": binaryPath,
		"args":    []string{"serve"},
	}
	config["mcpServers"] = mcpServers

	// Write back
	output, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, output, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("Added clauder to %s\n", configPath)
	fmt.Printf("Binary: %s\n", binaryPath)
	fmt.Println("\nRestart Claude Code to load the new MCP server.")
	return nil
}

func setupProjectConfig(binaryPath string) error {
	configPath := ".mcp.json"

	// Read existing config or create new one
	config := MCPConfig{
		McpServers: make(map[string]MCPServer),
	}

	data, err := os.ReadFile(configPath)
	if err == nil {
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("failed to parse existing config: %w", err)
		}
		if config.McpServers == nil {
			config.McpServers = make(map[string]MCPServer)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read config: %w", err)
	}

	// Add clauder
	config.McpServers["clauder"] = MCPServer{
		Command: binaryPath,
		Args:    []string{"serve"},
	}

	// Write back
	output, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, output, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("Added clauder to %s\n", configPath)
	fmt.Printf("Binary: %s\n", binaryPath)
	fmt.Println("\nRestart Claude Code to load the new MCP server.")
	return nil
}
