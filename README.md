# openclawder

**Persistent memory and cross-instance coordination for AI agents.**

A fork of [clauder](https://github.com/MaorBril/clauder) by MaorBril, extended for the [OpenClaw](https://openclaw.ai) ecosystem.

---

## What it does

Your AI agents forget everything when their session ends. openclawder fixes that.

- **Persistent memory** — facts and decisions survive session restarts
- **Cross-instance messaging** — agents talk to each other across terminals and repos
- **OpenClaw integration** — the main OpenClaw agent uses CLI tools; spawned Claude Code sub-agents get MCP tools
- **Web dashboard** — see all running instances, messages, and stored facts at a glance

Works with: Claude Code, OpenClaw, Cursor, Windsurf, OpenCode, Codex CLI, Gemini CLI

---

## Installation

**macOS / Linux:**
```bash
curl -sSL https://raw.githubusercontent.com/orsi-bit/openclawder/main/install.sh | sh
```

**Windows (PowerShell):**
```powershell
irm https://raw.githubusercontent.com/orsi-bit/openclawder/main/install.ps1 | iex
```

**Go:**
```bash
go install github.com/orsi-bit/openclawder@latest
```

---

## Setup

### Claude Code (global)
```bash
openclawder setup
```

### OpenClaw
```bash
openclawder setup --openclaw
```

Configures all agent workspaces (`~/.openclaw/workspace*`):
- `AGENTS.md` — CLI instructions for the main OpenClaw agent
- `CLAUDE.md` — MCP instructions for spawned Claude Code sub-agents
- `~/OPENCLAW.md` — full integration guide

The main OpenClaw agent uses CLI commands directly. Spawned Claude Code sub-agents get MCP tools automatically.

### Other tools
```bash
openclawder setup --cursor      # Cursor editor
openclawder setup --windsurf    # Windsurf
openclawder setup --opencode    # OpenCode
openclawder setup --codex       # OpenAI Codex CLI
openclawder setup --gemini      # Google Gemini CLI
```

---

## CLI

```bash
openclawder status                          # stored facts + running instances
openclawder instances                       # list running sessions
openclawder send <id> "message"             # message another agent
openclawder messages                        # check incoming
openclawder remember "fact"                 # store persistently
openclawder recall "query"                  # search stored facts
openclawder ui                              # web dashboard (localhost:8765)
```

---

## MCP tools (inside Claude Code sessions)

| Tool | Description |
|---|---|
| `mcp__openclawder__remember` | Store a fact or decision |
| `mcp__openclawder__recall` | Search stored facts |
| `mcp__openclawder__get_context` | Load context for current directory |
| `mcp__openclawder__list_instances` | List running sessions |
| `mcp__openclawder__send_message` | Message another instance |
| `mcp__openclawder__get_messages` | Check incoming messages |

---

## OpenClaw integration

openclawder bridges two agent layers in OpenClaw:

**Main agent (OpenClaw):** Uses CLI tools via exec. No MCP needed.
**Sub-agents (Claude Code):** Get MCP tools injected via CLAUDE.md.

Run `openclawder setup --openclaw` once and both layers are configured.

---

## Multiple instances in the same directory

```bash
openclawder wrap --name frontend   # Terminal 1
openclawder wrap --name backend    # Terminal 2
openclawder wrap --name tests      # Terminal 3
```

Each gets a unique ID. Message them individually or broadcast to the whole directory.

---

## Web dashboard

```bash
openclawder ui
```

Opens `http://localhost:8765` — live view of all instances, message history, and stored facts.

---

## Data

All data stored locally in `~/.clauder/` (SQLite). No cloud sync.

To opt out of anonymous usage telemetry:
```bash
export OPENCLAWDER_NO_TELEMETRY=1
# or
export DO_NOT_TRACK=1
```

---

## Thanks

openclawder is a fork of **[clauder](https://github.com/MaorBril/clauder)** by [Maor Bril](https://github.com/MaorBril).

Maor built something genuinely useful — persistent memory and cross-instance coordination for AI coding agents — and made it MIT licensed. We forked it to extend it for the OpenClaw ecosystem, but the foundation is entirely his work. If you find openclawder useful, go star the original. The man deserves it. ⭐

---

MIT License · Fork of [MaorBril/clauder](https://github.com/MaorBril/clauder)
