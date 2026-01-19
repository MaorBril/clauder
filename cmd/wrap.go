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
	"github.com/maorbril/clauder/internal/store"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var wrapCmd = &cobra.Command{
	Use:   "wrap [claude args...]",
	Short: "Run Claude Code with clauder wrapper",
	Long: `Runs Claude Code as a subprocess with full terminal passthrough.

This wrapper mode allows clauder to intercept and augment Claude Code sessions.
All arguments are passed directly to the claude command.

The wrapper monitors for incoming messages from other Claude instances and
automatically prompts Claude to check them when the input line is empty.

Examples:
  clauder wrap                    # Start interactive Claude Code session
  clauder wrap -p "fix the bug"   # Pass a prompt to Claude Code
  clauder wrap --resume           # Resume previous session`,
	DisableFlagParsing: true,
	RunE:               runWrap,
}

// inputTracker monitors user keystrokes to determine if the input line is empty
type inputTracker struct {
	mu            sync.Mutex
	buffer        []byte
	lastKeystroke time.Time
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
	ptmx         *os.File
	tracker      *inputTracker
	stopCh       chan struct{}
	prompt       string
	checkEvery   time.Duration
	idleTime     time.Duration
	cooldown     time.Duration
	lastInjected time.Time
}

func newMessageWatcher(s store.Store, workDir string, ptmx *os.File, tracker *inputTracker) *messageWatcher {
	return &messageWatcher{
		store:      s,
		workDir:    workDir,
		ptmx:       ptmx,
		tracker:    tracker,
		stopCh:     make(chan struct{}),
		prompt:     "[You have a new message] - Read your clauder messages using get_messages and respond to them.",
		checkEvery: 5 * time.Second,
		idleTime:   2 * time.Second,
		cooldown:   60 * time.Second, // Don't re-inject for at least 60 seconds
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

	// Find instances in our directory
	instances, err := w.store.GetInstances()
	if err != nil {
		return
	}

	// Check for unread messages to any instance in our directory
	hasUnread := false
	for _, inst := range instances {
		if inst.Directory != w.workDir {
			continue
		}
		messages, err := w.store.GetMessages(inst.ID, true) // unread only
		if err != nil {
			continue
		}
		if len(messages) > 0 {
			hasUnread = true
			break
		}
	}

	if !hasUnread {
		return
	}

	// Check if we can safely inject
	if !w.tracker.CanInject(w.idleTime) {
		return
	}

	// Inject the prompt
	w.inject(w.prompt)
	w.lastInjected = time.Now()
}

func (w *messageWatcher) inject(text string) {
	// Send characters one by one with small delays to simulate typing
	for _, ch := range text {
		_, _ = w.ptmx.WriteString(string(ch))
		time.Sleep(5 * time.Millisecond)
	}
	// Send Enter (just CR - terminal Enter key)
	time.Sleep(10 * time.Millisecond)
	_, _ = w.ptmx.WriteString("\r")
}

func runWrap(cmd *cobra.Command, args []string) error {
	// Handle help flag manually since we disabled flag parsing
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return cmd.Help()
		}
	}

	// Check if stdin is a terminal
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("wrap command requires an interactive terminal")
	}

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Open the store for message monitoring
	dataDir := getDataDir()
	s, err := store.NewSQLiteStore(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer func() { _ = s.Close() }()

	// Create the claude command with all passed arguments
	c := exec.Command("claude", args...)
	c.Dir = workDir

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
	watcher := newMessageWatcher(s, workDir, ptmx, tracker)
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
