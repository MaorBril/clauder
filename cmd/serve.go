package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/maorbril/clauder/internal/mcp"
	"github.com/maorbril/clauder/internal/store"
	"github.com/maorbril/clauder/internal/telemetry"
	"github.com/spf13/cobra"
)

var instanceName string

func init() {
	serveCmd.Flags().StringVar(&instanceName, "name", "", "Instance name for multi-instance setups (e.g., 'backend', 'frontend')")
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the MCP server for Claude Code",
	Long:  `Starts clauder as an MCP server. This is typically invoked by Claude Code, not directly.`,
	RunE:  runServe,
}

func runServe(cmd *cobra.Command, args []string) error {
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

	// Check env var for instance name (passed from wrap command)
	name := instanceName
	if name == "" {
		name = os.Getenv("CLAUDER_INSTANCE_NAME")
	}

	// Generate directory ID (used for grouping instances in same directory)
	directoryID := generateDirectoryID(workDir)

	// Clean up stale instances before checking for collisions
	_ = s.CleanupStaleInstances(5 * time.Minute)

	// Track whether user explicitly named this instance
	explicitlyNamed := name != ""
	autoNamed := false

	// Handle collision detection for unnamed instances
	if name == "" {
		hasActive, err := s.CheckDirectoryHasActiveInstance(directoryID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[clauder] WARNING: failed to check for existing instances: %v\n", err)
		} else if hasActive {
			// Collision detected - auto-generate a unique name
			name = generateShortID()
			autoNamed = true
			fmt.Fprintf(os.Stderr, "[clauder] Multiple instances detected in directory, using auto-generated name: %s\n", name)
			telemetry.TrackMultiInstance()
		}
	}

	// Track serve usage
	telemetry.TrackServe(explicitlyNamed, autoNamed)

	// Generate instance ID (includes name if provided)
	instanceID := generateInstanceID(directoryID, name)

	// Use PID-based index ID for Bleve to ensure each process gets its own index
	// This prevents file locking issues when multiple processes run in the same directory
	indexID := fmt.Sprintf("%d", os.Getpid())

	fmt.Fprintf(os.Stderr, "[clauder] PID=%d starting, workDir=%s, instanceID=%s, indexID=%s\n", os.Getpid(), workDir, instanceID, indexID)

	// Initialize per-process Bleve index for full-text search
	fmt.Fprintf(os.Stderr, "[clauder] Initializing Bleve index...\n")
	if err := s.InitIndex(indexID); err != nil {
		// Log warning but continue - search will fall back to SQLite
		fmt.Fprintf(os.Stderr, "[clauder] WARNING: failed to initialize search index: %v\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "[clauder] Bleve index initialized successfully\n")
	}

	// Register this instance
	if err := s.RegisterInstance(instanceID, directoryID, name, workDir, "", os.Getpid()); err != nil {
		return fmt.Errorf("failed to register instance: %w", err)
	}

	// Setup cleanup on exit
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		_ = s.UnregisterInstance(instanceID)
		cancel()
		os.Exit(0)
	}()

	// Heartbeat goroutine
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.Heartbeat(instanceID)
			}
		}
	}()

	// Run MCP server
	server := mcp.NewServer(s, instanceID, directoryID, workDir)
	if err := server.Run(); err != nil {
		_ = s.UnregisterInstance(instanceID)
		return err
	}

	_ = s.UnregisterInstance(instanceID)
	return nil
}

// generateInstanceID creates the full instance ID
// Format: directoryID (unnamed) or directoryID:name (named)
func generateInstanceID(directoryID, name string) string {
	if name == "" {
		return directoryID
	}
	return directoryID + ":" + name
}

// generateShortID creates a short random ID for auto-named instances
func generateShortID() string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

