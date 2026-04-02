package cmd

import (
	"encoding/json"
	"io"
	"os"

	"github.com/maorbril/clauder/internal/store"
	"github.com/spf13/cobra"
)

const tokensPerToolCall = 100

var petHookCmd = &cobra.Command{
	Use:    "pet-hook",
	Short:  "Feed the pet from a Claude Code PostToolUse hook",
	Hidden: true,
	RunE:   runPetHook,
}

func init() {
	rootCmd.AddCommand(petHookCmd)
}

type hookInput struct {
	Cwd      string `json:"cwd"`
	ToolName string `json:"tool_name"`
}

// toolMoodBoost maps tool names to happiness boosts from creative activity.
var toolMoodBoost = map[string]int{
	"Edit":         2,
	"Write":        2,
	"MultiEdit":    2,
	"NotebookEdit": 1,
	"Bash":         1,
}

func runPetHook(cmd *cobra.Command, args []string) error {
	// Read hook JSON from stdin
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil // silently skip
	}

	var input hookInput
	if err := json.Unmarshal(data, &input); err != nil || input.Cwd == "" {
		input.Cwd, _ = os.Getwd()
	}

	dataDir := getDataDir()
	s, err := store.NewSQLiteStore(dataDir)
	if err != nil {
		return nil // silently skip if store unavailable
	}
	defer s.Close()

	// Feed the pet — silently ignore if no pet exists yet
	_, _ = s.FeedPet(input.Cwd, tokensPerToolCall)

	// Boost mood for creative activities
	if boost, ok := toolMoodBoost[input.ToolName]; ok {
		_ = s.ActivityBoost(input.Cwd, boost)
	}
	return nil
}
