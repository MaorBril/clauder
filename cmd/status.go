package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/maorbril/clauder/internal/store"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current context and memory stats",
	Long:  `Show statistics about stored facts, running instances, and pending messages.`,
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	dataDir := getDataDir()
	s, err := store.NewSQLiteStore(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer func() { _ = s.Close() }()

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Get facts stats
	allFacts, err := s.GetFacts("", nil, "", 0)
	if err != nil {
		return fmt.Errorf("failed to get facts: %w", err)
	}
	localFacts, err := s.GetFacts("", nil, workDir, 0)
	if err != nil {
		return fmt.Errorf("failed to get local facts: %w", err)
	}

	// Get instances
	if err := s.CleanupStaleInstances(5 * time.Minute); err != nil {
		return fmt.Errorf("failed to cleanup stale instances: %w", err)
	}
	instances, err := s.GetInstances()
	if err != nil {
		return fmt.Errorf("failed to get instances: %w", err)
	}

	fmt.Println("Clauder Status")
	fmt.Println("==============")
	fmt.Printf("Data directory: %s\n", dataDir)
	fmt.Printf("Working directory: %s\n\n", workDir)

	fmt.Println("Facts")
	fmt.Println("-----")
	fmt.Printf("Total facts: %d\n", len(allFacts))
	fmt.Printf("Local facts (this directory): %d\n\n", len(localFacts))

	fmt.Println("Instances")
	fmt.Println("---------")
	fmt.Printf("Running instances: %d\n", len(instances))

	if len(instances) > 0 {
		fmt.Println()
		for _, inst := range instances {
			fmt.Printf("  %s - %s\n", inst.ID, inst.Directory)
		}
	}

	return nil
}
