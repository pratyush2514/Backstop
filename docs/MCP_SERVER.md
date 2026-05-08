# backstop MCP Server

The MCP server is the no-code/config-first path for AI development tools. It
wraps the backstop gateway and exposes safe database tools to MCP-compatible
clients.

```text
AI tool -> backstop MCP server -> backstop gateway -> PostgreSQL
```

Do not give the AI tool raw PostgreSQL credentials. Give it backstop MCP access.

## Local Development

Start the backstop backend:

```powershell
docker compose -f deploy\docker-compose.yml -p backstop_oss_e2e up -d --build
```

Build the MCP server:

```powershell
npm install
npm run build --workspace @backstop/mcp-server
```

Run it over stdio:

```powershell
$env:BACKSTOP_URL = "http://localhost:8080"
$env:BACKSTOP_TOKEN = "dev-token"
$env:BACKSTOP_AGENT_ID = "cursor-local"
npx backstop-mcp
```

For a published release, the intended command is:

```powershell
npx @backstop/mcp-server
```

## MCP Config

```json
{
  "mcpServers": {
    "backstop": {
      "command": "npx",
      "args": ["@backstop/mcp-server"],
      "env": {
        "BACKSTOP_URL": "http://localhost:8080",
        "BACKSTOP_TOKEN": "dev-token",
        "BACKSTOP_AGENT_ID": "cursor-local"
      }
    }
  }
}
```

## Where Does `BACKSTOP_AGENT_ID` Come From?

The user chooses it. It should be a stable name for the MCP client or agent, not
a secret and not a per-request random value.

Examples:

```text
cursor-local
claude-desktop-dev
codex-staging-agent
backend-maintenance-agent
```

If omitted, the MCP server uses a local fallback like `backstop-mcp-<username>`.
That is acceptable for demos, but explicit ids are better for audit and
quarantine.

## Tools

```text
backstop_execute_query
backstop_analyze_query
backstop_list_snapshots
backstop_get_safety_status
backstop_get_pending_approvals
backstop_get_audit_events
backstop_get_alerts
```

MCP mode profiles are controlled with `BACKSTOP_MCP_MODE`:

```bash
BACKSTOP_MCP_MODE=agent     # execute/analyze/status, no approval tools
BACKSTOP_MCP_MODE=operator  # approve/deny/audit/alerts, no SQL execution
BACKSTOP_MCP_MODE=readonly  # analyze/status/audit/alerts, no mutation
BACKSTOP_MCP_MODE=admin     # all tools, including emergency pause/resume
```

These approval tools are intentionally unavailable to autonomous `agent` mode:

```text
backstop_approve_query
backstop_deny_query
```

Enable only for trusted operator clients:

```text
BACKSTOP_MCP_MODE=operator
```

Autonomous agents should normally be able to request risky work, but not approve
their own risky work.

