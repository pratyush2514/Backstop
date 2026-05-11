# backstop Incident Response

This runbook is for local/self-hosted OSS deployments. It assumes agents use the
backstop gateway, snapshots are written by the sync sidecar, and native
backup/PITR drills are configured for database-level failures.

## First Actions

1. Pause risky execution:

```bash
curl -X POST http://localhost:8080/admin/pause \
  -H "Authorization: Bearer <admin-token>" \
  -H "Content-Type: application/json" \
  -d '{"reason":"incident investigation"}'
```

2. Check pending approvals, alerts, and health:

```bash
curl -H "Authorization: Bearer <operator-token>" http://localhost:8080/pending
curl -H "Authorization: Bearer <operator-token>" http://localhost:8080/metadata/alerts
curl -H "Authorization: Bearer <operator-token>" http://localhost:8080/metadata/health
```

3. Preserve evidence: gateway logs, SQLite metadata DB, sidecar logs, object
storage manifests, and PostgreSQL logs.

## AI Agent Repeated Destructive SQL

Symptoms:
- repeated blocked `DROP`, `TRUNCATE`, unscoped `DELETE`, or high-impact writes;
- agent quarantine alerts;
- many approval denials/timeouts for the same agent/table.

Actions:
- keep the gateway paused for writes;
- deny pending destructive approvals;
- remove raw database credentials from the agent environment;
- review the agent prompt/tool loop for retry behavior;
- rotate the agent token if it may be exposed.

Resume only after a safe query path is confirmed through the gateway.

## Critical Approval Requested

Before approving:
- verify the `query_sha256` shown in the pending approval;
- verify `environment` and `cluster_id`;
- verify the snapshot ID and table match the operation;
- check sidecar freshness and latest snapshot age;
- confirm the operation cannot be replaced by a safer scoped query.

If any field is missing, stale, or unexpected, deny the approval.

## Gateway Bypass Detected

Bypass means prevention is degraded. The gateway cannot classify or block SQL it
does not see.

Actions:
- pause gateway writes;
- identify the direct role/application/client address in alerts or PostgreSQL
  `pg_stat_activity`;
- revoke or rotate direct credentials given to humans, scripts, migration tools,
  or agents;
- enforce network routing so agent credentials can only reach the gateway;
- rely on snapshots, WAL, and native backups until prevention posture is healthy.

## Raw PostgreSQL Credentials Leaked To An Agent

Actions:
- revoke/rotate the leaked PostgreSQL role password immediately;
- inspect PostgreSQL logs for direct destructive queries;
- create a new scoped backstop token for the agent;
- confirm the agent has no direct DB connection string;
- run a recovery readiness check before resuming writes.

## Sidecar Stale Or Stopped

Symptoms:
- stale snapshot alerts;
- missing sync heartbeat;
- critical queries blocked for recovery readiness.

Actions:
- do not approve table-destructive SQL;
- restart the sidecar and verify it writes new metadata;
- confirm object storage write/read access;
- run a fresh snapshot and validate the manifest before resuming critical work.

## Object Storage Unavailable Or Mutable

Run:

```bash
backstop doctor storage-permissions --storage <s3-url> --strict --json
```

Actions:
- fix write/read failures before relying on recovery;
- if delete is allowed in production, tighten bucket/IAM/MinIO policy;
- verify existing snapshot and WAL objects can be read;
- do not treat a manifest as recoverable until the data object is readable.

## Snapshot Restore Fails Validation

Actions:
- use `backstop recover --db <url> --storage <s3-url> --table <table>` for the
  guided path;
- restore into `<table>_recovered`, not directly over the original table;
- if scripting, run `backstop restore-validate`;
- compare row counts and schema artifacts;
- inspect FK warnings and application-level consistency;
- generate a copy-back plan and review locks/conflicts before applying.

## DROP DATABASE Outside Gateway

Table snapshots cannot recover a dropped database.

Use one of:
- logical restore from `pg_dump` backup;
- physical base backup plus archived WAL for PITR;
- provider/platform restore if available outside OSS backstop.

After restore:
- rotate leaked/bypass credentials;
- point agents back to the gateway only;
- run native backup and WAL archive/fetch drills before resuming production use.

## Recovery Boundaries

backstop reduces blast radius, but it does not remove recovery planning:
- gateway protection only applies to traffic routed through it;
- table snapshots are not PITR;
- `DROP DATABASE`, `DROP SCHEMA`, extension removal, functions, triggers, and
  global objects require native backup/PITR;
- recovery readiness lowers risk but does not guarantee zero data loss;
- every backup path must be restored and validated before it is trusted.

