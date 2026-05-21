package mcp

import (
	"fmt"
	"strings"

	"github.com/maorbril/clauder/internal/review"
	"github.com/maorbril/clauder/internal/store"
	"github.com/maorbril/clauder/internal/telemetry"
)

const maxPlanSize = 256 << 10 // 256KB

// reviewManager constructs a Manager bound to this server's store on demand.
// Manager state (subscribers) is per-process, but we don't currently rely on
// in-process subscribers from MCP — events are persisted and the HTTP server
// owns the SSE fan-out.
func (s *Server) reviewManager() *review.Manager {
	return review.NewManager(s.store)
}

func (s *Server) toolSubmitPlanForReview(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("submit_plan_for_review")

	plan, _ := args["plan_markdown"].(string)
	plan = strings.TrimSpace(plan)
	if plan == "" {
		return errorResult("plan_markdown is required and must be non-empty")
	}
	if len(plan) > maxPlanSize {
		return errorResult(fmt.Sprintf("plan_markdown exceeds maximum size of %d bytes", maxPlanSize))
	}
	title, _ := args["title"].(string)

	rm := s.reviewManager()
	sess, err := rm.Create(review.CreateOpts{
		Title:        title,
		PlanMarkdown: plan,
		WorkDir:      s.workDir,
		DirectoryID:  s.directoryID,
		InstanceID:   s.instanceID,
	})
	if err != nil {
		return errorResult(fmt.Sprintf("failed to create review: %v", err))
	}

	url := rm.ReviewURL(sess.ID)
	msg := fmt.Sprintf(
		"Plan submitted for review.\n\n"+
			"session_id: %s\nurl: %s\n\n"+
			"STOP. Do not start building. Wait for the user to comment in the review UI or approve the plan. "+
			"When the user comments, you will receive a clauder message describing the comment — reply via reply_to_comment, "+
			"or address it by submitting a revised plan via submit_plan_revision. "+
			"When the user approves, you will receive a clauder message saying so; only then begin implementation.",
		sess.ID, url,
	)
	return textResult(msg)
}

func (s *Server) toolSubmitPlanRevision(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("submit_plan_revision")

	sessionID, _ := args["session_id"].(string)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errorResult("session_id is required")
	}
	plan, _ := args["plan_markdown"].(string)
	plan = strings.TrimSpace(plan)
	if plan == "" {
		return errorResult("plan_markdown is required and must be non-empty")
	}
	if len(plan) > maxPlanSize {
		return errorResult(fmt.Sprintf("plan_markdown exceeds maximum size of %d bytes", maxPlanSize))
	}

	rm := s.reviewManager()
	rev, err := rm.SubmitRevision(sessionID, plan)
	if err != nil {
		return errorResult(err.Error())
	}
	url := rm.ReviewURL(sessionID)
	return textResult(fmt.Sprintf(
		"Submitted revision %d for session %s.\nReview at: %s\n\nWait for further feedback or approval before building.",
		rev.RevisionNumber, sessionID, url,
	))
}

func (s *Server) toolReplyToComment(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("reply_to_comment")

	sessionID, _ := args["session_id"].(string)
	parentID, _ := args["parent_comment_id"].(string)
	body, _ := args["body"].(string)
	sessionID = strings.TrimSpace(sessionID)
	parentID = strings.TrimSpace(parentID)
	body = strings.TrimSpace(body)
	if sessionID == "" || parentID == "" || body == "" {
		return errorResult("session_id, parent_comment_id, and body are all required")
	}

	rm := s.reviewManager()
	c, err := rm.AddAgentReply(sessionID, parentID, body)
	if err != nil {
		return errorResult(err.Error())
	}
	return textResult(fmt.Sprintf("Replied to comment %s with reply %s.", parentID, c.ID))
}

func (s *Server) toolGetReviewPlan(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("get_review_plan")

	sessionID, _ := args["session_id"].(string)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errorResult("session_id is required")
	}
	rm := s.reviewManager()
	state, err := rm.GetState(sessionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if state == nil {
		return errorResult("session not found")
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("session: %s\n", state.Session.ID))
	sb.WriteString(fmt.Sprintf("title: %s\n", state.Session.Title))
	sb.WriteString(fmt.Sprintf("status: %s\n", state.Session.Status))
	if state.Current != nil {
		sb.WriteString(fmt.Sprintf("revision: %d\n\n", state.Current.RevisionNumber))
		sb.WriteString(state.Current.PlanMarkdown)
	}
	openComments := countOpen(state.Comments)
	if openComments > 0 {
		sb.WriteString(fmt.Sprintf("\n\n---\n%d open comment(s) — see %s\n", openComments, rm.ReviewURL(sessionID)))
	}
	return textResult(sb.String())
}

func (s *Server) toolListReviewSessions(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("list_review_sessions")

	var statuses []string
	if raw, ok := args["statuses"].([]interface{}); ok {
		for _, r := range raw {
			if s, ok := r.(string); ok && s != "" {
				statuses = append(statuses, s)
			}
		}
	}
	mineOnly, _ := args["mine_only"].(bool)

	rm := s.reviewManager()
	var sessions []store.ReviewSession
	var err error
	if mineOnly {
		sessions, err = s.store.ListReviewSessionsByInstance(s.instanceID, statuses)
	} else {
		sessions, err = s.store.ListReviewSessionsByStatus(statuses, 100)
	}
	if err != nil {
		return errorResult(err.Error())
	}
	if len(sessions) == 0 {
		return textResult("No plan-review sessions found.")
	}
	var sb strings.Builder
	for _, sess := range sessions {
		sb.WriteString(fmt.Sprintf("- %s [%s] %s — %s\n", sess.ID, sess.Status, truncate(sess.Title, 60), rm.ReviewURL(sess.ID)))
	}
	return textResult(sb.String())
}

func countOpen(comments []store.ReviewComment) int {
	n := 0
	for _, c := range comments {
		if c.Status == store.CommentStatusOpen {
			n++
		}
	}
	return n
}
