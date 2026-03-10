package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/orsi-bit/openclawder/internal/telemetry"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

var (
	setupGlobal     bool
	setupProject    bool
	setupOpencode   bool
	setupCodex      bool
	setupGemini     bool
	setupCursor     bool
	setupWindsurf   bool
	setupOpenclaw   bool
	setupAllowAll   bool
	setupSkipClaude bool
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Add openclawder MCP server to AI coding tools",
	Long: `Adds openclawder as an MCP server to various AI coding tool configurations.

By default, adds to the global Claude Code config (~/.claude.json).

Supported tools:
  --project   Claude Code project config (.mcp.json)
  --opencode  OpenCode (opencode.json)
  --codex     OpenAI Codex CLI (~/.codex/config.toml)
  --gemini    Google Gemini CLI (~/.gemini/settings.json)
  --cursor    Cursor editor (~/.cursor/mcp.json)
  --windsurf  Windsurf editor (~/.codeium/windsurf/mcp_config.json)
  --openclaw  OpenClaw agent workspace (~/.openclaw/)`,
	RunE: runSetup,
}

func init() {
	setupCmd.Flags().BoolVarP(&setupGlobal, "global", "g", false, "Add to global Claude config (~/.claude.json)")
	setupCmd.Flags().BoolVarP(&setupProject, "project", "p", false, "Add to project config (.mcp.json)")
	setupCmd.Flags().BoolVarP(&setupOpencode, "opencode", "o", false, "Add to OpenCode config (opencode.json)")
	setupCmd.Flags().BoolVar(&setupCodex, "codex", false, "Add to OpenAI Codex config (~/.codex/config.toml)")
	setupCmd.Flags().BoolVar(&setupGemini, "gemini", false, "Add to Google Gemini CLI config (~/.gemini/settings.json)")
	setupCmd.Flags().BoolVar(&setupCursor, "cursor", false, "Add to Cursor editor config (~/.cursor/mcp.json)")
	setupCmd.Flags().BoolVar(&setupWindsurf, "windsurf", false, "Add to Windsurf editor config (~/.codeium/windsurf/mcp_config.json)")
	setupCmd.Flags().BoolVar(&setupOpenclaw, "openclaw", false, "Add to OpenClaw agent workspace (~/.openclaw/)")
	setupCmd.Flags().BoolVarP(&setupAllowAll, "allow-all", "a", false, "Pre-approve all openclawder commands (no permission prompts)")
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
		return fmt.Errorf("failed to find openclawder binary: %w", err)
	}

	// Determine which config file to use
	if !setupGlobal && !setupProject && !setupOpencode && !setupCodex && !setupGemini && !setupCursor && !setupWindsurf && !setupOpenclaw {
		// Default to global
		setupGlobal = true
	}

	// Handle OpenClaw setup
	if setupOpenclaw {
		telemetry.TrackSetup("openclaw")
		return setupOpenClawConfig(binaryPath)
	}

	// Handle non-Claude Code setups (simpler - no permission prompts or CLAUDE.md)
	if setupOpencode {
		telemetry.TrackSetup("opencode")
		return setupOpencodeConfig(binaryPath)
	}
	if setupCodex {
		telemetry.TrackSetup("codex")
		return setupCodexConfig(binaryPath)
	}
	if setupGemini {
		telemetry.TrackSetup("gemini")
		return setupGeminiConfig(binaryPath)
	}
	if setupCursor {
		telemetry.TrackSetup("cursor")
		return setupCursorConfig(binaryPath)
	}
	if setupWindsurf {
		telemetry.TrackSetup("windsurf")
		return setupWindsurfConfig(binaryPath)
	}

	// Ask about pre-approving commands if not specified via flag
	if !setupAllowAll {
		setupAllowAll = askYesNo("Pre-approve all openclawder commands? (no permission prompts)")
	}

	// Setup MCP config
	var configErr error
	if setupProject {
		telemetry.TrackSetup("claude-code-project")
		configErr = setupProjectConfig(binaryPath)
	} else {
		telemetry.TrackSetup("claude-code-global")
		configErr = setupGlobalConfig(binaryPath)
	}
	if configErr != nil {
		return configErr
	}

	// Setup CLAUDE.md unless skipped
	if !setupSkipClaude {
		if err := setupOpenclawderMD(); err != nil {
			fmt.Printf("Warning: failed to update CLAUDE.md: %v\n", err)
		}
	}

	return nil
}

func getBinaryPath() (string, error) {
	// First try to find in PATH
	path, err := exec.LookPath("openclawder")
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
	mcpServers["openclawder"] = map[string]interface{}{
		"command": binaryPath,
		"args":    []string{"serve"},
	}
	config["mcpServers"] = mcpServers

	// Always auto-approve get_context (read-only, safe)
	addGetContextPermission(config)

	// Add permission rules for all commands if user wants
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

	fmt.Printf("Added openclawder to %s\n", configPath)
	fmt.Printf("Binary: %s\n", binaryPath)
	if setupAllowAll {
		fmt.Println("Pre-approved all openclawder MCP commands.")
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
	config.McpServers["openclawder"] = MCPServer{
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

	fmt.Printf("Added openclawder to %s\n", configPath)
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
	mcp["openclawder"] = map[string]interface{}{
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

	fmt.Printf("Added openclawder to %s\n", configPath)
	fmt.Printf("Binary: %s\n", binaryPath)
	fmt.Println("\nRestart OpenCode to load the new MCP server.")
	return nil
}

func setupCodexConfig(binaryPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(home, ".codex")
	configPath := filepath.Join(configDir, "config.toml")

	// Create .codex directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Read existing config or create new one
	config := make(map[string]interface{})

	data, err := os.ReadFile(configPath)
	if err == nil {
		if err := toml.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("failed to parse existing config: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read config: %w", err)
	}

	// Get or create mcp_servers section
	mcpServers, ok := config["mcp_servers"].(map[string]interface{})
	if !ok {
		mcpServers = make(map[string]interface{})
	}

	// Add clauder with Codex's format
	mcpServers["openclawder"] = map[string]interface{}{
		"command": binaryPath,
		"args":    []string{"serve"},
	}
	config["mcp_servers"] = mcpServers

	// Write back with TOML formatting
	output, err := toml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, output, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("Added openclawder to %s\n", configPath)
	fmt.Printf("Binary: %s\n", binaryPath)
	fmt.Println("\nRestart Codex to load the new MCP server.")
	return nil
}

func setupGeminiConfig(binaryPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(home, ".gemini")
	configPath := filepath.Join(configDir, "settings.json")

	// Create .gemini directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

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

	// Get or create mcpServers section
	mcpServers, ok := config["mcpServers"].(map[string]interface{})
	if !ok {
		mcpServers = make(map[string]interface{})
	}

	// Add clauder with Gemini CLI's format
	mcpServers["openclawder"] = map[string]interface{}{
		"command": binaryPath,
		"args":    []string{"serve"},
	}
	config["mcpServers"] = mcpServers

	// Write back with pretty formatting
	output, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, output, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("Added openclawder to %s\n", configPath)
	fmt.Printf("Binary: %s\n", binaryPath)
	fmt.Println("\nRestart Gemini CLI to load the new MCP server.")
	return nil
}

func setupCursorConfig(binaryPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(home, ".cursor")
	configPath := filepath.Join(configDir, "mcp.json")

	// Create .cursor directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

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
	config.McpServers["openclawder"] = MCPServer{
		Command: binaryPath,
		Args:    []string{"serve"},
	}

	// Write back with pretty formatting
	output, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, output, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("Added openclawder to %s\n", configPath)
	fmt.Printf("Binary: %s\n", binaryPath)
	fmt.Println("\nRestart Cursor to load the new MCP server.")
	return nil
}

func setupWindsurfConfig(binaryPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(home, ".codeium", "windsurf")
	configPath := filepath.Join(configDir, "mcp_config.json")

	// Create .codeium/windsurf directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

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
	config.McpServers["openclawder"] = MCPServer{
		Command: binaryPath,
		Args:    []string{"serve"},
	}

	// Write back with pretty formatting
	output, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, output, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("Added openclawder to %s\n", configPath)
	fmt.Printf("Binary: %s\n", binaryPath)
	fmt.Println("\nRestart Windsurf to load the new MCP server.")
	return nil
}

// setupOpenClawConfig configures openclawder for OpenClaw agent workspaces
func setupOpenClawConfig(binaryPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	openclawDir := filepath.Join(home, ".openclaw")
	configPath := filepath.Join(openclawDir, "openclaw.json")

	// Discover workspace paths
	workspaces := discoverOpenClawWorkspaces(configPath, openclawDir)

	if len(workspaces) == 0 {
		fmt.Println("No OpenClaw workspaces found. Using default: ~/.openclaw/workspace/")
		defaultWs := filepath.Join(openclawDir, "workspace")
		workspaces = []string{defaultWs}
	}

	var configured []string

	for _, ws := range workspaces {
		// Create workspace dir if it doesn't exist
		if err := os.MkdirAll(ws, 0755); err != nil {
			fmt.Printf("Warning: failed to create workspace %s: %v\n", ws, err)
			continue
		}

		// Append CLI instructions to AGENTS.md
		if err := setupAgentsMD(ws, binaryPath); err != nil {
			fmt.Printf("Warning: failed to update AGENTS.md in %s: %v\n", ws, err)
		}

		// Setup CLAUDE.md in the workspace for spawned Claude Code sub-agents
		if err := setupOpenclawderMDAt(ws); err != nil {
			fmt.Printf("Warning: failed to update CLAUDE.md in %s: %v\n", ws, err)
		}

		configured = append(configured, ws)
	}

	// Write ~/OPENCLAW.md with instructions for the main OpenClaw agent
	openclawMDPath := filepath.Join(home, "OPENCLAW.md")
	if err := writeOpenClawMD(openclawMDPath, binaryPath); err != nil {
		fmt.Printf("Warning: failed to write %s: %v\n", openclawMDPath, err)
	}

	// Print success summary
	fmt.Println("\n=== OpenClaw + openclawder setup complete ===")
	fmt.Printf("Binary: %s\n", binaryPath)
	fmt.Println("\nConfigured workspaces:")
	for _, ws := range configured {
		fmt.Printf("  - %s (AGENTS.md + CLAUDE.md)\n", ws)
	}
	fmt.Printf("\nAgent instructions: %s\n", openclawMDPath)
	fmt.Println("\nThe main OpenClaw agent can use openclawder CLI commands.")
	fmt.Println("Spawned Claude Code sub-agents get MCP tools via CLAUDE.md.")

	return nil
}

// discoverOpenClawWorkspaces parses ~/.openclaw/openclaw.json to find workspace paths,
// or falls back to scanning for workspace directories.
func discoverOpenClawWorkspaces(configPath, openclawDir string) []string {
	var workspaces []string

	data, err := os.ReadFile(configPath)
	if err == nil {
		// Strip single-line // comments and trailing commas for JSON5 compat
		cleaned := stripJSON5(string(data))

		var config map[string]interface{}
		if err := json.Unmarshal([]byte(cleaned), &config); err == nil {
			workspaces = extractWorkspacePaths(config)
		}
	}

	if len(workspaces) > 0 {
		// Expand ~ in paths
		home, _ := os.UserHomeDir()
		for i, ws := range workspaces {
			if strings.HasPrefix(ws, "~") {
				workspaces[i] = filepath.Join(home, ws[1:])
			}
		}
		return workspaces
	}

	// Fallback: scan for workspace-* directories
	entries, err := os.ReadDir(openclawDir)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "workspace") {
			workspaces = append(workspaces, filepath.Join(openclawDir, entry.Name()))
		}
	}

	return workspaces
}

// stripJSON5 removes single-line // comments and trailing commas from JSON5 text
func stripJSON5(input string) string {
	// Remove single-line comments (// ...) but not inside strings
	re := regexp.MustCompile(`(?m)^\s*//.*$`)
	result := re.ReplaceAllString(input, "")

	// Also remove inline comments after values
	reInline := regexp.MustCompile(`(?m)(["}\]0-9true false null]),?\s*//.*$`)
	result = reInline.ReplaceAllString(result, "$1,")

	// Remove trailing commas before } or ]
	reTrailing := regexp.MustCompile(`,\s*([}\]])`)
	result = reTrailing.ReplaceAllString(result, "$1")

	return result
}

// extractWorkspacePaths extracts workspace paths from OpenClaw config
func extractWorkspacePaths(config map[string]interface{}) []string {
	var paths []string

	agents, ok := config["agents"].(map[string]interface{})
	if !ok {
		return nil
	}

	// Check agents.defaults.workspace
	if defaults, ok := agents["defaults"].(map[string]interface{}); ok {
		if ws, ok := defaults["workspace"].(string); ok {
			paths = append(paths, ws)
		}
	}

	// Check agents.list[*].workspace
	if list, ok := agents["list"].([]interface{}); ok {
		for _, item := range list {
			if agent, ok := item.(map[string]interface{}); ok {
				if ws, ok := agent["workspace"].(string); ok {
					// Avoid duplicates
					found := false
					for _, p := range paths {
						if p == ws {
							found = true
							break
						}
					}
					if !found {
						paths = append(paths, ws)
					}
				}
			}
		}
	}

	return paths
}

// setupAgentsMD appends openclawder CLI instructions to AGENTS.md in the given workspace
func setupAgentsMD(workspaceDir, binaryPath string) error {
	agentsMDPath := filepath.Join(workspaceDir, "AGENTS.md")

	instructions := `
## OpenClawder CLI — Persistent Memory & Cross-Instance Coordination

The main OpenClaw agent uses the openclawder CLI directly (not MCP tools).
Binary location: ` + binaryPath + `

### Available Commands
- ` + "`openclawder instances`" + ` — List running Claude Code sub-agent sessions
- ` + "`openclawder send <instance-id> \"message\"`" + ` — Send a message to a running sub-agent
- ` + "`openclawder recall \"query\"`" + ` — Search stored facts and decisions
- ` + "`openclawder remember \"fact\"`" + ` — Store a fact persistently
- ` + "`openclawder status`" + ` — View stored facts count and running instances
- ` + "`openclawder messages`" + ` — Check incoming messages

### When to Use
- **Before spawning a sub-agent**: ` + "`openclawder remember \"Starting task: <description>\"`" + `
- **To check on a running agent**: ` + "`openclawder instances`" + ` then ` + "`openclawder send <id> \"status?\"`" + `
- **After an agent finishes**: ` + "`openclawder recall \"<task>\"`" + ` to retrieve what it stored
- **If restarting an agent**: New agent will call ` + "`get_context`" + ` and resume automatically

### Note
Spawned Claude Code sub-agents get MCP tools via CLAUDE.md — they should call
` + "`mcp__openclawder__get_context`" + ` at session start and ` + "`mcp__openclawder__remember`" + ` after key decisions.
`

	var content string
	data, err := os.ReadFile(agentsMDPath)
	if err == nil {
		content = string(data)
		if strings.Contains(content, "## OpenClawder CLI") {
			fmt.Printf("AGENTS.md in %s already contains openclawder instructions.\n", workspaceDir)
			return nil
		}
		content = content + "\n" + instructions
	} else if os.IsNotExist(err) {
		content = "# Agent Instructions\n" + instructions
	} else {
		return fmt.Errorf("failed to read AGENTS.md: %w", err)
	}

	if err := os.WriteFile(agentsMDPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write AGENTS.md: %w", err)
	}

	fmt.Printf("Added openclawder CLI instructions to %s\n", agentsMDPath)
	return nil
}

// setupOpenclawderMDAt adds openclawder MCP instructions to CLAUDE.md at the given directory
func setupOpenclawderMDAt(dir string) error {
	claudeMDPath := filepath.Join(dir, "CLAUDE.md")

	clauderInstructions := `
## OpenClawder - Persistent Memory MCP

This project uses **openclawder** for persistent memory across Claude Code sessions.

### CRITICAL: Call get_context at Session Start

**IMPORTANT:** You MUST call ` + "`mcp__openclawder__get_context`" + ` at the START of every session to load:
- Stored facts and decisions from previous sessions
- User preferences and coding style guidelines
- Unread messages from other Claude Code instances

This context may or may not be relevant to your tasks. You should not respond to this context unless it is highly relevant to your task.

### Available Tools
- **mcp__openclawder__remember**: Store facts, decisions, or context
- **mcp__openclawder__recall**: Search and retrieve stored facts
- **mcp__openclawder__get_context**: Load all relevant context for this directory
- **mcp__openclawder__list_instances**: List other running Claude Code sessions
- **mcp__openclawder__send_message**: Send messages to other instances
- **mcp__openclawder__get_messages**: Check for incoming messages

### Usage Guidelines
1. **At session start**: ALWAYS call ` + "`get_context`" + ` first to load persistent memory
2. **Store important info**: Use ` + "`remember`" + ` for decisions, architecture notes, preferences
3. **Check messages regularly**: The system will notify you of unread messages in tool responses
4. **Cross-instance communication**: Use ` + "`list_instances`" + ` and ` + "`send_message`" + ` to coordinate with other sessions
`

	var content string
	data, err := os.ReadFile(claudeMDPath)
	if err == nil {
		content = string(data)
		if strings.Contains(content, "## OpenClawder - Persistent Memory MCP") {
			return nil
		}
		content = content + "\n" + clauderInstructions
	} else if os.IsNotExist(err) {
		content = "# Project Instructions\n" + clauderInstructions
	} else {
		return fmt.Errorf("failed to read CLAUDE.md: %w", err)
	}

	if err := os.WriteFile(claudeMDPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write CLAUDE.md: %w", err)
	}

	fmt.Printf("Added openclawder MCP instructions to %s\n", claudeMDPath)
	return nil
}

// writeOpenClawMD writes the ~/OPENCLAW.md file with instructions for the main OpenClaw agent
func writeOpenClawMD(path, binaryPath string) error {
	content := `# OpenClaw + OpenClawder Integration

## What is openclawder?
Persistent memory and cross-instance coordination for AI agents. CLI available at: ` + binaryPath + `

## For the Main OpenClaw Agent (you)
You cannot call MCP tools directly, but you CAN use the openclawder CLI via exec:

- ` + "`openclawder status`" + ` — see stored facts and running instances
- ` + "`openclawder instances`" + ` — list running Claude Code sessions
- ` + "`openclawder send <instance-id> \"message\"`" + ` — send a message to a running agent
- ` + "`openclawder messages`" + ` — check incoming messages
- ` + "`openclawder recall \"query\"`" + ` — search stored facts
- ` + "`openclawder remember \"fact\"`" + ` — store a fact persistently

## For Claude Code Sub-Agents (Boaz, etc.)
They get MCP tools automatically via CLAUDE.md. They should call ` + "`mcp__openclawder__get_context`" + ` at session start and ` + "`mcp__openclawder__remember`" + ` after key decisions.

## When to Use
- Before spawning an agent: ` + "`openclawder remember \"Starting task: <description>\"`" + `
- To check on a running agent: ` + "`openclawder instances`" + ` then ` + "`openclawder send <id> \"status update?\"`" + `
- After an agent finishes: ` + "`openclawder recall \"<task>\"`" + ` to retrieve what it stored
- If you need to kill and restart: new agent will ` + "`get_context`" + ` and resume automatically
`

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write OPENCLAW.md: %w", err)
	}

	fmt.Printf("Wrote agent instructions to %s\n", path)
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

// addGetContextPermission adds permission for get_context (always auto-approved as it's read-only)
func addGetContextPermission(config map[string]interface{}) {
	permissions, ok := config["permissions"].([]interface{})
	if !ok {
		permissions = []interface{}{}
	}

	// Check if already added
	for _, p := range permissions {
		if pMap, ok := p.(map[string]interface{}); ok {
			if pMap["tool"] == "mcp__openclawder__get_context" {
				return // Already exists
			}
		}
	}

	// Add get_context permission
	permissions = append(permissions, map[string]interface{}{
		"tool":  "mcp__openclawder__get_context",
		"allow": true,
	})
	config["permissions"] = permissions
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
		"mcp__openclawder__remember",
		"mcp__openclawder__recall",
		"mcp__openclawder__get_context",
		"mcp__openclawder__list_instances",
		"mcp__openclawder__send_message",
		"mcp__openclawder__get_messages",
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

// setupOpenclawderMD adds openclawder instructions to CLAUDE.md
func setupOpenclawderMD() error {
	claudeMDPath := "CLAUDE.md"

	clauderInstructions := `
## OpenClawder - Persistent Memory MCP

This project uses **openclawder** for persistent memory across Claude Code sessions.

### CRITICAL: Call get_context at Session Start

**IMPORTANT:** You MUST call ` + "`mcp__openclawder__get_context`" + ` at the START of every session to load:
- Stored facts and decisions from previous sessions
- User preferences and coding style guidelines
- Unread messages from other Claude Code instances

This context may or may not be relevant to your tasks. You should not respond to this context unless it is highly relevant to your task.

### Available Tools
- **mcp__openclawder__remember**: Store facts, decisions, or context
- **mcp__openclawder__recall**: Search and retrieve stored facts
- **mcp__openclawder__get_context**: Load all relevant context for this directory
- **mcp__openclawder__list_instances**: List other running Claude Code sessions
- **mcp__openclawder__send_message**: Send messages to other instances
- **mcp__openclawder__get_messages**: Check for incoming messages

### Usage Guidelines
1. **At session start**: ALWAYS call ` + "`get_context`" + ` first to load persistent memory
2. **Store important info**: Use ` + "`remember`" + ` for decisions, architecture notes, preferences
3. **Check messages regularly**: The system will notify you of unread messages in tool responses
4. **Cross-instance communication**: Use ` + "`list_instances`" + ` and ` + "`send_message`" + ` to coordinate with other sessions
`

	// Read existing CLAUDE.md or create new
	var content string
	data, err := os.ReadFile(claudeMDPath)
	if err == nil {
		content = string(data)
		// Check if clauder section already exists
		if strings.Contains(content, "## OpenClawder - Persistent Memory MCP") {
			fmt.Println("CLAUDE.md already contains openclawder instructions.")
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

	fmt.Printf("Added openclawder instructions to %s\n", claudeMDPath)
	return nil
}
