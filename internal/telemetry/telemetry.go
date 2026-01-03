package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/posthog/posthog-go"
)

const (
	// PostHog API key - public, write-only key
	posthogAPIKey = "phc_XOVLo87Qh1MjNnn3fBc5WoW9OHK1zQxmEQ0UMwnXRp5"
	posthogHost   = "https://us.i.posthog.com"
)

var (
	client   posthog.Client
	once     sync.Once
	disabled bool
	anonID   string
)

// Init initializes the telemetry client
func Init() {
	once.Do(func() {
		// Check for opt-out
		if os.Getenv("CLAUDER_NO_TELEMETRY") != "" || os.Getenv("DO_NOT_TRACK") == "1" {
			disabled = true
			return
		}

		// Generate anonymous ID based on machine identifier
		anonID = generateAnonID()

		var err error
		client, err = posthog.NewWithConfig(posthogAPIKey, posthog.Config{
			Endpoint: posthogHost,
			Interval: 5 * time.Second, // Flush every 5 seconds
		})
		if err != nil {
			disabled = true
			return
		}
	})
}

// Close flushes and closes the telemetry client
func Close() {
	if client != nil {
		_ = client.Close()
	}
}

// Track sends an event to PostHog
func Track(event string, properties map[string]interface{}) {
	if disabled || client == nil {
		return
	}

	props := posthog.NewProperties()
	props.Set("os", runtime.GOOS)
	props.Set("arch", runtime.GOARCH)
	props.Set("version", Version)

	for k, v := range properties {
		props.Set(k, v)
	}

	_ = client.Enqueue(posthog.Capture{
		DistinctId: anonID,
		Event:      event,
		Properties: props,
	})
}

// TrackInstall tracks an installation event
func TrackInstall(method string) {
	Track("install", map[string]interface{}{
		"method": method,
	})
}

// TrackSetup tracks a setup event
func TrackSetup(tool string) {
	Track("setup", map[string]interface{}{
		"tool": tool,
	})
}

// TrackCommand tracks a CLI command usage
func TrackCommand(command string) {
	Track("command", map[string]interface{}{
		"command": command,
	})
}

// TrackMCPTool tracks an MCP tool usage
func TrackMCPTool(tool string) {
	Track("mcp_tool", map[string]interface{}{
		"tool": tool,
	})
}

// TrackError tracks an error event (anonymized)
func TrackError(context string) {
	Track("error", map[string]interface{}{
		"context": context,
	})
}

// generateAnonID creates a stable anonymous ID for this machine
func generateAnonID() string {
	// Use home directory + hostname as a stable identifier
	home, _ := os.UserHomeDir()
	hostname, _ := os.Hostname()

	data := home + hostname + "clauder-salt-v1"
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:16])
}

// Version is set by the calling package
var Version = "dev"

// SetVersion sets the version for telemetry events
func SetVersion(v string) {
	Version = v
}
