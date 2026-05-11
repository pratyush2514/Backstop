<div align="center">

<img src="https://raw.githubusercontent.com/pratyush2514/Backstop/main/frontend/public/logo.png" alt="Backstop" width="72" height="72" />

# Backstop

**Open-source safety gateway and recovery layer for AI agents that touch PostgreSQL.**

Stop the next `DROP TABLE`. Approve before it runs. Restore supported tables from a verified recovery point if it gets through.

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-red.svg)](./LICENSE)
[![npm: @backstop/client](https://img.shields.io/npm/v/@backstop/client?label=%40backstop%2Fclient&color=cc0000)](https://www.npmjs.com/package/@backstop/client)
[![npm: @backstop/mcp-server](https://img.shields.io/npm/v/@backstop/mcp-server?label=%40backstop%2Fmcp-server&color=cc0000)](https://www.npmjs.com/package/@backstop/mcp-server)
[![Go](https://img.shields.io/badge/gateway-Go_1.22+-00ADD8?logo=go&logoColor=white)](./gateway)
[![Python](https://img.shields.io/badge/sdk-Python_3.11+-3776AB?logo=python&logoColor=white)](./sdk/python)

</div>

---

## What is Backstop?

Backstop sits between your AI agent and your PostgreSQL database. Queries routed through Backstop are intercepted, classified by risk level, checked against recovery readiness when needed, and held for human approval before risky writes touch production.

If one slips through, the sidecar has already snapshotted the table. You run the guided recovery CLI, restore into a recovered table, validate it, then copy back what you need.

```
AI Agent  ──►  Backstop Gateway  ──►  PostgreSQL
                     │
              ┌──────┴──────┐
              │  classify   │  SAFE → pass through
              │  snapshot   │  HIGH → require approval
              │  recover    │  CRITICAL → block or approve + restore point
              └─────────────┘
```

**Why it exists:** On April 27, 2026, a Cursor/Opus coding session deleted an entire production database in nine seconds. Backstop is the guardrail for routed PostgreSQL access: it blocks database-level destruction, gates table-level destruction on recovery readiness, and gives operators a practical restore path.

---

## How It Works

```
                     ┌────────────────────────────────────────────────────┐
                     │                 backstop runtime                   │
                     │                                                    │
  ┌──────────┐       │   ┌──────────────┐    ┌─────────────────────┐    │
  │ AI Agent │──SQL──┼──►│   Gateway    │    │   Sync Sidecar      │    │
  │ (any)    │       │   │              │    │                     │    │
  └──────────┘       │   │ 1. AST parse │    │ • Snapshots every   │    │
                     │   │ 2. Classify  │◄───│   60 s (Parquet/S3) │    │
  ┌──────────┐       │   │ 3. Policy    │    │ • Heartbeat to GW   │    │
  │ Operator │◄──────┼───│ 4. Approve?  │    │ • Bypass detection  │    │
  │ (human)  │──────►│   │ 5. Execute   │    │ • Prometheus metrics│    │
  └──────────┘       │   └──────┬───────┘    └─────────────────────┘    │
                     │          │                                        │
                     │          ▼                                        │
                     │   ┌──────────────┐    ┌─────────────────────┐    │
                     │   │  PostgreSQL  │    │   MinIO / S3        │    │
                     │   └──────────────┘    │  (snapshot store)   │    │
                     │                       └─────────────────────┘    │
                     └────────────────────────────────────────────────────┘
```

### The four safety layers

| Layer | What it does |
|---|---|
| **Gateway policy** | AST-based SQL classification (SAFE → IMPACT_CRITICAL), configurable rules, agent quarantine |
| **Recovery readiness** | Blocks CRITICAL writes unless a verified, fresh snapshot exists first |
| **Bypass detection** | Monitors `pg_stat_activity` — alerts if an agent connects directly, skipping the gateway |
| **Recovery tooling** | Table restore, logical backup, PITR preparation, WAL archive/fetch drills |

---

## Quick Start

**Prerequisites:** Docker, Node.js 18+, Go 1.22+

### 1 — See the shortest local demo path

```bash
git clone https://github.com/pratyush2514/Backstop.git
cd Backstop
npm install
npm run demo
```

The demo command prints the local happy path, health URLs, and the exact E2E commands to prove the stack.

### 2 — Run the end-to-end drill

```bash
npm run e2e
```

The drill starts Postgres, MinIO, the sync sidecar, and the gateway. It seeds a small database, verifies snapshots, blocks `DROP DATABASE`, approves a `DROP TABLE`, restores the dropped table, and emits a JSON report.

Run the PITR/WAL drill when validating full database recovery:

```bash
npm run e2e:pitr
```

This starts disposable Postgres and MinIO containers, archives real WAL, restores a physical base backup into a separate Postgres container, replays WAL, and validates the target recovery point.

Windows:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\e2e.ps1
```

### 3 — Install without cloning

**macOS / Linux**

```bash
curl -fsSL https://raw.githubusercontent.com/pratyush2514/Backstop/main/scripts/install.sh | sh
```

**Windows PowerShell**

```powershell
irm https://raw.githubusercontent.com/pratyush2514/Backstop/main/scripts/install.ps1 | iex
```

Then verify and start:

```bash
backstop-oss doctor       # check Go, Docker, storage, and DB connectivity
backstop-oss up           # start gateway + sidecar locally
backstop-oss mcp-config   # print ready-to-paste MCP config
```

See [docs/INSTALL.md](./docs/INSTALL.md) for full setup details.

---

## Integrations

### MCP Server (Claude, Cursor, Windsurf, any MCP client)

Give your AI tool a safe database tool instead of a raw connection string:

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

`npx @backstop/mcp-server` bootstraps the full runtime automatically — gateway, sync sidecar, metadata DB, and MinIO. No Docker commands, no manual ports. Works out of the box.

**Mode profiles** (`BACKSTOP_MCP_MODE`):

| Mode | Tools available | For |
|---|---|---|
| `agent` _(default)_ | execute, analyze, status, snapshots | Autonomous AI agents |
| `operator` | approve, deny, audit, alerts | Human operators reviewing approvals |
| `readonly` | analyze, status, audit, alerts | Observers, CI pipelines |
| `admin` | all tools + pause/resume | Infrastructure teams |

Connect an operator session alongside an agent session for the full approval workflow.

### Node.js / TypeScript SDK

```bash
npm install @backstop/client
```

```ts
import { BackstopClient } from "@backstop/client";

// Managed local mode — auto-starts the full runtime
const backstop = await BackstopClient.local({
  postgresUrl: process.env.POSTGRES_URL!,
  agentId: "billing-agent-dev",
});

// Queries pass through the gateway's policy engine
const result = await backstop.query("SELECT count(*) FROM users");

// Analyze without executing
const analysis = await backstop.analyzeQuery("DELETE FROM sessions WHERE expired = true");
console.log(analysis.risk_level); // "HIGH"

// List available recovery points
const snapshots = await backstop.listSnapshots({ table: "users" });

// Approval workflow (operator)
const pending = await backstop.getPendingApprovals();
await backstop.approve(pending[0].id);   // or .deny()
```

**Typed errors** for clean handling:

```ts
import { BackstopPolicyBlockedError, BackstopApprovalRequiredError } from "@backstop/client";

try {
  await backstop.query("DROP TABLE audit_logs");
} catch (err) {
  if (err instanceof BackstopApprovalRequiredError) {
    console.log("Approval ID:", err.approvalId);
  }
  if (err instanceof BackstopPolicyBlockedError) {
    console.log("Blocked:", err.reason);
  }
}
```

### Python SDK

```bash
pip install backstop
```

**Drop-in psycopg2 wrapper:**

```python
import psycopg2
import backstop

raw_conn = psycopg2.connect(DATABASE_URL)
db = backstop.guard(
    conn=raw_conn,
    storage="s3://my-bucket@http://localhost:9000",
    actor="gpt-4-agent",
    mode="protect",   # or "monitor" (log only) / "block" (raise on CRITICAL)
)

# DROP TABLE auto-snapshots the table first, then executes
db.execute("DROP TABLE users")
```

**SQLAlchemy:**

```python
from sqlalchemy import create_engine
import backstop

engine = create_engine(DATABASE_URL)
safe_engine = backstop.protect_engine(
    engine,
    storage="s3://my-bucket@http://localhost:9000",
    actor="django-agent",
)
```

**CLI:**

```bash
backstop doctor launch                  # launch readiness summary
backstop doctor native-tools            # verify pg_dump, pg_restore, pg_basebackup
backstop doctor storage-permissions \
  --storage s3://my-bucket              # verify object storage permissions
backstop recover --table users \
  --db postgresql://localhost:5432/app \
  --storage s3://my-bucket              # guided table recovery
backstop backup logical-create \
  --db postgresql://localhost:5432/app \
  --storage s3://my-bucket              # full logical backup
backstop pitr prepare-restore \
  --storage s3://my-bucket \
  --cluster-id prod \
  --backup-id base_123 \
  --target-dir ./restore-data           # prepare PITR files
```

---

## Risk Classification

Backstop uses PostgreSQL's own AST parser (not regex) to classify every query:

| Level | Examples | Default behavior |
|---|---|---|
| `SAFE` | `SELECT`, `EXPLAIN` | Pass through |
| `HIGH` | `INSERT`, scoped `UPDATE`, `DELETE` with `WHERE`, non-destructive DDL | Require approval |
| `CRITICAL` | `DROP TABLE`, `TRUNCATE`, `ALTER TABLE DROP COLUMN` | Require approval + verified snapshot |
| `IMPACT_CRITICAL` | High-impact writes or writes touching protected tables/columns | Require approval + recovery readiness when applicable |

**Escalation rules** (configurable in `policy.json`):

```jsonc
{
  "require_approval_for_risks": ["HIGH", "IMPACT_CRITICAL", "CRITICAL"],
  "block_operations": ["DROP DATABASE", "DROP SCHEMA"],
  "require_recovery_for_critical": true,
  "max_snapshot_age_seconds": 300,
  "max_write_rows_without_critical": 10000,
  "protected_tables": ["payments", "audit_logs"],
  "protected_columns": ["users.email", "users.password_hash"],
  "quarantine_duration_seconds": 300
}
```

---

## Approval Workflow

When a query requires approval:

```
Agent sends:  DROP TABLE sessions
              ↓
Gateway:      risk=CRITICAL, snapshot=verified, status=PENDING_APPROVAL
              id=appr_a1b2c3d4
              ↓
Operator:     GET /pending
              → [{ id, agent_id, risk, query, estimated_rows, created_at }]
              ↓
Operator:     POST /approve/appr_a1b2c3d4
              ↓
Gateway:      executes DROP TABLE → audit record written
```

Approvals time out (default 5 min) and auto-deny. Every decision is written to the audit log.

---

## Snapshot & Recovery

The **sync sidecar** snapshots tables to S3-compatible storage (MinIO for local, AWS S3 for production) in Parquet format on a configurable interval. It captures every discovered table on startup by default, then captures new, changed, or retry-needed tables on later polls. `--snapshot-every-poll=true` keeps the older full-every-poll behavior.

```
s3://bucket/backstop/
  users/
    snap_a3f9.parquet          ← columnar row data
    snap_a3f9.manifest.json    ← schema DDL, row count, checksum, FK/index definitions
  payments/
    snap_b7e2.parquet
    snap_b7e2.manifest.json
```

**Guided restore of a dropped table:**

```bash
backstop recover \
  --db postgresql://localhost:5432/app \
  --storage s3://prod-snapshots \
  --table users
```

The wizard lists valid checksummed snapshots, restores into `users_recovered` by default, runs restore validation automatically, and prints copyback SQL only after validation passes. The lower-level `backstop restore`, `backstop restore-validate`, and `backstop restore-copyback-plan` commands remain available for automation.

The manifest contains `CREATE TABLE` DDL plus captured indexes and constraints. Restore rebuilds a recovered target table from that manifest and row data, but table snapshots are not full-cluster PITR and some PostgreSQL objects live outside a single table.

---

## Architecture

```
backstop/
├── gateway/          Go — HTTP gateway, SQL policy engine, approval workflow
│   ├── main.go       server startup, route handlers
│   ├── policy.go     decision engine (configurable rules)
│   ├── sql_analyzer.go  PostgreSQL AST classification
│   ├── approval.go   approval queue with timeout
│   ├── impact.go     row-count analysis, protected column detection
│   ├── mcp_server.go MCP protocol handler
│   └── metadata_store.go  SQLite audit/health store
│
├── sync/             Go — snapshot sidecar
│   ├── main.go       startup, config
│   ├── snapshot.go   Parquet writer, S3 upload, manifest
│   ├── poller.go     per-table poll loop
│   ├── bypass.go     pg_stat_activity monitoring
│   └── alert.go      webhook alerts (stale snapshot, bypass detected)
│
├── packages/
│   ├── mcp-server/   TypeScript — MCP server wrapping the gateway
│   └── node-sdk/     TypeScript — @backstop/client for Node agents
│
├── sdk/python/       Python — guarded connections, CLI, restore
│   └── backstop/
│       ├── guard.py        GuardedConnection (psycopg2 wrapper)
│       ├── parser.py       SQL risk classification
│       ├── snapshot.py     Parquet + S3 snapshot writer
│       ├── restore.py      table reconstruction from snapshot
│       ├── sqlalchemy.py   SQLAlchemy engine integration
│       └── cli.py          `backstop` CLI commands
│
├── frontend/         Next.js 16 — landing page + docs site
├── examples/         MCP, Node, config examples
├── docs/             Setup, publishing, runbooks, incident response
└── scripts/          Install scripts, E2E runner
```

---

## Honest Limits

Backstop does not protect against everything. Be aware:

- **Gateway bypass**: If an agent has raw database credentials, it can connect directly and skip all policy checks. Revoking credentials and routing through the gateway is the only prevention.
- **Database-level recovery**: Backstop snapshots tables. It cannot restore a dropped database, dropped schema, stored functions, triggers, grants, or custom types. Use PostgreSQL PITR or logical backups for that.
- **Transactional consistency**: Table snapshots are not point-in-time consistent across multiple tables. For multi-table operations, snapshot timing matters.
- **Semantic SQL**: Backstop classifies structure, not intent. `UPDATE users SET role='admin'` on the wrong rows is a logic error that no SQL gateway can catch.
- **Metadata storage**: SQLite is local-only. Multi-node deployments need clear volume ownership and are not officially supported yet.
- **PostgreSQL only**: MySQL, SQLite, and other databases are not supported.

---

## Development

**Requirements:** Go 1.22+, Python 3.11+, Node.js 18+, Docker

```bash
# Install dependencies
npm install

# Run all tests
npm test
cd gateway && go test ./... -count=1
cd sync    && go test ./... -count=1
cd sdk/python && python -m pytest tests -q

# Full local E2E
npm run e2e
npm run e2e:pitr
```

See [CONTRIBUTING.md](./CONTRIBUTING.md) for PR expectations and the safety philosophy.

---

## Contributing

Contributions are welcome. A few non-negotiables:

- Do not weaken fail-closed behavior without an explicit policy flag and tests.
- Do not add paid or proprietary runtime dependencies to the OSS core.
- Add tests for any change to safety, approval, or recovery behavior.
- Update docs when you change setup, APIs, or safety guarantees.

See [CONTRIBUTING.md](./CONTRIBUTING.md) for full details.

---

## License

[Apache 2.0](./LICENSE) — free to use, modify, and distribute. Commercial distribution requires attribution.

---

<div align="center">

Built to prevent the next production incident.<br/>
If backstop helped you, leave a ⭐ — it helps others find it.

</div>
