package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/maorbril/clauder/internal/store"
	"github.com/spf13/cobra"
)

var rememberTags []string

var rememberCmd = &cobra.Command{
	Use:   "remember [fact]",
	Short: "Store a fact or piece of context",
	Long:  `Store a fact, decision, or piece of context that should persist across Claude Code sessions.`,
	Args:  cobra.MinimumNArgs(1),
	RunE:  runRemember,
}

func init() {
	rememberCmd.Flags().StringSliceVarP(&rememberTags, "tags", "t", nil, "Tags to categorize the fact")
}

func runRemember(cmd *cobra.Command, args []string) error {
	dataDir := getDataDir()
	s, err := store.NewSQLiteStore(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer s.Close()

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	fact := strings.Join(args, " ")
	stored, err := s.AddFact(fact, rememberTags, workDir)
	if err != nil {
		return fmt.Errorf("failed to store fact: %w", err)
	}

	fmt.Printf("Stored fact #%d\n", stored.ID)
	return nil
}
