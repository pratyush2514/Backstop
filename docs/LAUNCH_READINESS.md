# backstop Launch Readiness

backstop has two recovery planes:

1. Fast object recovery: SDK and sidecar table snapshots written as Parquet.
2. PostgreSQL-native disaster recovery: `pg_dump`, `pg_restore`,
   `pg_basebackup`, and WAL archive/fetch helpers.

The native plane is the authority for full database disaster recovery and
PostgreSQL object fidelity. Table snapshots are intentionally not presented as
a replacement for PostgreSQL-native backups.

## Required Services

- PostgreSQL 16 or compatible PostgreSQL server.
- S3-compatible object storage.
- `backstop-sync` sidecar for frequent table recovery points.
- `backstop-gateway` for controlled agent execution.
- Python `backstop` CLI image or package installed wherever backup/restore jobs run.
- A durable SQLite metadata location, for example
  `--metadata-db /metadata/backstop.db`.
- Optional JSONL audit export location, for example `--audit-log /data/audit.jsonl`.
- A gateway auth token supplied by `--auth-token` /
  `BACKSTOP_GATEWAY_AUTH_TOKEN`, or a scoped token file supplied by
  `--auth-tokens-file` / `BACKSTOP_AUTH_TOKENS_FILE`.

## PostgreSQL Fidelity

Use:

```text
backstop backup logical-create --db <postgres-url> --storage <s3-url>
backstop backup logical-restore --db <target-postgres-url> --storage <s3-url> --backup-id <id>
```

This path uses `pg_dump --format=custom --blobs` and `pg_restore`. It is the
supported path for non-public schemas, triggers, RLS policies, custom types,
partitioned tables, materialized views, functions, grants, and extensions that
`pg_dump` can represent.

## PITR

PITR requires PostgreSQL archiving to be enabled at the database server:

```text
wal_level = replica
archive_mode = on
archive_command = 'backstop wal archive --storage s3://bucket@endpoint --cluster-id prod --wal-file %p --wal-name %f'
```

Create a physical base backup:

```text
backstop pitr basebackup --db <replication-url> --storage <s3-url> --cluster-id prod
```

Prepare a restore directory:

```text
backstop pitr prepare-restore \
  --storage <s3-url> \
  --cluster-id prod \
  --backup-id <base-id> \
  --target-dir /var/lib/postgresql/data \
  --target-time "2026-05-01 12:30:00+00"
```

The restore preparation writes `recovery.signal` and a `restore_command` that
uses `backstop wal fetch`.

## Gateway Safety

The gateway executes queries itself and applies the default production policy:

- SAFE SQL executes immediately.
- HIGH SQL requires approval.
- CRITICAL SQL requires approval and, for table-recoverable operations, latest
  sidecar snapshot verification.
- `DROP DATABASE`, `DROP SCHEMA`, parser failures, and unknown AST statements
  are blocked fail-closed.
- semantic impact analysis can promote high-blast-radius `UPDATE`/`DELETE`
  statements to `IMPACT_CRITICAL`;
- repeated risky attempts can quarantine an agent.

Protected endpoints require auth by default. Local development without auth
requires the explicit `--allow-insecure-no-auth=true` flag. Tokens are accepted
through either `Authorization: Bearer <token>` or `X-Backstop-Token: <token>`.
Production-like deployments should use scoped tokens for agents, operators,
metrics, restore workflows, and admin actions instead of one shared token.

CRITICAL table-level operations require:

- authenticated access to the MCP, approval, pending, audit, metadata, and
  metrics endpoints;
- a token scope that allows the requested action;
- human approval;
- a supplied sidecar `snapshot_id`;
- verification that the snapshot exists in configured storage;
- verification that it is the latest `sync-sidecar` table-scope snapshot for
  the affected table.
- recovery readiness checks for snapshot age, sidecar heartbeat, data object
  existence, and configured recovery groups.

Operators approve with:

```text
Authorization: Bearer <gateway-token>
POST /approve/<approval-id>
```

Approval records are bound to the exact `query_sha256`, environment, cluster,
risk, table, and snapshot ID. Audit/approval entries are written to SQLite
metadata and can also be appended to the configured JSONL audit file for simple
log shipping.

Emergency pause is available to admin tokens:

```text
POST /admin/pause
POST /admin/resume
GET /admin/status
```

Database-level destructive operations such as `DROP DATABASE` and `DROP SCHEMA`
are blocked because table snapshots cannot recover them. Use the native recovery
plane for full database recovery.

## AI Agent Integration

AI tools should be configured to use backstop instead of raw PostgreSQL
credentials:

```text
AI agent -> backstop MCP server or Node SDK -> backstop gateway -> PostgreSQL
```

For MCP-compatible tools, use `@backstop/mcp-server` with:

```text
BACKSTOP_URL=http://localhost:8080
BACKSTOP_TOKEN=<gateway-token>
BACKSTOP_AGENT_ID=<stable-agent-name>
```

`BACKSTOP_AGENT_ID` is chosen by the developer/operator. It should be stable and
human-readable, for example `cursor-local`, `codex-dev-agent`, or
`support-agent-prod`. It is used for audit logs, approvals, retry detection, and
quarantine; it is not a secret and should not be random per request.

For custom Node agents, use `@backstop/client`. See `docs/MCP_SERVER.md`,
`docs/NODE_SDK.md`, and `docs/AI_AGENT_SETUP.md`.

## Metadata And Observability

SQLite metadata is the product source of truth for backend/dashboard data:

- Gateway: `audit_events`, `approvals`, `health_checks`.
- Sync sidecar: `snapshots`, `alerts`, `health_checks`, bypass posture.
- Python native CLI: `native_backups`, `health_checks` when `--metadata-db` is
  supplied.

Gateway read APIs:

```text
GET /metadata/snapshots?table=users
GET /metadata/audit?agent_id=<agent>&risk=CRITICAL
GET /metadata/alerts
GET /metadata/health
GET /metrics
```

Sidecar metrics are exposed when `backstop-sync --metrics-listen :9091` is set:

```text
GET /metrics
```

Both Go services keep SQLite in WAL mode with a busy timeout so a shared local
metadata volume can support concurrent gateway and sidecar writes.

The sync sidecar can also run bypass detection:

```text
backstop-sync \
  --bypass-detection=true \
  --agent-roles backstop_agent \
  --gateway-application-name backstop-gateway
```

This polls `pg_stat_activity` and marks prevention posture as degraded when
agent-like roles connect outside the expected gateway path.

## Launch Validation Drills

Run these before launch and after infrastructure changes:

```text
backstop doctor launch --storage <s3-url> --table <table> --metadata-db /metadata/backstop.db
backstop doctor native-tools --json
backstop doctor storage-permissions --storage <s3-url> --strict --json
backstop drill wal-archive-fetch --storage <s3-url> --cluster-id prod --json
backstop drill pitr-prepare --storage <s3-url> --cluster-id prod --target-dir <dir> --simulate --json
backstop drill logical-backup-restore --source-db <url> --target-db <url> --storage <s3-url> --json
```

`backstop doctor launch` is the operator-friendly summary. Use the drill
commands as the deterministic proof behind the verdict and in CI.

Pass `--metadata-db /metadata/backstop.db` to persist drill health and native
backup manifests.

For a local OSS E2E drill:

```bash
npm run e2e
```

For the full local PostgreSQL PITR/WAL drill:

```bash
npm run e2e:pitr
```

This starts a disposable source PostgreSQL and MinIO stack, takes a real
`pg_basebackup`, writes before/after target markers, restores the base backup
into a separate PostgreSQL container, replays WAL through `backstop wal fetch`,
and validates that only the before-target marker exists.

or:

```bash
make e2e
```

## Least Privilege Defaults

Agents should connect only through the gateway. The agent database role should
not own databases or protected schemas, and should not have database-level
destructive privileges:

```sql
CREATE ROLE backstop_agent LOGIN PASSWORD '<replace-me>';
REVOKE CREATE ON SCHEMA public FROM backstop_agent;
REVOKE ALL ON DATABASE appdb FROM PUBLIC;
GRANT CONNECT ON DATABASE appdb TO backstop_agent;
GRANT USAGE ON SCHEMA public TO backstop_agent;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO backstop_agent;
```

Use a separate backup role for native backup jobs and a separate gateway role
for the minimum application tables it is allowed to query or modify.

Minimal S3 IAM actions for object storage credentials:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:PutObject", "s3:GetObject", "s3:ListBucket"],
      "Resource": ["arn:aws:s3:::<bucket>", "arn:aws:s3:::<bucket>/backstop/*"]
    }
  ]
}
```

## Packaging

Container entry points:

- `sync/Dockerfile` -> `backstop-sync`
- `gateway/Dockerfile` -> `backstop-gateway`
- `sdk/python/Dockerfile` -> `backstop` CLI with PostgreSQL client tools
- `deploy/postgres/Dockerfile` -> local PostgreSQL image with `backstop`
  installed so `archive_command` can upload WAL through `backstop wal archive`

Local service composition is in `deploy/docker-compose.yml`. It configures
real WAL archiving to MinIO, gateway auth with the development token
`dev-token`, sidecar metrics, a shared SQLite metadata volume, and durable
gateway audit export. Replace those development values before any shared or
production deployment.

