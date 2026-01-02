package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	setupGlobal     bool
	setupProject    bool
	setupOpencode   bool
	setupAllowAll   bool
	setupSkipClaude bool
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Add clauder to Claude Code or OpenCode MCP configuration",
	Long: `Adds clauder as an MCP server to Claude Code or OpenCode configuration.

By default, adds to the global Claude Code config (~/.claude.json).
Use --project to add to .mcp.json in current directory instead.
Use --opencode to add to opencode.json for OpenCode integration.`,
	RunE: runSetup,
}

func init() {
	setupCmd.Flags().BoolVarP(&setupGlobal, "global", "g", false, "Add to global Claude config (~/.claude.json)")
	setupCmd.Flags().BoolVarP(&setupProject, "project", "p", false, "Add to project config (.mcp.json)")
	setupCmd.Flags().BoolVarP(&setupOpencode, "opencode", "o", false, "Add to OpenCode config (opencode.json)")
	setupCmd.Flags().BoolVarP(&setupAllowAll, "allow-all", "a", false, "Pre-approve all clauder commands (no permission prompts)")
	setupCmd.Flags().BoolVar(&setupSkipClaude, "skip-claude-md", false, "Skip adding instructions to CLAUDE.md")
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
	if !setupGlobal && !setupProject && !setupOpencode {
		// Default to global
		setupGlobal = true
	}

	// OpenCode setup is simpler - doesn't need permission prompts or CLAUDE.md
	if setupOpencode {
		return setupOpencodeConfig(binaryPath)
	}

	// Ask about pre-approving commands if not specified via flag
	if !setupAllowAll {
		setupAllowAll = askYesNo("Pre-approve all clauder commands? (no permission prompts)")
	}

	// Setup MCP config
	var configErr error
	if setupProject {
		configErr = setupProjectConfig(binaryPath)
	} else {
		configErr = setupGlobalConfig(binaryPath)
	}
	if configErr != nil {
		return configErr
	}

	// Setup CLAUDE.md unless skipped
	if !setupSkipClaude {
		if err := setupClaudeMD(); err != nil {
			fmt.Printf("Warning: failed to update CLAUDE.md: %v\n", err)
		}
	}

	return nil
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

	// Add permission rules if user wants to pre-approve all commands
	if setupAllowAll {
		addPermissionRules(config)
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
	if setupAllowAll {
		fmt.Println("Pre-approved all clauder MCP commands.")
	}
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

func setupOpencodeConfig(binaryPath string) error {
	configPath := "opencode.json"

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

	// Add schema if not present
	if _, ok := config["$schema"]; !ok {
		config["$schema"] = "https://opencode.ai/config.json"
	}

	// Get or create mcp section
	mcp, ok := config["mcp"].(map[string]interface{})
	if !ok {
		mcp = make(map[string]interface{})
	}

	// Add clauder with OpenCode's format
	mcp["clauder"] = map[string]interface{}{
		"type":    "local",
		"command": []string{binaryPath, "serve"},
		"enabled": true,
	}
	config["mcp"] = mcp

	// Write back with pretty formatting
	output, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, output, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("Added clauder to %s\n", configPath)
	fmt.Printf("Binary: %s\n", binaryPath)
	fmt.Println("\nRestart OpenCode to load the new MCP server.")
	return nil
}

// askYesNo prompts the user with a yes/no question
func askYesNo(question string) bool {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s [y/N]: ", question)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

// addPermissionRules adds MCP tool permissions to allow clauder commands without prompts
func addPermissionRules(config map[string]interface{}) {
	// Get or create permissions array
	permissions, ok := config["permissions"].([]interface{})
	if !ok {
		permissions = []interface{}{}
	}

	// Clauder MCP tools to allow
	clauderTools := []string{
		"mcp__clauder__remember",
		"mcp__clauder__recall",
		"mcp__clauder__get_context",
		"mcp__clauder__list_instances",
		"mcp__clauder__send_message",
		"mcp__clauder__get_messages",
	}

	// Add permission rules for each tool
	for _, tool := range clauderTools {
		rule := map[string]interface{}{
			"tool":  tool,
			"allow": true,
		}
		permissions = append(permissions, rule)
	}

	config["permissions"] = permissions
}

// setupClaudeMD adds clauder instructions to CLAUDE.md
func setupClaudeMD() error {
	claudeMDPath := "CLAUDE.md"

	clauderInstructions := `
## Clauder - Persistent Memory MCP

This project uses **clauder** for persistent memory across Claude Code sessions.

### Available Tools
- **mcp__clauder__remember**: Store facts, decisions, or context
- **mcp__clauder__recall**: Search and retrieve stored facts
- **mcp__clauder__get_context**: Load all relevant context for this directory
- **mcp__clauder__list_instances**: List other running Claude Code sessions
- **mcp__clauder__send_message**: Send messages to other instances
- **mcp__clauder__get_messages**: Check for incoming messages

### Usage Guidelines
1. **At session start**: Call ` + "`get_context`" + ` to load persistent memory
2. **Store important info**: Use ` + "`remember`" + ` for decisions, architecture notes, preferences
3. **Periodic message check**: Call ` + "`get_messages`" + ` periodically to check for messages from other instances
4. **Cross-instance communication**: Use ` + "`list_instances`" + ` and ` + "`send_message`" + ` to coordinate with other sessions
`

	// Read existing CLAUDE.md or create new
	var content string
	data, err := os.ReadFile(claudeMDPath)
	if err == nil {
		content = string(data)
		// Check if clauder section already exists
		if strings.Contains(content, "## Clauder - Persistent Memory MCP") {
			fmt.Println("CLAUDE.md already contains clauder instructions.")
			return nil
		}
		// Append to existing content
		content = content + "\n" + clauderInstructions
	} else if os.IsNotExist(err) {
		// Create new file
		content = "# Project Instructions\n" + clauderInstructions
	} else {
		return fmt.Errorf("failed to read CLAUDE.md: %w", err)
	}

	if err := os.WriteFile(claudeMDPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write CLAUDE.md: %w", err)
	}

	fmt.Printf("Added clauder instructions to %s\n", claudeMDPath)
	return nil
}
