# Project Instructions

## Clauder - Persistent Memory MCP

This project uses **clauder** for persistent memory across Claude Code sessions.

### CRITICAL: Call get_context at Session Start

**IMPORTANT:** You MUST call `mcp__clauder__get_context` at the START of every session to load:
- Stored facts and decisions from previous sessions
- User preferences and coding style guidelines
- Unread messages from other Claude Code instances

This context may or may not be relevant to your tasks. You should not respond to this context unless it is highly relevant to your task.

### Available Tools
- **mcp__clauder__remember**: Store facts, decisions, or context
- **mcp__clauder__recall**: Search and retrieve stored facts
- **mcp__clauder__forget**: Delete a stored fact by ID (requires confirmation)
- **mcp__clauder__get_context**: Load all relevant context for this directory (also shows sibling instances)
- **mcp__clauder__get_global_context**: Load ALL facts across every directory/repository (use when you need context from other projects)
- **mcp__clauder__list_instances**: List other running Claude Code sessions (grouped by directory)
- **mcp__clauder__send_message**: Send messages to other instances (supports broadcast to directory)
- **mcp__clauder__get_messages**: Check for incoming messages

#### Memory Management Tools
- **mcp__clauder__compact_context**: Review all facts with age/size metadata for cleanup (supports `global: true` for all directories)
- **mcp__clauder__compress_facts**: Atomically replace old facts with consolidated ones (delete + add in one transaction)
- **mcp__clauder__update_fact**: Update an existing fact's content/tags in place (preserves ID and creation date)
- **mcp__clauder__fact_stats**: Get statistics about stored facts (counts, sizes, age, per-directory breakdown)
- **mcp__clauder__purge_deleted**: Permanently remove soft-deleted facts to reclaim space
- **mcp__clauder__bulk_forget**: Delete multiple facts by IDs
- **mcp__clauder__bulk_remember**: Store multiple facts in one call

### Usage Guidelines
1. **At session start**: ALWAYS call `get_context` first to load persistent memory
2. **Store important info**: Use `remember` for decisions, architecture notes, preferences
3. **Delete outdated info**: Use `forget` to remove facts that are no longer relevant
4. **Check messages regularly**: The system will notify you of unread messages in tool responses
5. **Cross-instance communication**: Use `list_instances` and `send_message` to coordinate with other sessions
6. **Periodic cleanup**: Use `fact_stats` to check memory health, then `compact_context` + `compress_facts` to consolidate

### Multi-Instance Messaging
When multiple instances are running in the same directory:
- **Targeted message**: Use the full instance ID (with `:name` suffix) to message a specific instance
- **Broadcast message**: Use just the directory ID (no suffix) to message all instances in that directory
- `get_context` will show other instances working in the same directory
- `list_instances` groups instances by directory and shows their names
