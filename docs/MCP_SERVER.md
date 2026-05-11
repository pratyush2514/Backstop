# backstop MCP Server

The MCP server is the no-code/config-first path for AI development tools. It
wraps the backstop gateway and exposes safe database tools to MCP-compatible
clients.

```text
AI tool -> backstop MCP server -> backstop gateway -> PostgreSQL
```

Do not give the AI tool raw PostgreSQL credentials. Give it backstop MCP access.

## Managed Local Mode

The default MCP happy path is managed local mode. The user only provides a
PostgreSQL URL and an agent identity:

```powershell
$env:BACKSTOP_POSTGRES_URL = "postgresql://postgres:password@localhost:5432/app"
$env:BACKSTOP_AGENT_ID = "cursor-local"
npx @backstop/mcp-server
```

On first run, the MCP package bootstraps the local Backstop runtime and reuses
it on later runs. That includes the gateway, sync sidecar, SQLite metadata, and
local S3-compatible snapshot storage used by the real recovery path.

## Existing Runtime Mode

If a team already runs Backstop elsewhere, MCP can attach to it explicitly:

```powershell
$env:BACKSTOP_URL = "https://backstop.internal.example"
$env:BACKSTOP_TOKEN = "operator-or-agent-token"
$env:BACKSTOP_AGENT_ID = "cursor-local"
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
        "BACKSTOP_POSTGRES_URL": "postgresql://postgres:password@localhost:5432/app",
        "BACKSTOP_AGENT_ID": "cursor-local"
      }
    }
  }
}
```

For an already-running Backstop deployment, replace `BACKSTOP_POSTGRES_URL` with
`BACKSTOP_URL` and `BACKSTOP_TOKEN`.

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
backstop_prepare_restore_snapshot
backstop_get_safety_status
backstop_get_pending_approvals
backstop_get_audit_events
backstop_get_alerts
```

MCP mode profiles are controlled with `BACKSTOP_MCP_MODE`:

```bash
BACKSTOP_MCP_MODE=agent     # execute/analyze/status, no approval tools
BACKSTOP_MCP_MODE=operator  # approve/deny/audit/alerts/restore plans, no SQL execution
BACKSTOP_MCP_MODE=readonly  # analyze/status/audit/alerts, no mutation
BACKSTOP_MCP_MODE=admin     # all tools, including emergency pause/resume
```

These approval tools are intentionally unavailable to autonomous `agent` mode:

```text
backstop_approve_query
backstop_deny_query
backstop_prepare_restore_snapshot
```

Enable only for trusted operator clients:

```text
BACKSTOP_MCP_MODE=operator
```

Autonomous agents should normally be able to request risky work, but not approve
their own risky work or receive restore instructions. Restore plans are
secret-safe: they use `BACKSTOP_RESTORE_DB` and never return the raw PostgreSQL
connection string to the MCP client.

