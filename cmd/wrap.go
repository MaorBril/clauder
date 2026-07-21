//go:build !windows

package cmd

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/maorbril/clauder/internal/store"
	"github.com/maorbril/clauder/internal/telegram"
	"github.com/maorbril/clauder/internal/telemetry"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	wrapInstanceName string
	wrapTelegram     bool
	wrapSlave        bool
)

func init() {
	wrapCmd.Flags().StringVarP(&wrapInstanceName, "name", "n", "", "Instance name for multi-instance setups (e.g., 'backend', 'frontend')")
	wrapCmd.Flags().BoolVar(&wrapTelegram, "telegram", false, "Enable Telegram bridge (requires CLAUDER_TELEGRAM_TOKEN)")
	wrapCmd.Flags().BoolVar(&wrapSlave, "slave", false, "Run in slave mode with auto-approved permissions for autonomous operation")
}

var wrapCmd = &cobra.Command{
	Use:   "wrap [flags] [-- claude args...]",
	Short: "Run Claude Code with clauder wrapper",
	Long: `Runs Claude Code as a subprocess with full terminal passthrough.

This wrapper mode allows clauder to intercept and augment Claude Code sessions.
Use -- to separate clauder flags from arguments passed to Claude Code.

The wrapper monitors for incoming messages from other Claude instances and
automatically prompts Claude to check them when the input line is empty.

Examples:
  clauder wrap                              # Start interactive Claude Code session
  clauder wrap -- -p "fix the bug"          # Pass a prompt to Claude Code
  clauder wrap -- --resume                  # Resume previous session
  clauder wrap --name backend               # Named instance
  clauder wrap --name backend -- --resume   # Named instance with claude args
  clauder wrap --telegram                   # Enable Telegram bridge
  clauder wrap --telegram --name bot        # Telegram with named instance
  clauder wrap --slave --name worker        # Autonomous slave instance`,
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
	injectMu     sync.Mutex    // serializes PTY injections to prevent interleaving
	tgBot        *telegram.Bot // when set, notify user via Telegram about unread messages

	// Pending injection queue: when CanInject=false, we queue the notification
	// and retry on subsequent ticks until the PTY is idle.
	pendingPrompt    string   // the prompt to inject once idle
	pendingNames     []string // instance names with unread messages (for dedup)
	pendingTgSent    bool     // whether we already sent the Telegram notification for this pending batch
	cantInjectLogged bool     // whether we already logged "CanInject=false" for this pending batch

	// In-flight guard: after injecting, we wait for Claude to process the
	// message (mark it read) before checking again. This prevents the watcher
	// from spinning on CanInject=false while Claude is busy responding to
	// the injection we just sent.
	inFlight      bool          // true after injection, cleared when unread count drops to 0
	inFlightSince time.Time     // when the injection was sent (safety timeout)
	inFlightMax   time.Duration // max time to wait before giving up on in-flight guard
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
		inFlightMax:  2 * time.Minute,  // safety timeout for in-flight guard
	}
}

// SetTelegramBot enables Telegram notifications for unread messages.
// When set, the watcher sends a Telegram alert when messages arrive
// and uses a shorter cooldown between checks.
func (w *messageWatcher) SetTelegramBot(bot *telegram.Bot) {
	w.tgBot = bot
	w.cooldown = 5 * time.Second // faster checks in telegram mode
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

// sameNames returns true if two sorted string slices contain the same elements.
func sameNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sliceContains returns true if slice contains the given element.
func sliceContains(slice []string, elem string) bool {
	for _, s := range slice {
		if s == elem {
			return true
		}
	}
	return false
}

func (w *messageWatcher) checkAndInject() {
	// Check cooldown - don't spam injections
	sinceLastInject := time.Since(w.lastInjected)
	if sinceLastInject < w.cooldown && w.pendingPrompt == "" && !w.inFlight {
		return
	}

	// Query instances in our directory using directoryID
	instances, err := w.store.GetInstancesByDirectory(w.directoryID)
	if err != nil {
		log.Printf("[watcher] GetInstancesByDirectory error: %v", err)
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

	// Also check for broadcast messages sent to the bare directory ID
	// (when messages are sent to a directory, not a specific instance).
	// Only check if w.instanceName is empty (this instance is the primary/directory-default).
	if w.instanceName == "" {
		broadcastMsgs, err := w.store.GetMessages(w.directoryID, true) // unread only
		if err == nil && len(broadcastMsgs) > 0 {
			// Found broadcast messages for the directory
			if !sliceContains(unreadFor, "primary") {
				unreadFor = append(unreadFor, "primary")
			}
		}
	}

	if len(unreadFor) == 0 {
		// No more unread messages — clear any pending injection and in-flight guard
		w.pendingPrompt = ""
		w.pendingNames = nil
		w.pendingTgSent = false
		w.cantInjectLogged = false
		w.inFlight = false
		return
	}

	// In-flight guard: after injecting, wait for Claude to process the message
	// (mark it read) before trying to inject again. We still check unread count
	// above so the guard clears once messages are read.
	if w.inFlight {
		if time.Since(w.inFlightSince) > w.inFlightMax {
			log.Printf("[watcher] in-flight guard timed out after %v, resuming", w.inFlightMax)
			w.inFlight = false
		} else {
			return
		}
	}

	// Sort for consistent dedup comparison
	sort.Strings(unreadFor)

	// Rebuild prompt only if the unread set changed (dedup)
	if w.pendingPrompt == "" || !sameNames(w.pendingNames, unreadFor) {
		// Build contextual prompt
		var prompt string
		if w.instanceName != "" {
			prompt = "[You have a new message] - Read your clauder messages using get_messages and respond to them."
		} else if len(unreadFor) == 1 {
			prompt = fmt.Sprintf("[New message for '%s'] - Read your clauder messages using get_messages.", unreadFor[0])
		} else {
			prompt = fmt.Sprintf("[Messages for %d instances] - Read your clauder messages using get_messages.", len(unreadFor))
		}

		if w.tgBot != nil {
			prompt += "\nForward the message contents to the user via Telegram using send_message with to=\"telegram\"."
		}

		w.pendingPrompt = prompt
		w.pendingNames = unreadFor
		w.cantInjectLogged = false // new batch: allow one retry log again

		// Send Telegram notification when we first detect unread messages
		if w.tgBot != nil && !w.pendingTgSent {
			w.sendTgNotice(unreadFor)
			w.pendingTgSent = true
		}
	}

	// Try to inject
	if w.tracker.CanInject(w.idleTime) {
		log.Printf("[watcher] injecting for %v", w.pendingNames)
		if w.tgBot != nil && !w.pendingTgSent {
			w.sendTgNotice(unreadFor)
		}
		w.inject(w.pendingPrompt)
		w.lastInjected = time.Now()
		w.inFlight = true
		w.inFlightSince = time.Now()
		w.pendingPrompt = ""
		w.pendingNames = nil
		w.pendingTgSent = false
		w.cantInjectLogged = false
		return
	}

	// Can't inject now — will retry on next tick. Log only once per pending
	// batch; otherwise this spins every tick while the user has typed-but-unsent
	// input (a non-empty buffer keeps CanInject false indefinitely).
	if w.pendingPrompt != "" && !w.cantInjectLogged {
		log.Printf("[watcher] %d unread but CanInject=false, will retry", len(unreadFor))
		w.cantInjectLogged = true
	}
}

func (w *messageWatcher) sendTgNotice(unreadFor []string) {
	var notice string
	if len(unreadFor) == 1 {
		notice = fmt.Sprintf("📬 Incoming message for '%s' — checking now.", unreadFor[0])
	} else {
		notice = fmt.Sprintf("📬 Incoming messages for %d instances — checking now.", len(unreadFor))
	}
	w.tgBot.SendText(notice)
}

func (w *messageWatcher) inject(text string) {
	w.injectMu.Lock()
	defer w.injectMu.Unlock()

	// Write the full text in one shot to preserve spaces.
	// Character-by-character injection was causing spaces to be dropped
	// by the PTY/terminal input handler.
	_, _ = w.ptmx.WriteString(text)
	// Send Enter (CR - what terminal Enter key sends in raw mode)
	time.Sleep(50 * time.Millisecond)
	_, _ = w.ptmx.WriteString("\r")
}

type wrapFlags struct {
	name     string
	telegram bool
	slave    bool
	help     bool
}

// slaveAllowedTools returns the set of tools auto-approved in slave mode.
// Allows file ops, search, bash, web access, and all MCP tools.
// Does NOT use --dangerously-skip-permissions so Claude Code's built-in
// safety checks (e.g., destructive bash commands) still apply.
func slaveAllowedTools() []string {
	return []string{
		"Read",
		"Write",
		"Edit",
		"Glob",
		"Grep",
		"Bash(*)",
		"WebFetch",
		"WebSearch",
		"mcp__clauder__*",
	}
}

// parseWrapArgs splits args into clauder flags and claude args using "--" as separator.
// Everything before "--" is parsed for clauder flags (--name, --help, --telegram).
// Everything after "--" is passed directly to claude.
// If no "--" is present, all args are passed to claude for backwards compatibility.
func parseWrapArgs(args []string) (flags wrapFlags, claudeArgs []string) {
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
			} else if args[i] == "-h" || args[i] == "--help" || args[i] == "--telegram" || args[i] == "--slave" {
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
				flags.name = clauderArgs[i]
			}
		case "--telegram":
			flags.telegram = true
		case "--slave":
			flags.slave = true
		case "-h", "--help":
			flags.help = true
		}
	}

	return flags, claudeArgs
}

func runWrap(cmd *cobra.Command, args []string) error {
	// Parse clauder flags vs claude args
	flags, claudeArgs := parseWrapArgs(args)
	if flags.help {
		return cmd.Help()
	}
	wrapInstanceName = flags.name
	wrapTelegram = flags.telegram
	wrapSlave = flags.slave

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

	// Redirect the standard logger to a file. In wrap mode stderr is the live
	// terminal that Claude Code's TUI draws on, so any log.Printf (notably the
	// message watcher's "CanInject=false, will retry" diagnostics) would
	// scribble over the screen — the well-known "screen full of can't-inject
	// errors" symptom. Send it to a log file, or discard it if that fails,
	// rather than ever writing to the terminal.
	if lf, err := os.OpenFile(filepath.Join(dataDir, "wrap.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		log.SetOutput(lf)
		defer func() { _ = lf.Close() }()
	} else {
		log.SetOutput(io.Discard)
	}

	s, err := store.NewSQLiteStore(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer func() { _ = s.Close() }()

	// When in slave or telegram mode, allow a curated set of tools for autonomous operation.
	if wrapSlave || wrapTelegram {
		for _, tool := range slaveAllowedTools() {
			claudeArgs = append(claudeArgs, "--allowedTools", tool)
		}
	}

	// Create the claude command with claude-specific arguments
	c := exec.Command("claude", claudeArgs...)
	c.Dir = workDir

	// Pass instance name to inner session via environment variable
	if wrapInstanceName != "" {
		c.Env = append(os.Environ(), "CLAUDER_INSTANCE_NAME="+wrapInstanceName)
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
			if ws, err := pty.GetsizeFull(os.Stdin); err == nil {
				_ = pty.Setsize(ptmx, ws)
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

	// Start Telegram bot if requested
	if wrapTelegram {
		tgInstanceID := directoryID
		if wrapInstanceName != "" {
			tgInstanceID = directoryID + ":" + wrapInstanceName
		}
		tgBot, err := telegram.NewBot(s, tgInstanceID)
		if err != nil {
			return fmt.Errorf("telegram: %w", err)
		}
		// Inject telegram messages directly into the PTY so they don't
		// require manual confirmation on the Claude Code instance.
		tgBot.SetInjector(func(text string) {
			prompt := fmt.Sprintf("[Telegram] %s\nReply to the user via Telegram using send_message with to=\"telegram\".", text)
			watcher.inject(prompt)
		})
		// Enable Telegram notifications for incoming instance messages
		watcher.SetTelegramBot(tgBot)
		if err := tgBot.Start(); err != nil {
			return fmt.Errorf("telegram: %w", err)
		}
		defer tgBot.Stop()
	}

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
