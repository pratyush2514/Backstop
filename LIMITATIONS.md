# backstop Limitations

backstop is useful, but it is not magic. These limits should stay visible in every
public release.

## Gateway Bypass

backstop prevents dangerous SQL only when the SQL goes through the gateway. If an
AI agent, developer script, migration tool, or background job connects directly
to PostgreSQL, the gateway cannot classify, approve, or block that query.

Use least-privilege roles, network boundaries, and bypass detection.

## Full Database Recovery

Table snapshots cannot recover:

- `DROP DATABASE`
- `DROP SCHEMA`
- extensions
- functions
- triggers
- grants
- custom types
- complete transactional database state

Full database recovery requires PostgreSQL-native logical backups or PITR with
base backups and WAL archiving configured before the incident.

## Table Snapshot Consistency

Table snapshots are useful for fast table recovery, but they are not the same as
transactionally consistent PITR. Multi-table operations, cascades, foreign keys,
and schema changes may require recovery groups or native PITR.

## Semantic SQL Risk

backstop can promote some high-impact writes to `IMPACT_CRITICAL`, but no static
or preflight analysis can fully understand every business consequence. A scoped
`UPDATE` can still be logically wrong.

## SQLite Metadata

SQLite is intentionally local-first for OSS. It is not a clustered metadata
store. Multi-node production deployments need clear volume ownership and should
eventually use a central metadata backend.

## PostgreSQL Only

The current implementation is PostgreSQL-first. MySQL support would require a
separate parser, backup model, restore model, and safety policy implementation.

