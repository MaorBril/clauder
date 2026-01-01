package cmd

import (
	"fmt"

	"github.com/maorbril/clauder/internal/store"
	"github.com/spf13/cobra"
)

var messagesAll bool

var messagesCmd = &cobra.Command{
	Use:   "messages <instance-id>",
	Short: "View messages for an instance",
	Long:  `View messages sent to a specific instance.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runMessages,
}

func init() {
	messagesCmd.Flags().BoolVarP(&messagesAll, "all", "a", false, "Show all messages, not just unread")
}

func runMessages(cmd *cobra.Command, args []string) error {
	dataDir := getDataDir()
	s, err := store.NewSQLiteStore(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer func() { _ = s.Close() }()

	instanceID := args[0]
	unreadOnly := !messagesAll

	messages, err := s.GetMessages(instanceID, unreadOnly)
	if err != nil {
		return fmt.Errorf("failed to get messages: %w", err)
	}

	if len(messages) == 0 {
		if unreadOnly {
			fmt.Println("No unread messages.")
		} else {
			fmt.Println("No messages.")
		}
		return nil
	}

	fmt.Printf("Found %d message(s):\n\n", len(messages))

	for _, m := range messages {
		readStatus := "unread"
		if m.ReadAt != nil {
			readStatus = fmt.Sprintf("read at %s", m.ReadAt.Format("15:04"))
		}
		fmt.Printf("#%d from %s (%s)\n", m.ID, m.FromInstance, readStatus)
		fmt.Printf("  Time: %s\n", m.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("  %s\n\n", m.Content)
	}

	return nil
}
