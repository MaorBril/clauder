package review

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/maorbril/clauder/internal/store"
)

const (
	// Sender ID used when the review system posts messages to the agent.
	ReviewSender = "review-ui"

	// Default port the dashboard listens on; reused for review URLs.
	defaultUIPort = 8765
)

// Manager owns the lifecycle of plan-review sessions and is the single place
// where MCP tools, HTTP handlers, and the CLI converge.
type Manager struct {
	store store.Store

	mu      sync.Mutex
	streams map[string][]chan store.ReviewEvent // sessionID -> SSE listeners
	port    int                                  // dashboard port for URL construction
}

func NewManager(s store.Store) *Manager {
	return &Manager{
		store:   s,
		streams: make(map[string][]chan store.ReviewEvent),
		port:    defaultUIPort,
	}
}

// SetUIPort lets callers (cmd/ui.go) tell the manager which port the dashboard
// is running on, so review URLs are accurate.
func (m *Manager) SetUIPort(p int) {
	if p > 0 {
		m.port = p
	}
}

// CreateOpts describes a brand-new plan submitted by the agent.
type CreateOpts struct {
	Title        string
	PlanMarkdown string
	WorkDir      string
	DirectoryID  string
	InstanceID   string
}

// Create persists a new review session with its first revision and returns it.
func (m *Manager) Create(opts CreateOpts) (*store.ReviewSession, error) {
	sessID := newID("rs")
	revID := newID("rv")
	sections := ParseSections(opts.PlanMarkdown)

	sess := store.ReviewSession{
		ID:                sessID,
		Title:             firstNonEmpty(opts.Title, fallbackTitle(opts.PlanMarkdown)),
		Status:            store.ReviewStatusAwaitingReview,
		WorkDir:           opts.WorkDir,
		DirectoryID:       opts.DirectoryID,
		InstanceID:        opts.InstanceID,
		CurrentRevisionID: revID,
	}
	if err := m.store.CreateReviewSession(sess); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	rev := store.ReviewRevision{
		ID:             revID,
		SessionID:      sessID,
		RevisionNumber: 0,
		PlanMarkdown:   opts.PlanMarkdown,
		Sections:       sections,
	}
	if err := m.store.AddReviewRevision(rev); err != nil {
		return nil, fmt.Errorf("add revision: %w", err)
	}

	return &sess, nil
}

// ReviewURL returns the canonical URL for opening a session in the browser.
func (m *Manager) ReviewURL(sessionID string) string {
	return fmt.Sprintf("http://localhost:%d/review/%s", m.port, sessionID)
}

// State is the consolidated view served to the HTML SPA.
type State struct {
	Session   store.ReviewSession   `json:"session"`
	Revisions []store.ReviewRevision `json:"revisions"`
	Current   *store.ReviewRevision  `json:"current"`
	Comments  []store.ReviewComment  `json:"comments"`
}

func (m *Manager) GetState(sessionID string) (*State, error) {
	sess, err := m.store.GetReviewSession(sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, nil
	}
	revs, err := m.store.ListReviewRevisions(sessionID)
	if err != nil {
		return nil, err
	}
	var current *store.ReviewRevision
	for i := range revs {
		if revs[i].ID == sess.CurrentRevisionID {
			current = &revs[i]
			break
		}
	}
	comments, err := m.store.ListReviewComments(sessionID)
	if err != nil {
		return nil, err
	}
	return &State{
		Session:   *sess,
		Revisions: revs,
		Current:   current,
		Comments:  comments,
	}, nil
}

// PatchPlan is a small-edit alternative to SubmitRevision. The agent supplies
// a unique substring (oldStr) from the current revision and its replacement;
// the server splices and produces a new revision without the agent having to
// re-emit the entire plan. Errors if oldStr is missing or non-unique.
func (m *Manager) PatchPlan(sessionID, oldStr, newStr string) (*store.ReviewRevision, error) {
	if oldStr == "" {
		return nil, fmt.Errorf("old_str is required")
	}
	sess, err := m.store.GetReviewSession(sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	rev, err := m.store.GetReviewRevision(sess.CurrentRevisionID)
	if err != nil {
		return nil, err
	}
	if rev == nil {
		return nil, fmt.Errorf("current revision missing for %s", sessionID)
	}
	count := strings.Count(rev.PlanMarkdown, oldStr)
	if count == 0 {
		return nil, fmt.Errorf("old_str not found in current revision")
	}
	if count > 1 {
		return nil, fmt.Errorf("old_str matches %d places; include more surrounding text to make it unique", count)
	}
	patched := strings.Replace(rev.PlanMarkdown, oldStr, newStr, 1)
	return m.SubmitRevision(sessionID, patched)
}

// SubmitRevision is called by the agent after addressing feedback.
// It re-anchors existing comments against the new plan and records an event.
func (m *Manager) SubmitRevision(sessionID, planMarkdown string) (*store.ReviewRevision, error) {
	sess, err := m.store.GetReviewSession(sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	if sess.Status == store.ReviewStatusApproved || sess.Status == store.ReviewStatusCancelled {
		return nil, fmt.Errorf("session %s is %s; cannot submit new revision", sessionID, sess.Status)
	}

	prev, err := m.store.GetReviewRevision(sess.CurrentRevisionID)
	if err != nil {
		return nil, err
	}

	revs, err := m.store.ListReviewRevisions(sessionID)
	if err != nil {
		return nil, err
	}
	nextNumber := len(revs)

	sections := ParseSections(planMarkdown)
	newRev := store.ReviewRevision{
		ID:             newID("rv"),
		SessionID:      sessionID,
		RevisionNumber: nextNumber,
		PlanMarkdown:   planMarkdown,
		Sections:       sections,
	}
	if err := m.store.AddReviewRevision(newRev); err != nil {
		return nil, err
	}

	if prev != nil {
		comments, err := m.store.ListReviewComments(sessionID)
		if err == nil {
			results := Reanchor(prev.PlanMarkdown, comments, planMarkdown, sections)
			for _, r := range results {
				_ = m.store.UpdateReviewCommentAnchor(
					r.CommentID, r.NewSectionID, r.NewFingerprint,
					r.NewStartOffset, r.NewEndOffset, r.NewStatus,
				)
			}
		}
	}

	if err := m.store.UpdateReviewCurrentRevision(sessionID, newRev.ID); err != nil {
		return nil, err
	}
	if sess.Status != store.ReviewStatusAwaitingReview {
		_ = m.store.UpdateReviewSessionStatus(sessionID, store.ReviewStatusAwaitingReview)
	}

	payload, _ := json.Marshal(map[string]any{
		"revision_id":     newRev.ID,
		"revision_number": newRev.RevisionNumber,
	})
	m.emit(sessionID, store.ReviewEvent{
		SessionID:   sessionID,
		Kind:        store.ReviewEventRevisionSubmitted,
		PayloadJSON: string(payload),
	})

	return &newRev, nil
}

// AddUserComment is called by the HTML SPA. It persists the comment, raises a
// review event, and notifies the agent via clauder's message system so the
// existing wrap watcher injects a prompt into the PTY.
type CommentOpts struct {
	Body              string
	AnchorSectionID   string
	AnchorStartOffset int
	AnchorEndOffset   int
	ParentCommentID   string
}

func (m *Manager) AddUserComment(sessionID string, opts CommentOpts) (*store.ReviewComment, error) {
	body := strings.TrimSpace(opts.Body)
	if body == "" {
		return nil, fmt.Errorf("comment body required")
	}
	sess, err := m.store.GetReviewSession(sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	rev, err := m.store.GetReviewRevision(sess.CurrentRevisionID)
	if err != nil {
		return nil, err
	}
	if rev == nil {
		return nil, fmt.Errorf("current revision missing for %s", sessionID)
	}

	fingerprint := ""
	if opts.AnchorEndOffset > opts.AnchorStartOffset && opts.AnchorEndOffset <= len(rev.PlanMarkdown) {
		fingerprint = Fingerprint(rev.PlanMarkdown[opts.AnchorStartOffset:opts.AnchorEndOffset])
	}

	c := store.ReviewComment{
		ID:                    newID("c"),
		SessionID:             sessionID,
		RevisionIDCreated:     rev.ID,
		ParentID:              opts.ParentCommentID,
		AnchorSectionID:       opts.AnchorSectionID,
		AnchorTextFingerprint: fingerprint,
		AnchorStartOffset:     opts.AnchorStartOffset,
		AnchorEndOffset:       opts.AnchorEndOffset,
		Status:                store.CommentStatusOpen,
		Author:                "user",
		Body:                  body,
	}
	if err := m.store.AddReviewComment(c); err != nil {
		return nil, err
	}

	kind := store.ReviewEventCommentAdded
	if opts.ParentCommentID != "" {
		kind = store.ReviewEventReplyAdded
	}
	payload, _ := json.Marshal(map[string]any{
		"comment_id":      c.ID,
		"parent_id":       c.ParentID,
		"section_id":      c.AnchorSectionID,
		"body":            c.Body,
		"is_reply":        c.ParentID != "",
	})
	m.emit(sessionID, store.ReviewEvent{
		SessionID:   sessionID,
		Kind:        kind,
		PayloadJSON: string(payload),
	})

	m.notifyAgent(sess, formatCommentNotice(sess, &c, rev))
	return &c, nil
}

// AddAgentReply is called by the agent (via MCP) to add a reply within a thread.
func (m *Manager) AddAgentReply(sessionID, parentID, body string) (*store.ReviewComment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("reply body required")
	}
	sess, err := m.store.GetReviewSession(sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	parent, err := m.store.GetReviewComment(parentID)
	if err != nil {
		return nil, err
	}
	if parent == nil || parent.SessionID != sessionID {
		return nil, fmt.Errorf("parent comment %s not found in session %s", parentID, sessionID)
	}

	c := store.ReviewComment{
		ID:                    newID("c"),
		SessionID:             sessionID,
		RevisionIDCreated:     sess.CurrentRevisionID,
		ParentID:              parentID,
		AnchorSectionID:       parent.AnchorSectionID,
		AnchorTextFingerprint: parent.AnchorTextFingerprint,
		AnchorStartOffset:     parent.AnchorStartOffset,
		AnchorEndOffset:       parent.AnchorEndOffset,
		Status:                store.CommentStatusOpen,
		Author:                "agent",
		Body:                  body,
	}
	if err := m.store.AddReviewComment(c); err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]any{
		"comment_id": c.ID,
		"parent_id":  parentID,
		"body":       body,
	})
	m.emit(sessionID, store.ReviewEvent{
		SessionID:   sessionID,
		Kind:        store.ReviewEventReplyAdded,
		PayloadJSON: string(payload),
	})
	return &c, nil
}

// RequestRevision is the freeform "I want changes" button on the SPA.
func (m *Manager) RequestRevision(sessionID, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Errorf("revision request body required")
	}
	sess, err := m.store.GetReviewSession(sessionID)
	if err != nil {
		return err
	}
	if sess == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}
	if err := m.store.UpdateReviewSessionStatus(sessionID, store.ReviewStatusRevising); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"body": body})
	m.emit(sessionID, store.ReviewEvent{
		SessionID:   sessionID,
		Kind:        store.ReviewEventRevisionRequested,
		PayloadJSON: string(payload),
	})
	m.notifyAgent(sess, formatRevisionNotice(sess, body))
	return nil
}

// Approve is the green button. Once approved the agent can start building.
func (m *Manager) Approve(sessionID string) error {
	sess, err := m.store.GetReviewSession(sessionID)
	if err != nil {
		return err
	}
	if sess == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}
	if err := m.store.UpdateReviewSessionStatus(sessionID, store.ReviewStatusApproved); err != nil {
		return err
	}
	m.emit(sessionID, store.ReviewEvent{
		SessionID:   sessionID,
		Kind:        store.ReviewEventApproved,
		PayloadJSON: "{}",
	})
	m.notifyAgent(sess, formatApprovalNotice(sess))
	return nil
}

// Cancel is the abandon button.
func (m *Manager) Cancel(sessionID string) error {
	sess, err := m.store.GetReviewSession(sessionID)
	if err != nil {
		return err
	}
	if sess == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}
	if err := m.store.UpdateReviewSessionStatus(sessionID, store.ReviewStatusCancelled); err != nil {
		return err
	}
	m.emit(sessionID, store.ReviewEvent{
		SessionID:   sessionID,
		Kind:        store.ReviewEventCancelled,
		PayloadJSON: "{}",
	})
	m.notifyAgent(sess, formatCancelNotice(sess))
	return nil
}

// Subscribe returns a channel that receives every event for the session.
// The caller must call the returned cancel function to remove the subscriber.
// The channel is never closed: emit drops to slow consumers via select-default,
// and once a subscriber is removed no goroutine will ever send to it again, so
// it becomes garbage. Closing here would race with emit's outside-lock send.
func (m *Manager) Subscribe(sessionID string) (<-chan store.ReviewEvent, func()) {
	ch := make(chan store.ReviewEvent, 16)
	m.mu.Lock()
	m.streams[sessionID] = append(m.streams[sessionID], ch)
	m.mu.Unlock()

	cancel := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		list := m.streams[sessionID]
		for i, c := range list {
			if c == ch {
				m.streams[sessionID] = append(list[:i], list[i+1:]...)
				return
			}
		}
	}
	return ch, cancel
}

// emit persists the event, sets its ID, and fans it out to live subscribers.
// Sends are non-blocking; a slow consumer just misses the event and is
// expected to resync via GET /state or by replaying ListReviewEvents.
func (m *Manager) emit(sessionID string, e store.ReviewEvent) {
	id, err := m.store.AddReviewEvent(e)
	if err == nil {
		e.ID = id
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}

	m.mu.Lock()
	subs := append([]chan store.ReviewEvent(nil), m.streams[sessionID]...)
	m.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// notifyAgent posts a clauder message to the agent's instance. The existing
// wrap watcher polls unread messages and injects them into the PTY.
func (m *Manager) notifyAgent(sess *store.ReviewSession, body string) {
	if sess.InstanceID == "" {
		return
	}
	_, _ = m.store.SendMessage(ReviewSender, sess.InstanceID, body)
}

func formatCommentNotice(sess *store.ReviewSession, c *store.ReviewComment, rev *store.ReviewRevision) string {
	section := "(general)"
	if c.AnchorSectionID != "" {
		section = c.AnchorSectionID
	} else if rev != nil {
		if s := FindSectionByOffset(rev.Sections, c.AnchorStartOffset); s != nil {
			section = s.ID
		}
	}
	verb := "commented"
	if c.ParentID != "" {
		verb = "replied"
	}
	return fmt.Sprintf(
		"[plan review %s] User %s on section %q: %q\n"+
			"To reply in this thread, call clauder.reply_to_comment(session_id=%q, parent_comment_id=%q, body=...).\n"+
			"For a SMALL textual edit (a few lines), prefer clauder.patch_plan(session_id=%q, old_str=..., new_str=...) — far cheaper than re-emitting the full plan.\n"+
			"For a STRUCTURAL change, call clauder.submit_plan_revision(session_id=%q, plan_markdown=...).\n"+
			"Do not start building yet — wait for the user to approve.",
		sess.ID, verb, section, c.Body, sess.ID, c.ID, sess.ID, sess.ID,
	)
}

func formatRevisionNotice(sess *store.ReviewSession, body string) string {
	return fmt.Sprintf(
		"[plan review %s] User requested revisions: %q\n"+
			"Submit a revised plan via clauder.submit_plan_revision(session_id=%q, plan_markdown=...).",
		sess.ID, body, sess.ID,
	)
}

func formatApprovalNotice(sess *store.ReviewSession) string {
	return fmt.Sprintf(
		"[plan review %s] APPROVED. Begin building according to the approved plan. "+
			"The full plan is available via clauder.get_review_plan(session_id=%q).",
		sess.ID, sess.ID,
	)
}

func formatCancelNotice(sess *store.ReviewSession) string {
	return fmt.Sprintf(
		"[plan review %s] User cancelled the review. Do not start building. "+
			"Ask the user what they want to do next.",
		sess.ID,
	)
}

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

func firstNonEmpty(a, b string) string {
	a = strings.TrimSpace(a)
	if a != "" {
		return a
	}
	return strings.TrimSpace(b)
}

func fallbackTitle(plan string) string {
	for _, line := range strings.Split(plan, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(t, "#"))
		}
	}
	for _, line := range strings.Split(plan, "\n") {
		t := strings.TrimSpace(line)
		if t != "" {
			if len(t) > 80 {
				t = t[:77] + "..."
			}
			return t
		}
	}
	return "Plan"
}

// FreePort tries to find a free TCP port; used when the dashboard isn't
// already known. Kept here so review URLs can resolve even from cmd/review.
func FreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return defaultUIPort
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
