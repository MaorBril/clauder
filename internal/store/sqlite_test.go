package store

import (
	"fmt"
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

func TestGetFacts_BleveSearch(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	_, _ = store.AddFact("golang programming language", nil, "/project")
	_, _ = store.AddFact("python scripting language", nil, "/project")
	_, _ = store.AddFact("rust systems programming", nil, "/project")

	// Search for "golang" should find the first fact
	facts, err := store.GetFacts("golang", nil, "", 10)
	if err != nil {
		t.Fatalf("GetFacts failed: %v", err)
	}
	if len(facts) != 1 {
		t.Errorf("expected 1 fact for 'golang', got %d", len(facts))
	}

	// Search for "programming" should find 2 facts
	facts, err = store.GetFacts("programming", nil, "", 10)
	if err != nil {
		t.Fatalf("GetFacts failed: %v", err)
	}
	if len(facts) != 2 {
		t.Errorf("expected 2 facts for 'programming', got %d", len(facts))
	}

	// Search for "language" should find 2 facts (golang and python)
	facts, err = store.GetFacts("language", nil, "", 10)
	if err != nil {
		t.Fatalf("GetFacts failed: %v", err)
	}
	if len(facts) != 2 {
		t.Errorf("expected 2 facts for 'language', got %d", len(facts))
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
	err := store.RegisterInstance("test-instance-id", "test-dir-id", "", "/test/dir", "/dev/ttys000", 12345)
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

	_ = store.RegisterInstance("old-instance", "old-dir-id", "", "/old", "", 111)
	_ = store.RegisterInstance("new-instance", "new-dir-id", "", "/new", "", 222)

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
	_ = store.RegisterInstance("sender", "sender-dir-id", "", "/sender", "", 1)
	_ = store.RegisterInstance("receiver", "receiver-dir-id", "", "/receiver", "", 2)

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

	_ = store.RegisterInstance("sender", "sender-dir-id", "", "/sender", "", 1)
	_ = store.RegisterInstance("receiver", "receiver-dir-id", "", "/receiver", "", 2)

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

func TestBulkAddFacts(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	facts := []BulkFact{
		{Content: "fact one", Tags: []string{"tag1"}},
		{Content: "fact two", Tags: nil},
		{Content: "fact three", Tags: []string{"tag2", "tag3"}},
	}

	stored, err := store.BulkAddFacts(facts, "/project")
	if err != nil {
		t.Fatalf("BulkAddFacts failed: %v", err)
	}
	if len(stored) != 3 {
		t.Fatalf("expected 3 stored facts, got %d", len(stored))
	}

	// Check IDs are unique and sequential
	if stored[0].ID >= stored[1].ID || stored[1].ID >= stored[2].ID {
		t.Error("expected sequential IDs")
	}

	// Check content preserved
	if stored[0].Content != "fact one" {
		t.Errorf("expected 'fact one', got '%s'", stored[0].Content)
	}
	if stored[1].Content != "fact two" {
		t.Errorf("expected 'fact two', got '%s'", stored[1].Content)
	}

	// Check tags preserved
	if len(stored[0].Tags) != 1 || stored[0].Tags[0] != "tag1" {
		t.Errorf("unexpected tags for fact one: %v", stored[0].Tags)
	}
	// nil tags should become empty
	if len(stored[1].Tags) != 0 {
		t.Errorf("expected empty tags for fact two, got %v", stored[1].Tags)
	}
	if len(stored[2].Tags) != 2 {
		t.Errorf("expected 2 tags for fact three, got %v", stored[2].Tags)
	}

	// Verify all stored in the right directory
	if stored[0].SourceDir != "/project" {
		t.Errorf("expected source_dir '/project', got '%s'", stored[0].SourceDir)
	}

	// Verify retrievable
	all, _ := store.GetAllFactsByDir("/project")
	if len(all) != 3 {
		t.Errorf("expected 3 retrievable facts, got %d", len(all))
	}
}

func TestBulkAddFacts_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	stored, err := store.BulkAddFacts([]BulkFact{}, "/project")
	if err != nil {
		t.Fatalf("BulkAddFacts failed: %v", err)
	}
	if len(stored) != 0 {
		t.Errorf("expected 0 stored facts, got %d", len(stored))
	}
}

func TestBulkAddFacts_Chunking(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Create more facts than bulkInsertChunkSize (100) to exercise chunking
	n := 250
	facts := make([]BulkFact, n)
	for i := range facts {
		facts[i] = BulkFact{Content: fmt.Sprintf("fact %d", i)}
	}

	stored, err := store.BulkAddFacts(facts, "/project")
	if err != nil {
		t.Fatalf("BulkAddFacts failed: %v", err)
	}
	if len(stored) != n {
		t.Fatalf("expected %d stored facts, got %d", n, len(stored))
	}

	// IDs should be sequential
	for i := 1; i < len(stored); i++ {
		if stored[i].ID != stored[i-1].ID+1 {
			t.Errorf("expected sequential IDs, but fact[%d].ID=%d, fact[%d].ID=%d",
				i-1, stored[i-1].ID, i, stored[i].ID)
			break
		}
	}

	// Content should match input order
	if stored[0].Content != "fact 0" {
		t.Errorf("expected 'fact 0', got '%s'", stored[0].Content)
	}
	if stored[n-1].Content != fmt.Sprintf("fact %d", n-1) {
		t.Errorf("expected 'fact %d', got '%s'", n-1, stored[n-1].Content)
	}

	// Verify all retrievable
	all, _ := store.GetAllFactsByDir("/project")
	if len(all) != n {
		t.Errorf("expected %d retrievable facts, got %d", n, len(all))
	}
}

func TestGetAllFactsByDir(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	_, _ = store.AddFact("fact1 in project1", []string{"arch"}, "/project1")
	_, _ = store.AddFact("fact2 in project1", nil, "/project1")
	_, _ = store.AddFact("fact in project2", nil, "/project2")

	facts, err := store.GetAllFactsByDir("/project1")
	if err != nil {
		t.Fatalf("GetAllFactsByDir failed: %v", err)
	}
	if len(facts) != 2 {
		t.Errorf("expected 2 facts, got %d", len(facts))
	}
	// Should be ordered by created_at
	if facts[0].Content != "fact1 in project1" {
		t.Errorf("expected first fact to be 'fact1 in project1', got '%s'", facts[0].Content)
	}
}

func TestGetAllFactsByDir_ExcludesDeleted(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	f1, _ := store.AddFact("active fact", nil, "/project")
	f2, _ := store.AddFact("deleted fact", nil, "/project")
	_ = store.SoftDeleteFact(f2.ID)

	facts, err := store.GetAllFactsByDir("/project")
	if err != nil {
		t.Fatalf("GetAllFactsByDir failed: %v", err)
	}
	if len(facts) != 1 {
		t.Errorf("expected 1 fact, got %d", len(facts))
	}
	if facts[0].ID != f1.ID {
		t.Errorf("expected fact ID %d, got %d", f1.ID, facts[0].ID)
	}
}

func TestGetAllFactsByDir_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	facts, err := store.GetAllFactsByDir("/nonexistent")
	if err != nil {
		t.Fatalf("GetAllFactsByDir failed: %v", err)
	}
	if len(facts) != 0 {
		t.Errorf("expected 0 facts, got %d", len(facts))
	}
}

func TestBulkSoftDeleteFacts(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	f1, _ := store.AddFact("fact 1", nil, "/project")
	f2, _ := store.AddFact("fact 2", nil, "/project")
	f3, _ := store.AddFact("fact 3", nil, "/project")

	deleted, err := store.BulkSoftDeleteFacts([]int64{f1.ID, f3.ID})
	if err != nil {
		t.Fatalf("BulkSoftDeleteFacts failed: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deleted, got %d", deleted)
	}

	// f2 should still be retrievable
	found, _ := store.GetFactByID(f2.ID)
	if found == nil {
		t.Error("expected f2 to still exist")
	}

	// f1 and f3 should not be retrievable
	gone1, _ := store.GetFactByID(f1.ID)
	if gone1 != nil {
		t.Error("expected f1 to be deleted")
	}
	gone3, _ := store.GetFactByID(f3.ID)
	if gone3 != nil {
		t.Error("expected f3 to be deleted")
	}
}

func TestBulkSoftDeleteFacts_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	deleted, err := store.BulkSoftDeleteFacts([]int64{})
	if err != nil {
		t.Fatalf("BulkSoftDeleteFacts failed: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted, got %d", deleted)
	}
}

func TestBulkSoftDeleteFacts_AlreadyDeleted(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	f1, _ := store.AddFact("fact 1", nil, "/project")
	_ = store.SoftDeleteFact(f1.ID)

	// Trying to bulk delete an already-deleted fact should affect 0 rows
	deleted, err := store.BulkSoftDeleteFacts([]int64{f1.ID})
	if err != nil {
		t.Fatalf("BulkSoftDeleteFacts failed: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted (already soft-deleted), got %d", deleted)
	}
}

func TestBulkSoftDeleteFacts_NonexistentIDs(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	deleted, err := store.BulkSoftDeleteFacts([]int64{99999, 88888})
	if err != nil {
		t.Fatalf("BulkSoftDeleteFacts failed: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted, got %d", deleted)
	}
}

func TestGetAllFacts(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	_, _ = store.AddFact("fact in project1", []string{"arch"}, "/project1")
	_, _ = store.AddFact("fact in project2", nil, "/project2")
	_, _ = store.AddFact("another fact in project1", nil, "/project1")

	facts, err := store.GetAllFacts()
	if err != nil {
		t.Fatalf("GetAllFacts failed: %v", err)
	}
	if len(facts) != 3 {
		t.Errorf("expected 3 facts, got %d", len(facts))
	}

	// Should be ordered by source_dir then created_at
	if facts[0].SourceDir != "/project1" {
		t.Errorf("expected first fact from /project1, got %s", facts[0].SourceDir)
	}
	if facts[1].SourceDir != "/project1" {
		t.Errorf("expected second fact from /project1, got %s", facts[1].SourceDir)
	}
	if facts[2].SourceDir != "/project2" {
		t.Errorf("expected third fact from /project2, got %s", facts[2].SourceDir)
	}
}

func TestGetAllFacts_ExcludesDeleted(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	f1, _ := store.AddFact("active fact", nil, "/project1")
	f2, _ := store.AddFact("deleted fact", nil, "/project2")
	_ = store.SoftDeleteFact(f2.ID)

	facts, err := store.GetAllFacts()
	if err != nil {
		t.Fatalf("GetAllFacts failed: %v", err)
	}
	if len(facts) != 1 {
		t.Errorf("expected 1 fact, got %d", len(facts))
	}
	if facts[0].ID != f1.ID {
		t.Errorf("expected fact ID %d, got %d", f1.ID, facts[0].ID)
	}
}

func TestGetAllFacts_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	facts, err := store.GetAllFacts()
	if err != nil {
		t.Fatalf("GetAllFacts failed: %v", err)
	}
	if len(facts) != 0 {
		t.Errorf("expected 0 facts, got %d", len(facts))
	}
}

// UpdateFact tests

func TestUpdateFact(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	original, _ := store.AddFact("original content", []string{"tag1"}, "/project")

	updated, err := store.UpdateFact(original.ID, "updated content", []string{"tag2", "tag3"})
	if err != nil {
		t.Fatalf("UpdateFact failed: %v", err)
	}
	if updated == nil {
		t.Fatal("expected non-nil updated fact")
	}
	if updated.Content != "updated content" {
		t.Errorf("expected 'updated content', got '%s'", updated.Content)
	}
	if len(updated.Tags) != 2 || updated.Tags[0] != "tag2" {
		t.Errorf("expected tags [tag2, tag3], got %v", updated.Tags)
	}
	// ID should be preserved
	if updated.ID != original.ID {
		t.Errorf("expected ID %d, got %d", original.ID, updated.ID)
	}
	// CreatedAt should be preserved
	if !updated.CreatedAt.Equal(original.CreatedAt) {
		t.Error("expected CreatedAt to be preserved")
	}
	// UpdatedAt should be newer
	if !updated.UpdatedAt.After(original.UpdatedAt) || updated.UpdatedAt.Equal(original.UpdatedAt) {
		t.Error("expected UpdatedAt to be newer than original")
	}
}

func TestUpdateFact_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	updated, err := store.UpdateFact(99999, "content", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated != nil {
		t.Error("expected nil for non-existent fact")
	}
}

func TestUpdateFact_DeletedFact(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	f, _ := store.AddFact("content", nil, "/project")
	_ = store.SoftDeleteFact(f.ID)

	updated, err := store.UpdateFact(f.ID, "new content", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated != nil {
		t.Error("expected nil for soft-deleted fact")
	}
}

// CompressFacts tests

func TestCompressFacts(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	f1, _ := store.AddFact("old fact A", []string{"tag1"}, "/project")
	f2, _ := store.AddFact("old fact B", nil, "/project")
	f3, _ := store.AddFact("old fact C", nil, "/project")

	deleted, added, err := store.CompressFacts(
		[]int64{f1.ID, f2.ID, f3.ID},
		[]BulkFact{
			{Content: "merged A+B+C", Tags: []string{"compacted"}},
		},
		"/project",
	)
	if err != nil {
		t.Fatalf("CompressFacts failed: %v", err)
	}
	if deleted != 3 {
		t.Errorf("expected 3 deleted, got %d", deleted)
	}
	if len(added) != 1 {
		t.Fatalf("expected 1 added, got %d", len(added))
	}
	if added[0].Content != "merged A+B+C" {
		t.Errorf("expected 'merged A+B+C', got '%s'", added[0].Content)
	}
	if len(added[0].Tags) != 1 || added[0].Tags[0] != "compacted" {
		t.Errorf("expected tags [compacted], got %v", added[0].Tags)
	}

	// Verify old facts are gone and new one exists
	remaining, _ := store.GetAllFactsByDir("/project")
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining fact, got %d", len(remaining))
	}
	if remaining[0].Content != "merged A+B+C" {
		t.Errorf("expected merged content, got '%s'", remaining[0].Content)
	}
}

func TestCompressFacts_DeleteOnly(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	f1, _ := store.AddFact("stale fact", nil, "/project")

	deleted, added, err := store.CompressFacts([]int64{f1.ID}, nil, "/project")
	if err != nil {
		t.Fatalf("CompressFacts failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}
	if len(added) != 0 {
		t.Errorf("expected 0 added, got %d", len(added))
	}

	remaining, _ := store.GetAllFactsByDir("/project")
	if len(remaining) != 0 {
		t.Errorf("expected 0 remaining, got %d", len(remaining))
	}
}

func TestCompressFacts_AddOnly(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	deleted, added, err := store.CompressFacts(
		nil,
		[]BulkFact{{Content: "new fact", Tags: []string{"fresh"}}},
		"/project",
	)
	if err != nil {
		t.Fatalf("CompressFacts failed: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted, got %d", deleted)
	}
	if len(added) != 1 {
		t.Fatalf("expected 1 added, got %d", len(added))
	}
}

func TestCompressFacts_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	deleted, added, err := store.CompressFacts(nil, nil, "/project")
	if err != nil {
		t.Fatalf("CompressFacts failed: %v", err)
	}
	if deleted != 0 || len(added) != 0 {
		t.Errorf("expected 0/0, got %d/%d", deleted, len(added))
	}
}

func TestCompressFacts_Atomicity(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	f1, _ := store.AddFact("will be deleted", nil, "/project")
	_, _ = store.AddFact("stays", nil, "/project")

	// Compress: delete f1, add a replacement, leave f2 alone
	deleted, added, err := store.CompressFacts(
		[]int64{f1.ID},
		[]BulkFact{{Content: "replacement for f1"}},
		"/project",
	)
	if err != nil {
		t.Fatalf("CompressFacts failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}
	if len(added) != 1 {
		t.Errorf("expected 1 added, got %d", len(added))
	}

	// f2 should be untouched
	remaining, _ := store.GetAllFactsByDir("/project")
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining facts, got %d", len(remaining))
	}
	contents := map[string]bool{}
	for _, f := range remaining {
		contents[f.Content] = true
	}
	if !contents["stays"] {
		t.Error("expected 'stays' to remain")
	}
	if !contents["replacement for f1"] {
		t.Error("expected 'replacement for f1' to be added")
	}
	if contents["will be deleted"] {
		t.Error("expected 'will be deleted' to be gone")
	}

	// f1 should be soft-deleted (not hard-deleted)
	var deletedCount int
	_ = store.db.QueryRow("SELECT COUNT(*) FROM facts WHERE deleted_at IS NOT NULL").Scan(&deletedCount)
	if deletedCount != 1 {
		t.Errorf("expected 1 soft-deleted fact, got %d", deletedCount)
	}
}

// PurgeDeletedFacts tests

func TestPurgeDeletedFacts(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	f1, _ := store.AddFact("active", nil, "/project")
	f2, _ := store.AddFact("to delete", nil, "/project")
	f3, _ := store.AddFact("also to delete", nil, "/project")
	_ = store.SoftDeleteFact(f2.ID)
	_ = store.SoftDeleteFact(f3.ID)

	purged, err := store.PurgeDeletedFacts()
	if err != nil {
		t.Fatalf("PurgeDeletedFacts failed: %v", err)
	}
	if purged != 2 {
		t.Errorf("expected 2 purged, got %d", purged)
	}

	// Active fact should still exist
	found, _ := store.GetFactByID(f1.ID)
	if found == nil {
		t.Error("expected active fact to still exist")
	}

	// Verify hard-deleted (not just soft-deleted)
	var totalCount int
	_ = store.db.QueryRow("SELECT COUNT(*) FROM facts").Scan(&totalCount)
	if totalCount != 1 {
		t.Errorf("expected 1 total fact in DB, got %d", totalCount)
	}
}

func TestPurgeDeletedFacts_NothingToDelete(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	_, _ = store.AddFact("active", nil, "/project")

	purged, err := store.PurgeDeletedFacts()
	if err != nil {
		t.Fatalf("PurgeDeletedFacts failed: %v", err)
	}
	if purged != 0 {
		t.Errorf("expected 0 purged, got %d", purged)
	}
}

func TestPurgeDeletedFacts_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	purged, err := store.PurgeDeletedFacts()
	if err != nil {
		t.Fatalf("PurgeDeletedFacts failed: %v", err)
	}
	if purged != 0 {
		t.Errorf("expected 0 purged, got %d", purged)
	}
}

// GetFactStats tests

func TestGetFactStats(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	_, _ = store.AddFact("fact in project1", []string{"arch"}, "/project1")
	_, _ = store.AddFact("another fact in project1", nil, "/project1")
	_, _ = store.AddFact("fact in project2", nil, "/project2")
	deleted, _ := store.AddFact("deleted fact", nil, "/project1")
	_ = store.SoftDeleteFact(deleted.ID)

	stats, err := store.GetFactStats()
	if err != nil {
		t.Fatalf("GetFactStats failed: %v", err)
	}

	if stats.TotalFacts != 3 {
		t.Errorf("expected 3 total facts, got %d", stats.TotalFacts)
	}
	if stats.DeletedFacts != 1 {
		t.Errorf("expected 1 deleted fact, got %d", stats.DeletedFacts)
	}
	if len(stats.ByDirectory) != 2 {
		t.Errorf("expected 2 directories, got %d", len(stats.ByDirectory))
	}

	dir1 := stats.ByDirectory["/project1"]
	if dir1.Count != 2 {
		t.Errorf("expected 2 facts in /project1, got %d", dir1.Count)
	}
	if dir1.Size == 0 {
		t.Error("expected non-zero size for /project1")
	}

	dir2 := stats.ByDirectory["/project2"]
	if dir2.Count != 1 {
		t.Errorf("expected 1 fact in /project2, got %d", dir2.Count)
	}
}

func TestGetFactStats_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	stats, err := store.GetFactStats()
	if err != nil {
		t.Fatalf("GetFactStats failed: %v", err)
	}
	if stats.TotalFacts != 0 {
		t.Errorf("expected 0 total facts, got %d", stats.TotalFacts)
	}
	if stats.DeletedFacts != 0 {
		t.Errorf("expected 0 deleted facts, got %d", stats.DeletedFacts)
	}
	if len(stats.ByDirectory) != 0 {
		t.Errorf("expected 0 directories, got %d", len(stats.ByDirectory))
	}
}

func TestGetFactStats_SizeCalculation(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	content1 := "short"
	content2 := "this is a much longer fact content for testing size"
	_, _ = store.AddFact(content1, nil, "/project")
	_, _ = store.AddFact(content2, nil, "/project")

	stats, err := store.GetFactStats()
	if err != nil {
		t.Fatalf("GetFactStats failed: %v", err)
	}

	expectedSize := len(content1) + len(content2)
	if stats.TotalSize != expectedSize {
		t.Errorf("expected total size %d, got %d", expectedSize, stats.TotalSize)
	}
}

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

