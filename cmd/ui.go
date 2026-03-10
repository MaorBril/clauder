package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/orsi-bit/openclawder/internal/store"
	"github.com/orsi-bit/openclawder/internal/ui"
	"github.com/spf13/cobra"
)

var (
	refreshInterval int
	port            int
	noBrowser       bool
)

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Launch the web dashboard",
	Long:  `Opens a web-based dashboard showing instances, messages, and facts.`,
	RunE:  runUI,
}

func init() {
	uiCmd.Flags().IntVarP(&refreshInterval, "refresh", "r", 3, "Auto-refresh interval in seconds")
	uiCmd.Flags().IntVarP(&port, "port", "p", 8765, "Port to run the web server on")
	uiCmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Don't automatically open browser")
}

func runUI(cmd *cobra.Command, args []string) error {
	dataDir := getDataDir()
	s, err := store.NewSQLiteStore(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer func() { _ = s.Close() }()

	// Clean up stale instances
	_ = s.CleanupStaleInstances(5 * time.Minute)

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	interval := time.Duration(refreshInterval) * time.Second
	if interval < time.Second {
		interval = time.Second
	}

	server, err := ui.NewWebServer(s, workDir, interval)
	if err != nil {
		return fmt.Errorf("failed to create web server: %w", err)
	}

	// Open browser automatically unless disabled
	if !noBrowser {
		url := fmt.Sprintf("http://localhost:%d", port)
		go func() {
			time.Sleep(500 * time.Millisecond)
			openBrowser(url)
		}()
	}

	return server.Start(port)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return
	}
	_ = cmd.Start()
}
