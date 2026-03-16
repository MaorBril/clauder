package mcp

import (
	"strings"
	"testing"
)

func TestSanitizeArgs_Empty(t *testing.T) {
	result := sanitizeArgs(nil)
	if result != "{}" {
		t.Errorf("expected '{}', got '%s'", result)
	}

	result = sanitizeArgs(map[string]interface{}{})
	if result != "{}" {
		t.Errorf("expected '{}', got '%s'", result)
	}
}

func TestSanitizeArgs_Normal(t *testing.T) {
	result := sanitizeArgs(map[string]interface{}{
		"query": "auth",
		"limit": float64(10),
	})
	if !strings.Contains(result, `"query":"auth"`) {
		t.Errorf("expected query in result, got '%s'", result)
	}
	if !strings.Contains(result, `"limit":10`) {
		t.Errorf("expected limit in result, got '%s'", result)
	}
}

func TestSanitizeArgs_TruncatesLargeFact(t *testing.T) {
	largeFact := strings.Repeat("x", 500)
	result := sanitizeArgs(map[string]interface{}{
		"fact": largeFact,
	})
	if len(result) > 300 {
		t.Errorf("expected truncated result, got length %d", len(result))
	}
	if !strings.Contains(result, "...") {
		t.Error("expected truncation indicator '...'")
	}
}

func TestSanitizeArgs_TruncatesContent(t *testing.T) {
	largeContent := strings.Repeat("y", 500)
	result := sanitizeArgs(map[string]interface{}{
		"content": largeContent,
	})
	if !strings.Contains(result, "...") {
		t.Error("expected truncation of content field")
	}
}

func TestSanitizeArgs_PreservesShortFact(t *testing.T) {
	result := sanitizeArgs(map[string]interface{}{
		"fact": "short fact",
	})
	if !strings.Contains(result, "short fact") {
		t.Errorf("expected short fact preserved, got '%s'", result)
	}
	if strings.Contains(result, "...") {
		t.Error("should not truncate short fact")
	}
}

func TestExtractResultSummary_Error(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{{Type: "text", Text: "something went wrong"}},
		IsError: true,
	}
	summary := extractResultSummary("remember", nil, result)
	if !strings.Contains(summary, `"error"`) {
		t.Errorf("expected error key, got '%s'", summary)
	}
	if !strings.Contains(summary, "something went wrong") {
		t.Errorf("expected error message in summary, got '%s'", summary)
	}
}

func TestExtractResultSummary_Remember(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{{Type: "text", Text: "Stored fact #42 with tags [test]"}},
	}
	summary := extractResultSummary("remember", nil, result)
	if !strings.Contains(summary, `"fact_id":42`) {
		t.Errorf("expected fact_id:42, got '%s'", summary)
	}
}

func TestExtractResultSummary_Recall(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{{Type: "text", Text: "Found 3 facts:\n[#1] fact one\n[#5] fact two\n[#9] fact three"}},
	}
	args := map[string]interface{}{"query": "auth"}
	summary := extractResultSummary("recall", args, result)
	if !strings.Contains(summary, `"result_count":3`) {
		t.Errorf("expected result_count:3, got '%s'", summary)
	}
	if !strings.Contains(summary, `"query":"auth"`) {
		t.Errorf("expected query in summary, got '%s'", summary)
	}
	if !strings.Contains(summary, `"fact_ids":[1,5,9]`) {
		t.Errorf("expected fact_ids, got '%s'", summary)
	}
}

func TestExtractResultSummary_Forget(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{{Type: "text", Text: "Deleted fact #7"}},
	}
	args := map[string]interface{}{"id": float64(7)}
	summary := extractResultSummary("forget", args, result)
	if !strings.Contains(summary, `"fact_id":7`) {
		t.Errorf("expected fact_id:7, got '%s'", summary)
	}
	if !strings.Contains(summary, `"confirmed":true`) {
		t.Errorf("expected confirmed:true, got '%s'", summary)
	}
}

func TestExtractResultSummary_GetMessages(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{{Type: "text", Text: "No unread messages."}},
	}
	summary := extractResultSummary("get_messages", nil, result)
	if !strings.Contains(summary, `"message_count":0`) {
		t.Errorf("expected message_count:0, got '%s'", summary)
	}
}

func TestExtractResultSummary_SendMessage(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{{Type: "text", Text: "Message #1 sent"}},
	}
	args := map[string]interface{}{"to": "abc:backend"}
	summary := extractResultSummary("send_message", args, result)
	if !strings.Contains(summary, `"to":"abc:backend"`) {
		t.Errorf("expected to field, got '%s'", summary)
	}
}

func TestExtractResultSummary_CompressFacts(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{{Type: "text", Text: "Compressed: deleted 3, added 1 new fact(s)"}},
	}
	summary := extractResultSummary("compress_facts", nil, result)
	if !strings.Contains(summary, `"deleted":3`) {
		t.Errorf("expected deleted:3, got '%s'", summary)
	}
	if !strings.Contains(summary, `"added":1`) {
		t.Errorf("expected added:1, got '%s'", summary)
	}
}

func TestExtractResultSummary_CompactContext(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{{Type: "text", Text: "**12 facts** in current directory"}},
	}
	summary := extractResultSummary("compact_context", nil, result)
	if !strings.Contains(summary, `"fact_count":12`) {
		t.Errorf("expected fact_count:12, got '%s'", summary)
	}
}

func TestExtractResultSummary_FactStats(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{{Type: "text", Text: "# Fact Statistics\n**Active facts:** 5"}},
	}
	summary := extractResultSummary("fact_stats", nil, result)
	if !strings.Contains(summary, `"active_facts":5`) {
		t.Errorf("expected active_facts:5, got '%s'", summary)
	}
}

func TestExtractResultSummary_PurgeDeleted(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{{Type: "text", Text: "Permanently removed 4 fact(s)"}},
	}
	summary := extractResultSummary("purge_deleted", nil, result)
	if !strings.Contains(summary, `"purged":4`) {
		t.Errorf("expected purged:4, got '%s'", summary)
	}
}

func TestExtractResultSummary_BulkForget(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{{Type: "text", Text: "Deleted 3 fact(s)"}},
	}
	summary := extractResultSummary("bulk_forget", nil, result)
	if !strings.Contains(summary, `"deleted":3`) {
		t.Errorf("expected deleted:3, got '%s'", summary)
	}
}

func TestExtractResultSummary_BulkRemember(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{{Type: "text", Text: "Stored 2 fact(s):\n#1: condensed\n#2: another"}},
	}
	summary := extractResultSummary("bulk_remember", nil, result)
	if !strings.Contains(summary, `"stored":2`) {
		t.Errorf("expected stored:2, got '%s'", summary)
	}
}

func TestExtractResultSummary_UnknownTool(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{{Type: "text", Text: "some output text here"}},
	}
	summary := extractResultSummary("unknown_tool", nil, result)
	if !strings.Contains(summary, `"result_length":21`) {
		t.Errorf("expected result_length, got '%s'", summary)
	}
}

func TestExtractResultSummary_EmptyContent(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{},
	}
	summary := extractResultSummary("remember", nil, result)
	// Should not panic with empty content
	if summary == "" {
		t.Error("expected non-empty summary")
	}
}

func TestExtractResultSummary_OptimizeContext(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{{Type: "text", Text: `# Context Health Report for /project

## Summary
- **20** active facts (4.2KB, ~1050 tokens)
- 5 never accessed, 3 stale, 12 healthy

## Never Accessed (5 facts, 890B)

## Stale (3 facts, 340B)

## Healthy (12 facts, 2.9KB)
`}},
	}
	summary := extractResultSummary("optimize_context", nil, result)
	if !strings.Contains(summary, `"never_accessed":5`) {
		t.Errorf("expected never_accessed:5, got '%s'", summary)
	}
	if !strings.Contains(summary, `"stale":3`) {
		t.Errorf("expected stale:3, got '%s'", summary)
	}
	if !strings.Contains(summary, `"healthy":12`) {
		t.Errorf("expected healthy:12, got '%s'", summary)
	}
}
