# @backstop/mcp-server

MCP server for using backstop as the database tool for AI agents.

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

If `BACKSTOP_URL` is omitted, the MCP package starts and manages a local
Backstop runtime automatically for that user. This keeps the real Backstop
gateway/sync/recovery path intact without making the user manually start Docker
or type a localhost gateway URL.

For local PostgreSQL development, `BACKSTOP_POSTGRES_URL` may be a normal
`postgresql://...@localhost:5432/...` URL. Managed local mode automatically
adds `sslmode=disable` for localhost if you did not specify an `sslmode`
yourself.

`BACKSTOP_AGENT_ID` is not issued by backstop. It is a stable name chosen by the
developer or operator so audit logs and approval screens can identify the
caller. Good values are `cursor-local`, `claude-desktop-dev`,
`codex-staging-agent`, or a team/service name.

Approval tools are disabled by default. Enable them only for trusted operator
clients:

```text
BACKSTOP_MCP_MODE=operator
```

Modes:

- `agent`: execute/analyze/status, no approval tools.
- `operator`: approve/deny/audit/alerts, no SQL execution.
- `readonly`: analyze/status/audit/alerts only.
- `admin`: all tools, including emergency pause/resume.

`BACKSTOP_MCP_ENABLE_APPROVAL_TOOLS=true` is kept for compatibility and maps to
`operator` when `BACKSTOP_MCP_MODE` is not set.

