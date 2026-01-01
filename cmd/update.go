package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

const (
	// Version is the current version of clauder
	Version = "0.1.0"

	// GitHubOwner is the GitHub repository owner
	GitHubOwner = "MaorBril"

	// GitHubRepo is the GitHub repository name
	GitHubRepo = "clauder"
)

var (
	checkOnly bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for and install updates",
	Long:  `Check GitHub releases for a newer version of clauder and optionally install it.`,
	RunE:  runUpdate,
}

func init() {
	rootCmd.AddCommand(updateCmd)
	updateCmd.Flags().BoolVar(&checkOnly, "check", false, "Only check for updates, don't install")
}

type GitHubRelease struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	Assets  []Asset `json:"assets"`
	Body    string  `json:"body"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func runUpdate(cmd *cobra.Command, args []string) error {
	fmt.Printf("Current version: %s\n", Version)
	fmt.Println("Checking for updates...")

	release, err := getLatestRelease()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	if latestVersion == Version {
		fmt.Println("You are already running the latest version.")
		return nil
	}

	fmt.Printf("New version available: %s\n", latestVersion)

	if checkOnly {
		fmt.Println("\nRun 'clauder update' without --check to install the update.")
		return nil
	}

	// Find the appropriate asset for this platform
	assetName := getAssetName()
	var downloadURL string
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no binary found for %s/%s in release %s", runtime.GOOS, runtime.GOARCH, release.TagName)
	}

	fmt.Printf("Downloading %s...\n", assetName)

	// Get the current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	// Download to a temporary file
	tmpFile, err := os.CreateTemp(filepath.Dir(execPath), "clauder-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %s", resp.Status)
	}

	_, err = io.Copy(tmpFile, resp.Body)
	_ = tmpFile.Close()
	if err != nil {
		return fmt.Errorf("failed to write update: %w", err)
	}

	// Make the new binary executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	// Replace the old binary
	oldPath := execPath + ".old"
	if err := os.Rename(execPath, oldPath); err != nil {
		return fmt.Errorf("failed to backup old binary: %w", err)
	}

	if err := os.Rename(tmpPath, execPath); err != nil {
		// Try to restore the old binary
		_ = os.Rename(oldPath, execPath)
		return fmt.Errorf("failed to install update: %w", err)
	}

	// Remove the old binary
	_ = os.Remove(oldPath)

	fmt.Printf("Successfully updated to version %s!\n", latestVersion)
	return nil
}

func getLatestRelease() (*GitHubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", GitHubOwner, GitHubRepo)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no releases found")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status: %s", resp.Status)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse release info: %w", err)
	}

	return &release, nil
}

func getAssetName() string {
	os := runtime.GOOS
	arch := runtime.GOARCH

	switch os {
	case "darwin":
		os = "darwin"
	case "linux":
		os = "linux"
	case "windows":
		os = "windows"
	}

	switch arch {
	case "amd64":
		arch = "amd64"
	case "arm64":
		arch = "arm64"
	}

	name := fmt.Sprintf("clauder-%s-%s", os, arch)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	return name
}
