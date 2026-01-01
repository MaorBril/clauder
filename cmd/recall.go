package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/maorbril/clauder/internal/store"
	"github.com/spf13/cobra"
)

var (
	recallTags       []string
	recallLimit      int
	recallCurrentDir bool
)

var recallCmd = &cobra.Command{
	Use:   "recall [query]",
	Short: "Search and retrieve stored facts",
	Long:  `Search and retrieve previously stored facts, decisions, and context.`,
	RunE:  runRecall,
}

func init() {
	recallCmd.Flags().StringSliceVarP(&recallTags, "tags", "t", nil, "Filter by tags")
	recallCmd.Flags().IntVarP(&recallLimit, "limit", "n", 20, "Maximum number of results")
	recallCmd.Flags().BoolVarP(&recallCurrentDir, "local", "l", false, "Only show facts from current directory")
}

func runRecall(cmd *cobra.Command, args []string) error {
	dataDir := getDataDir()
	s, err := store.NewSQLiteStore(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer func() { _ = s.Close() }()

	query := strings.Join(args, " ")

	sourceDir := ""
	if recallCurrentDir {
		sourceDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
	}

	facts, err := s.GetFacts(query, recallTags, sourceDir, recallLimit)
	if err != nil {
		return fmt.Errorf("failed to recall facts: %w", err)
	}

	if len(facts) == 0 {
		fmt.Println("No facts found.")
		return nil
	}

	fmt.Printf("Found %d fact(s):\n\n", len(facts))

	for _, f := range facts {
		fmt.Printf("#%d [%s]\n", f.ID, f.CreatedAt.Format("2006-01-02 15:04"))
		if len(f.Tags) > 0 {
			fmt.Printf("Tags: %s\n", strings.Join(f.Tags, ", "))
		}
		fmt.Printf("Dir: %s\n", f.SourceDir)
		fmt.Printf("%s\n\n", f.Content)
	}

	return nil
}
