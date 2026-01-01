package cmd

import (
	"fmt"
	"strings"

	"github.com/maorbril/clauder/internal/store"
	"github.com/spf13/cobra"
)

var sendCmd = &cobra.Command{
	Use:   "send <instance-id> <message>",
	Short: "Send a message to another instance",
	Long:  `Send a message to another running clauder instance.`,
	Args:  cobra.MinimumNArgs(2),
	RunE:  runSend,
}

func runSend(cmd *cobra.Command, args []string) error {
	dataDir := getDataDir()
	s, err := store.NewSQLiteStore(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer func() { _ = s.Close() }()

	to := args[0]
	content := strings.Join(args[1:], " ")

	// Check if target instance exists
	target, err := s.GetInstance(to)
	if err != nil {
		return fmt.Errorf("failed to find instance: %w", err)
	}
	if target == nil {
		return fmt.Errorf("instance '%s' not found", to)
	}

	msg, err := s.SendMessage("cli", to, content)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	fmt.Printf("Message #%d sent to %s\n", msg.ID, to)
	return nil
}
