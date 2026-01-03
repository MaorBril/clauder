# Clauder

An MCP server that provides AI coding tools with persistent memory and multi-instance communication.

Supports: Claude Code, OpenCode, OpenAI Codex CLI, and Google Gemini CLI.

## Features

- **Persistent Memory**: Store facts, decisions, and context across Claude Code sessions
- **Multi-Instance Communication**: Discover and message other Claude Code instances running in different directories
- **Automatic Context Injection**: Load relevant context based on your working directory

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
```

### As MCP Server

Start the server (typically done automatically by Claude Code):

```bash
clauder serve
```

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
