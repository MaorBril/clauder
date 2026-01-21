package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/maorbril/clauder/internal/mcp"
	"github.com/maorbril/clauder/internal/store"
	"github.com/spf13/cobra"
)

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

	// Generate directory hash for grouping instances by directory
	dirHash := GenerateDirectoryHash(workDir)

	// Use a composed instance ID: directoryHash_pid
	// This allows multiple instances per directory while enabling message fallback
	instanceID := generateInstanceID(workDir, os.Getpid())

	// Use PID-based index ID for Bleve to ensure each process gets its own index
	// This prevents file locking issues when multiple processes run in the same directory
	indexID := fmt.Sprintf("%d", os.Getpid())

	fmt.Fprintf(os.Stderr, "[clauder] PID=%d starting, workDir=%s, indexID=%s\n", os.Getpid(), workDir, indexID)

	// Initialize per-process Bleve index for full-text search
	fmt.Fprintf(os.Stderr, "[clauder] Initializing Bleve index...\n")
	if err := s.InitIndex(indexID); err != nil {
		// Log warning but continue - search will fall back to SQLite
		fmt.Fprintf(os.Stderr, "[clauder] WARNING: failed to initialize search index: %v\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "[clauder] Bleve index initialized successfully\n")
	}

	// Register this instance
	if err := s.RegisterInstance(instanceID, os.Getpid(), workDir, dirHash, ""); err != nil {
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
	server := mcp.NewServer(s, instanceID, workDir)
	if err := server.Run(); err != nil {
		_ = s.UnregisterInstance(instanceID)
		return err
	}

	_ = s.UnregisterInstance(instanceID)
	return nil
}

// generateInstanceID creates a composed instance ID: directoryHash_pid
// This allows multiple instances per directory while enabling message fallback
// to other instances in the same directory if the target instance is gone
func generateInstanceID(directory string, pid int) string {
	dirHash := GenerateDirectoryHash(directory)
	return fmt.Sprintf("%s_%d", dirHash, pid)
}

// GenerateDirectoryHash creates a hash of the directory path
// Used for grouping instances by directory and message fallback routing
// Uses 16 bytes (32 hex chars) to match legacy instance ID format for backwards compatibility
func GenerateDirectoryHash(directory string) string {
	hash := sha256.Sum256([]byte(directory))
	return hex.EncodeToString(hash[:16])
}

