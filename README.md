# Clauder

An MCP server that provides Claude Code with persistent memory and multi-instance communication.

## Features

- **Persistent Memory**: Store facts, decisions, and context across Claude Code sessions
- **Multi-Instance Communication**: Discover and message other Claude Code instances running in different directories
- **Automatic Context Injection**: Load relevant context based on your working directory

## Installation

```bash
go install github.com/maorbril/clauder@latest
```

Or build from source:

```bash
git clone https://github.com/maorbril/clauder.git
cd clauder
make build
```

## Setup

Run the setup command to configure Claude Code to use Clauder:

```bash
clauder setup
```

This will add the MCP server configuration to your Claude Code settings.

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

## License

MIT License - see [LICENSE](LICENSE) for details.
