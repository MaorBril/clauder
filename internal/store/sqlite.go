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
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
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

	// Migration: Add directory_id and name columns for multi-instance support
	_, _ = s.db.Exec("ALTER TABLE instances ADD COLUMN directory_id TEXT NOT NULL DEFAULT ''")
	_, _ = s.db.Exec("ALTER TABLE instances ADD COLUMN name TEXT NOT NULL DEFAULT ''")
	// For existing instances, set directory_id to the existing id (which was the directory hash)
	_, _ = s.db.Exec("UPDATE instances SET directory_id = id WHERE directory_id = ''")
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_instances_directory_id ON instances(directory_id)")

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

func (s *SQLiteStore) Close() error {
	if s.index != nil {
		_ = s.index.Close()
	}
	return s.db.Close()
}
