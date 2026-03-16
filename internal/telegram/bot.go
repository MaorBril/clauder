package telegram

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/maorbril/clauder/internal/store"
	qrcode "github.com/skip2/go-qrcode"
)

const (
	settingChatID       = "telegram_chat_id"
	settingTelegramLock = "telegram_instance_lock"
)

// Injector is a callback that injects text directly into the Claude Code PTY.
// When set, incoming Telegram messages are injected as prompts instead of
// being stored as clauder messages (which would require manual confirmation).
type Injector func(text string)

// Bot bridges Telegram messages with clauder's messaging system.
type Bot struct {
	api        *tgbotapi.BotAPI
	store      store.Store
	instanceID string
	chatID     int64
	injector   Injector
	stopCh     chan struct{}
	notifyCh   chan struct{} // signals outboundLoop to check for messages immediately
	wg         sync.WaitGroup
}

// SetInjector sets a callback that injects text directly into the Claude Code PTY.
// When set, incoming messages bypass the clauder message store and are injected
// as prompts immediately, without requiring user confirmation.
func (b *Bot) SetInjector(fn Injector) {
	b.injector = fn
}

// NewBot creates a Telegram bot. Token comes from CLAUDER_TELEGRAM_TOKEN env var.
// Returns an error if another instance already holds the telegram lock.
func NewBot(s store.Store, instanceID string) (*Bot, error) {
	token := os.Getenv("CLAUDER_TELEGRAM_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("CLAUDER_TELEGRAM_TOKEN environment variable is required")
	}

	// Check and acquire the telegram lock
	if err := acquireLock(s, instanceID); err != nil {
		return nil, err
	}

	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		_ = releaseLock(s, instanceID)
		return nil, fmt.Errorf("failed to connect to Telegram API: %w", err)
	}

	bot := &Bot{
		api:        api,
		store:      s,
		instanceID: instanceID,
		stopCh:     make(chan struct{}),
		notifyCh:   make(chan struct{}, 1),
	}

	// Load stored chat ID
	chatIDStr, err := s.GetSetting(settingChatID)
	if err != nil {
		_ = releaseLock(s, instanceID)
		return nil, fmt.Errorf("failed to read chat ID setting: %w", err)
	}
	if chatIDStr != "" {
		bot.chatID, _ = strconv.ParseInt(chatIDStr, 10, 64)
	}

	return bot, nil
}

// Start begins the bot's polling loops. If no chat ID is stored, it shows a QR code
// and waits for the user to send /start.
func (b *Bot) Start() error {
	if b.chatID == 0 {
		if err := b.waitForPairing(); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(os.Stderr, "[clauder] Telegram connected to @%s (chat %d)\r\n", b.api.Self.UserName, b.chatID)
	}

	b.wg.Add(2)
	go b.receiveLoop()
	go b.outboundLoop()
	return nil
}

// Stop shuts down the bot and releases the telegram lock.
func (b *Bot) Stop() {
	close(b.stopCh)
	b.api.StopReceivingUpdates()
	b.wg.Wait()
	_ = releaseLock(b.store, b.instanceID)
}

// waitForPairing displays a QR code and waits for the first /start message.
func (b *Bot) waitForPairing() error {
	botURL := fmt.Sprintf("https://t.me/%s", b.api.Self.UserName)

	fmt.Fprintf(os.Stderr, "\r\n[clauder] Scan this QR code to connect Telegram:\r\n\r\n")
	printQR(botURL)
	fmt.Fprintf(os.Stderr, "\r\n  Or open: %s\r\n", botURL)
	fmt.Fprintf(os.Stderr, "[clauder] Waiting for Telegram connection...\r\n\r\n")

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30
	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-b.stopCh:
			return fmt.Errorf("stopped while waiting for pairing")
		case update, ok := <-updates:
			if !ok {
				return fmt.Errorf("update channel closed")
			}
			if update.Message == nil {
				continue
			}
			b.chatID = update.Message.Chat.ID
			if err := b.store.SetSetting(settingChatID, strconv.FormatInt(b.chatID, 10)); err != nil {
				return fmt.Errorf("failed to store chat ID: %w", err)
			}
			fmt.Fprintf(os.Stderr, "[clauder] Telegram connected to chat %d\r\n", b.chatID)

			// Send welcome message
			msg := tgbotapi.NewMessage(b.chatID, "Connected to clauder! Messages you send here will be forwarded to your Claude session.")
			_, _ = b.api.Send(msg)
			return nil
		}
	}
}

// receiveLoop polls Telegram for incoming messages and stores them as clauder messages.
func (b *Bot) receiveLoop() {
	defer b.wg.Done()

	consecutiveErrors := 0
	const maxConsecutiveErrors = 3
	lastUpdateID := 0

	for {
		select {
		case <-b.stopCh:
			return
		default:
		}

		u := tgbotapi.NewUpdate(lastUpdateID + 1)
		u.Timeout = 30

		updates, err := b.api.GetUpdates(u)
		if err != nil {
			if strings.Contains(err.Error(), "Conflict") {
				fmt.Fprintf(os.Stderr, "[clauder] telegram: another bot instance is polling — stopping receiver\r\n")
				return
			}
			consecutiveErrors++
			if consecutiveErrors >= maxConsecutiveErrors {
				fmt.Fprintf(os.Stderr, "[clauder] telegram: too many errors, stopping receiver: %v\r\n", err)
				return
			}
			fmt.Fprintf(os.Stderr, "[clauder] telegram: poll error (%d/%d): %v\r\n", consecutiveErrors, maxConsecutiveErrors, err)
			time.Sleep(time.Duration(consecutiveErrors) * 3 * time.Second)
			continue
		}

		consecutiveErrors = 0
		for _, update := range updates {
			if update.UpdateID >= lastUpdateID {
				lastUpdateID = update.UpdateID
			}
			if update.Message == nil || update.Message.Chat.ID != b.chatID {
				continue
			}
			text := update.Message.Text
			if text == "" {
				continue
			}
			if b.injector != nil {
				// Inject directly into the PTY — no confirmation needed
				b.injector(text)
			} else {
				_, err := b.store.SendMessage("telegram", b.instanceID, text)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[clauder] telegram: failed to store message: %v\r\n", err)
				}
			}
		}
	}
}

// Notify signals the outbound loop to check for messages immediately.
// Non-blocking — safe to call from any goroutine.
func (b *Bot) Notify() {
	select {
	case b.notifyCh <- struct{}{}:
	default:
	}
}

// outboundLoop watches for messages from the claude instance addressed to "telegram"
// and forwards them to the Telegram chat.
func (b *Bot) outboundLoop() {
	defer b.wg.Done()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-b.stopCh:
			return
		case <-b.notifyCh:
			b.forwardOutbound()
		case <-ticker.C:
			b.forwardOutbound()
		}
	}
}

func (b *Bot) forwardOutbound() {
	messages, err := b.store.GetMessages("telegram", true)
	if err != nil || len(messages) == 0 {
		return
	}

	for _, m := range messages {
		text := m.Content
		// Truncate very long messages (Telegram limit is 4096)
		if len(text) > 4000 {
			text = text[:4000] + "\n... (truncated)"
		}

		msg := tgbotapi.NewMessage(b.chatID, text)
		_, err := b.api.Send(msg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[clauder] telegram: failed to send: %v\r\n", err)
			continue
		}
		_ = b.store.MarkMessageRead(m.ID)
	}
}

// acquireLock ensures only one instance can use --telegram at a time.
func acquireLock(s store.Store, instanceID string) error {
	existing, err := s.GetSetting(settingTelegramLock)
	if err != nil {
		return fmt.Errorf("failed to check telegram lock: %w", err)
	}

	if existing != "" && existing != instanceID {
		// Check if the locking process is still alive
		parts := strings.SplitN(existing, "|", 2)
		if len(parts) == 2 {
			pid, _ := strconv.Atoi(parts[1])
			if pid > 0 && isProcessRunning(pid) {
				return fmt.Errorf("another instance (%s) already has --telegram active", parts[0])
			}
		}
		// Stale lock — take it over
	}

	lockVal := fmt.Sprintf("%s|%d", instanceID, os.Getpid())
	return s.SetSetting(settingTelegramLock, lockVal)
}

func releaseLock(s store.Store, instanceID string) error {
	existing, _ := s.GetSetting(settingTelegramLock)
	if strings.HasPrefix(existing, instanceID+"|") {
		return s.DeleteSetting(settingTelegramLock)
	}
	return nil
}

// isProcessRunning checks if a process with the given PID is still alive.
func isProcessRunning(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(nil) == nil
}

// printQR renders a QR code to stderr using Unicode half-block characters.
func printQR(url string) {
	qr, err := qrcode.New(url, qrcode.Medium)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  (could not generate QR code: %v)\r\n", err)
		return
	}

	bitmap := qr.Bitmap()
	rows := len(bitmap)

	// Process two rows at a time using half-block characters
	for y := 0; y < rows; y += 2 {
		fmt.Fprint(os.Stderr, "  ")
		for x := range bitmap[y] {
			top := bitmap[y][x]
			bottom := false
			if y+1 < rows {
				bottom = bitmap[y+1][x]
			}

			// true = black module, false = white module
			switch {
			case top && bottom:
				fmt.Fprint(os.Stderr, "\u2588") // █ full block
			case top && !bottom:
				fmt.Fprint(os.Stderr, "\u2580") // ▀ upper half
			case !top && bottom:
				fmt.Fprint(os.Stderr, "\u2584") // ▄ lower half
			default:
				fmt.Fprint(os.Stderr, " ") // space
			}
		}
		fmt.Fprint(os.Stderr, "\r\n")
	}
}
