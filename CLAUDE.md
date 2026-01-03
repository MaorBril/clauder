# Project Instructions

## Clauder - Persistent Memory MCP

This project uses **clauder** for persistent memory across Claude Code sessions.

### Available Tools
- **mcp__clauder__remember**: Store facts, decisions, or context
- **mcp__clauder__recall**: Search and retrieve stored facts
- **mcp__clauder__get_context**: Load all relevant context for this directory
- **mcp__clauder__list_instances**: List other running Claude Code sessions
- **mcp__clauder__send_message**: Send messages to other instances
- **mcp__clauder__get_messages**: Check for incoming messages

### Usage Guidelines
1. **At session start**: Call `get_context` to load persistent memory
2. **Store important info**: Use `remember` for decisions, architecture notes, preferences
3. **Periodic message check**: Call `get_messages` periodically to check for messages from other instances
4. **Cross-instance communication**: Use `list_instances` and `send_message` to coordinate with other sessions
