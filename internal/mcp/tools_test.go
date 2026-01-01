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
	server := NewServer(s, "test-instance", "/test/workdir")
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

	// Register target instance
	_ = server.store.RegisterInstance("target-instance", 123, "/target")

	result := server.toolSendMessage(map[string]interface{}{
		"to":      "target-instance",
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

	result := server.toolSendMessage(map[string]interface{}{
		"to":      "nonexistent-instance",
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

	// Register target instance
	_ = server.store.RegisterInstance("target-instance", 123, "/target")

	largeContent := strings.Repeat("x", MaxMessageSize+1)
	result := server.toolSendMessage(map[string]interface{}{
		"to":      "target-instance",
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
	_ = server.store.RegisterInstance("test-instance", 1, "/test")
	_ = server.store.RegisterInstance("sender", 2, "/sender")

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
	_ = server.store.RegisterInstance("test-instance", 1, "/test")
	_ = server.store.RegisterInstance("sender", 2, "/sender")

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
	if !strings.Contains(result.Content[0].Text, "global fact") {
		t.Error("expected to find global fact")
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
	if !strings.Contains(result.Content[0].Text, "No other running instances") {
		t.Errorf("unexpected result: %s", result.Content[0].Text)
	}
}

func TestToolListInstances_WithInstances(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Register some instances
	_ = server.store.RegisterInstance("instance-1", 123, "/dir1")
	_ = server.store.RegisterInstance("instance-2", 456, "/dir2")

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
