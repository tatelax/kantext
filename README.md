# Kantext

A TDD-focused Kanban board that tracks tasks alongside their tests, stores everything in a Markdown file, and provides an MCP server for LLM integration.

![images/board.png](images/board.png)

## Features

- **MCP server** for AI assistant integration (Claude, etc.)
- **Markdown storage** - tasks and settings saved in `TASKS.md` (git-friendly)
- **Visual Kanban board** with drag-and-drop
- **Test execution** with configurable test runner (Go, pytest, Jest, etc.)
- **Real-time updates** via WebSocket

## Prerequisites

- [Go 1.21+](https://go.dev/dl/)

## Installation

### macOS / Linux

```bash
# Clone and build
git clone https://github.com/yourusername/kantext.git
cd kantext
make build-all

# Optional: Add to PATH
sudo cp bin/kantext bin/kantext-mcp /usr/local/bin/
```

### Windows

```powershell
# Clone and build
git clone https://github.com/yourusername/kantext.git
cd kantext
go build -o bin/kantext.exe ./cmd
go build -o bin/kantext-mcp.exe ./cmd/mcp

# Add bin/ folder to your PATH or copy executables to a folder in PATH
```

## Quick Start

```bash
# Run from your project directory
cd /path/to/your/project
kantext -port 8080

# Open http://localhost:8080
```

This creates a `TASKS.md` file in your project directory with your tasks and settings.

## Tasks

### Regular Tasks
Simple tasks without tests - just a title and optional acceptance criteria.

### Test-Linked Tasks
Tasks with associated test files. When you create a task with `requires_test: true`, it must have passing tests before it can be marked complete.

- Add tests via the UI or MCP `update_task` tool
- Tests are specified as `file:function` pairs (e.g., `internal/auth/auth_test.go:TestLogin`)
- Run tests from the board or via MCP `run_test`
- Tasks auto-move to "Done" when all tests pass

### Stale Tasks
Tasks are marked stale if not updated within a configurable period (default: 7 days). Configure via the Settings UI or edit the YAML front matter in TASKS.md.

## Configuration

Settings are stored in `TASKS.md` using YAML front matter. This keeps everything in one file that's easy to version control.

Example TASKS.md with settings:
```markdown
---
stale_threshold_days: 14
test_runner:
  command: go test -v -count=1 -run ^{testFunc}$ {testPath}
  pass_string: PASS
  fail_string: FAIL
  no_tests_string: no tests to run
---
# Kantext Tasks

## Inbox

## In Progress

## Done
```

### Command Line Options

```bash
# Run with custom working directory
kantext -workdir /path/to/your/project -port 8080

# Default: uses current directory
kantext
```

### Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `stale_threshold_days` | 7 | Days before a task is marked stale |
| `test_runner.command` | `go test -v -count=1 -run ^{testFunc}$ {testPath}` | Test command template |
| `test_runner.pass_string` | `PASS` | String indicating test passed |
| `test_runner.fail_string` | `FAIL` | String indicating test failed |
| `test_runner.no_tests_string` | `no tests to run` | String when no tests found |

### Test Runner Examples

**Go (default):**
```yaml
test_runner:
  command: go test -v -count=1 -run ^{testFunc}$ {testPath}
```

**Python pytest:**
```yaml
test_runner:
  command: pytest {testPath}::{testFunc} -v
```

**JavaScript Jest:**
```yaml
test_runner:
  command: npx jest {testPath} -t {testFunc}
```

## MCP Server

For AI assistant integration (Claude Code, etc.), add to your MCP config:

```json
{
  "mcpServers": {
    "kantext": {
      "command": "/path/to/kantext-mcp",
      "args": ["-workdir", "/path/to/your/project"]
    }
  }
}
```

**Available tools:**
- `list_tasks` - View all tasks by column
- `create_task` - Create a new task
- `get_task` - Get task details including test output
- `update_task` - Update task properties
- `run_test` - Run a task's tests
- `move_task` - Move task between columns
- `delete_task` - Delete a task

## Build Commands

```bash
make build        # Build web server
make build-mcp    # Build MCP server
make build-all    # Build both
make run          # Run web server
make clean        # Remove binaries
```

## License

MIT
