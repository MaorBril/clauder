package store

import "time"

// Review session statuses
const (
	ReviewStatusAwaitingReview = "awaiting_review"
	ReviewStatusRevising       = "revising"
	ReviewStatusApproved       = "approved"
	ReviewStatusCancelled      = "cancelled"
)

// Comment statuses
const (
	CommentStatusOpen     = "open"
	CommentStatusResolved = "resolved"
	CommentStatusOrphan   = "orphaned"
)

// Review event kinds
const (
	ReviewEventCommentAdded       = "comment_added"
	ReviewEventReplyAdded         = "reply_added"
	ReviewEventRevisionSubmitted  = "revision_submitted"
	ReviewEventApproved           = "approved"
	ReviewEventCancelled          = "cancelled"
	ReviewEventRevisionRequested  = "revision_requested"
)

// ReviewSession is one plan-review interaction between agent and user.
type ReviewSession struct {
	ID                string     `json:"id"`
	Title             string     `json:"title"`
	Status            string     `json:"status"`
	WorkDir           string     `json:"work_dir"`
	DirectoryID       string     `json:"directory_id"`
	InstanceID        string     `json:"instance_id"`
	CurrentRevisionID string     `json:"current_revision_id"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	ApprovedAt        *time.Time `json:"approved_at,omitempty"`
}

// ReviewSection is the parsed structural anchor for a markdown heading.
type ReviewSection struct {
	ID          string `json:"id"`           // stable slug from heading text
	Title       string `json:"title"`        // heading text
	Level       int    `json:"level"`        // 1..6
	StartOffset int    `json:"start_offset"` // byte offset in plan_markdown where this section starts (heading line)
	EndOffset   int    `json:"end_offset"`   // byte offset where the next equal-or-higher heading starts (exclusive)
}

// ReviewRevision is one version of a plan within a session.
type ReviewRevision struct {
	ID             string          `json:"id"`
	SessionID      string          `json:"session_id"`
	RevisionNumber int             `json:"revision_number"`
	PlanMarkdown   string          `json:"plan_markdown"`
	Sections       []ReviewSection `json:"sections"`
	CreatedAt      time.Time       `json:"created_at"`
}

// ReviewComment is a comment or threaded reply anchored to a plan revision.
type ReviewComment struct {
	ID                    string    `json:"id"`
	SessionID             string    `json:"session_id"`
	RevisionIDCreated     string    `json:"revision_id_created"`
	ParentID              string    `json:"parent_id,omitempty"`
	AnchorSectionID       string    `json:"anchor_section_id"`
	AnchorTextFingerprint string    `json:"anchor_text_fingerprint"`
	AnchorStartOffset     int       `json:"anchor_start_offset"`
	AnchorEndOffset       int       `json:"anchor_end_offset"`
	Status                string    `json:"status"`
	Author                string    `json:"author"` // "user" or "agent"
	Body                  string    `json:"body"`
	CreatedAt             time.Time `json:"created_at"`
}

// ReviewEvent is an audit-log entry that also signals the agent.
type ReviewEvent struct {
	ID          int64     `json:"id"`
	SessionID   string    `json:"session_id"`
	Kind        string    `json:"kind"`
	PayloadJSON string    `json:"payload_json"`
	CreatedAt   time.Time `json:"created_at"`
}
