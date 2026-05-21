package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/maorbril/clauder/internal/store"
	"github.com/spf13/cobra"
)

var (
	reviewOpenAll   bool
	reviewServePort int
)

var reviewCmd = &cobra.Command{
	Use:   "review [session-id]",
	Short: "List plan-review sessions or open one in the browser",
	Long: `Plan reviews are markdown plans your agent submits via the submit_plan_for_review
MCP tool. Without arguments this lists recent sessions. With a session ID,
opens the review page in your browser.`,
	RunE: runReview,
}

func init() {
	reviewCmd.Flags().BoolVarP(&reviewOpenAll, "all", "a", false, "Include approved and cancelled sessions in the list")
	reviewCmd.Flags().IntVarP(&reviewServePort, "port", "p", 8765, "Port the review UI is served on")
}

func runReview(cmd *cobra.Command, args []string) error {
	dataDir := getDataDir()
	s, err := store.NewSQLiteStore(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer func() { _ = s.Close() }()

	if len(args) == 1 {
		url := fmt.Sprintf("http://localhost:%d/review/%s", reviewServePort, args[0])
		fmt.Println(url)
		openBrowser(url)
		return nil
	}

	var statuses []string
	if !reviewOpenAll {
		statuses = []string{store.ReviewStatusAwaitingReview, store.ReviewStatusRevising}
	}
	sessions, err := s.ListReviewSessionsByStatus(statuses, 50)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Fprintln(os.Stderr, "No plan-review sessions.")
		return nil
	}
	for _, sess := range sessions {
		title := sess.Title
		if len(title) > 60 {
			title = title[:57] + "..."
		}
		age := time.Since(sess.UpdatedAt).Round(time.Minute)
		fmt.Printf("%s  %-16s  %s  (updated %s ago)\n  http://localhost:%d/review/%s\n",
			sess.ID, sess.Status, title, age, reviewServePort, sess.ID)
	}
	return nil
}
