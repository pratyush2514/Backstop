# backstop

backstop is an open-source safety gateway and recovery layer for AI agents that touch PostgreSQL.

It is intentionally local-first and OSS-only: Go, Python, PostgreSQL, SQLite, MinIO/S3-compatible storage, Docker Compose, and Prometheus text metrics.

## Quickstart

Run the OSS end-to-end drill:

```bash
npm run e2e
```

The drill starts Postgres, MinIO, the sync sidecar, and the gateway. It seeds a small database, verifies snapshots, blocks `DROP DATABASE`, approves a `DROP TABLE`, restores the dropped table, and emits a JSON report.

Windows users can also run the compatibility wrapper:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\e2e.ps1
```

Sample MCP requests live in `examples/mcp`.

## Install Without Cloning

macOS/Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/pratyush2514/Backstop/main/scripts/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/pratyush2514/Backstop/main/scripts/install.ps1 | iex
```

Then:

```bash
backstop-oss doctor
backstop-oss up
backstop-oss mcp-config
```

See `docs/INSTALL.md`.

## AI Agent Integration

For MCP-compatible AI tools, use the backstop MCP server so the agent only sees a
safe database tool:

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

In this managed local mode, `npx @backstop/mcp-server` bootstraps the local
Backstop runtime for the user: gateway, sync sidecar, metadata DB, and a local
S3-compatible snapshot store. Users do not need to start Docker or manually
enter a `localhost` gateway URL in the normal MCP path.

For custom Node agents, use `@backstop/client`:

```ts
import { BackstopClient } from "@backstop/client";

const backstop = await BackstopClient.local({
  postgresUrl: process.env.POSTGRES_URL,
  agentId: process.env.BACKSTOP_AGENT_ID ?? "my-ai-agent",
});

await backstop.query("SELECT count(*) FROM users");
```

`BACKSTOP_AGENT_ID` is chosen by the developer/operator. It should be a stable
human-readable caller name like `cursor-local`, `codex-dev-agent`, or
`support-agent-prod`; it is not a secret and should not be random per request.

See `docs/MCP_SERVER.md`, `docs/NODE_SDK.md`, and `docs/AI_AGENT_SETUP.md`.

## Safety Model

backstop has four OSS safety layers:

- Gateway policy: PostgreSQL AST classification, approval, impact analysis, agent quarantine.
- Recovery readiness: latest snapshot, sidecar heartbeat, object existence, RPO checks.
- Bypass detection: `pg_stat_activity` monitoring for direct agent-like DB connections.
- Recovery tooling: table restore, logical backups, PITR preparation, WAL archive/fetch drills.

## Honest Limits

- The gateway only prevents queries routed through it.
- Direct database credentials bypass prevention and degrade backstop to recovery-only.
- Table snapshots are not a replacement for PITR.
- SQLite metadata is local/single-node.
- Schema/database-level recovery requires native backups or PITR.

See `docs/INSTALL.md`, `docs/PUBLISHING.md`, `docs/LAUNCH_READINESS.md`, `docs/RUNBOOKS.md`, and `docs/INCIDENT_RESPONSE.md`.

