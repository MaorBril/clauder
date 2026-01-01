package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestStore(t *testing.T) (*SQLiteStore, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "clauder-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	store, err := NewSQLiteStore(tmpDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("failed to create store: %v", err)
	}
	cleanup := func() {
		_ = store.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return store, cleanup
}

// Fact tests

func TestAddFact(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	fact, err := store.AddFact("test fact content", nil, "/test/dir")
	if err != nil {
		t.Fatalf("AddFact failed: %v", err)
	}
	if fact.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if fact.Content != "test fact content" {
		t.Errorf("expected content 'test fact content', got '%s'", fact.Content)
	}
	if fact.SourceDir != "/test/dir" {
		t.Errorf("expected source_dir '/test/dir', got '%s'", fact.SourceDir)
	}
	if len(fact.Tags) != 0 {
		t.Errorf("expected empty tags, got %v", fact.Tags)
	}
}

func TestAddFact_WithTags(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	tags := []string{"architecture", "decision"}
	fact, err := store.AddFact("use sqlite for persistence", tags, "/project")
	if err != nil {
		t.Fatalf("AddFact failed: %v", err)
	}
	if len(fact.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(fact.Tags))
	}
	if fact.Tags[0] != "architecture" || fact.Tags[1] != "decision" {
		t.Errorf("unexpected tags: %v", fact.Tags)
	}
}

func TestGetFacts_ByQuery(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	_, _ = store.AddFact("golang is great for CLI tools", nil, "/project1")
	_, _ = store.AddFact("python is great for scripts", nil, "/project2")
	_, _ = store.AddFact("rust has great memory safety", nil, "/project3")

	facts, err := store.GetFacts("golang", nil, "", 10)
	if err != nil {
		t.Fatalf("GetFacts failed: %v", err)
	}
	if len(facts) != 1 {
		t.Errorf("expected 1 fact, got %d", len(facts))
	}
	if len(facts) > 0 && facts[0].Content != "golang is great for CLI tools" {
		t.Errorf("unexpected fact content: %s", facts[0].Content)
	}
}

func TestGetFacts_ByDirectory(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	_, _ = store.AddFact("fact in project1", nil, "/project1")
	_, _ = store.AddFact("fact in project2", nil, "/project2")
	_, _ = store.AddFact("another fact in project1", nil, "/project1")

	facts, err := store.GetFacts("", nil, "/project1", 10)
	if err != nil {
		t.Fatalf("GetFacts failed: %v", err)
	}
	if len(facts) != 2 {
		t.Errorf("expected 2 facts, got %d", len(facts))
	}
}

func TestGetFacts_ByTags(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	_, _ = store.AddFact("architectural decision", []string{"architecture"}, "/project")
	_, _ = store.AddFact("bug fix note", []string{"bugfix"}, "/project")
	_, _ = store.AddFact("another arch note", []string{"architecture", "important"}, "/project")

	facts, err := store.GetFacts("", []string{"architecture"}, "", 10)
	if err != nil {
		t.Fatalf("GetFacts failed: %v", err)
	}
	if len(facts) != 2 {
		t.Errorf("expected 2 facts with architecture tag, got %d", len(facts))
	}
}

func TestGetFacts_FTSSpecialChars(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	_, _ = store.AddFact("normal fact", nil, "/project")
	_, _ = store.AddFact("fact with OR keyword", nil, "/project")

	// Query with FTS operator should be safely escaped
	facts, err := store.GetFacts("OR", nil, "", 10)
	if err != nil {
		t.Fatalf("GetFacts with special chars failed: %v", err)
	}
	// Should find the fact containing "OR" literally, not trigger FTS OR operator
	if len(facts) != 1 {
		t.Errorf("expected 1 fact, got %d", len(facts))
	}

	// Test with quotes
	_, _ = store.AddFact(`fact with "quotes"`, nil, "/project")
	_, err = store.GetFacts(`"quotes"`, nil, "", 10)
	if err != nil {
		t.Fatalf("GetFacts with quotes failed: %v", err)
	}
}

func TestGetFacts_LimitBounds(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Add many facts
	for i := 0; i < 150; i++ {
		_, _ = store.AddFact("test fact", nil, "/project")
	}

	// Default limit when 0
	facts, err := store.GetFacts("", nil, "", 0)
	if err != nil {
		t.Fatalf("GetFacts failed: %v", err)
	}
	if len(facts) != DefaultLimit {
		t.Errorf("expected default limit %d, got %d", DefaultLimit, len(facts))
	}

	// Explicit limit
	facts, err = store.GetFacts("", nil, "", 50)
	if err != nil {
		t.Fatalf("GetFacts failed: %v", err)
	}
	if len(facts) != 50 {
		t.Errorf("expected 50 facts, got %d", len(facts))
	}

	// Max limit cap
	facts, err = store.GetFacts("", nil, "", 5000)
	if err != nil {
		t.Fatalf("GetFacts failed: %v", err)
	}
	if len(facts) > MaxLimit {
		t.Errorf("expected max %d facts, got %d", MaxLimit, len(facts))
	}
}

func TestGetFactByID(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	created, _ := store.AddFact("findable fact", []string{"test"}, "/dir")
	found, err := store.GetFactByID(created.ID)
	if err != nil {
		t.Fatalf("GetFactByID failed: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find fact")
	}
	if found.Content != "findable fact" {
		t.Errorf("unexpected content: %s", found.Content)
	}

	// Non-existent ID
	notFound, err := store.GetFactByID(99999)
	if err != nil {
		t.Fatalf("GetFactByID failed: %v", err)
	}
	if notFound != nil {
		t.Error("expected nil for non-existent ID")
	}
}

func TestDeleteFact(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	fact, _ := store.AddFact("to be deleted", nil, "/dir")
	err := store.DeleteFact(fact.ID)
	if err != nil {
		t.Fatalf("DeleteFact failed: %v", err)
	}

	found, _ := store.GetFactByID(fact.ID)
	if found != nil {
		t.Error("expected fact to be deleted")
	}
}

// Instance tests

func TestInstance_Lifecycle(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Register
	err := store.RegisterInstance("test-instance-id", 12345, "/test/dir")
	if err != nil {
		t.Fatalf("RegisterInstance failed: %v", err)
	}

	// Get
	inst, err := store.GetInstance("test-instance-id")
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}
	if inst == nil {
		t.Fatal("expected to find instance")
	}
	if inst.PID != 12345 {
		t.Errorf("expected PID 12345, got %d", inst.PID)
	}
	if inst.Directory != "/test/dir" {
		t.Errorf("expected directory '/test/dir', got '%s'", inst.Directory)
	}

	// Heartbeat
	time.Sleep(10 * time.Millisecond)
	err = store.Heartbeat("test-instance-id")
	if err != nil {
		t.Fatalf("Heartbeat failed: %v", err)
	}
	updated, _ := store.GetInstance("test-instance-id")
	if !updated.LastHeartbeat.After(inst.LastHeartbeat) {
		t.Error("expected heartbeat to update timestamp")
	}

	// List
	instances, err := store.GetInstances()
	if err != nil {
		t.Fatalf("GetInstances failed: %v", err)
	}
	if len(instances) != 1 {
		t.Errorf("expected 1 instance, got %d", len(instances))
	}

	// Unregister
	err = store.UnregisterInstance("test-instance-id")
	if err != nil {
		t.Fatalf("UnregisterInstance failed: %v", err)
	}
	gone, _ := store.GetInstance("test-instance-id")
	if gone != nil {
		t.Error("expected instance to be unregistered")
	}
}

func TestInstance_Cleanup(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	_ = store.RegisterInstance("old-instance", 111, "/old")
	_ = store.RegisterInstance("new-instance", 222, "/new")

	// Manually make one instance stale by setting last_heartbeat to 10 minutes ago
	staleTime := time.Now().Add(-10 * time.Minute)
	_, _ = store.db.Exec("UPDATE instances SET last_heartbeat = ? WHERE id = 'old-instance'", staleTime)

	err := store.CleanupStaleInstances(5 * time.Minute)
	if err != nil {
		t.Fatalf("CleanupStaleInstances failed: %v", err)
	}

	old, _ := store.GetInstance("old-instance")
	if old != nil {
		t.Error("expected old instance to be cleaned up")
	}

	newInst, _ := store.GetInstance("new-instance")
	if newInst == nil {
		t.Error("expected new instance to remain")
	}
}

// Message tests

func TestMessage_SendAndReceive(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Setup instances
	_ = store.RegisterInstance("sender", 1, "/sender")
	_ = store.RegisterInstance("receiver", 2, "/receiver")

	// Send message
	msg, err := store.SendMessage("sender", "receiver", "hello!")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if msg.ID == 0 {
		t.Error("expected non-zero message ID")
	}
	if msg.Content != "hello!" {
		t.Errorf("unexpected content: %s", msg.Content)
	}

	// Receive unread only
	messages, err := store.GetMessages("receiver", true)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(messages))
	}
	if messages[0].ReadAt != nil {
		t.Error("expected message to be unread")
	}
}

func TestMessage_MarkRead(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	_ = store.RegisterInstance("sender", 1, "/sender")
	_ = store.RegisterInstance("receiver", 2, "/receiver")

	msg, _ := store.SendMessage("sender", "receiver", "test message")

	err := store.MarkMessageRead(msg.ID)
	if err != nil {
		t.Fatalf("MarkMessageRead failed: %v", err)
	}

	// Should not appear in unread
	unread, _ := store.GetMessages("receiver", true)
	if len(unread) != 0 {
		t.Errorf("expected 0 unread messages, got %d", len(unread))
	}

	// Should appear in all messages
	all, _ := store.GetMessages("receiver", false)
	if len(all) != 1 {
		t.Errorf("expected 1 total message, got %d", len(all))
	}
	if all[0].ReadAt == nil {
		t.Error("expected message to be marked as read")
	}
}

// Database tests

func TestNewSQLiteStore_CreatesDirectory(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "clauder-test-nonexistent")
	_ = os.RemoveAll(tmpDir) // Ensure it doesn't exist

	store, err := NewSQLiteStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() {
		_ = store.Close()
		_ = os.RemoveAll(tmpDir)
	}()

	if _, err := os.Stat(tmpDir); os.IsNotExist(err) {
		t.Error("expected directory to be created")
	}
}

func TestSanitizeFTSQuery(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", `"simple"`},
		{"with spaces", `"with spaces"`},
		{`with "quotes"`, `"with ""quotes"""`},
		{"OR AND NOT", `"OR AND NOT"`},
		{"test*", `"test*"`},
	}

	for _, tt := range tests {
		result := sanitizeFTSQuery(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeFTSQuery(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
