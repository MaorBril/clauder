package mcp

import (
	"os"
	"strings"
	"testing"

	"github.com/maorbril/clauder/internal/store"
)

func setupTestServer(t *testing.T) (*Server, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "clauder-mcp-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	s, err := store.NewSQLiteStore(tmpDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("failed to create store: %v", err)
	}
	server := NewServer(s, "test-instance", "test-directory-id", "/test/workdir")
	cleanup := func() {
		_ = s.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return server, cleanup
}

// Remember tool tests

func TestToolRemember_Valid(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolRemember(map[string]interface{}{
		"fact": "test fact content",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Stored fact #") {
		t.Errorf("unexpected result: %s", result.Content[0].Text)
	}
}

func TestToolRemember_EmptyFact(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolRemember(map[string]interface{}{
		"fact": "",
	})

	if !result.IsError {
		t.Error("expected error for empty fact")
	}
	if !strings.Contains(result.Content[0].Text, "fact is required") {
		t.Errorf("unexpected error message: %s", result.Content[0].Text)
	}
}

func TestToolRemember_MissingFact(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolRemember(map[string]interface{}{})

	if !result.IsError {
		t.Error("expected error for missing fact")
	}
}

func TestToolRemember_TooLarge(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create a fact larger than MaxFactSize
	largeFact := strings.Repeat("x", MaxFactSize+1)
	result := server.toolRemember(map[string]interface{}{
		"fact": largeFact,
	})

	if !result.IsError {
		t.Error("expected error for oversized fact")
	}
	if !strings.Contains(result.Content[0].Text, "exceeds maximum size") {
		t.Errorf("unexpected error message: %s", result.Content[0].Text)
	}
}

func TestToolRemember_TooManyTags(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tags := make([]interface{}, MaxTagCount+1)
	for i := range tags {
		tags[i] = "tag"
	}

	result := server.toolRemember(map[string]interface{}{
		"fact": "test",
		"tags": tags,
	})

	if !result.IsError {
		t.Error("expected error for too many tags")
	}
	if !strings.Contains(result.Content[0].Text, "too many tags") {
		t.Errorf("unexpected error message: %s", result.Content[0].Text)
	}
}

func TestToolRemember_TagTooLong(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	longTag := strings.Repeat("x", MaxTagLength+1)
	result := server.toolRemember(map[string]interface{}{
		"fact": "test",
		"tags": []interface{}{longTag},
	})

	if !result.IsError {
		t.Error("expected error for oversized tag")
	}
	if !strings.Contains(result.Content[0].Text, "tag exceeds maximum length") {
		t.Errorf("unexpected error message: %s", result.Content[0].Text)
	}
}

func TestToolRemember_WithTags(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolRemember(map[string]interface{}{
		"fact": "architectural decision",
		"tags": []interface{}{"architecture", "decision"},
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content[0].Text)
	}
}

// Recall tool tests

func TestToolRecall_Valid(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Store some facts first
	server.toolRemember(map[string]interface{}{"fact": "golang is great"})
	server.toolRemember(map[string]interface{}{"fact": "python is also great"})

	result := server.toolRecall(map[string]interface{}{
		"query": "golang",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "golang is great") {
		t.Errorf("expected to find golang fact, got: %s", result.Content[0].Text)
	}
}

func TestToolRecall_NoResults(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolRecall(map[string]interface{}{
		"query": "nonexistent",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "No facts found") {
		t.Errorf("unexpected result: %s", result.Content[0].Text)
	}
}

func TestToolRecall_CurrentDirOnly(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Store fact (will use server's workDir: /test/workdir)
	server.toolRemember(map[string]interface{}{"fact": "local fact"})

	// Store another fact directly to a different directory
	_, _ = server.store.AddFact("other dir fact", nil, "/other/dir")

	result := server.toolRecall(map[string]interface{}{
		"current_dir_only": true,
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content[0].Text)
	}
	if strings.Contains(result.Content[0].Text, "other dir fact") {
		t.Error("should not contain facts from other directories")
	}
	if !strings.Contains(result.Content[0].Text, "local fact") {
		t.Error("should contain local fact")
	}
}

// SendMessage tool tests

func TestToolSendMessage_Valid(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Register target instance with a named instance ID (contains colon)
	_ = server.store.RegisterInstance("target-dir-id:target", "target-dir-id", "target", "/target", "", 123)

	result := server.toolSendMessage(map[string]interface{}{
		"to":      "target-dir-id:target",
		"content": "hello!",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Message #") {
		t.Errorf("unexpected result: %s", result.Content[0].Text)
	}
}

func TestToolSendMessage_InvalidInstance(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Use an ID with colon to target a specific instance (not broadcast)
	result := server.toolSendMessage(map[string]interface{}{
		"to":      "nonexistent-dir:instance",
		"content": "hello!",
	})

	if !result.IsError {
		t.Error("expected error for nonexistent instance")
	}
	if !strings.Contains(result.Content[0].Text, "not found") {
		t.Errorf("unexpected error message: %s", result.Content[0].Text)
	}
}

func TestToolSendMessage_MissingTo(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolSendMessage(map[string]interface{}{
		"content": "hello!",
	})

	if !result.IsError {
		t.Error("expected error for missing 'to'")
	}
}

func TestToolSendMessage_MissingContent(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolSendMessage(map[string]interface{}{
		"to": "some-instance",
	})

	if !result.IsError {
		t.Error("expected error for missing content")
	}
}

func TestToolSendMessage_TooLarge(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Register target instance with named ID
	_ = server.store.RegisterInstance("target-dir-id:target", "target-dir-id", "target", "/target", "", 123)

	largeContent := strings.Repeat("x", MaxMessageSize+1)
	result := server.toolSendMessage(map[string]interface{}{
		"to":      "target-dir-id:target",
		"content": largeContent,
	})

	if !result.IsError {
		t.Error("expected error for oversized message")
	}
	if !strings.Contains(result.Content[0].Text, "exceeds maximum size") {
		t.Errorf("unexpected error message: %s", result.Content[0].Text)
	}
}

// GetMessages tool tests

func TestToolGetMessages_NoMessages(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolGetMessages(map[string]interface{}{})

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "No unread messages") {
		t.Errorf("unexpected result: %s", result.Content[0].Text)
	}
}

func TestToolGetMessages_WithMessages(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Register this instance and a sender
	_ = server.store.RegisterInstance("test-instance", "test-dir-id", "", "/test", "", 1)
	_ = server.store.RegisterInstance("sender", "sender-dir-id", "", "/sender", "", 2)

	// Send a message to our instance
	_, _ = server.store.SendMessage("sender", "test-instance", "hello from sender!")

	result := server.toolGetMessages(map[string]interface{}{})

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "hello from sender!") {
		t.Errorf("expected to find message, got: %s", result.Content[0].Text)
	}
}

func TestToolGetMessages_MarksAsRead(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Register instances
	_ = server.store.RegisterInstance("test-instance", "test-dir-id", "", "/test", "", 1)
	_ = server.store.RegisterInstance("sender", "sender-dir-id", "", "/sender", "", 2)

	// Send a message
	_, _ = server.store.SendMessage("sender", "test-instance", "test message")

	// First call should return the message and mark it as read
	result1 := server.toolGetMessages(map[string]interface{}{})
	if !strings.Contains(result1.Content[0].Text, "test message") {
		t.Error("expected to find message on first call")
	}

	// Second call with unread_only should return no messages
	result2 := server.toolGetMessages(map[string]interface{}{
		"unread_only": true,
	})
	if !strings.Contains(result2.Content[0].Text, "No unread messages") {
		t.Errorf("expected no unread messages, got: %s", result2.Content[0].Text)
	}
}

// GetContext tool tests

func TestToolGetContext_Empty(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolGetContext(map[string]interface{}{})

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "No stored context yet") {
		t.Errorf("unexpected result: %s", result.Content[0].Text)
	}
}

func TestToolGetContext_WithFacts(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Add local and global facts
	server.toolRemember(map[string]interface{}{"fact": "local fact"})
	_, _ = server.store.AddFact("global fact", nil, "/other/dir")

	result := server.toolGetContext(map[string]interface{}{})

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "local fact") {
		t.Error("expected to find local fact")
	}
	if strings.Contains(result.Content[0].Text, "global fact") {
		t.Error("should not contain facts from other directories")
	}
}

// ListInstances tool tests

func TestToolListInstances_NoInstances(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolListInstances(map[string]interface{}{})

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "No running instances found") {
		t.Errorf("unexpected result: %s", result.Content[0].Text)
	}
}

func TestToolListInstances_WithInstances(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Register some instances
	_ = server.store.RegisterInstance("instance-1", "dir1-id", "", "/dir1", "", 123)
	_ = server.store.RegisterInstance("instance-2", "dir2-id", "", "/dir2", "", 456)

	result := server.toolListInstances(map[string]interface{}{})

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "instance-1") {
		t.Error("expected to find instance-1")
	}
	if !strings.Contains(result.Content[0].Text, "instance-2") {
		t.Error("expected to find instance-2")
	}
}

// CompactContext tool tests

func TestToolCompactContext_Empty(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolCompactContext(map[string]interface{}{})

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Nothing to compact") {
		t.Errorf("unexpected result: %s", result.Content[0].Text)
	}
}

func TestToolCompactContext_WithFacts(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	server.toolRemember(map[string]interface{}{
		"fact": "important architecture decision",
		"tags": []interface{}{"architecture"},
	})
	server.toolRemember(map[string]interface{}{
		"fact": "stale PR note",
	})

	result := server.toolCompactContext(map[string]interface{}{})

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Found 2 facts") {
		t.Errorf("expected 'Found 2 facts', got: %s", text)
	}
	if !strings.Contains(text, "important architecture decision") {
		t.Error("expected to find first fact content")
	}
	if !strings.Contains(text, "stale PR note") {
		t.Error("expected to find second fact content")
	}
	if !strings.Contains(text, "architecture") {
		t.Error("expected to find tag")
	}
	if !strings.Contains(text, "## Instructions") {
		t.Error("expected analysis instructions")
	}
	if !strings.Contains(text, "bulk_forget") {
		t.Error("expected reference to bulk_forget tool")
	}
}

func TestToolCompactContext_OnlyCurrentDir(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Add fact in current dir
	server.toolRemember(map[string]interface{}{"fact": "local fact"})
	// Add fact in different dir
	_, _ = server.store.AddFact("other dir fact", nil, "/other/dir")

	result := server.toolCompactContext(map[string]interface{}{})

	text := result.Content[0].Text
	if !strings.Contains(text, "local fact") {
		t.Error("expected to find local fact")
	}
	if strings.Contains(text, "other dir fact") {
		t.Error("should not contain facts from other directories")
	}
}

// BulkForget tool tests

func TestToolBulkForget_Valid(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	server.toolRemember(map[string]interface{}{"fact": "fact one"})
	server.toolRemember(map[string]interface{}{"fact": "fact two"})
	server.toolRemember(map[string]interface{}{"fact": "fact three"})

	// Get all facts to find IDs
	facts, _ := server.store.GetAllFactsByDir("/test/workdir")
	if len(facts) != 3 {
		t.Fatalf("expected 3 facts, got %d", len(facts))
	}

	// Delete first and third
	result := server.toolBulkForget(map[string]interface{}{
		"ids": []interface{}{float64(facts[0].ID), float64(facts[2].ID)},
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Deleted 2 fact(s)") {
		t.Errorf("unexpected result: %s", result.Content[0].Text)
	}

	// Verify only the second fact remains
	remaining, _ := server.store.GetAllFactsByDir("/test/workdir")
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining fact, got %d", len(remaining))
	}
	if remaining[0].Content != "fact two" {
		t.Errorf("expected 'fact two' to remain, got '%s'", remaining[0].Content)
	}
}

func TestToolBulkForget_EmptyIDs(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolBulkForget(map[string]interface{}{
		"ids": []interface{}{},
	})

	if !result.IsError {
		t.Error("expected error for empty IDs")
	}
}

func TestToolBulkForget_MissingIDs(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolBulkForget(map[string]interface{}{})

	if !result.IsError {
		t.Error("expected error for missing IDs")
	}
}

func TestToolBulkForget_InvalidIDType(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolBulkForget(map[string]interface{}{
		"ids": []interface{}{"not-a-number"},
	})

	if !result.IsError {
		t.Error("expected error for invalid ID type")
	}
}

// BulkRemember tool tests

func TestToolBulkRemember_Valid(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolBulkRemember(map[string]interface{}{
		"facts": []interface{}{
			map[string]interface{}{"fact": "condensed fact one", "tags": []interface{}{"compacted"}},
			map[string]interface{}{"fact": "condensed fact two"},
		},
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Stored 2 fact(s)") {
		t.Errorf("unexpected result: %s", text)
	}
	if !strings.Contains(text, "condensed fact one") {
		t.Error("expected to find first fact in result")
	}
	if !strings.Contains(text, "condensed fact two") {
		t.Error("expected to find second fact in result")
	}

	// Verify stored in the right directory
	facts, _ := server.store.GetAllFactsByDir("/test/workdir")
	if len(facts) != 2 {
		t.Errorf("expected 2 stored facts, got %d", len(facts))
	}
}

func TestToolBulkRemember_EmptyArray(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolBulkRemember(map[string]interface{}{
		"facts": []interface{}{},
	})

	if !result.IsError {
		t.Error("expected error for empty facts array")
	}
}

func TestToolBulkRemember_MissingFacts(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolBulkRemember(map[string]interface{}{})

	if !result.IsError {
		t.Error("expected error for missing facts")
	}
}

func TestToolBulkRemember_InvalidEntry(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolBulkRemember(map[string]interface{}{
		"facts": []interface{}{"not an object"},
	})

	if !result.IsError {
		t.Error("expected error for invalid entry")
	}
}

func TestToolBulkRemember_EmptyFactContent(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolBulkRemember(map[string]interface{}{
		"facts": []interface{}{
			map[string]interface{}{"fact": ""},
		},
	})

	if !result.IsError {
		t.Error("expected error for empty fact content")
	}
}

func TestToolBulkRemember_CompactionWorkflow(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Simulate a full compaction workflow:
	// 1. Store some initial facts
	server.toolRemember(map[string]interface{}{"fact": "old fact A"})
	server.toolRemember(map[string]interface{}{"fact": "old fact B"})
	server.toolRemember(map[string]interface{}{"fact": "old fact C"})

	// 2. compact_context to see them all
	compactResult := server.toolCompactContext(map[string]interface{}{})
	if !strings.Contains(compactResult.Content[0].Text, "Found 3 facts") {
		t.Fatalf("expected 3 facts in compact result")
	}

	// 3. Get IDs for bulk_forget
	facts, _ := server.store.GetAllFactsByDir("/test/workdir")

	// 4. bulk_forget the old facts
	forgetResult := server.toolBulkForget(map[string]interface{}{
		"ids": []interface{}{float64(facts[0].ID), float64(facts[1].ID), float64(facts[2].ID)},
	})
	if !strings.Contains(forgetResult.Content[0].Text, "Deleted 3 fact(s)") {
		t.Fatalf("expected 3 deletions, got: %s", forgetResult.Content[0].Text)
	}

	// 5. bulk_remember the condensed facts
	rememberResult := server.toolBulkRemember(map[string]interface{}{
		"facts": []interface{}{
			map[string]interface{}{"fact": "merged fact from A+B+C", "tags": []interface{}{"compacted"}},
		},
	})
	if !strings.Contains(rememberResult.Content[0].Text, "Stored 1 fact(s)") {
		t.Fatalf("expected 1 stored, got: %s", rememberResult.Content[0].Text)
	}

	// 6. Verify final state
	remaining, _ := server.store.GetAllFactsByDir("/test/workdir")
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining fact, got %d", len(remaining))
	}
	if remaining[0].Content != "merged fact from A+B+C" {
		t.Errorf("expected merged content, got '%s'", remaining[0].Content)
	}
}

// GetGlobalContext tool tests

func TestToolGetGlobalContext_Empty(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	result := server.toolGetGlobalContext(map[string]interface{}{})

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "No stored facts across any directory") {
		t.Errorf("unexpected result: %s", result.Content[0].Text)
	}
}

func TestToolGetGlobalContext_MultipleDirs(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Add facts across multiple directories
	server.toolRemember(map[string]interface{}{"fact": "local fact"})
	_, _ = server.store.AddFact("other project fact", []string{"architecture"}, "/other/project")
	_, _ = server.store.AddFact("third project fact", nil, "/third/project")

	result := server.toolGetGlobalContext(map[string]interface{}{})

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Global Context (all directories)") {
		t.Error("expected global context header")
	}
	if !strings.Contains(text, "local fact") {
		t.Error("expected to find local fact")
	}
	if !strings.Contains(text, "other project fact") {
		t.Error("expected to find other project fact")
	}
	if !strings.Contains(text, "third project fact") {
		t.Error("expected to find third project fact")
	}
	if !strings.Contains(text, "/other/project") {
		t.Error("expected to find /other/project directory header")
	}
	if !strings.Contains(text, "/third/project") {
		t.Error("expected to find /third/project directory header")
	}
	if !strings.Contains(text, "architecture") {
		t.Error("expected to find tag")
	}
	if !strings.Contains(text, "3 fact(s) across 3 directory(ies)") {
		t.Errorf("expected summary line, got: %s", text)
	}
}

func TestToolGetGlobalContext_ExcludesDeleted(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	server.toolRemember(map[string]interface{}{"fact": "active fact"})
	deleted, _ := server.store.AddFact("deleted fact", nil, "/other/dir")
	_ = server.store.SoftDeleteFact(deleted.ID)

	result := server.toolGetGlobalContext(map[string]interface{}{})

	text := result.Content[0].Text
	if !strings.Contains(text, "active fact") {
		t.Error("expected to find active fact")
	}
	if strings.Contains(text, "deleted fact") {
		t.Error("should not contain deleted fact")
	}
}

func TestToolGetGlobalContext_GroupsByDirectory(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	_, _ = server.store.AddFact("fact A in dir1", nil, "/dir1")
	_, _ = server.store.AddFact("fact B in dir1", nil, "/dir1")
	_, _ = server.store.AddFact("fact C in dir2", nil, "/dir2")

	result := server.toolGetGlobalContext(map[string]interface{}{})

	text := result.Content[0].Text
	// Should show directory headers with counts
	if !strings.Contains(text, "/dir1 (2 facts)") {
		t.Errorf("expected '/dir1 (2 facts)' header, got: %s", text)
	}
	if !strings.Contains(text, "/dir2 (1 facts)") {
		t.Errorf("expected '/dir2 (1 facts)' header, got: %s", text)
	}
}

// Helper function tests

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a longer string", 10, "this is..."},
		{"abc", 10, "abc"},
		{"longer text here", 10, "longer ..."},
	}

	for _, tt := range tests {
		result := truncate(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}
