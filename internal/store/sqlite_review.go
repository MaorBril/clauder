package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (s *SQLiteStore) CreateReviewSession(sess ReviewSession) error {
	now := time.Now()
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = now
	}
	if sess.UpdatedAt.IsZero() {
		sess.UpdatedAt = now
	}
	_, err := s.db.Exec(`
		INSERT INTO review_sessions
			(id, title, status, work_dir, directory_id, instance_id, current_revision_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, sess.ID, sess.Title, sess.Status, sess.WorkDir, sess.DirectoryID, sess.InstanceID,
		sess.CurrentRevisionID, sess.CreatedAt, sess.UpdatedAt)
	return err
}

func scanReviewSession(row interface {
	Scan(dest ...any) error
}) (*ReviewSession, error) {
	var sess ReviewSession
	var approvedAt sql.NullTime
	err := row.Scan(
		&sess.ID, &sess.Title, &sess.Status, &sess.WorkDir, &sess.DirectoryID,
		&sess.InstanceID, &sess.CurrentRevisionID,
		&sess.CreatedAt, &sess.UpdatedAt, &approvedAt,
	)
	if err != nil {
		return nil, err
	}
	if approvedAt.Valid {
		t := approvedAt.Time
		sess.ApprovedAt = &t
	}
	return &sess, nil
}

func (s *SQLiteStore) GetReviewSession(id string) (*ReviewSession, error) {
	row := s.db.QueryRow(`
		SELECT id, title, status, work_dir, directory_id, instance_id, current_revision_id,
		       created_at, updated_at, approved_at
		FROM review_sessions WHERE id = ?
	`, id)
	sess, err := scanReviewSession(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return sess, err
}

func (s *SQLiteStore) UpdateReviewSessionStatus(id, status string) error {
	now := time.Now()
	if status == ReviewStatusApproved {
		_, err := s.db.Exec(`
			UPDATE review_sessions
			SET status = ?, updated_at = ?, approved_at = ?
			WHERE id = ?
		`, status, now, now, id)
		return err
	}
	_, err := s.db.Exec(`
		UPDATE review_sessions SET status = ?, updated_at = ? WHERE id = ?
	`, status, now, id)
	return err
}

func (s *SQLiteStore) UpdateReviewCurrentRevision(id, revisionID string) error {
	_, err := s.db.Exec(`
		UPDATE review_sessions SET current_revision_id = ?, updated_at = ? WHERE id = ?
	`, revisionID, time.Now(), id)
	return err
}

func (s *SQLiteStore) ListReviewSessionsByStatus(statuses []string, limit int) ([]ReviewSession, error) {
	if limit <= 0 || limit > MaxLimit {
		limit = DefaultLimit
	}
	if len(statuses) == 0 {
		rows, err := s.db.Query(`
			SELECT id, title, status, work_dir, directory_id, instance_id, current_revision_id,
			       created_at, updated_at, approved_at
			FROM review_sessions
			ORDER BY updated_at DESC
			LIMIT ?
		`, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return collectSessions(rows)
	}
	placeholders := strings.Repeat("?,", len(statuses))
	placeholders = placeholders[:len(placeholders)-1]
	q := fmt.Sprintf(`
		SELECT id, title, status, work_dir, directory_id, instance_id, current_revision_id,
		       created_at, updated_at, approved_at
		FROM review_sessions
		WHERE status IN (%s)
		ORDER BY updated_at DESC
		LIMIT ?
	`, placeholders)
	args := make([]any, 0, len(statuses)+1)
	for _, st := range statuses {
		args = append(args, st)
	}
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectSessions(rows)
}

func (s *SQLiteStore) ListReviewSessionsByInstance(instanceID string, statuses []string) ([]ReviewSession, error) {
	if len(statuses) == 0 {
		rows, err := s.db.Query(`
			SELECT id, title, status, work_dir, directory_id, instance_id, current_revision_id,
			       created_at, updated_at, approved_at
			FROM review_sessions
			WHERE instance_id = ?
			ORDER BY updated_at DESC
		`, instanceID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return collectSessions(rows)
	}
	placeholders := strings.Repeat("?,", len(statuses))
	placeholders = placeholders[:len(placeholders)-1]
	q := fmt.Sprintf(`
		SELECT id, title, status, work_dir, directory_id, instance_id, current_revision_id,
		       created_at, updated_at, approved_at
		FROM review_sessions
		WHERE instance_id = ? AND status IN (%s)
		ORDER BY updated_at DESC
	`, placeholders)
	args := make([]any, 0, len(statuses)+1)
	args = append(args, instanceID)
	for _, st := range statuses {
		args = append(args, st)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectSessions(rows)
}

func collectSessions(rows *sql.Rows) ([]ReviewSession, error) {
	var out []ReviewSession
	for rows.Next() {
		sess, err := scanReviewSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sess)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) AddReviewRevision(r ReviewRevision) error {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	sectionsJSON, err := json.Marshal(r.Sections)
	if err != nil {
		return fmt.Errorf("marshal sections: %w", err)
	}
	_, err = s.db.Exec(`
		INSERT INTO review_revisions
			(id, session_id, revision_number, plan_markdown, sections_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, r.ID, r.SessionID, r.RevisionNumber, r.PlanMarkdown, string(sectionsJSON), r.CreatedAt)
	return err
}

func (s *SQLiteStore) GetReviewRevision(id string) (*ReviewRevision, error) {
	row := s.db.QueryRow(`
		SELECT id, session_id, revision_number, plan_markdown, sections_json, created_at
		FROM review_revisions WHERE id = ?
	`, id)
	var r ReviewRevision
	var sectionsJSON string
	err := row.Scan(&r.ID, &r.SessionID, &r.RevisionNumber, &r.PlanMarkdown, &sectionsJSON, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if sectionsJSON != "" {
		if err := json.Unmarshal([]byte(sectionsJSON), &r.Sections); err != nil {
			return nil, fmt.Errorf("unmarshal sections: %w", err)
		}
	}
	return &r, nil
}

func (s *SQLiteStore) ListReviewRevisions(sessionID string) ([]ReviewRevision, error) {
	rows, err := s.db.Query(`
		SELECT id, session_id, revision_number, plan_markdown, sections_json, created_at
		FROM review_revisions
		WHERE session_id = ?
		ORDER BY revision_number ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReviewRevision
	for rows.Next() {
		var r ReviewRevision
		var sectionsJSON string
		if err := rows.Scan(&r.ID, &r.SessionID, &r.RevisionNumber, &r.PlanMarkdown, &sectionsJSON, &r.CreatedAt); err != nil {
			return nil, err
		}
		if sectionsJSON != "" {
			if err := json.Unmarshal([]byte(sectionsJSON), &r.Sections); err != nil {
				return nil, fmt.Errorf("unmarshal sections: %w", err)
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) AddReviewComment(c ReviewComment) error {
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
	if c.Status == "" {
		c.Status = CommentStatusOpen
	}
	_, err := s.db.Exec(`
		INSERT INTO review_comments
			(id, session_id, revision_id_created, parent_id, anchor_section_id,
			 anchor_text_fingerprint, anchor_start_offset, anchor_end_offset,
			 status, author, body, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, c.ID, c.SessionID, c.RevisionIDCreated, c.ParentID,
		c.AnchorSectionID, c.AnchorTextFingerprint,
		c.AnchorStartOffset, c.AnchorEndOffset,
		c.Status, c.Author, c.Body, c.CreatedAt)
	return err
}

func (s *SQLiteStore) UpdateReviewCommentAnchor(id, sectionID, fingerprint string, startOffset, endOffset int, status string) error {
	_, err := s.db.Exec(`
		UPDATE review_comments
		SET anchor_section_id = ?, anchor_text_fingerprint = ?,
		    anchor_start_offset = ?, anchor_end_offset = ?, status = ?
		WHERE id = ?
	`, sectionID, fingerprint, startOffset, endOffset, status, id)
	return err
}

func (s *SQLiteStore) ListReviewComments(sessionID string) ([]ReviewComment, error) {
	rows, err := s.db.Query(`
		SELECT id, session_id, revision_id_created, parent_id, anchor_section_id,
		       anchor_text_fingerprint, anchor_start_offset, anchor_end_offset,
		       status, author, body, created_at
		FROM review_comments
		WHERE session_id = ?
		ORDER BY created_at ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReviewComment
	for rows.Next() {
		var c ReviewComment
		if err := rows.Scan(
			&c.ID, &c.SessionID, &c.RevisionIDCreated, &c.ParentID,
			&c.AnchorSectionID, &c.AnchorTextFingerprint,
			&c.AnchorStartOffset, &c.AnchorEndOffset,
			&c.Status, &c.Author, &c.Body, &c.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetReviewComment(id string) (*ReviewComment, error) {
	row := s.db.QueryRow(`
		SELECT id, session_id, revision_id_created, parent_id, anchor_section_id,
		       anchor_text_fingerprint, anchor_start_offset, anchor_end_offset,
		       status, author, body, created_at
		FROM review_comments WHERE id = ?
	`, id)
	var c ReviewComment
	err := row.Scan(
		&c.ID, &c.SessionID, &c.RevisionIDCreated, &c.ParentID,
		&c.AnchorSectionID, &c.AnchorTextFingerprint,
		&c.AnchorStartOffset, &c.AnchorEndOffset,
		&c.Status, &c.Author, &c.Body, &c.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *SQLiteStore) AddReviewEvent(e ReviewEvent) (int64, error) {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	if e.PayloadJSON == "" {
		e.PayloadJSON = "{}"
	}
	res, err := s.db.Exec(`
		INSERT INTO review_events (session_id, kind, payload_json, created_at)
		VALUES (?, ?, ?, ?)
	`, e.SessionID, e.Kind, e.PayloadJSON, e.CreatedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *SQLiteStore) ListReviewEvents(sessionID string, afterID int64, limit int) ([]ReviewEvent, error) {
	if limit <= 0 || limit > MaxLimit {
		limit = DefaultLimit
	}
	rows, err := s.db.Query(`
		SELECT id, session_id, kind, payload_json, created_at
		FROM review_events
		WHERE session_id = ? AND id > ?
		ORDER BY id ASC
		LIMIT ?
	`, sessionID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReviewEvent
	for rows.Next() {
		var e ReviewEvent
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Kind, &e.PayloadJSON, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
