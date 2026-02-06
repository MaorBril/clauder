package mcp

import (
	"fmt"
	"strings"
	"time"

	"github.com/maorbril/clauder/internal/store"
	"github.com/maorbril/clauder/internal/telemetry"
)

// Size limits for DoS prevention
const (
	MaxFactSize    = 1 << 20  // 1MB
	MaxMessageSize = 64 << 10 // 64KB
	MaxTagLength   = 256
	MaxTagCount    = 50
)

func (s *Server) toolRemember(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("remember")
	fact, ok := args["fact"].(string)
	if !ok || fact == "" {
		return errorResult("fact is required")
	}

	if len(fact) > MaxFactSize {
		return errorResult(fmt.Sprintf("fact exceeds maximum size of %d bytes", MaxFactSize))
	}

	var tags []string
	if tagsRaw, ok := args["tags"].([]interface{}); ok {
		if len(tagsRaw) > MaxTagCount {
			return errorResult(fmt.Sprintf("too many tags (max %d)", MaxTagCount))
		}
		for _, t := range tagsRaw {
			if tag, ok := t.(string); ok {
				if len(tag) > MaxTagLength {
					return errorResult(fmt.Sprintf("tag exceeds maximum length of %d", MaxTagLength))
				}
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

func (s *Server) toolForget(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("forget")

	// Get and validate the fact ID
	idFloat, ok := args["id"].(float64)
	if !ok {
		return errorResult("'id' is required and must be a number")
	}
	id := int64(idFloat)

	// Check if user has confirmed the deletion
	confirm, _ := args["confirm"].(bool)
	if !confirm {
		// Fetch the fact to show what would be deleted
		fact, err := s.store.GetFactByID(id)
		if err != nil {
			return errorResult(fmt.Sprintf("failed to retrieve fact: %v", err))
		}
		if fact == nil {
			return errorResult(fmt.Sprintf("fact #%d not found", id))
		}

		// Return the fact details and ask for confirmation
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("**Fact #%d to be deleted:**\n\n", fact.ID))
		sb.WriteString(fmt.Sprintf("Content: %s\n", fact.Content))
		if len(fact.Tags) > 0 {
			sb.WriteString(fmt.Sprintf("Tags: %s\n", strings.Join(fact.Tags, ", ")))
		}
		sb.WriteString(fmt.Sprintf("Directory: %s\n", fact.SourceDir))
		sb.WriteString(fmt.Sprintf("Created: %s\n\n", fact.CreatedAt.Format("2006-01-02 15:04")))
		sb.WriteString("⚠️ To confirm deletion, call forget again with confirm=true")
		return textResult(sb.String())
	}

	// Fetch the fact first to verify it exists
	fact, err := s.store.GetFactByID(id)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to retrieve fact: %v", err))
	}
	if fact == nil {
		return errorResult(fmt.Sprintf("fact #%d not found", id))
	}

	// Perform soft delete
	if err := s.store.SoftDeleteFact(id); err != nil {
		return errorResult(fmt.Sprintf("failed to delete fact: %v", err))
	}

	return textResult(fmt.Sprintf("Deleted fact #%d: %s", id, truncate(fact.Content, 100)))
}

func (s *Server) toolRecall(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("recall")
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
	telemetry.TrackMCPTool("get_context")

	// Check for unread messages first
	unreadMessages, _ := s.store.GetMessages(s.instanceID, true)

	// Get facts from current directory
	localFacts, err := s.store.GetFacts("", nil, s.workDir, 50)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to get local context: %v", err))
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Context for %s\n\n", s.workDir))

	// Show unread messages prominently at the top
	if len(unreadMessages) > 0 {
		sb.WriteString(fmt.Sprintf("## ⚠️ Unread Messages (%d)\n\n", len(unreadMessages)))
		for _, m := range unreadMessages {
			sb.WriteString(fmt.Sprintf("**From:** %s @ %s\n", m.FromInstance[:8], m.CreatedAt.Format("15:04")))
			sb.WriteString(fmt.Sprintf("> %s\n\n", m.Content))
		}
		sb.WriteString("Use `get_messages` to mark as read and see full details.\n\n")
	}

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

	if len(localFacts) == 0 {
		sb.WriteString("No stored context yet. Use the `remember` tool to store facts and decisions.\n")
	}

	// Show sibling instances in the same directory
	if s.directoryID != "" {
		siblings, _ := s.store.GetInstancesByDirectory(s.directoryID)
		if len(siblings) > 1 {
			sb.WriteString("\n## Other Instances in This Directory\n\n")
			for _, sib := range siblings {
				if sib.ID == s.instanceID {
					continue
				}
				displayName := sib.Name
				if displayName == "" {
					displayName = "(primary)"
				}
				sb.WriteString(fmt.Sprintf("- **%s** [%s] - last active %s\n",
					sib.ID, displayName, sib.LastHeartbeat.Format("15:04:05")))
			}
			sb.WriteString("\nUse `send_message` to communicate with these instances.\n")
		}
	}

	return textResult(sb.String())
}

func (s *Server) toolGetGlobalContext(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("get_global_context")

	facts, err := s.store.GetAllFacts()
	if err != nil {
		return errorResult(fmt.Sprintf("failed to get global context: %v", err))
	}

	var sb strings.Builder
	sb.WriteString("# Global Context (all directories)\n\n")

	if len(facts) == 0 {
		sb.WriteString("No stored facts across any directory.\n")
		return textResult(sb.String())
	}

	// Group facts by directory
	byDir := make(map[string][]store.Fact)
	var dirOrder []string
	for _, f := range facts {
		if _, exists := byDir[f.SourceDir]; !exists {
			dirOrder = append(dirOrder, f.SourceDir)
		}
		byDir[f.SourceDir] = append(byDir[f.SourceDir], f)
	}

	totalFacts := 0
	for _, dir := range dirOrder {
		dirFacts := byDir[dir]
		totalFacts += len(dirFacts)
		sb.WriteString(fmt.Sprintf("## %s (%d facts)\n\n", dir, len(dirFacts)))
		for _, f := range dirFacts {
			tagStr := ""
			if len(f.Tags) > 0 {
				tagStr = fmt.Sprintf(" [%s]", strings.Join(f.Tags, ", "))
			}
			sb.WriteString(fmt.Sprintf("- **#%d** %s%s\n", f.ID, f.Content, tagStr))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("---\nTotal: %d fact(s) across %d directory(ies)\n", totalFacts, len(dirOrder)))

	return textResult(sb.String())
}

func (s *Server) toolListInstances(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("list_instances")
	// Cleanup stale instances first
	_ = s.store.CleanupStaleInstances(5 * time.Minute)

	instances, err := s.store.GetInstances()
	if err != nil {
		return errorResult(fmt.Sprintf("failed to list instances: %v", err))
	}

	if len(instances) == 0 {
		return textResult("No running instances found.")
	}

	// Group instances by directory
	byDir := make(map[string][]struct {
		id        string
		name      string
		started   time.Time
		heartbeat time.Time
		isCurrent bool
	})

	for _, inst := range instances {
		byDir[inst.Directory] = append(byDir[inst.Directory], struct {
			id        string
			name      string
			started   time.Time
			heartbeat time.Time
			isCurrent bool
		}{
			id:        inst.ID,
			name:      inst.Name,
			started:   inst.StartedAt,
			heartbeat: inst.LastHeartbeat,
			isCurrent: inst.ID == s.instanceID,
		})
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Running Instances (%d total)\n\n", len(instances)))

	for dir, dirInstances := range byDir {
		sb.WriteString(fmt.Sprintf("## %s\n", dir))
		if len(dirInstances) > 1 {
			sb.WriteString(fmt.Sprintf("*(directory ID for broadcast: %s)*\n", s.getDirectoryIDFromInstanceID(dirInstances[0].id)))
		}
		sb.WriteString("\n")

		for _, inst := range dirInstances {
			marker := ""
			if inst.isCurrent {
				marker = " ← **this instance**"
			}

			displayName := inst.name
			if displayName == "" {
				displayName = "(primary)"
			}

			sb.WriteString(fmt.Sprintf("- **%s** [%s]%s\n", inst.id, displayName, marker))
			sb.WriteString(fmt.Sprintf("  Started: %s | Last heartbeat: %s\n",
				inst.started.Format("15:04:05"),
				inst.heartbeat.Format("15:04:05")))
		}
		sb.WriteString("\n")
	}

	return textResult(sb.String())
}

// getDirectoryIDFromInstanceID extracts the directory ID from an instance ID
// Instance ID format: "directoryID" or "directoryID:name"
func (s *Server) getDirectoryIDFromInstanceID(instanceID string) string {
	if idx := strings.Index(instanceID, ":"); idx != -1 {
		return instanceID[:idx]
	}
	return instanceID
}

func (s *Server) toolSendMessage(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("send_message")
	to, ok := args["to"].(string)
	if !ok || to == "" {
		return errorResult("'to' instance ID is required")
	}

	content, ok := args["content"].(string)
	if !ok || content == "" {
		return errorResult("'content' is required")
	}

	if len(content) > MaxMessageSize {
		return errorResult(fmt.Sprintf("message exceeds maximum size of %d bytes", MaxMessageSize))
	}

	broadcast, _ := args["broadcast"].(bool)

	// Check if this looks like a directory ID (no colon) or explicit broadcast
	isDirectoryTarget := broadcast || !strings.Contains(to, ":")

	if isDirectoryTarget {
		// Broadcast to all instances in the directory
		instances, err := s.store.GetInstancesByDirectory(to)
		if err != nil {
			return errorResult(fmt.Sprintf("failed to find instances: %v", err))
		}

		if len(instances) == 0 {
			return errorResult(fmt.Sprintf("no active instances found in directory '%s'", to))
		}

		sent := 0
		for _, inst := range instances {
			if inst.ID == s.instanceID {
				continue // Don't send to self
			}
			if _, err := s.store.SendMessage(s.instanceID, inst.ID, content); err == nil {
				sent++
			}
		}

		if sent == 0 {
			return textResult("No other instances to send to (you may be the only instance in this directory)")
		}

		telemetry.TrackBroadcast(sent)
		return textResult(fmt.Sprintf("Message broadcast to %d instance(s) in directory", sent))
	}

	// Specific instance target
	target, err := s.store.GetInstance(to)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to find instance: %v", err))
	}
	if target == nil {
		// Instance not found - check if there are siblings to suggest
		dirID := s.getDirectoryIDFromInstanceID(to)
		siblings, _ := s.store.GetInstancesByDirectory(dirID)

		if len(siblings) > 0 {
			// Filter out self and build suggestion list
			var names []string
			for _, sib := range siblings {
				if sib.ID != s.instanceID {
					displayName := sib.Name
					if displayName == "" {
						displayName = "(primary)"
					}
					names = append(names, fmt.Sprintf("%s [%s]", sib.ID, displayName))
				}
			}

			if len(names) > 0 {
				return errorResult(fmt.Sprintf(
					"Instance '%s' not found. Other instances in this directory: %s. "+
						"Use directory ID '%s' to broadcast to all.",
					to, strings.Join(names, ", "), dirID))
			}
		}

		return errorResult(fmt.Sprintf("instance '%s' not found", to))
	}

	msg, err := s.store.SendMessage(s.instanceID, to, content)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to send message: %v", err))
	}

	return textResult(fmt.Sprintf("Message #%d sent to %s", msg.ID, to))
}

func (s *Server) toolGetMessages(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("get_messages")
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
			if err := s.store.MarkMessageRead(m.ID); err != nil {
				// Log error but don't fail the entire operation
				sb.WriteString(fmt.Sprintf("  (warning: failed to mark as read: %v)\n", err))
			}
		}
	}

	return textResult(sb.String())
}

func (s *Server) toolCompactContext(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("compact_context")

	facts, err := s.store.GetAllFactsByDir(s.workDir)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to get facts: %v", err))
	}

	if len(facts) == 0 {
		return textResult("No facts found for this directory. Nothing to compact.")
	}

	// Calculate total size
	totalSize := 0
	for _, f := range facts {
		totalSize += len(f.Content)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Context Compaction for %s\n\n", s.workDir))
	sb.WriteString(fmt.Sprintf("Found %d facts (total ~%s).\n\n", len(facts), formatSize(totalSize)))
	sb.WriteString("## Facts to Review\n\n")

	for _, f := range facts {
		tagStr := ""
		if len(f.Tags) > 0 {
			tagStr = fmt.Sprintf(" Tags: [%s]", strings.Join(f.Tags, ", "))
		}
		sb.WriteString(fmt.Sprintf("### #%d [%s]%s\n", f.ID, f.CreatedAt.Format("2006-01-02"), tagStr))
		sb.WriteString(f.Content)
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Instructions\n")
	sb.WriteString("Analyze each fact above. For each one, decide:\n")
	sb.WriteString("- **KEEP**: Still relevant and useful for future sessions\n")
	sb.WriteString("- **DELETE**: Stale, session-specific, or no longer relevant (old PRs, resolved bugs, superseded decisions)\n")
	sb.WriteString("- **MERGE**: Can be combined with other facts into a more concise version\n\n")
	sb.WriteString("Then:\n")
	sb.WriteString("1. Call `bulk_forget` with the IDs of facts to DELETE and MERGE (the originals)\n")
	sb.WriteString("2. Call `bulk_remember` with an array of newly merged/compacted facts\n")
	sb.WriteString("3. Report a summary of what changed\n")

	return textResult(sb.String())
}

func (s *Server) toolBulkForget(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("bulk_forget")

	idsRaw, ok := args["ids"].([]interface{})
	if !ok || len(idsRaw) == 0 {
		return errorResult("'ids' is required and must be a non-empty array of fact IDs")
	}

	ids := make([]int64, 0, len(idsRaw))
	for _, raw := range idsRaw {
		idFloat, ok := raw.(float64)
		if !ok {
			return errorResult("each ID must be a number")
		}
		ids = append(ids, int64(idFloat))
	}

	deleted, err := s.store.BulkSoftDeleteFacts(ids)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to bulk delete facts: %v", err))
	}

	return textResult(fmt.Sprintf("Deleted %d fact(s).", deleted))
}

func (s *Server) toolBulkRemember(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("bulk_remember")

	factsRaw, ok := args["facts"].([]interface{})
	if !ok || len(factsRaw) == 0 {
		return errorResult("'facts' is required and must be a non-empty array")
	}

	bulkFacts := make([]store.BulkFact, 0, len(factsRaw))
	for i, raw := range factsRaw {
		obj, ok := raw.(map[string]interface{})
		if !ok {
			return errorResult(fmt.Sprintf("facts[%d]: each entry must be an object with 'fact' and optional 'tags'", i))
		}

		content, ok := obj["fact"].(string)
		if !ok || content == "" {
			return errorResult(fmt.Sprintf("facts[%d]: 'fact' is required and must be a non-empty string", i))
		}

		if len(content) > MaxFactSize {
			return errorResult(fmt.Sprintf("facts[%d]: exceeds maximum size of %d bytes", i, MaxFactSize))
		}

		var tags []string
		if tagsRaw, ok := obj["tags"].([]interface{}); ok {
			if len(tagsRaw) > MaxTagCount {
				return errorResult(fmt.Sprintf("facts[%d]: too many tags (max %d)", i, MaxTagCount))
			}
			for _, t := range tagsRaw {
				if tag, ok := t.(string); ok {
					if len(tag) > MaxTagLength {
						return errorResult(fmt.Sprintf("facts[%d]: tag exceeds maximum length of %d", i, MaxTagLength))
					}
					tags = append(tags, tag)
				}
			}
		}

		bulkFacts = append(bulkFacts, store.BulkFact{Content: content, Tags: tags})
	}

	stored, err := s.store.BulkAddFacts(bulkFacts, s.workDir)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to store facts: %v", err))
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Stored %d fact(s):\n", len(stored)))
	for _, f := range stored {
		sb.WriteString(fmt.Sprintf("- #%d: %s\n", f.ID, truncate(f.Content, 80)))
	}

	return textResult(sb.String())
}

// Helpers

func formatSize(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	}
	return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
}

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

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// getUnreadCount returns the number of unread messages for this instance
func (s *Server) getUnreadCount() int {
	messages, err := s.store.GetMessages(s.instanceID, true)
	if err != nil {
		return 0
	}
	return len(messages)
}

// appendNotifications adds unread message count to results (except get_messages itself)
func (s *Server) appendNotifications(result ToolResult, skipMessages bool) ToolResult {
	if skipMessages {
		return result
	}
	unreadCount := s.getUnreadCount()
	if unreadCount > 0 && len(result.Content) > 0 {
		notification := fmt.Sprintf("\n\n---\n📬 You have %d unread message(s). Use `get_messages` to read them.", unreadCount)
		result.Content[0].Text += notification
	}
	return result
}
