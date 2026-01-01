# Kantext Remote Server Architecture Evaluation

## Executive Summary

This document evaluates how Kantext could be modified to run on a remote server while maintaining seamless integration with users' local development environments. The primary challenge is bridging the gap between a remote-hosted web UI and MCP server with the local filesystem where tests run and project context exists.

## Current Architecture (Local-Only)

```
┌─────────────────────────────────────────────────────────────┐
│                     User's Machine                           │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────────┐  │
│  │   Browser   │◄──►│ Web Server  │◄──►│    TASKS.md     │  │
│  │  (Frontend) │    │  (Go:8081)  │    │  (Local File)   │  │
│  └─────────────┘    └──────┬──────┘    └────────┬────────┘  │
│                            │                     │           │
│                      ┌─────▼─────┐         ┌─────▼─────┐    │
│                      │WebSocket  │         │Test Runner│    │
│                      │   Hub     │         │ (go test) │    │
│                      └───────────┘         └───────────┘    │
│                                                              │
│  ┌─────────────┐    ┌─────────────┐                         │
│  │   Claude    │◄──►│ MCP Server  │──────► TASKS.md         │
│  │   (Client)  │    │  (stdin/out)│                         │
│  └─────────────┘    └─────────────┘                         │
└─────────────────────────────────────────────────────────────┘
```

### Key Local Dependencies

1. **TASKS.md** - Primary data store in project directory
2. **Test Runner** - Executes `go test`, `pytest`, etc. locally
3. **Git Blame** - Reads `.git` for author tracking
4. **File Watcher** - Monitors filesystem for external changes
5. **MCP Server** - Runs as local process, communicates via stdio

---

## Proposed Remote Architecture

### Option A: Hybrid Model (Recommended)

**Concept:** Remote web server + Local sync agent

```
┌─────────────────────────────────────────────────────────────┐
│                    Remote Server (Cloud)                     │
│  ┌─────────────────────────────────────────────────────────┐│
│  │                   Kantext Cloud                          ││
│  │  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐  ││
│  │  │   Web UI    │◄──►│  API Server │◄──►│  Database   │  ││
│  │  │  (Static)   │    │   (Go/WS)   │    │(PostgreSQL) │  ││
│  │  └─────────────┘    └──────┬──────┘    └─────────────┘  ││
│  │                            │                             ││
│  │                      ┌─────▼─────┐    ┌─────────────┐   ││
│  │                      │ WebSocket │    │ MCP Bridge  │   ││
│  │                      │   Hub     │    │   Server    │   ││
│  │                      └───────────┘    └──────┬──────┘   ││
│  └──────────────────────────────────────────────┼──────────┘│
└─────────────────────────────────────────────────┼───────────┘
                                                  │
                          ◄── Secure Tunnel ──►   │
                                                  │
┌─────────────────────────────────────────────────┼───────────┐
│                     User's Machine              ▼           │
│  ┌─────────────────────────────────────────────────────────┐│
│  │                   Kantext Agent                          ││
│  │  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐  ││
│  │  │   Syncer    │◄──►│ Test Runner │    │ File Watch  │  ││
│  │  │  (WebSocket)│    │  (local)    │    │  (fsnotify) │  ││
│  │  └──────┬──────┘    └─────────────┘    └──────┬──────┘  ││
│  │         │                                      │         ││
│  │         ▼                                      ▼         ││
│  │    ┌─────────────────────────────────────────────┐      ││
│  │    │                  TASKS.md                    │      ││
│  │    │               (Local Project)                │      ││
│  │    └─────────────────────────────────────────────┘      ││
│  └──────────────────────────────────────────────────────────┘│
│                                                              │
│  ┌─────────────┐         ┌─────────────────────────────────┐│
│  │   Claude    │◄───────►│     MCP Proxy (local)           ││
│  │  (Desktop)  │  stdio  │  Forwards to Kantext Cloud      ││
│  └─────────────┘         └─────────────────────────────────┘│
└─────────────────────────────────────────────────────────────┘
```

#### Components

**1. Kantext Cloud (Remote)**
- Hosted web application (Go server)
- PostgreSQL database for task storage
- User authentication (OAuth, API keys)
- Multi-project/workspace support
- WebSocket hub for real-time updates
- MCP bridge for remote tool calls

**2. Kantext Agent (Local)**
- Lightweight Go binary (~5MB)
- Runs as background daemon
- Bi-directional sync with cloud
- Executes tests locally
- Monitors TASKS.md for external changes
- Reports results to cloud

**3. MCP Proxy (Local)**
- Thin wrapper that accepts MCP protocol
- Forwards requests to cloud API
- Returns responses to Claude

---

### Option B: Browser-Based Local Access

**Concept:** Remote UI connects directly to local server via localhost

```
┌───────────────────────────────────────────────────────────┐
│                    Remote Server                           │
│  ┌─────────────────────────────────────────────────────┐  │
│  │              Kantext Web App (Static)                │  │
│  │   - Serves HTML/CSS/JS only                         │  │
│  │   - No backend API                                  │  │
│  │   - JavaScript connects to localhost:8081           │  │
│  └─────────────────────────────────────────────────────┘  │
└───────────────────────────────────────────────────────────┘
                              │
                         Loads JS/CSS
                              │
                              ▼
┌───────────────────────────────────────────────────────────┐
│                      User's Browser                        │
│  ┌─────────────────────────────────────────────────────┐  │
│  │              Kantext Frontend (JS)                   │  │
│  │   - API calls to http://localhost:8081              │  │
│  │   - WebSocket to ws://localhost:8081/ws             │  │
│  └────────────────────────┬────────────────────────────┘  │
└───────────────────────────┼───────────────────────────────┘
                            │
                    Local network calls
                            │
                            ▼
┌───────────────────────────────────────────────────────────┐
│                     User's Machine                         │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐   │
│  │ Web Server  │◄──►│  TASKS.md   │◄──►│ Test Runner │   │
│  │ (localhost) │    │   (local)   │    │   (local)   │   │
│  └─────────────┘    └─────────────┘    └─────────────┘   │
└───────────────────────────────────────────────────────────┘
```

**Pros:**
- Simple architecture
- No sync complexity
- Full local control

**Cons:**
- Requires local server always running
- CORS configuration needed
- No multi-device access
- MCP still requires local process

---

### Option C: Full Cloud with Remote Execution

**Concept:** Everything in cloud, tests run in containers

```
┌─────────────────────────────────────────────────────────────┐
│                    Kantext Cloud Platform                    │
│  ┌────────────────────────────────────────────────────────┐ │
│  │                     Control Plane                       │ │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌───────────┐ │ │
│  │  │   Web   │  │   API   │  │Database │  │MCP Server │ │ │
│  │  │   UI    │  │ Server  │  │(Postgres)│  │ (Remote)  │ │ │
│  │  └─────────┘  └────┬────┘  └─────────┘  └─────┬─────┘ │ │
│  └────────────────────┼──────────────────────────┼────────┘ │
│                       │                          │          │
│  ┌────────────────────▼──────────────────────────▼────────┐ │
│  │                   Execution Plane                       │ │
│  │  ┌─────────────────────────────────────────────────┐   │ │
│  │  │              User Project Container              │   │ │
│  │  │  ┌─────────┐  ┌─────────┐  ┌─────────────────┐  │   │ │
│  │  │  │  Clone  │  │  Build  │  │   Test Runner   │  │   │ │
│  │  │  │  Repo   │──►│  Code   │──►│   (isolated)    │  │   │ │
│  │  │  └─────────┘  └─────────┘  └─────────────────┘  │   │ │
│  │  └─────────────────────────────────────────────────┘   │ │
│  └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

**Pros:**
- Fully hosted, zero local setup
- Multi-device access
- Centralized management

**Cons:**
- Complex infrastructure (Kubernetes, Docker)
- Expensive to run
- Repository access/secrets management
- Latency for test execution
- May not work for all test types (hardware, local services)

---

## Recommended Approach: Hybrid Model (Option A)

### Why Hybrid?

1. **Test execution must be local** - Many tests require local environment (databases, services, hardware)
2. **TASKS.md should sync** - Users want local file for git commits and IDE viewing
3. **MCP works best locally** - Claude desktop app expects local MCP servers
4. **Web UI can be remote** - Provides seamless multi-device access

### Implementation Phases

#### Phase 1: Cloud Infrastructure (Week 1-2)

**1.1 Database Schema**
```sql
CREATE TABLE workspaces (
    id UUID PRIMARY KEY,
    name VARCHAR(255),
    owner_id UUID REFERENCES users(id),
    created_at TIMESTAMP
);

CREATE TABLE tasks (
    id UUID PRIMARY KEY,
    workspace_id UUID REFERENCES workspaces(id),
    title TEXT,
    acceptance_criteria TEXT,
    priority VARCHAR(20),
    column_slug VARCHAR(50),
    requires_test BOOLEAN,
    test_status VARCHAR(20),
    tests_passed INT,
    tests_total INT,
    last_output TEXT,
    created_at TIMESTAMP,
    created_by VARCHAR(255),
    updated_at TIMESTAMP,
    updated_by VARCHAR(255),
    position INT,
    version INT  -- For optimistic locking
);

CREATE TABLE columns (
    id UUID PRIMARY KEY,
    workspace_id UUID REFERENCES workspaces(id),
    slug VARCHAR(50),
    name VARCHAR(255),
    position INT
);

CREATE TABLE test_specs (
    id UUID PRIMARY KEY,
    task_id UUID REFERENCES tasks(id),
    file_path TEXT,
    function_name VARCHAR(255)
);

CREATE TABLE agents (
    id UUID PRIMARY KEY,
    workspace_id UUID REFERENCES workspaces(id),
    name VARCHAR(255),
    token_hash VARCHAR(255),
    last_seen TIMESTAMP,
    status VARCHAR(20)
);
```

**1.2 Authentication**
```go
// OAuth providers (Google, GitHub)
// API key generation for agents
// JWT tokens for web sessions
```

**1.3 API Endpoints**
```
POST   /api/v1/auth/login
POST   /api/v1/auth/register
GET    /api/v1/workspaces
POST   /api/v1/workspaces
GET    /api/v1/workspaces/:id/tasks
POST   /api/v1/workspaces/:id/tasks
PUT    /api/v1/workspaces/:id/tasks/:taskId
DELETE /api/v1/workspaces/:id/tasks/:taskId
POST   /api/v1/workspaces/:id/tasks/:taskId/test-results
WS     /api/v1/workspaces/:id/ws
POST   /api/v1/agents/register
POST   /api/v1/agents/heartbeat
```

#### Phase 2: Kantext Agent (Week 2-3)

**2.1 Agent Binary**
```go
// cmd/agent/main.go
package main

type Agent struct {
    workspaceID string
    token       string
    cloudURL    string
    workDir     string

    taskStore   *TaskStore      // Local TASKS.md
    testRunner  *TestRunner
    fileWatcher *FileWatcher
    syncer      *CloudSyncer
}

func (a *Agent) Run() {
    // Connect to cloud
    // Start bi-directional sync
    // Watch for local changes
    // Execute test requests from cloud
}
```

**2.2 Sync Protocol**
```go
type SyncMessage struct {
    Type      string    `json:"type"`
    Timestamp time.Time `json:"timestamp"`
    Version   int       `json:"version"`
    Data      any       `json:"data"`
}

// Message types:
// - task_created
// - task_updated
// - task_deleted
// - test_requested
// - test_result
// - full_sync
```

**2.3 Conflict Resolution**
```go
// Last-write-wins with version tracking
// Server maintains authoritative version number
// Agent detects conflicts via version mismatch
// UI shows conflict resolution dialog when needed
```

**2.4 Installation Experience**
```bash
# One-line install
curl -fsSL https://kantext.io/install.sh | sh

# Or via npm/homebrew
npm install -g kantext-agent
brew install kantext

# Setup (interactive)
kantext init
# Opens browser for authentication
# Prompts for workspace selection
# Creates .kantext/config.yaml

# Run
kantext agent start
# Or as service
kantext agent install-service
```

#### Phase 3: MCP Integration (Week 3-4)

**3.1 MCP Proxy**
```go
// cmd/mcp-proxy/main.go
// Accepts MCP protocol on stdin/stdout
// Forwards to cloud API
// Handles authentication transparently

func main() {
    config := LoadConfig()
    proxy := NewMCPProxy(config.CloudURL, config.Token)
    proxy.ServeStdio()
}
```

**3.2 Claude Desktop Configuration**
```json
{
  "mcpServers": {
    "kantext": {
      "command": "kantext",
      "args": ["mcp"],
      "env": {
        "KANTEXT_WORKSPACE": "my-project"
      }
    }
  }
}
```

**3.3 Remote MCP (Optional)**
```
For web-based Claude (claude.ai):
- Kantext Cloud exposes MCP over SSE
- User adds Kantext as connected service
- Full MCP tools available in web interface
```

#### Phase 4: Web Frontend Updates (Week 4)

**4.1 Authentication UI**
- Login/signup pages
- Workspace selection
- Agent status dashboard
- API key management

**4.2 Multi-workspace Support**
- Workspace switcher
- Shared workspaces
- Role-based permissions (owner, editor, viewer)

**4.3 Agent Connection Status**
```javascript
// Show agent connection state
// Display sync status
// Alert when agent offline
// Show test execution in real-time
```

---

## Configuration Experience

### Goal: Minimal User Configuration

**Step 1: Sign Up (30 seconds)**
```
1. Visit kantext.io
2. Click "Sign in with GitHub"
3. Authorize Kantext
4. Done - workspace created automatically
```

**Step 2: Install Agent (60 seconds)**
```bash
# macOS/Linux
curl -fsSL https://kantext.io/install.sh | sh

# Windows
winget install kantext

# Or universal
npx kantext-agent
```

**Step 3: Connect Workspace (30 seconds)**
```bash
cd /path/to/my/project
kantext init

# Opens browser: "Connect this project to Kantext?"
# Click "Connect"
# Agent starts automatically
```

**Step 4: Add to Claude (30 seconds)**
```bash
kantext claude-setup

# Automatically adds to Claude Desktop config
# Or prints MCP config for manual setup
```

**Total Time: ~3 minutes**

---

## Technical Challenges & Solutions

### Challenge 1: Test Execution Security

**Problem:** Agent executes arbitrary commands from cloud

**Solutions:**
- Agent only runs pre-configured test commands
- Test command templates stored locally, not from cloud
- User explicitly approves test configurations
- Sandboxing via containers (optional)

### Challenge 2: Network Reliability

**Problem:** What happens when cloud is unreachable?

**Solutions:**
- Local TASKS.md remains source of truth during offline
- Agent queues changes for sync when reconnected
- Web UI shows "offline mode" with local fallback
- MCP proxy falls back to local-only mode

### Challenge 3: Data Ownership

**Problem:** Users want their data in their repo

**Solutions:**
- TASKS.md always synced to local file
- Can export all data as markdown
- Delete account removes all cloud data
- Self-hosted option for enterprises

### Challenge 4: Real-time Sync Conflicts

**Problem:** Two users edit same task simultaneously

**Solutions:**
- Optimistic locking with version numbers
- Last-write-wins with conflict notification
- UI shows "Task was updated by X" warning
- Merge changes where possible (non-conflicting fields)

### Challenge 5: Multi-Project Support

**Problem:** User has many projects, each with TASKS.md

**Solutions:**
- Each project directory = separate workspace
- Agent can watch multiple directories
- Web UI shows workspace switcher
- MCP proxy includes workspace in requests

---

## Security Considerations

### Authentication
- OAuth 2.0 (GitHub, Google, email magic links)
- API tokens for agents (revocable, time-limited)
- JWT for web sessions (short-lived, refresh tokens)

### Authorization
- Workspace-level permissions
- RBAC: Owner, Editor, Viewer
- Audit logs for all changes

### Data Protection
- TLS everywhere (HTTPS, WSS)
- Encryption at rest (database)
- Agent tokens stored securely (keychain/credential manager)
- No plaintext secrets in logs

### Agent Security
- Token rotation
- IP allowlisting (optional)
- Rate limiting
- Anomaly detection

---

## Cost Estimation (Cloud Hosting)

### Small Scale (< 100 users)
- Fly.io or Railway: $20-50/month
- PostgreSQL: $15-25/month
- **Total: ~$50-75/month**

### Medium Scale (100-1000 users)
- AWS/GCP managed: $200-500/month
- RDS PostgreSQL: $50-100/month
- CDN for static assets: $20/month
- **Total: ~$300-600/month**

### Large Scale (1000+ users)
- Kubernetes cluster: $500-1500/month
- Managed database: $200-500/month
- CDN + DDoS protection: $100/month
- **Total: ~$1000-2000/month**

---

## Migration Path from Local

### Existing Users

```bash
# Upgrade to new version
kantext upgrade

# Connect existing project
kantext cloud connect

# Imports existing TASKS.md to cloud
# Agent continues running locally
# No data loss, seamless transition
```

### New Users
- Default experience is cloud-connected
- Local-only mode still available for air-gapped environments

---

## Alternative: Simpler Approach with Tunneling

For fastest time-to-market, consider **ngrok/Cloudflare Tunnel** approach:

```bash
# User runs local server as before
kantext serve -port 8081

# Start tunnel to expose locally
kantext tunnel start
# Your Kantext is available at: https://abc123.kantext.io
```

**Pros:**
- Minimal backend changes
- User keeps full local control
- Quick to implement

**Cons:**
- Requires local server always running
- Each user needs unique subdomain
- Tunnel reliability varies
- Less seamless than true cloud

---

## Recommendation Summary

| Approach | Complexity | User Experience | Time to Build |
|----------|------------|-----------------|---------------|
| **A: Hybrid (Recommended)** | High | Excellent | 4-6 weeks |
| B: Browser + Localhost | Low | Good | 1 week |
| C: Full Cloud | Very High | Good | 8-12 weeks |
| D: Tunneling | Medium | Fair | 2 weeks |

**Recommendation:** Start with **Option A (Hybrid)** but implement in phases:

1. **Week 1-2:** Cloud backend with auth, database, API
2. **Week 3:** Basic agent with sync
3. **Week 4:** MCP proxy integration
4. **Week 5-6:** Polish, testing, documentation

This approach provides the best user experience while keeping tests and project files local where they belong.

---

## Next Steps

1. **Validate Architecture** - Review with potential users
2. **Define MVP Scope** - What's the minimum for launch?
3. **Choose Hosting** - Fly.io recommended for starting
4. **Design Database Schema** - Finalize data model
5. **Build Cloud Backend** - API server first
6. **Develop Agent** - Start with basic sync
7. **Update Frontend** - Add auth, workspace switching
8. **Test End-to-End** - Full workflow validation
9. **Documentation** - Setup guides, troubleshooting
10. **Beta Launch** - Limited user testing
