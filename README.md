# Clauder

**Make your AI coding agents talk to each other.**

You're working on a frontend. Claude is helping. Meanwhile, another Claude is working on the backend in a different terminal. They have no idea the other exists.

With clauder, they can communicate. The frontend Claude asks the backend Claude: "What's the API contract?" and gets an answer. No copy-pasting. No context switching. Just agents collaborating.

---

## The Problem

Modern development often means multiple services, multiple repos, multiple terminals. You might have:
- Claude helping with the frontend in one directory
- Claude working on the API in another
- Claude setting up infrastructure in a third

But they're completely isolated. You become the messenger, copying context between sessions. "The backend team decided to use REST" — except there is no backend team, just another Claude instance that can't talk to this one.

## The Solution

Clauder connects your Claude instances:

```bash
# Install
curl -sSL https://raw.githubusercontent.com/MaorBril/clauder/main/install.sh | sh

# Set up with Claude Code
clauder setup

# Now your Claude instances can find and message each other
```

From any Claude session:
- **Discover**: "List other Claude instances" → sees what's running where
- **Communicate**: "Ask the backend instance about the API schema" → sends a message, gets a response
- **Coordinate**: Agents working on related services stay in sync

## What Else?

- **Persistent Memory**: Facts and decisions survive session restarts
- **Web Dashboard**: See all running instances, messages, and stored context
- **Works Everywhere**: Claude Code, Cursor, Windsurf, OpenCode, Codex CLI, Gemini CLI

## Installation

### Quick Install (Recommended)

**macOS / Linux:**
```bash
curl -sSL https://raw.githubusercontent.com/MaorBril/clauder/main/install.sh | sh
```

**Windows (PowerShell):**
```powershell
irm https://raw.githubusercontent.com/MaorBril/clauder/main/install.ps1 | iex
```

Installs to `~/.local/bin` (Unix) or `%LOCALAPPDATA%\clauder` (Windows).

### With Go

```bash
go install github.com/MaorBril/clauder@latest
```

### Manual Download

Download the binary for your platform from [Releases](https://github.com/MaorBril/clauder/releases):

| Platform | Binary |
|----------|--------|
| macOS (Apple Silicon) | `clauder-darwin-arm64` |
| macOS (Intel) | `clauder-darwin-amd64` |
| Linux (x64) | `clauder-linux-amd64` |
| Linux (ARM64) | `clauder-linux-arm64` |
| Windows (x64) | `clauder-windows-amd64.exe` |

### Build from Source

```bash
git clone https://github.com/MaorBril/clauder.git
cd clauder
make build
```

## Setup

### Claude Code

Run the setup command to configure Claude Code to use Clauder:

```bash
clauder setup
```

This will add the MCP server configuration to your Claude Code settings.

### Cursor / Windsurf

For [Cursor](https://cursor.sh) or [Windsurf](https://codeium.com/windsurf):

```bash
clauder setup --cursor
# or
clauder setup --windsurf
```

### OpenCode

Clauder also works with [OpenCode](https://opencode.ai). Run:

```bash
clauder setup --opencode
```

This creates an `opencode.json` in your project directory with the MCP configuration.

### OpenAI Codex CLI

For [Codex CLI](https://github.com/openai/codex):

```bash
clauder setup --codex
```

This adds clauder to `~/.codex/config.toml`.

### Google Gemini CLI

For [Gemini CLI](https://github.com/google-gemini/gemini-cli):

```bash
clauder setup --gemini
```

This adds clauder to `~/.gemini/settings.json`.

## Usage

### CLI Commands

```bash
# Store a fact
clauder remember "Project uses SQLite for persistence"

# Recall facts
clauder recall "database"

# List running instances
clauder instances

# Send a message to another instance
clauder send <instance-id> "Hello from another directory"

# Check messages
clauder messages

# View status
clauder status

# Launch web dashboard
clauder ui
```

### Web Dashboard

Launch an interactive web dashboard to monitor all clauder activity:

```bash
clauder ui
```

This opens a browser to `http://localhost:8765` with:
- **Instances**: View all running Claude Code sessions with status (active/idle/leader)
- **Messages**: Full message history with filtering by read/unread, sender, recipient
- **Facts**: Browse stored facts with filtering by tags, source directory, local/global

Options:
- `-p, --port`: Port to run on (default: 8765)
- `-r, --refresh`: Auto-refresh interval in seconds (default: 3)
- `--no-browser`: Don't automatically open browser

Keyboard shortcuts: `1`/`2`/`3` to switch views, `R` to refresh, `Esc` to close modals.

### As MCP Server

Start the server (typically done automatically by Claude Code):

```bash
clauder serve
```

### Multiple Instances in Same Directory

Running multiple Claude sessions in the same project? Use the `--name` flag to differentiate them:

```bash
# Terminal 1 - working on frontend
clauder wrap --name frontend

# Terminal 2 - working on backend
clauder wrap --name backend

# Terminal 3 - running tests
clauder wrap --name tests
```

Each named instance gets a unique ID and can be messaged individually:
- **Targeted message**: Send to a specific instance by its full ID (includes `:name`)
- **Broadcast message**: Send to all instances in a directory by using the directory ID

Without `--name`, the second instance in the same directory automatically gets a unique suffix to avoid conflicts.

### MCP Tools

When used as an MCP server, clauder provides these tools:

| Tool | Description |
|------|-------------|
| `remember` | Store a fact, decision, or piece of context |
| `recall` | Search and retrieve stored facts |
| `forget` | Delete a stored fact (with confirmation) |
| `get_context` | Load all relevant context for the current directory |
| `list_instances` | List other running Claude Code sessions (grouped by directory) |
| `send_message` | Send a message to a specific instance or broadcast to all in a directory |
| `get_messages` | Check for incoming messages |

## Data Storage

All data is stored in `~/.clauder/` directory using SQLite.

## Telemetry

Clauder collects anonymous usage data to help improve the tool. This includes:
- OS and architecture
- Commands and features used (not content)
- Version information

**No personal data, file contents, or facts are ever collected.**

To opt out, set one of these environment variables:
```bash
export CLAUDER_NO_TELEMETRY=1
# or
export DO_NOT_TRACK=1
```

## License

MIT License - see [LICENSE](LICENSE) for details.
