package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Limits for query bounds
const (
	MaxLimit     = 1000
	DefaultLimit = 100
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(dataDir string) (*SQLiteStore, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "clauder.db")
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &SQLiteStore{db: db}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return store, nil
}

func (s *SQLiteStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS facts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		content TEXT NOT NULL,
		tags TEXT DEFAULT '[]',
		source_dir TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_facts_source_dir ON facts(source_dir);
	CREATE INDEX IF NOT EXISTS idx_facts_created_at ON facts(created_at);

	CREATE VIRTUAL TABLE IF NOT EXISTS facts_fts USING fts5(content, content=facts, content_rowid=id);

	CREATE TRIGGER IF NOT EXISTS facts_ai AFTER INSERT ON facts BEGIN
		INSERT INTO facts_fts(rowid, content) VALUES (new.id, new.content);
	END;

	CREATE TRIGGER IF NOT EXISTS facts_ad AFTER DELETE ON facts BEGIN
		INSERT INTO facts_fts(facts_fts, rowid, content) VALUES('delete', old.id, old.content);
	END;

	CREATE TRIGGER IF NOT EXISTS facts_au AFTER UPDATE ON facts BEGIN
		INSERT INTO facts_fts(facts_fts, rowid, content) VALUES('delete', old.id, old.content);
		INSERT INTO facts_fts(rowid, content) VALUES (new.id, new.content);
	END;

	CREATE TABLE IF NOT EXISTS instances (
		id TEXT PRIMARY KEY,
		pid INTEGER NOT NULL,
		directory TEXT NOT NULL,
		started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_heartbeat DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		from_instance TEXT NOT NULL,
		to_instance TEXT NOT NULL,
		content TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		read_at DATETIME
	);

	CREATE INDEX IF NOT EXISTS idx_messages_to ON messages(to_instance);
	CREATE INDEX IF NOT EXISTS idx_messages_unread ON messages(to_instance, read_at);
	`

	_, err := s.db.Exec(schema)
	return err
}

// sanitizeFTSQuery escapes special FTS5 operators to prevent query injection
func sanitizeFTSQuery(query string) string {
	// Escape double quotes by doubling them
	query = strings.ReplaceAll(query, `"`, `""`)
	// Wrap the entire query in quotes to treat it as a phrase/literal
	return `"` + query + `"`
}

// Facts

func (s *SQLiteStore) AddFact(content string, tags []string, sourceDir string) (*Fact, error) {
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	result, err := s.db.Exec(
		"INSERT INTO facts (content, tags, source_dir, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		content, string(tagsJSON), sourceDir, now, now,
	)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &Fact{
		ID:        id,
		Content:   content,
		Tags:      tags,
		SourceDir: sourceDir,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (s *SQLiteStore) GetFacts(query string, tags []string, sourceDir string, limit int) ([]Fact, error) {
	var args []interface{}
	var conditions []string

	baseQuery := "SELECT f.id, f.content, f.tags, f.source_dir, f.created_at, f.updated_at FROM facts f"

	if query != "" {
		baseQuery = "SELECT f.id, f.content, f.tags, f.source_dir, f.created_at, f.updated_at FROM facts f JOIN facts_fts fts ON f.id = fts.rowid WHERE fts.content MATCH ?"
		// Sanitize FTS query to prevent operator injection
		args = append(args, sanitizeFTSQuery(query))
	}

	if sourceDir != "" {
		conditions = append(conditions, "f.source_dir = ?")
		args = append(args, sourceDir)
	}

	if len(tags) > 0 {
		for _, tag := range tags {
			// Escape any quotes in tag for LIKE pattern safety
			safeTag := strings.ReplaceAll(tag, `"`, `""`)
			conditions = append(conditions, "f.tags LIKE ?")
			args = append(args, "%\""+safeTag+"\"%")
		}
	}

	if len(conditions) > 0 {
		if query != "" {
			baseQuery += " AND " + strings.Join(conditions, " AND ")
		} else {
			baseQuery += " WHERE " + strings.Join(conditions, " AND ")
		}
	}

	baseQuery += " ORDER BY f.updated_at DESC"

	// Apply limit bounds
	if limit <= 0 {
		limit = DefaultLimit
	} else if limit > MaxLimit {
		limit = MaxLimit
	}
	baseQuery += fmt.Sprintf(" LIMIT %d", limit)

	rows, err := s.db.Query(baseQuery, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var facts []Fact
	for rows.Next() {
		var f Fact
		var tagsJSON string
		if err := rows.Scan(&f.ID, &f.Content, &tagsJSON, &f.SourceDir, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(tagsJSON), &f.Tags); err != nil {
			// If tags are corrupted, initialize to empty slice
			f.Tags = []string{}
		}
		facts = append(facts, f)
	}

	return facts, rows.Err()
}

func (s *SQLiteStore) GetFactByID(id int64) (*Fact, error) {
	var f Fact
	var tagsJSON string
	err := s.db.QueryRow(
		"SELECT id, content, tags, source_dir, created_at, updated_at FROM facts WHERE id = ?",
		id,
	).Scan(&f.ID, &f.Content, &tagsJSON, &f.SourceDir, &f.CreatedAt, &f.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(tagsJSON), &f.Tags); err != nil {
		// If tags are corrupted, initialize to empty slice
		f.Tags = []string{}
	}
	return &f, nil
}

func (s *SQLiteStore) DeleteFact(id int64) error {
	_, err := s.db.Exec("DELETE FROM facts WHERE id = ?", id)
	return err
}

// Instances

func (s *SQLiteStore) RegisterInstance(id string, pid int, directory string) error {
	now := time.Now()
	_, err := s.db.Exec(
		"INSERT OR REPLACE INTO instances (id, pid, directory, started_at, last_heartbeat) VALUES (?, ?, ?, ?, ?)",
		id, pid, directory, now, now,
	)
	return err
}

func (s *SQLiteStore) Heartbeat(id string) error {
	_, err := s.db.Exec("UPDATE instances SET last_heartbeat = ? WHERE id = ?", time.Now(), id)
	return err
}

func (s *SQLiteStore) UnregisterInstance(id string) error {
	_, err := s.db.Exec("DELETE FROM instances WHERE id = ?", id)
	return err
}

func (s *SQLiteStore) GetInstances() ([]Instance, error) {
	rows, err := s.db.Query("SELECT id, pid, directory, started_at, last_heartbeat FROM instances ORDER BY started_at DESC")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var instances []Instance
	for rows.Next() {
		var i Instance
		if err := rows.Scan(&i.ID, &i.PID, &i.Directory, &i.StartedAt, &i.LastHeartbeat); err != nil {
			return nil, err
		}
		instances = append(instances, i)
	}
	return instances, rows.Err()
}

func (s *SQLiteStore) GetInstance(id string) (*Instance, error) {
	var i Instance
	err := s.db.QueryRow(
		"SELECT id, pid, directory, started_at, last_heartbeat FROM instances WHERE id = ?",
		id,
	).Scan(&i.ID, &i.PID, &i.Directory, &i.StartedAt, &i.LastHeartbeat)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &i, nil
}

func (s *SQLiteStore) CleanupStaleInstances(maxAge time.Duration) error {
	cutoff := time.Now().Add(-maxAge)
	_, err := s.db.Exec("DELETE FROM instances WHERE last_heartbeat < ?", cutoff)
	return err
}

// Messages

func (s *SQLiteStore) SendMessage(from, to, content string) (*Message, error) {
	now := time.Now()
	result, err := s.db.Exec(
		"INSERT INTO messages (from_instance, to_instance, content, created_at) VALUES (?, ?, ?, ?)",
		from, to, content, now,
	)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &Message{
		ID:           id,
		FromInstance: from,
		ToInstance:   to,
		Content:      content,
		CreatedAt:    now,
	}, nil
}

func (s *SQLiteStore) GetMessages(toInstance string, unreadOnly bool) ([]Message, error) {
	query := "SELECT id, from_instance, to_instance, content, created_at, read_at FROM messages WHERE to_instance = ?"
	if unreadOnly {
		query += " AND read_at IS NULL"
	}
	query += " ORDER BY created_at ASC"

	rows, err := s.db.Query(query, toInstance)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var messages []Message
	for rows.Next() {
		var m Message
		var readAt sql.NullTime
		if err := rows.Scan(&m.ID, &m.FromInstance, &m.ToInstance, &m.Content, &m.CreatedAt, &readAt); err != nil {
			return nil, err
		}
		if readAt.Valid {
			m.ReadAt = &readAt.Time
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

func (s *SQLiteStore) MarkMessageRead(id int64) error {
	_, err := s.db.Exec("UPDATE messages SET read_at = ? WHERE id = ?", time.Now(), id)
	return err
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
