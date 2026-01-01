package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (s *Server) toolRemember(args map[string]interface{}) ToolResult {
	fact, ok := args["fact"].(string)
	if !ok || fact == "" {
		return errorResult("fact is required")
	}

	var tags []string
	if tagsRaw, ok := args["tags"].([]interface{}); ok {
		for _, t := range tagsRaw {
			if tag, ok := t.(string); ok {
				tags = append(tags, tag)
			}
		}
	}

	stored, err := s.store.AddFact(fact, tags, s.workDir)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to store fact: %v", err))
	}

	return textResult(fmt.Sprintf("Stored fact #%d: %s", stored.ID, truncate(fact, 100)))
}

func (s *Server) toolRecall(args map[string]interface{}) ToolResult {
	query, _ := args["query"].(string)

	var tags []string
	if tagsRaw, ok := args["tags"].([]interface{}); ok {
		for _, t := range tagsRaw {
			if tag, ok := t.(string); ok {
				tags = append(tags, tag)
			}
		}
	}

	sourceDir := ""
	if currentDirOnly, ok := args["current_dir_only"].(bool); ok && currentDirOnly {
		sourceDir = s.workDir
	}

	limit := 20
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	facts, err := s.store.GetFacts(query, tags, sourceDir, limit)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to recall facts: %v", err))
	}

	if len(facts) == 0 {
		return textResult("No facts found matching your query.")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d fact(s):\n\n", len(facts)))

	for _, f := range facts {
		sb.WriteString(fmt.Sprintf("**#%d** [%s]\n", f.ID, f.CreatedAt.Format("2006-01-02 15:04")))
		if len(f.Tags) > 0 {
			sb.WriteString(fmt.Sprintf("Tags: %s\n", strings.Join(f.Tags, ", ")))
		}
		sb.WriteString(fmt.Sprintf("Dir: %s\n", f.SourceDir))
		sb.WriteString(fmt.Sprintf("%s\n\n", f.Content))
	}

	return textResult(sb.String())
}

func (s *Server) toolGetContext(args map[string]interface{}) ToolResult {
	// Get facts from current directory
	localFacts, err := s.store.GetFacts("", nil, s.workDir, 50)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to get local context: %v", err))
	}

	// Get recent global facts (from all directories)
	globalFacts, err := s.store.GetFacts("", nil, "", 20)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to get global context: %v", err))
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Context for %s\n\n", s.workDir))

	if len(localFacts) > 0 {
		sb.WriteString("## Local Facts (this directory)\n\n")
		for _, f := range localFacts {
			tagStr := ""
			if len(f.Tags) > 0 {
				tagStr = fmt.Sprintf(" [%s]", strings.Join(f.Tags, ", "))
			}
			sb.WriteString(fmt.Sprintf("- %s%s\n", f.Content, tagStr))
		}
		sb.WriteString("\n")
	}

	// Filter global facts to exclude local ones
	var otherFacts []struct {
		content string
		dir     string
		tags    []string
	}
	localIDs := make(map[int64]bool)
	for _, f := range localFacts {
		localIDs[f.ID] = true
	}
	for _, f := range globalFacts {
		if !localIDs[f.ID] {
			otherFacts = append(otherFacts, struct {
				content string
				dir     string
				tags    []string
			}{f.Content, f.SourceDir, f.Tags})
		}
	}

	if len(otherFacts) > 0 {
		sb.WriteString("## Recent Facts (other directories)\n\n")
		for _, f := range otherFacts {
			tagStr := ""
			if len(f.tags) > 0 {
				tagStr = fmt.Sprintf(" [%s]", strings.Join(f.tags, ", "))
			}
			sb.WriteString(fmt.Sprintf("- %s (%s)%s\n", f.content, f.dir, tagStr))
		}
	}

	if len(localFacts) == 0 && len(otherFacts) == 0 {
		sb.WriteString("No stored context yet. Use the `remember` tool to store facts and decisions.\n")
	}

	return textResult(sb.String())
}

func (s *Server) toolListInstances(args map[string]interface{}) ToolResult {
	// Cleanup stale instances first
	s.store.CleanupStaleInstances(5 * time.Minute)

	instances, err := s.store.GetInstances()
	if err != nil {
		return errorResult(fmt.Sprintf("failed to list instances: %v", err))
	}

	if len(instances) == 0 {
		return textResult("No other running instances found.")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d running instance(s):\n\n", len(instances)))

	for _, inst := range instances {
		status := ""
		if inst.ID == s.instanceID {
			status = " (this instance)"
		}
		sb.WriteString(fmt.Sprintf("**%s**%s\n", inst.ID, status))
		sb.WriteString(fmt.Sprintf("  Directory: %s\n", inst.Directory))
		sb.WriteString(fmt.Sprintf("  Started: %s\n", inst.StartedAt.Format("2006-01-02 15:04:05")))
		sb.WriteString(fmt.Sprintf("  Last heartbeat: %s\n\n", inst.LastHeartbeat.Format("15:04:05")))
	}

	return textResult(sb.String())
}

func (s *Server) toolSendMessage(args map[string]interface{}) ToolResult {
	to, ok := args["to"].(string)
	if !ok || to == "" {
		return errorResult("'to' instance ID is required")
	}

	content, ok := args["content"].(string)
	if !ok || content == "" {
		return errorResult("'content' is required")
	}

	// Check if target instance exists
	target, err := s.store.GetInstance(to)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to find instance: %v", err))
	}
	if target == nil {
		return errorResult(fmt.Sprintf("instance '%s' not found", to))
	}

	msg, err := s.store.SendMessage(s.instanceID, to, content)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to send message: %v", err))
	}

	return textResult(fmt.Sprintf("Message #%d sent to %s", msg.ID, to))
}

func (s *Server) toolGetMessages(args map[string]interface{}) ToolResult {
	unreadOnly := true
	if val, ok := args["unread_only"].(bool); ok {
		unreadOnly = val
	}

	messages, err := s.store.GetMessages(s.instanceID, unreadOnly)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to get messages: %v", err))
	}

	if len(messages) == 0 {
		if unreadOnly {
			return textResult("No unread messages.")
		}
		return textResult("No messages.")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d message(s):\n\n", len(messages)))

	for _, m := range messages {
		readStatus := "unread"
		if m.ReadAt != nil {
			readStatus = fmt.Sprintf("read at %s", m.ReadAt.Format("15:04"))
		}
		sb.WriteString(fmt.Sprintf("**#%d** from %s (%s)\n", m.ID, m.FromInstance, readStatus))
		sb.WriteString(fmt.Sprintf("  Time: %s\n", m.CreatedAt.Format("2006-01-02 15:04:05")))
		sb.WriteString(fmt.Sprintf("  %s\n\n", m.Content))

		// Mark as read
		if m.ReadAt == nil {
			s.store.MarkMessageRead(m.ID)
		}
	}

	return textResult(sb.String())
}

// Helpers

func textResult(text string) ToolResult {
	return ToolResult{
		Content: []ContentBlock{{Type: "text", Text: text}},
	}
}

func errorResult(msg string) ToolResult {
	return ToolResult{
		Content: []ContentBlock{{Type: "text", Text: msg}},
		IsError: true,
	}
}

func jsonResult(v interface{}) ToolResult {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errorResult(fmt.Sprintf("failed to marshal result: %v", err))
	}
	return textResult(string(data))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
