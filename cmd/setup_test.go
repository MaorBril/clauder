package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

// Test helpers

func setupTempHome(t *testing.T) (string, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "clauder-setup-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}
	return tmpDir, cleanup
}

func setupTempProject(t *testing.T) (string, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "clauder-project-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	originalWd, err := os.Getwd()
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("failed to change directory: %v", err)
	}
	cleanup := func() {
		_ = os.Chdir(originalWd)
		_ = os.RemoveAll(tmpDir)
	}
	return tmpDir, cleanup
}

// setTestHome sets HOME env var for testing and returns cleanup func
func setTestHome(t *testing.T, tmpHome string) func() {
	t.Helper()
	originalHome := os.Getenv("HOME")
	if err := os.Setenv("HOME", tmpHome); err != nil {
		t.Fatalf("failed to set HOME: %v", err)
	}
	return func() {
		_ = os.Setenv("HOME", originalHome)
	}
}

// writeTestFile writes data to a file, failing the test on error
func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("failed to write test file %s: %v", path, err)
	}
}

// mkdirTest creates a directory, failing the test on error
func mkdirTest(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("failed to create directory %s: %v", path, err)
	}
}

// Claude Code Global Config Tests

func TestSetupGlobalConfig_NewFile(t *testing.T) {
	tmpHome, cleanup := setupTempHome(t)
	defer cleanup()

	restoreHome := setTestHome(t, tmpHome)
	defer restoreHome()

	binaryPath := "/usr/local/bin/clauder"
	setupAllowAll = false

	err := setupGlobalConfig(binaryPath)
	if err != nil {
		t.Fatalf("setupGlobalConfig failed: %v", err)
	}

	// Verify file was created
	configPath := filepath.Join(tmpHome, ".claude.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	mcpServers, ok := config["mcpServers"].(map[string]interface{})
	if !ok {
		t.Fatal("expected mcpServers in config")
	}

	clauder, ok := mcpServers["clauder"].(map[string]interface{})
	if !ok {
		t.Fatal("expected clauder in mcpServers")
	}

	if clauder["command"] != binaryPath {
		t.Errorf("expected command %s, got %s", binaryPath, clauder["command"])
	}

	args, ok := clauder["args"].([]interface{})
	if !ok || len(args) != 1 || args[0] != "serve" {
		t.Errorf("expected args [serve], got %v", clauder["args"])
	}
}

func TestSetupGlobalConfig_MergeExisting(t *testing.T) {
	tmpHome, cleanup := setupTempHome(t)
	defer cleanup()

	restoreHome := setTestHome(t, tmpHome)
	defer restoreHome()

	// Create existing config with other settings
	configPath := filepath.Join(tmpHome, ".claude.json")
	existingConfig := map[string]interface{}{
		"someOtherSetting": "value",
		"mcpServers": map[string]interface{}{
			"existingServer": map[string]interface{}{
				"command": "existing",
				"args":    []string{"arg1"},
			},
		},
	}
	data, _ := json.MarshalIndent(existingConfig, "", "  ")
	writeTestFile(t, configPath, data)

	binaryPath := "/usr/local/bin/clauder"
	setupAllowAll = false

	err := setupGlobalConfig(binaryPath)
	if err != nil {
		t.Fatalf("setupGlobalConfig failed: %v", err)
	}

	// Verify merged config
	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	// Check existing setting preserved
	if config["someOtherSetting"] != "value" {
		t.Error("expected existing setting to be preserved")
	}

	mcpServers := config["mcpServers"].(map[string]interface{})

	// Check existing server preserved
	if _, ok := mcpServers["existingServer"]; !ok {
		t.Error("expected existingServer to be preserved")
	}

	// Check clauder added
	if _, ok := mcpServers["clauder"]; !ok {
		t.Error("expected clauder to be added")
	}
}

func TestSetupGlobalConfig_WithPermissions(t *testing.T) {
	tmpHome, cleanup := setupTempHome(t)
	defer cleanup()

	restoreHome := setTestHome(t, tmpHome)
	defer restoreHome()

	binaryPath := "/usr/local/bin/clauder"
	setupAllowAll = true
	defer func() { setupAllowAll = false }()

	err := setupGlobalConfig(binaryPath)
	if err != nil {
		t.Fatalf("setupGlobalConfig failed: %v", err)
	}

	configPath := filepath.Join(tmpHome, ".claude.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	permissions, ok := config["permissions"].([]interface{})
	if !ok {
		t.Fatal("expected permissions in config")
	}

	if len(permissions) == 0 {
		t.Error("expected permission rules to be added")
	}

	// Check that clauder tools are in permissions
	foundRemember := false
	for _, p := range permissions {
		perm := p.(map[string]interface{})
		if perm["tool"] == "mcp__clauder__remember" {
			foundRemember = true
			if perm["allow"] != true {
				t.Error("expected allow to be true")
			}
		}
	}
	if !foundRemember {
		t.Error("expected mcp__clauder__remember in permissions")
	}
}

// Claude Code Project Config Tests

func TestSetupProjectConfig_NewFile(t *testing.T) {
	_, cleanup := setupTempProject(t)
	defer cleanup()

	binaryPath := "/usr/local/bin/clauder"

	err := setupProjectConfig(binaryPath)
	if err != nil {
		t.Fatalf("setupProjectConfig failed: %v", err)
	}

	data, err := os.ReadFile(".mcp.json")
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var config MCPConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	clauder, ok := config.McpServers["clauder"]
	if !ok {
		t.Fatal("expected clauder in mcpServers")
	}

	if clauder.Command != binaryPath {
		t.Errorf("expected command %s, got %s", binaryPath, clauder.Command)
	}

	if len(clauder.Args) != 1 || clauder.Args[0] != "serve" {
		t.Errorf("expected args [serve], got %v", clauder.Args)
	}
}

func TestSetupProjectConfig_MergeExisting(t *testing.T) {
	_, cleanup := setupTempProject(t)
	defer cleanup()

	// Create existing config
	existingConfig := MCPConfig{
		McpServers: map[string]MCPServer{
			"existingServer": {
				Command: "existing",
				Args:    []string{"arg1"},
			},
		},
	}
	data, _ := json.MarshalIndent(existingConfig, "", "  ")
	writeTestFile(t, ".mcp.json", data)

	binaryPath := "/usr/local/bin/clauder"

	err := setupProjectConfig(binaryPath)
	if err != nil {
		t.Fatalf("setupProjectConfig failed: %v", err)
	}

	data, err = os.ReadFile(".mcp.json")
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	var config MCPConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	if _, ok := config.McpServers["existingServer"]; !ok {
		t.Error("expected existingServer to be preserved")
	}

	if _, ok := config.McpServers["clauder"]; !ok {
		t.Error("expected clauder to be added")
	}
}

// OpenCode Config Tests

func TestSetupOpencodeConfig_NewFile(t *testing.T) {
	_, cleanup := setupTempProject(t)
	defer cleanup()

	binaryPath := "/usr/local/bin/clauder"

	err := setupOpencodeConfig(binaryPath)
	if err != nil {
		t.Fatalf("setupOpencodeConfig failed: %v", err)
	}

	data, err := os.ReadFile("opencode.json")
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	// Check schema
	if config["$schema"] != "https://opencode.ai/config.json" {
		t.Error("expected $schema to be set")
	}

	mcp, ok := config["mcp"].(map[string]interface{})
	if !ok {
		t.Fatal("expected mcp in config")
	}

	clauder, ok := mcp["clauder"].(map[string]interface{})
	if !ok {
		t.Fatal("expected clauder in mcp")
	}

	if clauder["type"] != "local" {
		t.Errorf("expected type 'local', got %s", clauder["type"])
	}

	if clauder["enabled"] != true {
		t.Error("expected enabled to be true")
	}

	command, ok := clauder["command"].([]interface{})
	if !ok || len(command) != 2 {
		t.Errorf("expected command array with 2 elements, got %v", clauder["command"])
	}
	if command[0] != binaryPath || command[1] != "serve" {
		t.Errorf("unexpected command: %v", command)
	}
}

func TestSetupOpencodeConfig_MergeExisting(t *testing.T) {
	_, cleanup := setupTempProject(t)
	defer cleanup()

	// Create existing config with other settings
	existingConfig := map[string]interface{}{
		"$schema": "https://opencode.ai/config.json",
		"model":   "gpt-4",
		"mcp": map[string]interface{}{
			"existingServer": map[string]interface{}{
				"type":    "local",
				"command": []string{"existing"},
			},
		},
	}
	data, _ := json.MarshalIndent(existingConfig, "", "  ")
	writeTestFile(t, "opencode.json", data)

	binaryPath := "/usr/local/bin/clauder"

	err := setupOpencodeConfig(binaryPath)
	if err != nil {
		t.Fatalf("setupOpencodeConfig failed: %v", err)
	}

	data, err = os.ReadFile("opencode.json")
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	if config["model"] != "gpt-4" {
		t.Error("expected model setting to be preserved")
	}

	mcp := config["mcp"].(map[string]interface{})
	if _, ok := mcp["existingServer"]; !ok {
		t.Error("expected existingServer to be preserved")
	}
	if _, ok := mcp["clauder"]; !ok {
		t.Error("expected clauder to be added")
	}
}

// Codex CLI Config Tests

func TestSetupCodexConfig_NewFile(t *testing.T) {
	tmpHome, cleanup := setupTempHome(t)
	defer cleanup()

	restoreHome := setTestHome(t, tmpHome)
	defer restoreHome()

	binaryPath := "/usr/local/bin/clauder"

	err := setupCodexConfig(binaryPath)
	if err != nil {
		t.Fatalf("setupCodexConfig failed: %v", err)
	}

	configPath := filepath.Join(tmpHome, ".codex", "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var config map[string]interface{}
	if err := toml.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	mcpServers, ok := config["mcp_servers"].(map[string]interface{})
	if !ok {
		t.Fatal("expected mcp_servers in config")
	}

	clauder, ok := mcpServers["clauder"].(map[string]interface{})
	if !ok {
		t.Fatal("expected clauder in mcp_servers")
	}

	if clauder["command"] != binaryPath {
		t.Errorf("expected command %s, got %s", binaryPath, clauder["command"])
	}

	args, ok := clauder["args"].([]interface{})
	if !ok || len(args) != 1 || args[0] != "serve" {
		t.Errorf("expected args [serve], got %v", clauder["args"])
	}
}

func TestSetupCodexConfig_CreatesDirectory(t *testing.T) {
	tmpHome, cleanup := setupTempHome(t)
	defer cleanup()

	restoreHome := setTestHome(t, tmpHome)
	defer restoreHome()

	codexDir := filepath.Join(tmpHome, ".codex")
	if _, err := os.Stat(codexDir); !os.IsNotExist(err) {
		t.Fatal("expected .codex directory to not exist initially")
	}

	err := setupCodexConfig("/usr/local/bin/clauder")
	if err != nil {
		t.Fatalf("setupCodexConfig failed: %v", err)
	}

	if _, err := os.Stat(codexDir); os.IsNotExist(err) {
		t.Error("expected .codex directory to be created")
	}
}

func TestSetupCodexConfig_MergeExisting(t *testing.T) {
	tmpHome, cleanup := setupTempHome(t)
	defer cleanup()

	restoreHome := setTestHome(t, tmpHome)
	defer restoreHome()

	// Create existing config
	codexDir := filepath.Join(tmpHome, ".codex")
	mkdirTest(t, codexDir)
	configPath := filepath.Join(codexDir, "config.toml")

	existingConfig := `
[model]
provider = "openai"
name = "gpt-4"

[mcp_servers.existingServer]
command = "existing"
args = ["arg1"]
`
	writeTestFile(t, configPath, []byte(existingConfig))

	binaryPath := "/usr/local/bin/clauder"

	err := setupCodexConfig(binaryPath)
	if err != nil {
		t.Fatalf("setupCodexConfig failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	var config map[string]interface{}
	if err := toml.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	// Check model settings preserved
	model, ok := config["model"].(map[string]interface{})
	if !ok {
		t.Fatal("expected model settings to be preserved")
	}
	if model["provider"] != "openai" {
		t.Error("expected model.provider to be preserved")
	}

	mcpServers := config["mcp_servers"].(map[string]interface{})
	if _, ok := mcpServers["existingServer"]; !ok {
		t.Error("expected existingServer to be preserved")
	}
	if _, ok := mcpServers["clauder"]; !ok {
		t.Error("expected clauder to be added")
	}
}

// Gemini CLI Config Tests

func TestSetupGeminiConfig_NewFile(t *testing.T) {
	tmpHome, cleanup := setupTempHome(t)
	defer cleanup()

	restoreHome := setTestHome(t, tmpHome)
	defer restoreHome()

	binaryPath := "/usr/local/bin/clauder"

	err := setupGeminiConfig(binaryPath)
	if err != nil {
		t.Fatalf("setupGeminiConfig failed: %v", err)
	}

	configPath := filepath.Join(tmpHome, ".gemini", "settings.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	mcpServers, ok := config["mcpServers"].(map[string]interface{})
	if !ok {
		t.Fatal("expected mcpServers in config")
	}

	clauder, ok := mcpServers["clauder"].(map[string]interface{})
	if !ok {
		t.Fatal("expected clauder in mcpServers")
	}

	if clauder["command"] != binaryPath {
		t.Errorf("expected command %s, got %s", binaryPath, clauder["command"])
	}

	args, ok := clauder["args"].([]interface{})
	if !ok || len(args) != 1 || args[0] != "serve" {
		t.Errorf("expected args [serve], got %v", clauder["args"])
	}
}

func TestSetupGeminiConfig_CreatesDirectory(t *testing.T) {
	tmpHome, cleanup := setupTempHome(t)
	defer cleanup()

	restoreHome := setTestHome(t, tmpHome)
	defer restoreHome()

	geminiDir := filepath.Join(tmpHome, ".gemini")
	if _, err := os.Stat(geminiDir); !os.IsNotExist(err) {
		t.Fatal("expected .gemini directory to not exist initially")
	}

	err := setupGeminiConfig("/usr/local/bin/clauder")
	if err != nil {
		t.Fatalf("setupGeminiConfig failed: %v", err)
	}

	if _, err := os.Stat(geminiDir); os.IsNotExist(err) {
		t.Error("expected .gemini directory to be created")
	}
}

func TestSetupGeminiConfig_MergeExisting(t *testing.T) {
	tmpHome, cleanup := setupTempHome(t)
	defer cleanup()

	restoreHome := setTestHome(t, tmpHome)
	defer restoreHome()

	// Create existing config
	geminiDir := filepath.Join(tmpHome, ".gemini")
	mkdirTest(t, geminiDir)
	configPath := filepath.Join(geminiDir, "settings.json")

	existingConfig := map[string]interface{}{
		"theme": "dark",
		"security": map[string]interface{}{
			"auth": map[string]interface{}{
				"selectedType": "gemini-api-key",
			},
		},
		"mcpServers": map[string]interface{}{
			"existingServer": map[string]interface{}{
				"command": "existing",
				"args":    []string{"arg1"},
			},
		},
	}
	data, _ := json.MarshalIndent(existingConfig, "", "  ")
	writeTestFile(t, configPath, data)

	binaryPath := "/usr/local/bin/clauder"

	err := setupGeminiConfig(binaryPath)
	if err != nil {
		t.Fatalf("setupGeminiConfig failed: %v", err)
	}

	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	// Check existing settings preserved
	if config["theme"] != "dark" {
		t.Error("expected theme setting to be preserved")
	}

	security, ok := config["security"].(map[string]interface{})
	if !ok {
		t.Error("expected security settings to be preserved")
	}
	auth := security["auth"].(map[string]interface{})
	if auth["selectedType"] != "gemini-api-key" {
		t.Error("expected auth settings to be preserved")
	}

	mcpServers := config["mcpServers"].(map[string]interface{})
	if _, ok := mcpServers["existingServer"]; !ok {
		t.Error("expected existingServer to be preserved")
	}
	if _, ok := mcpServers["clauder"]; !ok {
		t.Error("expected clauder to be added")
	}
}

// CLAUDE.md Tests

func TestSetupClaudeMD_NewFile(t *testing.T) {
	_, cleanup := setupTempProject(t)
	defer cleanup()

	err := setupClaudeMD()
	if err != nil {
		t.Fatalf("setupClaudeMD failed: %v", err)
	}

	data, err := os.ReadFile("CLAUDE.md")
	if err != nil {
		t.Fatalf("failed to read CLAUDE.md: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "# Project Instructions") {
		t.Error("expected Project Instructions header")
	}
	if !strings.Contains(content, "## Clauder - Persistent Memory MCP") {
		t.Error("expected Clauder section")
	}
	if !strings.Contains(content, "mcp__clauder__remember") {
		t.Error("expected tool documentation")
	}
}

func TestSetupClaudeMD_AppendExisting(t *testing.T) {
	_, cleanup := setupTempProject(t)
	defer cleanup()

	// Create existing CLAUDE.md
	existingContent := "# My Project\n\nSome existing instructions.\n"
	writeTestFile(t, "CLAUDE.md", []byte(existingContent))

	err := setupClaudeMD()
	if err != nil {
		t.Fatalf("setupClaudeMD failed: %v", err)
	}

	data, err := os.ReadFile("CLAUDE.md")
	if err != nil {
		t.Fatalf("failed to read CLAUDE.md: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "# My Project") {
		t.Error("expected existing content to be preserved")
	}
	if !strings.Contains(content, "## Clauder - Persistent Memory MCP") {
		t.Error("expected Clauder section to be appended")
	}
}

func TestSetupClaudeMD_SkipIfExists(t *testing.T) {
	_, cleanup := setupTempProject(t)
	defer cleanup()

	// Create CLAUDE.md with clauder section already
	existingContent := "# My Project\n\n## Clauder - Persistent Memory MCP\n\nAlready configured.\n"
	writeTestFile(t, "CLAUDE.md", []byte(existingContent))

	err := setupClaudeMD()
	if err != nil {
		t.Fatalf("setupClaudeMD failed: %v", err)
	}

	data, err := os.ReadFile("CLAUDE.md")
	if err != nil {
		t.Fatalf("failed to read CLAUDE.md: %v", err)
	}
	content := string(data)

	// Should not have duplicate sections
	count := strings.Count(content, "## Clauder - Persistent Memory MCP")
	if count != 1 {
		t.Errorf("expected exactly 1 clauder section, found %d", count)
	}
}

// Helper function tests

func TestAskYesNo(t *testing.T) {
	// This function reads from stdin, so we can't easily test it without
	// mocking stdin. We'll skip interactive tests.
	t.Skip("askYesNo requires stdin interaction")
}

func TestGetBinaryPath(t *testing.T) {
	// This will return the test binary path, which is acceptable
	path, err := getBinaryPath()
	if err != nil {
		t.Fatalf("getBinaryPath failed: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path")
	}
}

// Error handling tests

func TestSetupGlobalConfig_InvalidJSON(t *testing.T) {
	tmpHome, cleanup := setupTempHome(t)
	defer cleanup()

	restoreHome := setTestHome(t, tmpHome)
	defer restoreHome()

	// Create invalid JSON config
	configPath := filepath.Join(tmpHome, ".claude.json")
	writeTestFile(t, configPath, []byte("invalid json {"))

	err := setupGlobalConfig("/usr/local/bin/clauder")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "failed to parse") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSetupCodexConfig_InvalidTOML(t *testing.T) {
	tmpHome, cleanup := setupTempHome(t)
	defer cleanup()

	restoreHome := setTestHome(t, tmpHome)
	defer restoreHome()

	// Create invalid TOML config
	codexDir := filepath.Join(tmpHome, ".codex")
	mkdirTest(t, codexDir)
	configPath := filepath.Join(codexDir, "config.toml")
	writeTestFile(t, configPath, []byte("invalid toml = ["))

	err := setupCodexConfig("/usr/local/bin/clauder")
	if err == nil {
		t.Error("expected error for invalid TOML")
	}
	if !strings.Contains(err.Error(), "failed to parse") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSetupGeminiConfig_InvalidJSON(t *testing.T) {
	tmpHome, cleanup := setupTempHome(t)
	defer cleanup()

	restoreHome := setTestHome(t, tmpHome)
	defer restoreHome()

	// Create invalid JSON config
	geminiDir := filepath.Join(tmpHome, ".gemini")
	mkdirTest(t, geminiDir)
	configPath := filepath.Join(geminiDir, "settings.json")
	writeTestFile(t, configPath, []byte("invalid json }"))

	err := setupGeminiConfig("/usr/local/bin/clauder")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "failed to parse") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSetupOpencodeConfig_InvalidJSON(t *testing.T) {
	_, cleanup := setupTempProject(t)
	defer cleanup()

	// Create invalid JSON config
	writeTestFile(t, "opencode.json", []byte("not valid json"))

	err := setupOpencodeConfig("/usr/local/bin/clauder")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "failed to parse") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSetupProjectConfig_InvalidJSON(t *testing.T) {
	_, cleanup := setupTempProject(t)
	defer cleanup()

	// Create invalid JSON config
	writeTestFile(t, ".mcp.json", []byte("{ broken json"))

	err := setupProjectConfig("/usr/local/bin/clauder")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "failed to parse") {
		t.Errorf("unexpected error: %v", err)
	}
}
