package cmd

import (
	"fmt"
	"time"

	"github.com/maorbril/clauder/internal/store"
	"github.com/spf13/cobra"
)

var instancesCmd = &cobra.Command{
	Use:   "instances",
	Short: "List running clauder instances",
	Long:  `List all running clauder instances across different directories.`,
	RunE:  runInstances,
}

func runInstances(cmd *cobra.Command, args []string) error {
	dataDir := getDataDir()
	s, err := store.NewSQLiteStore(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer func() { _ = s.Close() }()

	// Cleanup stale instances
	_ = s.CleanupStaleInstances(5 * time.Minute)

	instances, err := s.GetInstances()
	if err != nil {
		return fmt.Errorf("failed to list instances: %w", err)
	}

	if len(instances) == 0 {
		fmt.Println("No running instances found.")
		return nil
	}

	fmt.Printf("Found %d running instance(s):\n\n", len(instances))

	for _, inst := range instances {
		fmt.Printf("%s\n", inst.ID)
		fmt.Printf("  PID: %d\n", inst.PID)
		fmt.Printf("  Directory: %s\n", inst.Directory)
		fmt.Printf("  Started: %s\n", inst.StartedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("  Last heartbeat: %s\n\n", inst.LastHeartbeat.Format("15:04:05"))
	}

	return nil
}
