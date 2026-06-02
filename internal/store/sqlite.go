package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/blevesearch/bleve/v2"
	_ "modernc.org/sqlite"
)

// Limits for query bounds
const (
	MaxLimit     = 1000
	DefaultLimit = 100
)

type SQLiteStore struct {
	db         *sql.DB
	index      bleve.Index
	dataDir    string
	instanceID string // Used for per-instance Bleve index
}

// FactDocument represents a fact for Bleve indexing
type FactDocument struct {
	Content   string `json:"content"`
	SourceDir string `json:"source_dir"`
}

func NewSQLiteStore(dataDir string) (*SQLiteStore, error) {
	debugLog("[NewSQLiteStore] Creating data directory: %s", dataDir)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "clauder.db")
	debugLog("[NewSQLiteStore] Opening database: %s", dbPath)
	// NOTE: modernc.org/sqlite uses the _pragma=name(value) DSN syntax, NOT the
	// mattn/go-sqlite3 _journal_mode=/_busy_timeout= form (which it silently
	// ignores, leaving journal_mode=delete and busy_timeout=0 -> immediate
	// "database is locked" errors under concurrent access).
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	// SQLite allows only a single writer. WAL permits concurrent readers, but
	// serializing connections eliminates the write-write contention that
	// busy_timeout alone can deadlock on (two pooled conns upgrading read->write).
	db.SetMaxOpenConns(1)
	debugLog("[NewSQLiteStore] Database opened successfully")

	store := &SQLiteStore{db: db, dataDir: dataDir}
	debugLog("[NewSQLiteStore] Running migrations...")
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}
	debugLog("[NewSQLiteStore] Migrations complete")

	return store, nil
}

// InitIndex initializes a per-instance Bleve index for full-text search.
// Each instance gets its own index to avoid file locking issues between processes.
// Call this for long-running processes (MCP server, UI) that benefit from full-text search.
// Short-lived CLI commands can skip this and use SQLite-only search.
func (s *SQLiteStore) InitIndex(instanceID string) error {
	s.instanceID = instanceID
	debugLog("[InitIndex] Starting for instanceID=%s", instanceID)

	// Clean up old shared index from previous versions (pre-v0.6.0)
	// This prevents the old facts.bleve from being left behind
	oldIndexPath := filepath.Join(s.dataDir, "facts.bleve")
	debugLog("[InitIndex] Removing old shared index at %s", oldIndexPath)
	_ = os.RemoveAll(oldIndexPath)

	// Clean up stale indexes from dead processes first
	debugLog("[InitIndex] Cleaning up stale indexes...")
	s.cleanupStaleIndexes()
	debugLog("[InitIndex] Stale index cleanup complete")

	// Create indexes directory
	indexDir := filepath.Join(s.dataDir, "indexes")
	debugLog("[InitIndex] Creating index directory: %s", indexDir)
	if err := os.MkdirAll(indexDir, 0755); err != nil {
		return fmt.Errorf("failed to create index directory: %w", err)
	}

	// Use instance-specific index path
	indexPath := filepath.Join(indexDir, instanceID+".bleve")
	debugLog("[InitIndex] Index path: %s", indexPath)

	// Always start fresh - delete existing index for this instance
	// This ensures clean state and avoids corruption issues
	debugLog("[InitIndex] Removing existing index...")
	_ = os.RemoveAll(indexPath)

	debugLog("[InitIndex] Creating new Bleve index...")
	index, err := createIndex(indexPath)
	if err != nil {
		return fmt.Errorf("failed to create search index: %w", err)
	}
	debugLog("[InitIndex] Bleve index created successfully")

	s.index = index

	// Index all existing facts
	debugLog("[InitIndex] Reindexing all facts...")
	if err := s.reindexAllFacts(); err != nil {
		_ = index.Close()
		s.index = nil
		return fmt.Errorf("failed to index facts: %w", err)
	}
	debugLog("[InitIndex] Reindexing complete")

	return nil
}

// debugLog writes debug output to stderr
func debugLog(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[clauder] "+format+"\n", args...)
}

// cleanupStaleIndexes removes index directories for processes that are no longer running
func (s *SQLiteStore) cleanupStaleIndexes() {
	indexDir := filepath.Join(s.dataDir, "indexes")
	entries, err := os.ReadDir(indexDir)
	if err != nil {
		debugLog("[cleanupStaleIndexes] Cannot read index dir: %v", err)
		return // Directory might not exist yet
	}

	debugLog("[cleanupStaleIndexes] Found %d entries in index dir", len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".bleve") {
			continue
		}
		pidStr := strings.TrimSuffix(name, ".bleve")

		// Try to parse as PID
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			// Not a PID-based index (maybe old format), remove it
			debugLog("[cleanupStaleIndexes] Removing non-PID index: %s", name)
			indexPath := filepath.Join(indexDir, name)
			_ = os.RemoveAll(indexPath)
			continue
		}

		// Check if process is still running
		debugLog("[cleanupStaleIndexes] Checking if PID %d is running...", pid)
		if isProcessRunning(pid) {
			debugLog("[cleanupStaleIndexes] PID %d is running, keeping index", pid)
			continue
		}

		// Process is dead, remove stale index
		debugLog("[cleanupStaleIndexes] PID %d is dead, removing index", pid)
		indexPath := filepath.Join(indexDir, name)
		_ = os.RemoveAll(indexPath)
	}
}

// isProcessRunning checks if a process with the given PID is still running
func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds. Send signal 0 to check if process exists.
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// CleanupIndex removes this instance's Bleve index. Call on shutdown for CLI commands.
func (s *SQLiteStore) CleanupIndex() {
	if s.index != nil {
		_ = s.index.Close()
		s.index = nil
	}
	if s.instanceID != "" {
		indexPath := filepath.Join(s.dataDir, "indexes", s.instanceID+".bleve")
		_ = os.RemoveAll(indexPath)
	}
}

// createIndex creates a new Bleve index at the given path
func createIndex(indexPath string) (bleve.Index, error) {
	// Create new index with custom mapping
	mapping := bleve.NewIndexMapping()

	// Create document mapping for facts
	factMapping := bleve.NewDocumentMapping()

	// Content field - use English analyzer for better search
	contentFieldMapping := bleve.NewTextFieldMapping()
	contentFieldMapping.Analyzer = "en"
	factMapping.AddFieldMappingsAt("content", contentFieldMapping)

	// SourceDir field - use keyword analyzer (exact match)
	sourceDirFieldMapping := bleve.NewTextFieldMapping()
	sourceDirFieldMapping.Analyzer = "keyword"
	factMapping.AddFieldMappingsAt("source_dir", sourceDirFieldMapping)

	mapping.AddDocumentMapping("fact", factMapping)
	mapping.DefaultMapping = factMapping

	return bleve.New(indexPath, mapping)
}

// reindexAllFacts indexes all existing facts into Bleve
func (s *SQLiteStore) reindexAllFacts() error {
	debugLog("[reindexAllFacts] Querying facts from SQLite...")
	rows, err := s.db.Query("SELECT id, content, source_dir FROM facts WHERE deleted_at IS NULL")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	debugLog("[reindexAllFacts] Creating batch...")
	batch := s.index.NewBatch()
	count := 0

	for rows.Next() {
		var id int64
		var content, sourceDir string
		if err := rows.Scan(&id, &content, &sourceDir); err != nil {
			return err
		}

		doc := FactDocument{
			Content:   content,
			SourceDir: sourceDir,
		}
		if err := batch.Index(strconv.FormatInt(id, 10), doc); err != nil {
			return err
		}

		count++
		// Commit in batches of 100
		if count%100 == 0 {
			debugLog("[reindexAllFacts] Committing batch at count=%d", count)
			if err := s.index.Batch(batch); err != nil {
				return err
			}
			batch = s.index.NewBatch()
		}
	}

	// Commit any remaining documents
	if batch.Size() > 0 {
		debugLog("[reindexAllFacts] Committing final batch, size=%d", batch.Size())
		if err := s.index.Batch(batch); err != nil {
			return err
		}
	}

	debugLog("[reindexAllFacts] Done, indexed %d facts", count)
	return rows.Err()
}

func (s *SQLiteStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS facts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		content TEXT NOT NULL,
		tags TEXT DEFAULT '[]',
		source_dir TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		deleted_at DATETIME
	);

	CREATE INDEX IF NOT EXISTS idx_facts_source_dir ON facts(source_dir);
	CREATE INDEX IF NOT EXISTS idx_facts_created_at ON facts(created_at);

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
	if err != nil {
		return err
	}

	// Migration: Add deleted_at column if it doesn't exist (for existing databases)
	_, _ = s.db.Exec("ALTER TABLE facts ADD COLUMN deleted_at DATETIME")

	// Create index on deleted_at (must be after the column migration for existing databases)
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_facts_deleted_at ON facts(deleted_at)")

	// Migration: Add tty and is_leader columns to instances (for existing databases)
	_, _ = s.db.Exec("ALTER TABLE instances ADD COLUMN tty TEXT DEFAULT ''")
	_, _ = s.db.Exec("ALTER TABLE instances ADD COLUMN is_leader INTEGER DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE instances ADD COLUMN is_idle INTEGER DEFAULT 0")

	// Migration: Add settings table for key-value config
	_, _ = s.db.Exec(`CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)

	// Migration: Add directory_id and name columns for multi-instance support
	_, _ = s.db.Exec("ALTER TABLE instances ADD COLUMN directory_id TEXT NOT NULL DEFAULT ''")
	_, _ = s.db.Exec("ALTER TABLE instances ADD COLUMN name TEXT NOT NULL DEFAULT ''")
	// For existing instances, set directory_id to the existing id (which was the directory hash)
	_, _ = s.db.Exec("UPDATE instances SET directory_id = id WHERE directory_id = ''")
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_instances_directory_id ON instances(directory_id)")

	// Migration: Add pet table for Tamagotchi feature
	_, _ = s.db.Exec(`CREATE TABLE IF NOT EXISTS pets (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL DEFAULT 'Clawde',
		hunger INTEGER NOT NULL DEFAULT 50,
		happiness INTEGER NOT NULL DEFAULT 50,
		energy INTEGER NOT NULL DEFAULT 50,
		total_tokens INTEGER NOT NULL DEFAULT 0,
		is_alive INTEGER NOT NULL DEFAULT 1,
		born_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_fed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_play_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)

	// Migration: Plan review tables
	_, _ = s.db.Exec(`CREATE TABLE IF NOT EXISTS review_sessions (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'awaiting_review',
		work_dir TEXT NOT NULL,
		directory_id TEXT NOT NULL,
		instance_id TEXT NOT NULL,
		current_revision_id TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		approved_at DATETIME
	)`)
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_review_sessions_instance ON review_sessions(instance_id)")
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_review_sessions_directory ON review_sessions(directory_id)")
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_review_sessions_status ON review_sessions(status)")

	_, _ = s.db.Exec(`CREATE TABLE IF NOT EXISTS review_revisions (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		revision_number INTEGER NOT NULL,
		plan_markdown TEXT NOT NULL,
		sections_json TEXT NOT NULL DEFAULT '[]',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_review_revisions_session ON review_revisions(session_id)")

	_, _ = s.db.Exec(`CREATE TABLE IF NOT EXISTS review_comments (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		revision_id_created TEXT NOT NULL,
		parent_id TEXT NOT NULL DEFAULT '',
		anchor_section_id TEXT NOT NULL DEFAULT '',
		anchor_text_fingerprint TEXT NOT NULL DEFAULT '',
		anchor_start_offset INTEGER NOT NULL DEFAULT 0,
		anchor_end_offset INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'open',
		author TEXT NOT NULL,
		body TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_review_comments_session ON review_comments(session_id)")
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_review_comments_parent ON review_comments(parent_id)")

	_, _ = s.db.Exec(`CREATE TABLE IF NOT EXISTS review_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		payload_json TEXT NOT NULL DEFAULT '{}',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_review_events_session ON review_events(session_id, id)")

	return nil
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

	// Index in Bleve if available
	if s.index != nil {
		doc := FactDocument{
			Content:   content,
			SourceDir: sourceDir,
		}
		if err := s.index.Index(strconv.FormatInt(id, 10), doc); err != nil {
			// Log error but don't fail - SQLite is the source of truth
			// The fact is stored, search just won't find it until reindex
			_ = err
		}
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

// bulkInsertChunkSize controls how many rows per multi-row INSERT.
// Each fact uses 5 bind variables; SQLite's default SQLITE_MAX_VARIABLE_NUMBER is 999.
// 100 * 5 = 500, well within the limit.
const bulkInsertChunkSize = 100

func (s *SQLiteStore) BulkAddFacts(facts []BulkFact, sourceDir string) ([]Fact, error) {
	if len(facts) == 0 {
		return []Fact{}, nil
	}

	now := time.Now()

	// Pre-marshal all tags before starting the transaction
	type preparedFact struct {
		content  string
		tags     []string
		tagsJSON string
	}
	prepared := make([]preparedFact, len(facts))
	for i, f := range facts {
		tags := f.Tags
		if tags == nil {
			tags = []string{}
		}
		tagsJSON, err := json.Marshal(tags)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal tags for fact %d: %w", i, err)
		}
		prepared[i] = preparedFact{content: f.Content, tags: tags, tagsJSON: string(tagsJSON)}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stored := make([]Fact, 0, len(facts))

	for chunkStart := 0; chunkStart < len(prepared); chunkStart += bulkInsertChunkSize {
		chunkEnd := chunkStart + bulkInsertChunkSize
		if chunkEnd > len(prepared) {
			chunkEnd = len(prepared)
		}
		chunk := prepared[chunkStart:chunkEnd]

		// Build multi-row INSERT: INSERT INTO facts (...) VALUES (?,?,?,?,?),(?,?,?,?,?),...
		var sb strings.Builder
		sb.WriteString("INSERT INTO facts (content, tags, source_dir, created_at, updated_at) VALUES ")
		args := make([]any, 0, len(chunk)*5)
		for i, pf := range chunk {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString("(?,?,?,?,?)")
			args = append(args, pf.content, pf.tagsJSON, sourceDir, now, now)
		}

		result, err := tx.Exec(sb.String(), args...)
		if err != nil {
			return nil, fmt.Errorf("failed to bulk insert facts: %w", err)
		}

		// LastInsertId returns the ID of the last row in the batch.
		// For multi-row INSERT in SQLite, IDs are sequential, so
		// first ID = lastID - (chunkLen - 1).
		lastID, err := result.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("failed to get last insert ID: %w", err)
		}
		firstID := lastID - int64(len(chunk)) + 1

		for i, pf := range chunk {
			stored = append(stored, Fact{
				ID:        firstID + int64(i),
				Content:   pf.content,
				Tags:      pf.tags,
				SourceDir: sourceDir,
				CreatedAt: now,
				UpdatedAt: now,
			})
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Index in Bleve if available
	if s.index != nil {
		batch := s.index.NewBatch()
		for _, f := range stored {
			doc := FactDocument{
				Content:   f.Content,
				SourceDir: f.SourceDir,
			}
			_ = batch.Index(strconv.FormatInt(f.ID, 10), doc)
		}
		_ = s.index.Batch(batch)
	}

	return stored, nil
}

func (s *SQLiteStore) GetFacts(query string, tags []string, sourceDir string, limit int) ([]Fact, error) {
	// Apply limit bounds
	if limit <= 0 {
		limit = DefaultLimit
	} else if limit > MaxLimit {
		limit = MaxLimit
	}

	// If there's a search query and Bleve is available, use it for relevance-ranked search
	if query != "" && s.index != nil {
		return s.searchFactsWithBleve(query, tags, sourceDir, limit)
	}

	// No query or no Bleve index - use SQLite directly
	// Falls back to LIKE-based search if query is provided
	return s.listFacts(tags, sourceDir, limit, query)
}

// searchFactsWithBleve uses Bleve for relevance-ranked full-text search
func (s *SQLiteStore) searchFactsWithBleve(query string, tags []string, sourceDir string, limit int) ([]Fact, error) {
	// Build Bleve query - use MatchQuery for literal matching (doesn't interpret operators)
	searchQuery := bleve.NewMatchQuery(query)
	searchQuery.SetField("content")

	// Create search request
	searchRequest := bleve.NewSearchRequest(searchQuery)
	searchRequest.Size = limit * 2 // Fetch extra to account for post-filtering

	// If filtering by sourceDir, add it to the query
	if sourceDir != "" {
		sourceDirQuery := bleve.NewMatchQuery(sourceDir)
		sourceDirQuery.SetField("source_dir")
		combinedQuery := bleve.NewConjunctionQuery(searchQuery, sourceDirQuery)
		searchRequest = bleve.NewSearchRequest(combinedQuery)
		searchRequest.Size = limit * 2
	}

	// Execute search
	searchResult, err := s.index.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	if len(searchResult.Hits) == 0 {
		return []Fact{}, nil
	}

	// Collect IDs in ranked order
	ids := make([]int64, 0, len(searchResult.Hits))
	for _, hit := range searchResult.Hits {
		id, err := strconv.ParseInt(hit.ID, 10, 64)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		return []Fact{}, nil
	}

	// Fetch facts from SQLite by IDs, preserving Bleve ranking order
	facts, err := s.getFactsByIDs(ids, tags)
	if err != nil {
		return nil, err
	}

	// Trim to limit
	if len(facts) > limit {
		facts = facts[:limit]
	}

	return facts, nil
}

// getFactsByIDs fetches facts by IDs while preserving order and applying tag filters
func (s *SQLiteStore) getFactsByIDs(ids []int64, tags []string) ([]Fact, error) {
	if len(ids) == 0 {
		return []Fact{}, nil
	}

	// Build query with IN clause
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		"SELECT id, content, tags, source_dir, created_at, updated_at FROM facts WHERE id IN (%s) AND deleted_at IS NULL",
		strings.Join(placeholders, ","),
	)

	// Add tag filters
	for _, tag := range tags {
		safeTag := strings.ReplaceAll(tag, `"`, `""`)
		query += " AND tags LIKE ?"
		args = append(args, "%\""+safeTag+"\"%")
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	// Build a map for reordering
	factMap := make(map[int64]Fact)
	for rows.Next() {
		var f Fact
		var tagsJSON string
		if err := rows.Scan(&f.ID, &f.Content, &tagsJSON, &f.SourceDir, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(tagsJSON), &f.Tags); err != nil {
			f.Tags = []string{}
		}
		factMap[f.ID] = f
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Reorder to match Bleve ranking
	facts := make([]Fact, 0, len(factMap))
	for _, id := range ids {
		if f, ok := factMap[id]; ok {
			facts = append(facts, f)
		}
	}

	return facts, nil
}

// listFacts returns facts with optional filters and search query
// When searchQuery is provided (fallback mode), uses LIKE-based search
func (s *SQLiteStore) listFacts(tags []string, sourceDir string, limit int, searchQuery string) ([]Fact, error) {
	var args []any
	var conditions []string

	conditions = append(conditions, "deleted_at IS NULL")

	if sourceDir != "" {
		conditions = append(conditions, "source_dir = ?")
		args = append(args, sourceDir)
	}

	// Fallback search using LIKE when Bleve is not available
	if searchQuery != "" {
		conditions = append(conditions, "content LIKE ?")
		args = append(args, "%"+searchQuery+"%")
	}

	for _, tag := range tags {
		safeTag := strings.ReplaceAll(tag, `"`, `""`)
		conditions = append(conditions, "tags LIKE ?")
		args = append(args, "%\""+safeTag+"\"%")
	}

	query := "SELECT id, content, tags, source_dir, created_at, updated_at FROM facts"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY updated_at DESC"
	query += fmt.Sprintf(" LIMIT %d", limit)

	rows, err := s.db.Query(query, args...)
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
			f.Tags = []string{}
		}
		facts = append(facts, f)
	}

	return facts, rows.Err()
}

func (s *SQLiteStore) GetAllFactsByDir(sourceDir string) ([]Fact, error) {
	rows, err := s.db.Query(
		"SELECT id, content, tags, source_dir, created_at, updated_at FROM facts WHERE source_dir = ? AND deleted_at IS NULL ORDER BY created_at",
		sourceDir,
	)
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
			f.Tags = []string{}
		}
		facts = append(facts, f)
	}

	return facts, rows.Err()
}

func (s *SQLiteStore) GetAllFacts() ([]Fact, error) {
	rows, err := s.db.Query(
		"SELECT id, content, tags, source_dir, created_at, updated_at FROM facts WHERE deleted_at IS NULL ORDER BY source_dir, created_at",
	)
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
		"SELECT id, content, tags, source_dir, created_at, updated_at FROM facts WHERE id = ? AND deleted_at IS NULL",
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

func (s *SQLiteStore) SoftDeleteFact(id int64) error {
	_, err := s.db.Exec("UPDATE facts SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL", time.Now(), id)
	if err != nil {
		return err
	}

	// Remove from Bleve index if available
	if s.index != nil {
		_ = s.index.Delete(strconv.FormatInt(id, 10))
	}

	return nil
}

func (s *SQLiteStore) BulkSoftDeleteFacts(ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, time.Now())
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}

	query := fmt.Sprintf(
		"UPDATE facts SET deleted_at = ? WHERE id IN (%s) AND deleted_at IS NULL",
		strings.Join(placeholders, ","),
	)

	result, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	// Remove from Bleve index if available
	if s.index != nil {
		batch := s.index.NewBatch()
		for _, id := range ids {
			batch.Delete(strconv.FormatInt(id, 10))
		}
		_ = s.index.Batch(batch)
	}

	return int(affected), nil
}

// UpdateFact updates the content and/or tags of an existing fact
func (s *SQLiteStore) UpdateFact(id int64, content string, tags []string) (*Fact, error) {
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	result, err := s.db.Exec(
		"UPDATE facts SET content = ?, tags = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL",
		content, string(tagsJSON), now, id,
	)
	if err != nil {
		return nil, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, nil // fact not found or already deleted
	}

	// Update Bleve index if available
	if s.index != nil {
		fact, err := s.GetFactByID(id)
		if err == nil && fact != nil {
			doc := FactDocument{
				Content:   fact.Content,
				SourceDir: fact.SourceDir,
			}
			_ = s.index.Index(strconv.FormatInt(id, 10), doc)
		}
	}

	return s.GetFactByID(id)
}

// CompressFacts atomically deletes old facts and adds new consolidated ones in a single transaction
func (s *SQLiteStore) CompressFacts(deleteIDs []int64, newFacts []BulkFact, sourceDir string) (int, []Fact, error) {
	if len(deleteIDs) == 0 && len(newFacts) == 0 {
		return 0, []Fact{}, nil
	}

	now := time.Now()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Soft-delete old facts
	deleted := 0
	if len(deleteIDs) > 0 {
		placeholders := make([]string, len(deleteIDs))
		args := make([]any, 0, len(deleteIDs)+1)
		args = append(args, now)
		for i, id := range deleteIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}

		query := fmt.Sprintf(
			"UPDATE facts SET deleted_at = ? WHERE id IN (%s) AND deleted_at IS NULL",
			strings.Join(placeholders, ","),
		)

		result, err := tx.Exec(query, args...)
		if err != nil {
			return 0, nil, fmt.Errorf("failed to delete facts: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return 0, nil, err
		}
		deleted = int(affected)
	}

	// Add new consolidated facts
	stored := make([]Fact, 0, len(newFacts))
	if len(newFacts) > 0 {
		// Pre-marshal tags
		type preparedFact struct {
			content  string
			tags     []string
			tagsJSON string
		}
		prepared := make([]preparedFact, len(newFacts))
		for i, f := range newFacts {
			tags := f.Tags
			if tags == nil {
				tags = []string{}
			}
			tagsJSON, err := json.Marshal(tags)
			if err != nil {
				return 0, nil, fmt.Errorf("failed to marshal tags for fact %d: %w", i, err)
			}
			prepared[i] = preparedFact{content: f.Content, tags: tags, tagsJSON: string(tagsJSON)}
		}

		for chunkStart := 0; chunkStart < len(prepared); chunkStart += bulkInsertChunkSize {
			chunkEnd := chunkStart + bulkInsertChunkSize
			if chunkEnd > len(prepared) {
				chunkEnd = len(prepared)
			}
			chunk := prepared[chunkStart:chunkEnd]

			var sb strings.Builder
			sb.WriteString("INSERT INTO facts (content, tags, source_dir, created_at, updated_at) VALUES ")
			args := make([]any, 0, len(chunk)*5)
			for i, pf := range chunk {
				if i > 0 {
					sb.WriteByte(',')
				}
				sb.WriteString("(?,?,?,?,?)")
				args = append(args, pf.content, pf.tagsJSON, sourceDir, now, now)
			}

			result, err := tx.Exec(sb.String(), args...)
			if err != nil {
				return 0, nil, fmt.Errorf("failed to insert facts: %w", err)
			}

			lastID, err := result.LastInsertId()
			if err != nil {
				return 0, nil, fmt.Errorf("failed to get last insert ID: %w", err)
			}
			firstID := lastID - int64(len(chunk)) + 1

			for i, pf := range chunk {
				stored = append(stored, Fact{
					ID:        firstID + int64(i),
					Content:   pf.content,
					Tags:      pf.tags,
					SourceDir: sourceDir,
					CreatedAt: now,
					UpdatedAt: now,
				})
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Update Bleve index
	if s.index != nil {
		batch := s.index.NewBatch()
		for _, id := range deleteIDs {
			batch.Delete(strconv.FormatInt(id, 10))
		}
		for _, f := range stored {
			doc := FactDocument{
				Content:   f.Content,
				SourceDir: f.SourceDir,
			}
			_ = batch.Index(strconv.FormatInt(f.ID, 10), doc)
		}
		_ = s.index.Batch(batch)
	}

	return deleted, stored, nil
}

// PurgeDeletedFacts permanently removes all soft-deleted facts from the database
func (s *SQLiteStore) PurgeDeletedFacts() (int, error) {
	result, err := s.db.Exec("DELETE FROM facts WHERE deleted_at IS NOT NULL")
	if err != nil {
		return 0, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return int(affected), nil
}

// GetFactStats returns statistics about all stored facts
func (s *SQLiteStore) GetFactStats() (*FactStats, error) {
	stats := &FactStats{
		ByDirectory: make(map[string]DirStats),
	}

	// Active facts by directory
	rows, err := s.db.Query(`
		SELECT source_dir, COUNT(*), SUM(LENGTH(content)), MIN(created_at), MAX(created_at)
		FROM facts WHERE deleted_at IS NULL
		GROUP BY source_dir
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var dir string
		var count, size int
		var oldestStr, newestStr string
		if err := rows.Scan(&dir, &count, &size, &oldestStr, &newestStr); err != nil {
			return nil, err
		}
		oldest, _ := time.Parse("2006-01-02 15:04:05-07:00", oldestStr)
		if oldest.IsZero() {
			oldest, _ = time.Parse("2006-01-02T15:04:05Z", oldestStr)
		}
		newest, _ := time.Parse("2006-01-02 15:04:05-07:00", newestStr)
		if newest.IsZero() {
			newest, _ = time.Parse("2006-01-02T15:04:05Z", newestStr)
		}
		stats.TotalFacts += count
		stats.TotalSize += size
		stats.ByDirectory[dir] = DirStats{
			Count:  count,
			Size:   size,
			Oldest: oldest,
			Newest: newest,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Deleted facts stats
	err = s.db.QueryRow(`
		SELECT COALESCE(COUNT(*), 0), COALESCE(SUM(LENGTH(content)), 0)
		FROM facts WHERE deleted_at IS NOT NULL
	`).Scan(&stats.DeletedFacts, &stats.DeletedSize)
	if err != nil {
		return nil, err
	}

	return stats, nil
}

// Instances

func (s *SQLiteStore) RegisterInstance(id, directoryID, name, directory, tty string, pid int) error {
	now := time.Now()
	_, err := s.db.Exec(
		"INSERT OR REPLACE INTO instances (id, directory_id, name, pid, directory, tty, started_at, last_heartbeat) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, directoryID, name, pid, directory, tty, now, now,
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
	rows, err := s.db.Query("SELECT id, directory_id, name, pid, directory, tty, is_leader, is_idle, started_at, last_heartbeat FROM instances ORDER BY started_at DESC")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var instances []Instance
	for rows.Next() {
		var i Instance
		var tty sql.NullString
		var isLeader, isIdle int
		if err := rows.Scan(&i.ID, &i.DirectoryID, &i.Name, &i.PID, &i.Directory, &tty, &isLeader, &isIdle, &i.StartedAt, &i.LastHeartbeat); err != nil {
			return nil, err
		}
		i.TTY = tty.String
		i.IsLeader = isLeader == 1
		i.IsIdle = isIdle == 1
		instances = append(instances, i)
	}
	return instances, rows.Err()
}

func (s *SQLiteStore) GetInstance(id string) (*Instance, error) {
	var i Instance
	var tty sql.NullString
	var isLeader, isIdle int
	err := s.db.QueryRow(
		"SELECT id, directory_id, name, pid, directory, tty, is_leader, is_idle, started_at, last_heartbeat FROM instances WHERE id = ?",
		id,
	).Scan(&i.ID, &i.DirectoryID, &i.Name, &i.PID, &i.Directory, &tty, &isLeader, &isIdle, &i.StartedAt, &i.LastHeartbeat)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	i.TTY = tty.String
	i.IsLeader = isLeader == 1
	i.IsIdle = isIdle == 1
	return &i, nil
}

func (s *SQLiteStore) CleanupStaleInstances(maxAge time.Duration) error {
	cutoff := time.Now().Add(-maxAge)
	_, err := s.db.Exec("DELETE FROM instances WHERE last_heartbeat < ?", cutoff)
	return err
}

func (s *SQLiteStore) GetInstancesByDirectory(directoryID string) ([]Instance, error) {
	rows, err := s.db.Query(
		"SELECT id, directory_id, name, pid, directory, tty, is_leader, is_idle, started_at, last_heartbeat FROM instances WHERE directory_id = ? ORDER BY started_at DESC",
		directoryID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var instances []Instance
	for rows.Next() {
		var i Instance
		var tty sql.NullString
		var isLeader, isIdle int
		if err := rows.Scan(&i.ID, &i.DirectoryID, &i.Name, &i.PID, &i.Directory, &tty, &isLeader, &isIdle, &i.StartedAt, &i.LastHeartbeat); err != nil {
			return nil, err
		}
		i.TTY = tty.String
		i.IsLeader = isLeader == 1
		i.IsIdle = isIdle == 1
		instances = append(instances, i)
	}
	return instances, rows.Err()
}

func (s *SQLiteStore) CheckDirectoryHasActiveInstance(directoryID string) (bool, error) {
	// Check if there's an active instance (heartbeat within last 5 minutes) in this directory
	cutoff := time.Now().Add(-5 * time.Minute)
	var count int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM instances WHERE directory_id = ? AND last_heartbeat > ?",
		directoryID, cutoff,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// TryBecomeLeader attempts to become leader if there is no current leader
// Returns true if this instance became leader
func (s *SQLiteStore) TryBecomeLeader(id string) (bool, error) {
	// Use a transaction to ensure atomicity
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	// Check if there's already a leader with a recent heartbeat (within 30 seconds)
	cutoff := time.Now().Add(-30 * time.Second)
	var currentLeader string
	err = tx.QueryRow(
		"SELECT id FROM instances WHERE is_leader = 1 AND last_heartbeat > ?",
		cutoff,
	).Scan(&currentLeader)

	if err == nil {
		// There's already a leader
		if currentLeader == id {
			// We're already the leader
			return true, tx.Commit()
		}
		return false, tx.Commit()
	}
	if err != sql.ErrNoRows {
		return false, err
	}

	// No active leader, try to become leader
	// First, clear any stale leader flags
	_, err = tx.Exec("UPDATE instances SET is_leader = 0")
	if err != nil {
		return false, err
	}

	// Set ourselves as leader
	result, err := tx.Exec("UPDATE instances SET is_leader = 1 WHERE id = ?", id)
	if err != nil {
		return false, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}

	return affected > 0, nil
}

// ReleaseLeadership releases leadership for this instance
func (s *SQLiteStore) ReleaseLeadership(id string) error {
	_, err := s.db.Exec("UPDATE instances SET is_leader = 0 WHERE id = ?", id)
	return err
}

// GetLeader returns the current leader instance, if any
func (s *SQLiteStore) GetLeader() (*Instance, error) {
	cutoff := time.Now().Add(-30 * time.Second)
	var i Instance
	var tty sql.NullString
	var isLeader, isIdle int
	err := s.db.QueryRow(
		"SELECT id, directory_id, name, pid, directory, tty, is_leader, is_idle, started_at, last_heartbeat FROM instances WHERE is_leader = 1 AND last_heartbeat > ?",
		cutoff,
	).Scan(&i.ID, &i.DirectoryID, &i.Name, &i.PID, &i.Directory, &tty, &isLeader, &isIdle, &i.StartedAt, &i.LastHeartbeat)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	i.TTY = tty.String
	i.IsLeader = isLeader == 1
	i.IsIdle = isIdle == 1
	return &i, nil
}

// SetIdle sets the idle status of an instance
func (s *SQLiteStore) SetIdle(id string, idle bool) error {
	val := 0
	if idle {
		val = 1
	}
	_, err := s.db.Exec("UPDATE instances SET is_idle = ? WHERE id = ?", val, id)
	return err
}

// GetIdleInstancesWithUnreadMessages returns instances that are marked idle
// and have unread messages
func (s *SQLiteStore) GetIdleInstancesWithUnreadMessages() ([]Instance, error) {
	// Find instances that:
	// 1. Are marked as idle (is_idle = 1)
	// 2. Have unread messages
	// 3. Have a valid TTY
	query := `
		SELECT DISTINCT i.id, i.directory_id, i.name, i.pid, i.directory, i.tty, i.is_leader, i.is_idle, i.started_at, i.last_heartbeat
		FROM instances i
		JOIN messages m ON m.to_instance = i.id
		WHERE i.is_idle = 1
		AND m.read_at IS NULL
		AND i.tty != ''
	`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var instances []Instance
	for rows.Next() {
		var i Instance
		var tty sql.NullString
		var isLeader, isIdle int
		if err := rows.Scan(&i.ID, &i.DirectoryID, &i.Name, &i.PID, &i.Directory, &tty, &isLeader, &isIdle, &i.StartedAt, &i.LastHeartbeat); err != nil {
			return nil, err
		}
		i.TTY = tty.String
		i.IsLeader = isLeader == 1
		i.IsIdle = isIdle == 1
		instances = append(instances, i)
	}
	return instances, rows.Err()
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

func (s *SQLiteStore) GetAllMessages(limit int) ([]Message, error) {
	if limit <= 0 {
		limit = DefaultLimit
	} else if limit > MaxLimit {
		limit = MaxLimit
	}

	query := fmt.Sprintf("SELECT id, from_instance, to_instance, content, created_at, read_at FROM messages ORDER BY created_at DESC LIMIT %d", limit)

	rows, err := s.db.Query(query)
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

// GetAnalytics returns time-series analytics data for facts, messages, and sessions
func (s *SQLiteStore) GetAnalytics(timeRange string) (*AnalyticsData, error) {
	data := &AnalyticsData{
		FactsByDirectory: make(map[string]int),
	}

	// Determine date filter
	var dateFilter string
	switch timeRange {
	case "7d":
		dateFilter = "AND created_at >= datetime('now', '-7 days')"
	case "30d":
		dateFilter = "AND created_at >= datetime('now', '-30 days')"
	case "90d":
		dateFilter = "AND created_at >= datetime('now', '-90 days')"
	default:
		dateFilter = "" // all time
	}

	// Facts by date
	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT DATE(created_at) as d, COUNT(*) as c
		FROM facts WHERE deleted_at IS NULL %s
		GROUP BY d ORDER BY d
	`, dateFilter))
	if err != nil {
		return nil, fmt.Errorf("facts by date: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var dc DateCount
		if err := rows.Scan(&dc.Date, &dc.Count); err != nil {
			return nil, err
		}
		data.FactsByDate = append(data.FactsByDate, dc)
		data.TotalFacts += dc.Count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Messages by date
	rows2, err := s.db.Query(fmt.Sprintf(`
		SELECT DATE(created_at) as d, COUNT(*) as c
		FROM messages WHERE 1=1 %s
		GROUP BY d ORDER BY d
	`, dateFilter))
	if err != nil {
		return nil, fmt.Errorf("messages by date: %w", err)
	}
	defer func() { _ = rows2.Close() }()
	for rows2.Next() {
		var dc DateCount
		if err := rows2.Scan(&dc.Date, &dc.Count); err != nil {
			return nil, err
		}
		data.MessagesByDate = append(data.MessagesByDate, dc)
		data.TotalMessages += dc.Count
	}
	if err := rows2.Err(); err != nil {
		return nil, err
	}

	// Sessions by date (instances started_at)
	rows3, err := s.db.Query(fmt.Sprintf(`
		SELECT DATE(started_at) as d, COUNT(*) as c
		FROM instances WHERE 1=1 %s
		GROUP BY d ORDER BY d
	`, strings.ReplaceAll(dateFilter, "created_at", "started_at")))
	if err != nil {
		return nil, fmt.Errorf("sessions by date: %w", err)
	}
	defer func() { _ = rows3.Close() }()
	for rows3.Next() {
		var dc DateCount
		if err := rows3.Scan(&dc.Date, &dc.Count); err != nil {
			return nil, err
		}
		data.SessionsByDate = append(data.SessionsByDate, dc)
		data.TotalSessions += dc.Count
	}
	if err := rows3.Err(); err != nil {
		return nil, err
	}

	// Facts by directory
	rows4, err := s.db.Query(`
		SELECT source_dir, COUNT(*) FROM facts
		WHERE deleted_at IS NULL
		GROUP BY source_dir ORDER BY COUNT(*) DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("facts by directory: %w", err)
	}
	defer func() { _ = rows4.Close() }()
	for rows4.Next() {
		var dir string
		var count int
		if err := rows4.Scan(&dir, &count); err != nil {
			return nil, err
		}
		data.FactsByDirectory[dir] = count
	}
	if err := rows4.Err(); err != nil {
		return nil, err
	}

	// This week vs last week trends
	_ = s.db.QueryRow(`
		SELECT COUNT(*) FROM facts
		WHERE deleted_at IS NULL AND created_at >= datetime('now', '-7 days')
	`).Scan(&data.FactsThisWeek)

	_ = s.db.QueryRow(`
		SELECT COUNT(*) FROM facts
		WHERE deleted_at IS NULL AND created_at >= datetime('now', '-14 days') AND created_at < datetime('now', '-7 days')
	`).Scan(&data.FactsLastWeek)

	_ = s.db.QueryRow(`
		SELECT COUNT(*) FROM messages
		WHERE created_at >= datetime('now', '-7 days')
	`).Scan(&data.MessagesThisWeek)

	_ = s.db.QueryRow(`
		SELECT COUNT(*) FROM messages
		WHERE created_at >= datetime('now', '-14 days') AND created_at < datetime('now', '-7 days')
	`).Scan(&data.MessagesLastWeek)

	return data, nil
}

// Settings

func (s *SQLiteStore) GetSetting(key string) (string, error) {
	var value string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (s *SQLiteStore) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		"INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = ?",
		key, value, time.Now(), value, time.Now(),
	)
	return err
}

func (s *SQLiteStore) DeleteSetting(key string) error {
	_, err := s.db.Exec("DELETE FROM settings WHERE key = ?", key)
	return err
}

// Pet (Tamagotchi)

func petSpecies(totalTokens int64) string {
	switch {
	case totalTokens < 1000:
		return "egg"
	case totalTokens < 10000:
		return "baby"
	case totalTokens < 100000:
		return "child"
	case totalTokens < 500000:
		return "teen"
	case totalTokens < 2000000:
		return "adult"
	default:
		return "elder"
	}
}

func (s *SQLiteStore) applyPetDecay(pet *PetState) {
	now := time.Now()

	// Hunger decays: lose 1 point per 5 minutes since last fed
	minutesSinceFed := now.Sub(pet.LastFedAt).Minutes()
	pet.Hunger = max(pet.Hunger-int(minutesSinceFed/5), 0)

	// Happiness decays: lose 1 point per 5 minutes since last play
	minutesSincePlay := now.Sub(pet.LastPlayAt).Minutes()
	pet.Happiness = max(pet.Happiness-int(minutesSincePlay/5), 0)

	// Energy is derived: average of hunger and happiness
	pet.Energy = (pet.Hunger + pet.Happiness) / 2

	// Pet dies if hunger and happiness are both 0 for too long
	if pet.Hunger == 0 && pet.Happiness == 0 && minutesSinceFed > 1440 {
		pet.IsAlive = false
	}

	pet.Species = petSpecies(pet.TotalTokens)
}

func (s *SQLiteStore) scanPet(row interface{ Scan(...interface{}) error }) (*PetState, error) {
	var pet PetState
	var isAlive int
	err := row.Scan(
		&pet.ID, &pet.Name, &pet.Hunger, &pet.Happiness,
		&pet.Energy, &pet.TotalTokens, &isAlive,
		&pet.BornAt, &pet.LastFedAt, &pet.LastPlayAt, &pet.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	pet.IsAlive = isAlive == 1
	s.applyPetDecay(&pet)
	return &pet, nil
}

func (s *SQLiteStore) savePet(pet *PetState) error {
	isAlive := 0
	if pet.IsAlive {
		isAlive = 1
	}
	_, err := s.db.Exec(`
		UPDATE pets SET name=?, hunger=?, happiness=?, energy=?, total_tokens=?,
		is_alive=?, last_fed_at=?, last_play_at=?, updated_at=?
		WHERE id=?`,
		pet.Name, pet.Hunger, pet.Happiness, pet.Energy, pet.TotalTokens,
		isAlive, pet.LastFedAt, pet.LastPlayAt, time.Now(), pet.ID,
	)
	return err
}

func (s *SQLiteStore) GetPet(workDir string) (*PetState, error) {
	row := s.db.QueryRow(`
		SELECT id, name, hunger, happiness, energy, total_tokens, is_alive,
		born_at, last_fed_at, last_play_at, updated_at
		FROM pets WHERE id = ?`, workDir)

	pet, err := s.scanPet(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Persist death if decay triggered it so the DB stays consistent.
	if !pet.IsAlive {
		_ = s.savePet(pet)
	}
	return pet, nil
}

func (s *SQLiteStore) CreatePet(workDir string, name string) (*PetState, error) {
	now := time.Now()
	_, err := s.db.Exec(`
		INSERT INTO pets (id, name, hunger, happiness, energy, total_tokens, is_alive,
		born_at, last_fed_at, last_play_at, updated_at)
		VALUES (?, ?, 50, 50, 50, 0, 1, ?, ?, ?, ?)`,
		workDir, name, now, now, now, now,
	)
	if err != nil {
		return nil, err
	}
	return s.GetPet(workDir)
}

func (s *SQLiteStore) FeedPet(workDir string, tokens int64) (*PetState, error) {
	pet, err := s.GetPet(workDir)
	if err != nil {
		return nil, err
	}
	if pet == nil {
		// Auto-create pet on first token usage
		pet, err = s.CreatePet(workDir, "Clawde")
		if err != nil {
			return nil, err
		}
	}

	if !pet.IsAlive {
		return pet, nil
	}

	// Feed the pet: tokens restore hunger
	pet.TotalTokens += tokens
	hungerGain := int(tokens / 50) // 50 tokens = 1 hunger point
	if hungerGain < 1 {
		hungerGain = 1
	}
	pet.Hunger += hungerGain
	if pet.Hunger > 100 {
		pet.Hunger = 100
	}

	// Feeding also gives a small happiness boost
	pet.Happiness += hungerGain / 3
	if pet.Happiness > 100 {
		pet.Happiness = 100
	}

	pet.LastFedAt = time.Now()
	pet.Energy = (pet.Hunger + pet.Happiness) / 2
	pet.Species = petSpecies(pet.TotalTokens)

	if err := s.savePet(pet); err != nil {
		return nil, err
	}
	return pet, nil
}

func (s *SQLiteStore) PlayWithPet(workDir string) (*PetState, error) {
	pet, err := s.GetPet(workDir)
	if err != nil {
		return nil, err
	}
	if pet == nil {
		return nil, fmt.Errorf("no pet found - use pet_status to hatch one first")
	}
	if !pet.IsAlive {
		return pet, nil
	}

	// Playing boosts happiness significantly
	pet.Happiness += 20
	if pet.Happiness > 100 {
		pet.Happiness = 100
	}

	// But costs a bit of hunger (playing is tiring)
	pet.Hunger -= 5
	if pet.Hunger < 0 {
		pet.Hunger = 0
	}

	pet.LastPlayAt = time.Now()
	pet.Energy = (pet.Hunger + pet.Happiness) / 2

	if err := s.savePet(pet); err != nil {
		return nil, err
	}
	return pet, nil
}

// ActivityBoost gives a small happiness boost for coding activities (Edit, Write, Bash, etc.)
// without resetting LastPlayAt, so natural decay still applies.
func (s *SQLiteStore) ActivityBoost(workDir string, amount int) error {
	pet, err := s.GetPet(workDir)
	if err != nil || pet == nil || !pet.IsAlive {
		return nil
	}
	pet.Happiness = min(pet.Happiness+amount, 100)
	pet.Energy = (pet.Hunger + pet.Happiness) / 2
	return s.savePet(pet)
}

func (s *SQLiteStore) RenamePet(workDir string, name string) (*PetState, error) {
	pet, err := s.GetPet(workDir)
	if err != nil {
		return nil, err
	}
	if pet == nil {
		return nil, fmt.Errorf("no pet found - use pet_status to hatch one first")
	}

	pet.Name = name
	if err := s.savePet(pet); err != nil {
		return nil, err
	}
	return pet, nil
}

func (s *SQLiteStore) RevivePet(workDir string) (*PetState, error) {
	now := time.Now()
	_, err := s.db.Exec(`
		UPDATE pets SET is_alive=1, hunger=30, happiness=30, energy=30,
		last_fed_at=?, last_play_at=?, updated_at=?
		WHERE id=?`, now, now, now, workDir)
	if err != nil {
		return nil, err
	}
	return s.GetPet(workDir)
}

func (s *SQLiteStore) Close() error {
	if s.index != nil {
		_ = s.index.Close()
	}
	return s.db.Close()
}
