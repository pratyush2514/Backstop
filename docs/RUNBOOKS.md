# backstop Recovery Runbooks

## Restore One Dropped Table

Use the guided workflow first. It restores to `<table>_recovered`, validates the
restore, and prints copyback SQL only after validation passes:

```text
backstop recover --db <postgres-url> --storage <s3-url> --table users
```

Use the lower-level commands when scripting or when an incident requires a
specific snapshot ID.

1. List snapshots:

```text
backstop snapshots list --db <postgres-url> --storage <s3-url> --table users
```

2. Preview restore:

```text
backstop restore --db <postgres-url> --storage <s3-url> --snapshot-id <snap-id> --table users --dry-run
```

3. Restore to safe default target:

```text
backstop restore --db <postgres-url> --storage <s3-url> --snapshot-id <snap-id> --table users
```

4. Validate recovered table, then decide whether to copy data back or rename.

5. Print copyback SQL after validation:

```text
backstop restore-copyback-plan --source-table users_recovered --target-table users
```

## Restore Full Logical Database

Use this when schema/object fidelity matters.

1. Provision a clean target database.
2. Restore:

```text
backstop backup logical-restore \
  --db <target-postgres-url> \
  --storage <s3-url> \
  --backup-id <dump-id> \
  --clean
```

3. Run application-level validation before redirecting traffic.

## Prepare PITR Restore

1. Stop PostgreSQL on the recovery host.
2. Ensure the target data directory is empty.
3. Prepare base backup plus recovery config:

```text
backstop pitr prepare-restore \
  --storage <s3-url> \
  --cluster-id prod \
  --backup-id <base-id> \
  --target-dir <postgres-data-dir> \
  --target-time "2026-05-01 12:30:00+00"
```

4. Start PostgreSQL. PostgreSQL will call `backstop wal fetch` through
   `restore_command` until it reaches the requested recovery target.
5. Inspect PostgreSQL logs and run validation queries.
6. Promote only after validation.

## Gateway Incident Flow

1. Review pending approval:

```text
GET /pending
Authorization: Bearer <gateway-token>
```

2. Confirm a latest sidecar snapshot exists for the target table:

```text
backstop snapshots list --db <postgres-url> --storage <s3-url> --table <table>
```

3. Approve only if the request includes that latest `snapshot_id`:

```text
POST /approve/<approval-id>
Authorization: Bearer <gateway-token>
```

4. If gateway blocks the query as non-table-recoverable, use logical or PITR
   recovery planning instead of overriding the block.

## Gateway Audit Review

1. Configure the gateway with durable audit storage:

```text
backstop-gateway \
  --metadata-db /metadata/backstop.db \
  --audit-log /data/audit.jsonl \
  --auth-token <gateway-token>
```

2. Review audit entries:

```text
GET /audit
Authorization: Bearer <gateway-token>
```

3. Review durable metadata:

```text
GET /metadata/audit?risk=CRITICAL
GET /metadata/snapshots?table=users
GET /metadata/alerts
GET /metadata/health
Authorization: Bearer <gateway-token>
```

4. Preserve `/metadata/backstop.db` and `/data/audit.jsonl` with the same
   retention controls used for application security logs.

## Launch Validation Drill

1. Run the launch readiness summary:

```text
backstop doctor launch --storage <s3-url> --table users --metadata-db /metadata/backstop.db
```

The verdict is `ready`, `degraded`, or `not_ready`. Treat `degraded` as a launch
blocker for recovery-sensitive deployments unless every degraded item is
understood and explicitly accepted.

2. Check native tools:

```text
backstop doctor native-tools --json
```

3. Verify WAL storage round trip:

```text
backstop drill wal-archive-fetch \
  --storage <s3-url> \
  --cluster-id prod \
  --metadata-db /metadata/backstop.db \
  --json
```

4. Verify PITR restore preparation:

```text
backstop drill pitr-prepare \
  --storage <s3-url> \
  --cluster-id prod \
  --target-dir <empty-restore-dir> \
  --simulate \
  --metadata-db /metadata/backstop.db \
  --json
```

5. For the local OSS Docker path, verify real PostgreSQL PITR/WAL recovery:

```text
npm run e2e:pitr
```

This drill validates actual restore startup and WAL replay, not just file
preparation. It must pass before claiming the local PITR path is launch-proven.

6. Verify logical backup/restore against disposable databases:

```text
backstop drill logical-backup-restore \
  --source-db <source-url> \
  --target-db <clean-target-url> \
  --storage <s3-url> \
  --metadata-db /metadata/backstop.db \
  --json
```

## Observability Checks

Gateway:

```text
GET /metrics
Authorization: Bearer <gateway-token>
```

Sync sidecar:

```text
GET http://<sync-host>:9091/metrics
```

Alert on stale table snapshots, consecutive snapshot failures, storage
unreachability, gateway blocks, and approval denials.

