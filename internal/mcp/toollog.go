package mcp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const argTruncateLen = 200

// sanitizeArgs JSON-encodes tool arguments, truncating large string values.
func sanitizeArgs(args map[string]interface{}) string {
	if len(args) == 0 {
		return "{}"
	}

	sanitized := make(map[string]interface{}, len(args))
	for k, v := range args {
		switch k {
		case "fact", "content", "facts":
			// Truncate large text fields
			if s, ok := v.(string); ok && len(s) > argTruncateLen {
				sanitized[k] = s[:argTruncateLen] + "..."
			} else {
				sanitized[k] = v
			}
		default:
			sanitized[k] = v
		}
	}

	data, err := json.Marshal(sanitized)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// extractResultSummary produces a condensed JSON summary of the tool result.
func extractResultSummary(toolName string, args map[string]interface{}, result ToolResult) string {
	text := ""
	if len(result.Content) > 0 {
		text = result.Content[0].Text
	}

	if result.IsError {
		errMsg := text
		if len(errMsg) > 100 {
			errMsg = errMsg[:100] + "..."
		}
		return jsonObj("error", errMsg)
	}

	switch toolName {
	case "get_context":
		return summarizeGetContext(text)
	case "get_global_context":
		return summarizeGetGlobalContext(text)
	case "remember":
		return summarizeRemember(text)
	case "recall":
		return summarizeRecall(text, args)
	case "forget":
		return summarizeForget(text, args)
	case "get_messages":
		return summarizeGetMessages(text)
	case "send_message":
		return summarizeSendMessage(args)
	case "compact_context":
		return summarizeCompactContext(text)
	case "compress_facts":
		return summarizeCompressFacts(text)
	case "bulk_forget":
		return summarizeBulkForget(text)
	case "bulk_remember":
		return summarizeBulkRemember(text)
	case "update_fact":
		return summarizeUpdateFact(text, args)
	case "fact_stats":
		return summarizeFactStats(text)
	case "purge_deleted":
		return summarizePurgeDeleted(text)
	case "optimize_context":
		return summarizeOptimizeContext(text)
	case "list_instances":
		return summarizeListInstances(text)
	case "rename_instance":
		name, _ := args["name"].(string)
		return fmt.Sprintf(`{"name":%s}`, jsonString(name))
	default:
		return jsonObj("result_length", len(text))
	}
}

// --- Per-tool summary extractors ---

var factCountRe = regexp.MustCompile(`\*\*(\d+) facts?\*\*`)
var storedFactRe = regexp.MustCompile(`Stored fact #(\d+)`)
var recallCountRe = regexp.MustCompile(`Found (\d+) fact`)
var factIDsRe = regexp.MustCompile(`\[#(\d+)\]`)
var deletedAddedRe = regexp.MustCompile(`deleted (\d+).*added (\d+)`)
var deletedCountRe = regexp.MustCompile(`Deleted (\d+)`)
var storedCountRe = regexp.MustCompile(`Stored (\d+)`)
var removedCountRe = regexp.MustCompile(`Permanently removed (\d+)`)
var activeFactsRe = regexp.MustCompile(`Active facts:\*\* (\d+)`)
var instanceCountRe = regexp.MustCompile(`(\d+) running instance`)

func summarizeGetContext(text string) string {
	factCount := strings.Count(text, "\n- ")
	messageCount := 0
	if strings.Contains(text, "Unread Messages") {
		// Count message entries after "Unread Messages" section
		parts := strings.SplitN(text, "Unread Messages", 2)
		if len(parts) > 1 {
			messageCount = strings.Count(parts[1], "\n- ")
		}
	}
	instanceCount := 0
	if m := instanceCountRe.FindStringSubmatch(text); len(m) > 1 {
		instanceCount, _ = strconv.Atoi(m[1])
	}
	return fmt.Sprintf(`{"fact_count":%d,"message_count":%d,"instance_count":%d}`, factCount, messageCount, instanceCount)
}

func summarizeGetGlobalContext(text string) string {
	factCount := strings.Count(text, "\n- ")
	dirCount := strings.Count(text, " facts)")
	return fmt.Sprintf(`{"fact_count":%d,"dir_count":%d}`, factCount, dirCount)
}

func summarizeRemember(text string) string {
	if m := storedFactRe.FindStringSubmatch(text); len(m) > 1 {
		return fmt.Sprintf(`{"fact_id":%s}`, m[1])
	}
	return `{"stored":true}`
}

func summarizeRecall(text string, args map[string]interface{}) string {
	query, _ := args["query"].(string)
	count := 0
	if m := recallCountRe.FindStringSubmatch(text); len(m) > 1 {
		count, _ = strconv.Atoi(m[1])
	}
	var ids []string
	for _, m := range factIDsRe.FindAllStringSubmatch(text, -1) {
		ids = append(ids, m[1])
	}
	if query != "" {
		return fmt.Sprintf(`{"query":%s,"result_count":%d,"fact_ids":[%s]}`,
			jsonString(query), count, strings.Join(ids, ","))
	}
	return fmt.Sprintf(`{"result_count":%d,"fact_ids":[%s]}`, count, strings.Join(ids, ","))
}

func summarizeForget(text string, args map[string]interface{}) string {
	id := jsonNum(args["id"])
	confirmed := strings.Contains(text, "Deleted") || strings.Contains(text, "deleted")
	return fmt.Sprintf(`{"fact_id":%s,"confirmed":%t}`, id, confirmed)
}

func summarizeGetMessages(text string) string {
	if strings.Contains(text, "No unread messages") || strings.Contains(text, "No messages") {
		return `{"message_count":0}`
	}
	count := strings.Count(text, "From:")
	if count == 0 {
		count = strings.Count(text, "\n- ")
	}
	return fmt.Sprintf(`{"message_count":%d}`, count)
}

func summarizeSendMessage(args map[string]interface{}) string {
	to, _ := args["to"].(string)
	return fmt.Sprintf(`{"to":%s}`, jsonString(to))
}

func summarizeCompactContext(text string) string {
	if m := factCountRe.FindStringSubmatch(text); len(m) > 1 {
		return fmt.Sprintf(`{"fact_count":%s}`, m[1])
	}
	return `{"fact_count":0}`
}

func summarizeCompressFacts(text string) string {
	if m := deletedAddedRe.FindStringSubmatch(text); len(m) > 2 {
		return fmt.Sprintf(`{"deleted":%s,"added":%s}`, m[1], m[2])
	}
	// Delete-only case
	if m := deletedCountRe.FindStringSubmatch(text); len(m) > 1 {
		return fmt.Sprintf(`{"deleted":%s,"added":0}`, m[1])
	}
	return `{"deleted":0,"added":0}`
}

func summarizeBulkForget(text string) string {
	if m := deletedCountRe.FindStringSubmatch(text); len(m) > 1 {
		return fmt.Sprintf(`{"deleted":%s}`, m[1])
	}
	return `{"deleted":0}`
}

func summarizeBulkRemember(text string) string {
	if m := storedCountRe.FindStringSubmatch(text); len(m) > 1 {
		return fmt.Sprintf(`{"stored":%s}`, m[1])
	}
	return `{"stored":0}`
}

func summarizeUpdateFact(_ string, args map[string]interface{}) string {
	id := jsonNum(args["id"])
	return fmt.Sprintf(`{"fact_id":%s}`, id)
}

func summarizeFactStats(text string) string {
	active := 0
	if m := activeFactsRe.FindStringSubmatch(text); len(m) > 1 {
		active, _ = strconv.Atoi(m[1])
	}
	return fmt.Sprintf(`{"active_facts":%d}`, active)
}

func summarizePurgeDeleted(text string) string {
	if m := removedCountRe.FindStringSubmatch(text); len(m) > 1 {
		return fmt.Sprintf(`{"purged":%s}`, m[1])
	}
	if strings.Contains(text, "No soft-deleted") {
		return `{"purged":0}`
	}
	return `{"preview":true}`
}

var neverAccessedRe = regexp.MustCompile(`Never Accessed \((\d+) fact`)
var staleRe = regexp.MustCompile(`Stale \((\d+) fact`)
var healthyRe = regexp.MustCompile(`Healthy \((\d+) fact`)

func summarizeOptimizeContext(text string) string {
	na, st, he := 0, 0, 0
	if m := neverAccessedRe.FindStringSubmatch(text); len(m) > 1 {
		na, _ = strconv.Atoi(m[1])
	}
	if m := staleRe.FindStringSubmatch(text); len(m) > 1 {
		st, _ = strconv.Atoi(m[1])
	}
	if m := healthyRe.FindStringSubmatch(text); len(m) > 1 {
		he, _ = strconv.Atoi(m[1])
	}
	return fmt.Sprintf(`{"never_accessed":%d,"stale":%d,"healthy":%d}`, na, st, he)
}

func summarizeListInstances(text string) string {
	if m := instanceCountRe.FindStringSubmatch(text); len(m) > 1 {
		return fmt.Sprintf(`{"instance_count":%s}`, m[1])
	}
	return `{"instance_count":0}`
}

// --- helpers ---

func jsonObj(key string, val interface{}) string {
	data, _ := json.Marshal(map[string]interface{}{key: val})
	return string(data)
}

func jsonString(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}

func jsonNum(v interface{}) string {
	switch n := v.(type) {
	case float64:
		return strconv.FormatInt(int64(n), 10)
	case int64:
		return strconv.FormatInt(n, 10)
	case int:
		return strconv.Itoa(n)
	default:
		return "0"
	}
}
