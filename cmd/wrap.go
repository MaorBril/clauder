//go:build !windows

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/orsi-bit/openclawder/internal/store"
	"github.com/orsi-bit/openclawder/internal/telemetry"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var wrapInstanceName string

func init() {
	wrapCmd.Flags().StringVarP(&wrapInstanceName, "name", "n", "", "Instance name for multi-instance setups (e.g., 'backend', 'frontend')")
}

var wrapCmd = &cobra.Command{
	Use:   "wrap [flags] [-- claude args...]",
	Short: "Run Claude Code with openclawder wrapper",
	Long: `Runs Claude Code as a subprocess with full terminal passthrough.

This wrapper mode allows openclawder to intercept and augment Claude Code sessions.
Use -- to separate openclawder flags from arguments passed to Claude Code.

The wrapper monitors for incoming messages from other Claude instances and
automatically prompts Claude to check them when the input line is empty.

Examples:
  openclawder wrap                              # Start interactive Claude Code session
  openclawder wrap -- -p "fix the bug"          # Pass a prompt to Claude Code
  openclawder wrap -- --resume                  # Resume previous session
  openclawder wrap --name backend               # Named instance
  openclawder wrap --name backend -- --resume   # Named instance with claude args`,
	DisableFlagParsing: true,
	RunE:               runWrap,
}

// inputTracker monitors user keystrokes to determine if the input line is empty
type inputTracker struct {
	mu            sync.Mutex
	buffer        []byte
	lastKeystroke time.Time
	inEscSeq      bool      // true if we're in the middle of an escape sequence
	escSeqStart   time.Time // when the escape sequence started
}

func newInputTracker() *inputTracker {
	return &inputTracker{
		lastKeystroke: time.Now(),
	}
}

// ProcessByte processes a single byte of user input and updates the buffer
func (t *inputTracker) ProcessByte(b byte) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Handle escape sequences - ESC starts a sequence, skip until we see a letter
	// Escape sequences are typically: ESC [ <params> <letter>
	// e.g., ESC[A (arrow up), ESC[1;5C (ctrl+right), etc.
	if b == 0x1b { // ESC
		t.inEscSeq = true
		t.escSeqStart = time.Now()
		return // Don't update lastKeystroke for terminal escape sequences
	}

	if t.inEscSeq {
		// Escape sequences timeout after 100ms (in case of incomplete sequence)
		if time.Since(t.escSeqStart) > 100*time.Millisecond {
			t.inEscSeq = false
		} else if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || b == '~' {
			// Letter or ~ terminates the escape sequence
			t.inEscSeq = false
			return // Don't update lastKeystroke for terminal escape sequences
		} else {
			// Middle of escape sequence (like '[' or numbers)
			return // Don't update lastKeystroke for terminal escape sequences
		}
	}

	t.lastKeystroke = time.Now()

	switch b {
	case '\r', '\n': // Enter - clear buffer
		t.buffer = nil
	case 0x7f, 0x08: // Backspace/Delete - remove last char
		if len(t.buffer) > 0 {
			t.buffer = t.buffer[:len(t.buffer)-1]
		}
	case 0x15: // Ctrl+U (kill line) - clear buffer
		t.buffer = nil
	case 0x03: // Ctrl+C - clear buffer
		t.buffer = nil
	case 0x17: // Ctrl+W (delete word) - remove last word
		// Simple implementation: remove until space or empty
		for len(t.buffer) > 0 && t.buffer[len(t.buffer)-1] != ' ' {
			t.buffer = t.buffer[:len(t.buffer)-1]
		}
	default:
		// Only track printable characters
		if b >= 32 && b < 127 {
			t.buffer = append(t.buffer, b)
		}
	}
}

// CanInject returns true if it's safe to inject a command
// (empty buffer and no recent keystrokes)
func (t *inputTracker) CanInject(idleTimeout time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return len(t.buffer) == 0 && time.Since(t.lastKeystroke) > idleTimeout
}

// messageWatcher monitors for unread messages and triggers injection
type messageWatcher struct {
	store        store.Store
	workDir      string
	directoryID  string
	instanceName string
	ptmx         *os.File
	tracker      *inputTracker
	stopCh       chan struct{}
	checkEvery   time.Duration
	idleTime     time.Duration
	cooldown     time.Duration
	lastInjected time.Time
}

func newMessageWatcher(s store.Store, workDir, directoryID, instanceName string, ptmx *os.File, tracker *inputTracker) *messageWatcher {
	return &messageWatcher{
		store:        s,
		workDir:      workDir,
		directoryID:  directoryID,
		instanceName: instanceName,
		ptmx:         ptmx,
		tracker:      tracker,
		stopCh:       make(chan struct{}),
		checkEvery:   5 * time.Second,
		idleTime:     2 * time.Second,
		cooldown:     60 * time.Second, // Don't re-inject for at least 60 seconds
	}
}

// Start begins monitoring for messages in a goroutine
func (w *messageWatcher) Start() {
	go w.run()
}

// Stop signals the watcher to stop
func (w *messageWatcher) Stop() {
	close(w.stopCh)
}

func (w *messageWatcher) run() {
	ticker := time.NewTicker(w.checkEvery)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.checkAndInject()
		}
	}
}

func (w *messageWatcher) checkAndInject() {
	// Check cooldown - don't spam injections
	if time.Since(w.lastInjected) < w.cooldown {
		return
	}

	// Query instances in our directory using directoryID
	instances, err := w.store.GetInstancesByDirectory(w.directoryID)
	if err != nil {
		return
	}

	// Check for unread messages, tracking which instances have them
	var unreadFor []string
	for _, inst := range instances {
		// If we have a specific name, only check messages for instances with that name
		if w.instanceName != "" && inst.Name != w.instanceName {
			continue
		}

		messages, err := w.store.GetMessages(inst.ID, true) // unread only
		if err != nil || len(messages) == 0 {
			continue
		}

		name := inst.Name
		if name == "" {
			name = "primary"
		}
		unreadFor = append(unreadFor, name)
	}

	if len(unreadFor) == 0 {
		return
	}

	// Check if we can safely inject
	if !w.tracker.CanInject(w.idleTime) {
		return
	}

	// Build contextual prompt
	var prompt string
	if w.instanceName != "" {
		// Named instance - simple prompt
		prompt = "[You have a new message] - Read your openclawder messages using get_messages and respond to them."
	} else if len(unreadFor) == 1 {
		prompt = fmt.Sprintf("[New message for '%s'] - Read your openclawder messages using get_messages.", unreadFor[0])
	} else {
		prompt = fmt.Sprintf("[Messages for %d instances] - Read your openclawder messages using get_messages.", len(unreadFor))
	}

	w.inject(prompt)
	w.lastInjected = time.Now()
}

func (w *messageWatcher) inject(text string) {
	// Send characters one by one with small delays to simulate typing
	for _, ch := range text {
		_, _ = w.ptmx.WriteString(string(ch))
		time.Sleep(5 * time.Millisecond)
	}
	// Send Enter (CR - what terminal Enter key sends in raw mode)
	time.Sleep(10 * time.Millisecond)
	_, _ = w.ptmx.WriteString("\r")
}

// parseWrapArgs splits args into clauder flags and claude args using "--" as separator.
// Everything before "--" is parsed for clauder flags (--name, --help).
// Everything after "--" is passed directly to claude.
// If no "--" is present, all args are passed to claude for backwards compatibility.
func parseWrapArgs(args []string) (name string, claudeArgs []string, help bool) {
	// Find the "--" separator
	sepIdx := -1
	for i, arg := range args {
		if arg == "--" {
			sepIdx = i
			break
		}
	}

	var clauderArgs []string
	if sepIdx >= 0 {
		clauderArgs = args[:sepIdx]
		claudeArgs = args[sepIdx+1:]
	} else {
		// No separator: check if any args look like clauder flags
		// For backwards compat, if no clauder flags are present, pass everything to claude
		for i := 0; i < len(args); i++ {
			if args[i] == "--name" || args[i] == "-n" {
				clauderArgs = append(clauderArgs, args[i])
				if i+1 < len(args) {
					i++
					clauderArgs = append(clauderArgs, args[i])
				}
			} else if args[i] == "-h" || args[i] == "--help" {
				clauderArgs = append(clauderArgs, args[i])
			} else {
				claudeArgs = append(claudeArgs, args[i])
			}
		}
	}

	// Parse clauder flags
	for i := 0; i < len(clauderArgs); i++ {
		switch clauderArgs[i] {
		case "--name", "-n":
			if i+1 < len(clauderArgs) {
				i++
				name = clauderArgs[i]
			}
		case "-h", "--help":
			help = true
		}
	}

	return name, claudeArgs, help
}

func runWrap(cmd *cobra.Command, args []string) error {
	// Parse clauder flags vs claude args
	name, claudeArgs, showHelp := parseWrapArgs(args)
	if showHelp {
		return cmd.Help()
	}
	wrapInstanceName = name

	// Check if stdin is a terminal
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("wrap command requires an interactive terminal")
	}

	// Track wrap usage
	telemetry.TrackWrap(wrapInstanceName != "")

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Generate directory ID for message queries
	directoryID := generateDirectoryID(workDir)

	// Open the store for message monitoring
	dataDir := getDataDir()
	s, err := store.NewSQLiteStore(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer func() { _ = s.Close() }()

	// Create the claude command with claude-specific arguments
	c := exec.Command("claude", claudeArgs...)
	c.Dir = workDir

	// Pass instance name to inner session via environment variable
	if wrapInstanceName != "" {
		c.Env = append(os.Environ(), "OPENCLAWDER_INSTANCE_NAME="+wrapInstanceName)
	}

	// Start the command with a PTY
	ptmx, err := pty.Start(c)
	if err != nil {
		return fmt.Errorf("failed to start claude with PTY: %w", err)
	}
	defer func() { _ = ptmx.Close() }()

	// Handle terminal resize (SIGWINCH)
	resizeCh := make(chan os.Signal, 1)
	signal.Notify(resizeCh, syscall.SIGWINCH)
	go func() {
		for range resizeCh {
			if err := pty.InheritSize(os.Stdin, ptmx); err != nil {
				fmt.Fprintf(os.Stderr, "error resizing pty: %s\n", err)
			}
		}
	}()
	// Initial resize
	resizeCh <- syscall.SIGWINCH
	defer signal.Stop(resizeCh)

	// Handle interrupt/terminate signals - forward to subprocess
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for sig := range sigCh {
			if c.Process != nil {
				_ = c.Process.Signal(sig)
			}
		}
	}()
	defer signal.Stop(sigCh)

	// Set stdin to raw mode for proper character passthrough
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("failed to set raw terminal mode: %w", err)
	}
	defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }()

	// Create input tracker
	tracker := newInputTracker()

	// Start message watcher
	watcher := newMessageWatcher(s, workDir, directoryID, wrapInstanceName, ptmx, tracker)
	watcher.Start()
	defer watcher.Stop()

	// Copy stdin to PTY with input tracking
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil || n == 0 {
				return
			}
			// Track the input
			tracker.ProcessByte(buf[0])
			// Pass through to PTY
			_, _ = ptmx.Write(buf[:n])
		}
	}()

	// Copy PTY output to stdout
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil || n == 0 {
				return
			}
			_, _ = os.Stdout.Write(buf[:n])
		}
	}()

	// Wait for the process to exit
	if err := c.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Return the same exit code as claude
			os.Exit(exitErr.ExitCode())
		}
		return err
	}

	return nil
}
