"""RestoreEngine — downloads Parquet from S3 and bulk-inserts rows into PostgreSQL.

Restore rules enforced here:
- Default target is ``{table}_recovered`` — never the original table.
- Schema created first, data inserted second, FK constraints added last.
- Restore is wrapped in a transaction; any failure causes ROLLBACK.
- Idempotent: ON CONFLICT DO NOTHING prevents duplicate rows.
- psycopg2.sql.Identifier used for all dynamic identifiers in DDL/DML.
"""

from __future__ import annotations

import io
import hashlib
import logging
import re
from typing import Any, Optional

import boto3
import pyarrow.parquet as pq
import psycopg2.extras
from psycopg2 import sql
from pydantic import BaseModel

from .snapshot import SNAPSHOT_CHUNK_SIZE, SnapshotEngine, SnapshotManifest

logger = logging.getLogger(__name__)


class RestorePreview(BaseModel):
    """Non-mutating summary of what a restore would do."""

    snapshot_id: str
    source_table: str
    target_table: str
    row_count: int
    snapshot_scope: str
    target_exists: bool
    target_row_count: Optional[int]
    will_create_table: bool
    will_apply_indexes: int
    will_apply_constraints: int


class RestoreEngine:
    """Restores a table from a previously captured snapshot.

    Args:
        s3_bucket: The S3 bucket containing the snapshot.
        s3_prefix: Key prefix used when snapshots were created (default: ``backstop``).
        endpoint_url: Optional custom endpoint for MinIO or other S3-compatible
            stores. Pass ``None`` to use real AWS S3.
    """

    def __init__(
        self,
        s3_bucket: str,
        s3_prefix: str = "backstop",
        endpoint_url: Optional[str] = None,
    ) -> None:
        self._bucket = s3_bucket
        self._prefix = s3_prefix.rstrip("/")
        self._endpoint_url = endpoint_url
        self._snapshot_engine = SnapshotEngine(
            s3_bucket=s3_bucket,
            s3_prefix=s3_prefix,
            endpoint_url=endpoint_url,
        )
        self._s3 = boto3.client(
            "s3",
            endpoint_url=endpoint_url,
            region_name="us-east-1",
        )

    def restore_table(
        self,
        conn: Any,
        snapshot_id: str,
        table: str,
        target_table: Optional[str] = None,
        dry_run: bool = False,
        conflict_policy: str = "skip",
    ) -> int:
        """Restore a table from a snapshot.

        The restore sequence is:
        1. Fetch manifest from S3.
        2. Download the Parquet data file.
        3. Create the target table using the captured schema DDL.
        4. Bulk-insert rows in :data:`~backstop.snapshot.SNAPSHOT_CHUNK_SIZE` batches
           with ``ON CONFLICT DO NOTHING`` for idempotency.
        5. Apply FK constraints.
        6. Commit.

        If any step fails, the transaction is rolled back and a
        :class:`RuntimeError` is raised.

        Args:
            conn: Active psycopg2 connection. Must NOT have autocommit enabled.
            snapshot_id: Snapshot identifier (e.g. ``snap_a3f91c2b``).
            table: Original table name that was snapshotted.
            target_table: Name to restore into. Defaults to ``{table}_recovered``
                if not provided. Pass the original table name explicitly to
                overwrite (use with caution).

        Returns:
            Number of rows restored.

        Raises:
            RuntimeError: If the restore fails (after rollback).
        """
        target = target_table or f"{table}_recovered"
        if conflict_policy not in {"skip", "overwrite", "fail"}:
            raise ValueError("conflict_policy must be one of: skip, overwrite, fail")

        if dry_run:
            self.preview_restore(conn, snapshot_id=snapshot_id, table=table, target_table=target_table)
            return 0

        try:
            manifest = self._snapshot_engine.get_manifest(snapshot_id=snapshot_id, table=table)
        except KeyError as exc:
            raise RuntimeError(f"Restore failed: snapshot not found. {exc}") from exc

        try:
            schema_name = manifest.schema_name or "public"
            self._validate_manifest_for_restore(manifest, table)
            # Adapt schema DDL to target table name
            adapted_ddl = self._adapt_ddl_for_target(manifest.schema_ddl, table, target)
            restored_count = 0

            with conn.cursor() as cur:
                cur.execute(sql.SQL("CREATE SCHEMA IF NOT EXISTS {}").format(sql.Identifier(schema_name)))
                cur.execute(
                    sql.SQL("SET LOCAL search_path TO {}, public").format(
                        sql.Identifier(schema_name)
                    )
                )
                self._lock_restore_target(cur, schema_name, target)

                # Step 1: Create table (schema only, no FK constraints)
                cur.execute(adapted_ddl.replace("CREATE TABLE", "CREATE TABLE IF NOT EXISTS", 1))
                row_count_before_insert = self._count_table_rows(cur, schema_name, target)

                # Step 2: Bulk insert in chunks
                for rows in self._iter_parquet_row_batches(manifest):
                    if not rows:
                        continue
                    columns = list(rows[0].keys())
                    insert_stmt = self._build_insert_statement(cur, schema_name, target, columns, conflict_policy)

                    batch = [tuple(row[c] for c in columns) for row in rows]
                    psycopg2.extras.execute_batch(cur, insert_stmt, batch)
                    if conflict_policy != "skip":
                        restored_count += len(rows)

                if conflict_policy == "skip":
                    row_count_after_insert = self._count_table_rows(cur, schema_name, target)
                    restored_count = max(row_count_after_insert - row_count_before_insert, 0)

                self._reset_sequences(cur, schema_name, target)

                # Step 3: Apply CHECK constraints, indexes, and FK constraints.
                for check_ddl in getattr(manifest, "check_constraints", []):
                    adapted_check = self._adapt_ddl_for_target(check_ddl, table, target)
                    if not self._try_execute_ddl(cur, adapted_check, "CHECK constraint"):
                        raise RuntimeError(f"CHECK constraint could not be applied: {adapted_check[:120]}")

                for fk_ddl in manifest.fk_constraints:
                    adapted_fk = self._adapt_ddl_for_target(fk_ddl, table, target)
                    if not self._try_execute_ddl(cur, adapted_fk, "FK constraint"):
                        raise RuntimeError(f"FK constraint could not be applied: {adapted_fk[:120]}")

                for idx_ddl in getattr(manifest, "indexes", []):
                    adapted_idx = self._adapt_index_ddl_for_target(idx_ddl, table, target)
                    if not self._try_execute_ddl(cur, adapted_idx, "index"):
                        raise RuntimeError(f"Index could not be applied: {adapted_idx[:120]}")

                validation = self._validate_restore_result(cur, schema_name, target, manifest)
                if not validation["ok"]:
                    raise RuntimeError(f"Restore validation failed: {validation}")

            conn.commit()

            logger.info(
                "[backstop] Restore complete and validated: snapshot=%s table=%r -> %r rows=%d",
                snapshot_id, table, target, restored_count,
            )
            return restored_count

        except Exception as exc:
            try:
                conn.rollback()
            except Exception as rb_exc:
                logger.error("[backstop] Rollback also failed: %s", rb_exc)
            raise RuntimeError(
                f"Restore failed and was rolled back: {exc}"
            ) from exc

    def preview_restore(
        self,
        conn: Any,
        snapshot_id: str,
        table: str,
        target_table: Optional[str] = None,
    ) -> RestorePreview:
        """Return a non-mutating preview of a restore operation."""
        target = target_table or f"{table}_recovered"
        try:
            manifest = self._snapshot_engine.get_manifest(snapshot_id=snapshot_id, table=table)
        except KeyError as exc:
            raise RuntimeError(f"Restore preview failed: snapshot not found. {exc}") from exc

        schema_name = manifest.schema_name or "public"
        with conn.cursor() as cur:
            cur.execute(
                """
                SELECT EXISTS (
                    SELECT FROM information_schema.tables
                    WHERE table_schema = %s AND table_name = %s
                )
                """,
                (schema_name, target),
            )
            target_exists = bool(cur.fetchone()[0])
            target_row_count: Optional[int] = None
            if target_exists:
                cur.execute(sql.SQL("SELECT COUNT(*) FROM {}").format(sql.Identifier(schema_name, target)))
                target_row_count = int(cur.fetchone()[0])

        return RestorePreview(
            snapshot_id=manifest.snapshot_id,
            source_table=table,
            target_table=target,
            row_count=manifest.row_count,
            snapshot_scope=manifest.snapshot_scope,
            target_exists=target_exists,
            target_row_count=target_row_count,
            will_create_table=not target_exists,
            will_apply_indexes=len(getattr(manifest, "indexes", [])),
            will_apply_constraints=len(manifest.fk_constraints) + len(getattr(manifest, "check_constraints", [])),
        )

    # ── Private helpers ──────────────────────────────────────────────────────

    def _parquet_to_rows(self, data: bytes) -> list[dict[str, Any]]:
        """Deserialize Parquet bytes into a list of row dicts.

        Args:
            data: Raw bytes of a Parquet file.

        Returns:
            List of row dictionaries.
        """
        buf = io.BytesIO(data)
        arrow_table = pq.read_table(buf)
        return arrow_table.to_pylist()

    def _iter_parquet_row_batches(self, manifest: SnapshotManifest) -> Any:
        """Yield restored rows in batches instead of materializing all at once.

        Verifies SHA256 integrity of the downloaded Parquet data against the
        manifest's data_sha256 field before processing any rows. This prevents
        inserting corrupted data into a live database — a hard error because
        the consequences of silent corruption are unrecoverable.
        """
        response = self._s3.get_object(Bucket=self._bucket, Key=manifest.s3_data_key)
        parquet_bytes = response["Body"].read()

        # Integrity verification: compute SHA256 of downloaded bytes and compare
        # to the manifest's recorded hash. This catches S3 transport corruption,
        # bit rot, and wrong-object retrieval.
        if not manifest.data_sha256:
            raise RuntimeError(
                f"Restore integrity check failed for snapshot {manifest.snapshot_id}: "
                "manifest is missing data_sha256. Refusing to restore an unverifiable snapshot."
            )
        actual_hash = hashlib.sha256(parquet_bytes).hexdigest()
        if actual_hash.lower() != manifest.data_sha256.lower():
            raise RuntimeError(
                f"Restore integrity check failed for snapshot {manifest.snapshot_id}: "
                f"parquet SHA256 mismatch (got {actual_hash}, "
                f"expected {manifest.data_sha256}). "
                f"Data may be corrupted in S3; restore aborted to prevent invalid data insertion."
            )
        logger.info(
            "[backstop] Restore data integrity verified: snapshot=%s sha256=%s",
            manifest.snapshot_id, actual_hash[:16] + "...",
        )

        parquet = pq.ParquetFile(io.BytesIO(parquet_bytes))
        for batch in parquet.iter_batches(batch_size=SNAPSHOT_CHUNK_SIZE):
            yield batch.to_pylist()

    def _build_insert_statement(self, cur: Any, schema_name: str, target: str, columns: list[str], conflict_policy: str) -> Any:
        """Build an INSERT statement using the requested conflict policy."""
        col_identifiers = sql.SQL(", ").join(sql.Identifier(c) for c in columns)
        placeholders = sql.SQL(", ").join(sql.Placeholder() for _ in columns)
        base = sql.SQL("INSERT INTO {table} ({cols}) VALUES ({vals})").format(
            table=sql.Identifier(schema_name, target),
            cols=col_identifiers,
            vals=placeholders,
        )

        if conflict_policy == "fail":
            return base

        if conflict_policy == "skip":
            return base + sql.SQL(" ON CONFLICT DO NOTHING")

        pk_cols = self._primary_key_columns(cur, schema_name, target)
        if not pk_cols:
            raise RuntimeError(
                "conflict_policy='overwrite' requires a primary key on the target table"
            )

        update_cols = [c for c in columns if c not in set(pk_cols)]
        if not update_cols:
            return base + sql.SQL(" ON CONFLICT DO NOTHING")

        conflict_cols = sql.SQL(", ").join(sql.Identifier(c) for c in pk_cols)
        assignments = sql.SQL(", ").join(
            sql.SQL("{col} = EXCLUDED.{col}").format(col=sql.Identifier(c))
            for c in update_cols
        )
        return base + sql.SQL(" ON CONFLICT ({conflict_cols}) DO UPDATE SET {assignments}").format(
            conflict_cols=conflict_cols,
            assignments=assignments,
        )

    def _primary_key_columns(self, cur: Any, schema_name: str, table: str) -> list[str]:
        """Return target table primary key columns in ordinal order."""
        cur.execute(
            """
            SELECT kcu.column_name
            FROM information_schema.table_constraints tc
            JOIN information_schema.key_column_usage kcu
              ON tc.constraint_name = kcu.constraint_name
             AND tc.table_schema = kcu.table_schema
            WHERE tc.constraint_type = 'PRIMARY KEY'
              AND tc.table_schema = %s
              AND tc.table_name = %s
            ORDER BY kcu.ordinal_position
            """,
            (schema_name, table),
        )
        return [row[0] for row in cur.fetchall()]

    def _lock_restore_target(self, cur: Any, schema_name: str, target: str) -> None:
        """Serialize concurrent restores into the same target table."""
        cur.execute("SELECT pg_advisory_xact_lock(hashtext(%s))", (f"backstop_restore:{schema_name}.{target}",))

    def _count_table_rows(self, cur: Any, schema_name: str, table: str) -> int:
        """Return row count for restore accounting while the advisory lock is held."""
        cur.execute(
            sql.SQL("SELECT COUNT(*) FROM {}").format(sql.Identifier(schema_name, table))
        )
        return int(cur.fetchone()[0])

    def _reset_sequences(self, cur: Any, schema_name: str, table: str) -> None:
        """Advance serial sequences after restoring explicit primary key values."""
        cur.execute(
            """
            SELECT column_name, column_default
            FROM information_schema.columns
            WHERE table_schema = %s
              AND table_name = %s
              AND column_default LIKE 'nextval%%'
            """,
            (schema_name, table),
        )
        for column_name, column_default in cur.fetchall():
            match = re.search(r"nextval\('([^']+)'::regclass\)", column_default or "")
            if not match:
                continue
            sequence_name = match.group(1)
            cur.execute(
                sql.SQL("SELECT COALESCE(MAX({col}), 0) FROM {table}").format(
                    col=sql.Identifier(column_name),
                    table=sql.Identifier(schema_name, table),
                )
            )
            max_value = int(cur.fetchone()[0] or 0)
            if max_value > 0:
                cur.execute("SELECT setval(%s, %s, true)", (sequence_name, max_value))

    def _try_execute_ddl(self, cur: Any, ddl: str, label: str) -> bool:
        """Best-effort DDL application that does not abort the restore.

        Returns True if the DDL was applied successfully, False if it failed.
        Failures are logged as warnings. The caller accumulates failures to
        provide a complete summary of what could not be restored.
        """
        savepoint = "backstop_restore_ddl"
        try:
            cur.execute(f"SAVEPOINT {savepoint}")
            cur.execute(ddl)
            cur.execute(f"RELEASE SAVEPOINT {savepoint}")
            return True
        except Exception as exc:
            try:
                cur.execute(f"ROLLBACK TO SAVEPOINT {savepoint}")
                cur.execute(f"RELEASE SAVEPOINT {savepoint}")
            except Exception:
                logger.exception("[backstop] Failed to clean up restore DDL savepoint")
            logger.warning(
                "[backstop] %s could not be applied during restore (skipping): %s — %s",
                label,
                ddl[:160],
                exc,
            )
            return False

    def _validate_manifest_for_restore(self, manifest: SnapshotManifest, table: str) -> None:
        """Fail closed before restore if the manifest is not authoritative."""
        if manifest.table_name != table:
            raise RuntimeError(
                f"manifest table mismatch: manifest has {manifest.table_name!r}, requested {table!r}"
            )
        if manifest.status != "valid":
            raise RuntimeError(
                f"snapshot {manifest.snapshot_id} is not valid: status={manifest.status!r} "
                f"validation_error={manifest.validation_error!r}"
            )
        if manifest.snapshot_scope not in {"table", "rows"}:
            raise RuntimeError(
                f"snapshot {manifest.snapshot_id} has unsupported scope {manifest.snapshot_scope!r}"
            )
        if not manifest.data_sha256:
            raise RuntimeError(
                f"snapshot {manifest.snapshot_id} is missing data_sha256 and cannot be restored safely"
            )

    def _validate_restore_result(self, cur: Any, schema_name: str, target: str, manifest: SnapshotManifest) -> dict[str, Any]:
        """Validate the target table inside the restore transaction before commit."""
        cur.execute(
            """
            SELECT EXISTS (
                SELECT FROM information_schema.tables
                WHERE table_schema = %s AND table_name = %s
            )
            """,
            (schema_name, target),
        )
        target_exists = bool(cur.fetchone()[0])
        row_count: Optional[int] = None
        if target_exists:
            cur.execute(sql.SQL("SELECT COUNT(*) FROM {}").format(sql.Identifier(schema_name, target)))
            row_count = int(cur.fetchone()[0])

        invalid_constraints = self._invalid_constraints(cur, schema_name, target) if target_exists else []
        table_scope_row_count_ok = manifest.snapshot_scope != "table" or row_count == manifest.row_count
        ok = bool(target_exists and table_scope_row_count_ok and not invalid_constraints)
        return {
            "ok": ok,
            "target_exists": target_exists,
            "snapshot_scope": manifest.snapshot_scope,
            "target_row_count": row_count,
            "manifest_row_count": manifest.row_count,
            "invalid_constraints": invalid_constraints,
        }

    def _invalid_constraints(self, cur: Any, schema_name: str, target: str) -> list[str]:
        cur.execute(
            """
            SELECT conname
            FROM pg_constraint
            WHERE conrelid = %s::regclass
              AND NOT convalidated
            ORDER BY conname
            """,
            (f"{schema_name}.{target}",),
        )
        return [row[0] for row in cur.fetchall()]

    def _adapt_ddl_for_target(self, ddl: str, original: str, target: str) -> str:
        """Replace the original table name with the target name in a DDL string.

        Also strips ``DEFAULT nextval(...)`` clauses — the original sequence may
        not exist after a DROP TABLE, and restored rows carry explicit ID values
        from the snapshot so no auto-increment default is needed.

        Args:
            ddl: The DDL string to adapt.
            original: The original table name to replace.
            target: The target table name to substitute in.

        Returns:
            The adapted DDL string.
        """
        # Replace table name (whole-word match to avoid partial replacements)
        escaped = re.escape(original)
        ddl = re.sub(rf"\b{escaped}\b", target, ddl)

        # Strip DEFAULT nextval(...) — sequences may not exist after a DROP TABLE.
        # Rows being restored carry explicit values, so no default is needed.
        ddl = re.sub(r"\s+DEFAULT\s+nextval\([^)]+\)", "", ddl, flags=re.IGNORECASE)

        return ddl

    def _adapt_index_ddl_for_target(self, ddl: str, original: str, target: str) -> str:
        """Retarget an index DDL statement and avoid name collisions."""
        retargeted = self._adapt_ddl_for_target(ddl, original, target)
        digest = hashlib.sha1(retargeted.encode("utf-8")).hexdigest()[:10]
        new_name = f"backstop_{target}_{digest}"
        return re.sub(
            r"CREATE\s+(UNIQUE\s+)?INDEX\s+(?:CONCURRENTLY\s+)?(\"?[A-Za-z_][A-Za-z0-9_]*\"?)\s+ON",
            lambda m: f"CREATE {m.group(1) or ''}INDEX IF NOT EXISTS {_quote_ident(new_name)} ON",
            retargeted,
            count=1,
            flags=re.IGNORECASE,
        )


def _quote_ident(identifier: str) -> str:
    return '"' + identifier.replace('"', '""') + '"'

