"""SnapshotEngine — captures table state to Parquet in S3.

Security rules enforced here:
- psycopg2.sql.Identifier for ALL dynamic table references — never f-strings.
- S3 keys are derived from snapshot_id (uuid hex), never user-provided strings.
- Row data is never logged.
"""

from __future__ import annotations

import io
import hashlib
import json
import logging
import re
import tempfile
from datetime import datetime, timezone
from typing import Any, Optional
from uuid import uuid4

import boto3
import pyarrow as pa
import pyarrow.parquet as pq
import psycopg2.extras
from boto3.s3.transfer import TransferConfig
from psycopg2 import sql
from pydantic import BaseModel, Field

logger = logging.getLogger(__name__)

# ── Constants ────────────────────────────────────────────────────────────────

MAX_INLINE_SNAPSHOT_ROWS = 100_000  # above this, warn (streaming is Phase 2)
SNAPSHOT_CHUNK_SIZE = 1_000         # rows per insert batch used by RestoreEngine
SPOOL_MAX_BYTES = 64 * 1024 * 1024
MULTIPART_THRESHOLD_BYTES = 16 * 1024 * 1024
MULTIPART_CHUNK_BYTES = 16 * 1024 * 1024


# ── Data Models ─────────────────────────────────────────────────────────────

class SnapshotManifest(BaseModel):
    """Metadata record for a single table snapshot.

    Attributes:
        snapshot_id: Unique identifier in the format ``snap_{8-char hex}``.
        timestamp: ISO 8601 UTC timestamp of when the snapshot was taken.
        table_name: The source table that was snapshotted.
        query: The dangerous query that triggered this snapshot.
        operation: Human-readable operation name (e.g. ``DROP TABLE``).
        actor: Optional user or agent identifier for audit purposes.
        row_count: Number of rows captured.
        schema_ddl: ``CREATE TABLE`` DDL statement for schema reconstruction.
        fk_constraints: ``ALTER TABLE ADD CONSTRAINT`` statements (restored last).
        s3_bucket: S3 bucket where the snapshot is stored.
        s3_data_key: S3 object key for the Parquet data file.
        s3_manifest_key: S3 object key for this manifest JSON file.
    """

    manifest_version: int = 1
    writer: str = "python-sdk"
    db_name: str = "unknown"
    schema_name: str = "public"
    snapshot_id: str
    timestamp: str
    table_name: str
    query: str
    operation: str
    actor: Optional[str]
    row_count: int
    schema_ddl: str
    fk_constraints: list[str]
    indexes: list[str] = Field(default_factory=list)
    check_constraints: list[str] = Field(default_factory=list)
    s3_bucket: str
    s3_data_key: str
    s3_manifest_key: str
    data_sha256: Optional[str] = None
    snapshot_scope: str = "table"
    source_select_sql: Optional[str] = None


# ── PostgreSQL type mapping ──────────────────────────────────────────────────

# Maps information_schema.columns.data_type to PostgreSQL type names.
_PG_TYPE_MAP: dict[str, str] = {
    "integer": "INTEGER",
    "bigint": "BIGINT",
    "smallint": "SMALLINT",
    "numeric": "NUMERIC",
    "real": "REAL",
    "double precision": "DOUBLE PRECISION",
    "boolean": "BOOLEAN",
    "text": "TEXT",
    "character varying": "VARCHAR",
    "character": "CHAR",
    "uuid": "UUID",
    "date": "DATE",
    "timestamp without time zone": "TIMESTAMP",
    "timestamp with time zone": "TIMESTAMPTZ",
    "time without time zone": "TIME",
    "time with time zone": "TIMETZ",
    "interval": "INTERVAL",
    "json": "JSON",
    "jsonb": "JSONB",
    "bytea": "BYTEA",
    "inet": "INET",
    "cidr": "CIDR",
    "macaddr": "MACADDR",
    "point": "POINT",
    "line": "LINE",
    "lseg": "LSEG",
    "box": "BOX",
    "path": "PATH",
    "polygon": "POLYGON",
    "circle": "CIRCLE",
    "bit": "BIT",
    "bit varying": "BIT VARYING",
    "money": "MONEY",
    "xml": "XML",
    "ARRAY": "TEXT[]",
}


def _pg_col_type(data_type: str, char_max_len: Optional[int], numeric_precision: Optional[int], numeric_scale: Optional[int]) -> str:
    """Convert information_schema column type info to a PostgreSQL type string."""
    mapped = _PG_TYPE_MAP.get(data_type, data_type.upper())
    if data_type == "character varying" and char_max_len is not None:
        return f"VARCHAR({char_max_len})"
    if data_type == "character" and char_max_len is not None:
        return f"CHAR({char_max_len})"
    if data_type == "numeric" and numeric_precision is not None and numeric_scale is not None:
        return f"NUMERIC({numeric_precision},{numeric_scale})"
    return mapped


# ── SnapshotEngine ───────────────────────────────────────────────────────────

class SnapshotEngine:
    """Captures table state to Apache Parquet stored in S3-compatible storage.

    Args:
        s3_bucket: Target S3 bucket name.
        s3_prefix: Key prefix for all objects written (default: ``backstop``).
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
        self._s3 = boto3.client(
            "s3",
            endpoint_url=endpoint_url,
            region_name="us-east-1",
        )
        self._transfer_config = TransferConfig(
            multipart_threshold=MULTIPART_THRESHOLD_BYTES,
            multipart_chunksize=MULTIPART_CHUNK_BYTES,
        )

    # ── Public methods ───────────────────────────────────────────────────────

    def capture_table(
        self,
        conn: Any,
        table: str,
        query: str,
        operation: str,
        actor: Optional[str] = None,
    ) -> SnapshotManifest:
        """Capture the current state of a table to S3 as a Parquet file.

        The snapshot is taken BEFORE the dangerous query is executed.
        This method is synchronous; the caller is responsible for calling
        it before ``cursor.execute(query)``.

        Args:
            conn: An active psycopg2 connection.
            table: Name of the table to snapshot.
            query: The dangerous SQL query that triggered this snapshot.
            operation: Human-readable operation name (e.g. ``DROP TABLE``).
            actor: Optional actor identifier for audit logging.

        Returns:
            A :class:`SnapshotManifest` describing what was captured and where.

        Raises:
            Exception: Any S3 or DB error propagates to the caller so that
                :class:`~backstop.guard.GuardedConnection` can log it and proceed.
        """
        select_sql = sql.SQL("SELECT * FROM {}").format(sql.Identifier(table))
        return self._capture(
            conn=conn,
            table=table,
            query=query,
            operation=operation,
            actor=actor,
            select_sql=select_sql,
            select_params=None,
            snapshot_scope="table",
            source_select_sql=None,
        )

    def capture_query(
        self,
        conn: Any,
        table: str,
        select_sql: str,
        select_params: Any,
        query: str,
        operation: str,
        actor: Optional[str] = None,
    ) -> SnapshotManifest:
        """Capture rows returned by a generated before-image SELECT query.

        This is used for scoped destructive operations such as
        ``DELETE FROM users WHERE id = %s`` where only the affected rows should
        be snapshotted. The SELECT is generated from a parsed SQL AST in
        :mod:`backstop.parser`; callers must not pass arbitrary user-authored SQL.
        """
        return self._capture(
            conn=conn,
            table=table,
            query=query,
            operation=operation,
            actor=actor,
            select_sql=select_sql,
            select_params=select_params,
            snapshot_scope="rows",
            source_select_sql=select_sql,
        )

    def list_snapshots(self, table: Optional[str] = None) -> list[SnapshotManifest]:
        """List all snapshots stored in S3, optionally filtered by table.

        Args:
            table: If provided, only return snapshots for this table.

        Returns:
            List of :class:`SnapshotManifest` objects sorted by timestamp
            descending (most recent first).
        """
        table_key = _safe_table_key(table) if table else None
        prefix = (
            f"{self._prefix}/snapshots/{table_key}/"
            if table
            else f"{self._prefix}/snapshots/"
        )

        manifests: list[SnapshotManifest] = []
        paginator = self._s3.get_paginator("list_objects_v2")
        for page in paginator.paginate(Bucket=self._bucket, Prefix=prefix):
            for obj in page.get("Contents", []):
                key: str = obj["Key"]
                if not key.endswith("/manifest.json"):
                    continue
                try:
                    response = self._s3.get_object(Bucket=self._bucket, Key=key)
                    raw = response["Body"].read()
                    manifests.append(SnapshotManifest.model_validate_json(raw))
                except Exception as exc:
                    logger.warning("[backstop] Failed to read manifest at %s: %s", key, exc)

        manifests.sort(key=lambda m: m.timestamp, reverse=True)
        return manifests

    def get_manifest(self, snapshot_id: str, table: str) -> SnapshotManifest:
        """Fetch a specific snapshot manifest by ID and table name.

        Args:
            snapshot_id: The snapshot identifier (e.g. ``snap_a3f91c2b``).
            table: The table name the snapshot belongs to.

        Returns:
            The :class:`SnapshotManifest` for the requested snapshot.

        Raises:
            KeyError: If the manifest does not exist in S3.
        """
        key = f"{self._prefix}/snapshots/{_safe_table_key(table)}/{snapshot_id}/manifest.json"
        try:
            response = self._s3.get_object(Bucket=self._bucket, Key=key)
            raw = response["Body"].read()
            return SnapshotManifest.model_validate_json(raw)
        except self._s3.exceptions.NoSuchKey:
            raise KeyError(f"Snapshot not found: id={snapshot_id!r}, table={table!r}")
        except Exception as exc:
            raise KeyError(
                f"Failed to retrieve manifest for id={snapshot_id!r}, table={table!r}: {exc}"
            ) from exc

    # ── Private helpers ──────────────────────────────────────────────────────

    def _get_table_ddl(self, conn: Any, table: str) -> str:
        """Build a CREATE TABLE DDL statement via information_schema queries.

        Uses parameterized queries only — no dynamic SQL with table names in
        the query text (only in the WHERE clause via ``%s``).

        Args:
            conn: Active psycopg2 connection.
            table: Name of the table to describe.

        Returns:
            A ``CREATE TABLE`` statement string.
        """
        col_query = """
            SELECT
                column_name,
                data_type,
                character_maximum_length,
                numeric_precision,
                numeric_scale,
                is_nullable,
                column_default
            FROM information_schema.columns
            WHERE table_name = %s
              AND table_schema = 'public'
            ORDER BY ordinal_position
        """
        pk_query = """
            SELECT kcu.column_name
            FROM information_schema.table_constraints tc
            JOIN information_schema.key_column_usage kcu
                ON tc.constraint_name = kcu.constraint_name
                AND tc.table_schema = kcu.table_schema
            WHERE tc.constraint_type = 'PRIMARY KEY'
              AND tc.table_name = %s
              AND tc.table_schema = 'public'
            ORDER BY kcu.ordinal_position
        """

        with conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
            cur.execute(col_query, (table,))
            columns = cur.fetchall()

            cur.execute(pk_query, (table,))
            pk_cols = [row["column_name"] for row in cur.fetchall()]

        if not columns:
            logger.warning("[backstop] No columns found for table %r — returning minimal DDL", table)
            return f"CREATE TABLE {_quote_ident(table)} ()"

        col_defs: list[str] = []
        for col in columns:
            col_type = _pg_col_type(
                col["data_type"],
                col["character_maximum_length"],
                col.get("numeric_precision"),
                col.get("numeric_scale"),
            )
            not_null = " NOT NULL" if col["is_nullable"] == "NO" else ""
            default = f" DEFAULT {col['column_default']}" if col["column_default"] else ""
            # Quote column name to handle reserved words (e.g. "name", "user", "order")
            col_defs.append(f'    "{col["column_name"]}" {col_type}{not_null}{default}')

        if pk_cols:
            quoted_pks = ", ".join(f'"{c}"' for c in pk_cols)
            col_defs.append(f"    PRIMARY KEY ({quoted_pks})")

        ddl_lines = ",\n".join(col_defs)
        return f"CREATE TABLE {_quote_ident(table)} (\n{ddl_lines}\n)"

    def _get_fk_constraints(self, conn: Any, table: str) -> list[str]:
        """Build ALTER TABLE ADD CONSTRAINT statements for FK constraints.

        Args:
            conn: Active psycopg2 connection.
            table: Name of the table to inspect.

        Returns:
            List of ``ALTER TABLE ... ADD CONSTRAINT ... FOREIGN KEY ...`` strings.
        """
        fk_query = """
            SELECT
                tc.constraint_name,
                kcu.column_name,
                ccu.table_name AS foreign_table_name,
                ccu.column_name AS foreign_column_name,
                rc.update_rule,
                rc.delete_rule
            FROM information_schema.table_constraints tc
            JOIN information_schema.key_column_usage kcu
                ON tc.constraint_name = kcu.constraint_name
                AND tc.table_schema = kcu.table_schema
            JOIN information_schema.referential_constraints rc
                ON tc.constraint_name = rc.constraint_name
                AND tc.table_schema = rc.constraint_schema
            JOIN information_schema.constraint_column_usage ccu
                ON rc.unique_constraint_name = ccu.constraint_name
                AND rc.unique_constraint_schema = ccu.table_schema
            WHERE tc.constraint_type = 'FOREIGN KEY'
              AND tc.table_name = %s
              AND tc.table_schema = 'public'
            ORDER BY tc.constraint_name, kcu.ordinal_position
        """
        with conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
            cur.execute(fk_query, (table,))
            rows = cur.fetchall()

        constraints: list[str] = []
        for row in rows:
            on_update = f" ON UPDATE {row['update_rule']}" if row["update_rule"] != "NO ACTION" else ""
            on_delete = f" ON DELETE {row['delete_rule']}" if row["delete_rule"] != "NO ACTION" else ""
            stmt = (
                f"ALTER TABLE {table} ADD CONSTRAINT {row['constraint_name']} "
                f"FOREIGN KEY ({row['column_name']}) "
                f"REFERENCES {row['foreign_table_name']} ({row['foreign_column_name']})"
                f"{on_update}{on_delete};"
            )
            constraints.append(stmt)

        return constraints

    def _get_indexes(self, conn: Any, table: str) -> list[str]:
        """Return non-primary index DDL statements for a table."""
        query = """
            SELECT i.indexdef
            FROM pg_indexes i
            WHERE i.schemaname = 'public'
              AND i.tablename = %s
              AND i.indexname NOT IN (
                  SELECT con.conname
                  FROM pg_constraint con
                  JOIN pg_class rel ON rel.oid = con.conrelid
                  JOIN pg_namespace nsp ON nsp.oid = rel.relnamespace
                  WHERE nsp.nspname = 'public'
                    AND rel.relname = %s
                    AND con.contype = 'p'
              )
            ORDER BY i.indexname
        """
        with conn.cursor() as cur:
            cur.execute(query, (table, table))
            return [row[0] for row in cur.fetchall()]

    def _get_check_constraints(self, conn: Any, table: str) -> list[str]:
        """Return ALTER TABLE statements for CHECK constraints."""
        query = """
            SELECT con.conname, pg_get_constraintdef(con.oid)
            FROM pg_constraint con
            JOIN pg_class rel ON rel.oid = con.conrelid
            JOIN pg_namespace nsp ON nsp.oid = rel.relnamespace
            WHERE nsp.nspname = 'public'
              AND rel.relname = %s
              AND con.contype = 'c'
            ORDER BY con.conname
        """
        with conn.cursor() as cur:
            cur.execute(query, (table,))
            rows = cur.fetchall()

        return [
            f"ALTER TABLE {_quote_ident(table)} ADD CONSTRAINT {_quote_ident(name)} {definition};"
            for name, definition in rows
        ]

    def _rows_to_parquet(self, rows: list[dict], schema_ddl: str) -> bytes:
        """Serialize a list of row dicts to Parquet bytes with snappy compression.

        Args:
            rows: List of row dictionaries from RealDictCursor.
            schema_ddl: CREATE TABLE DDL (not used for type inference in Phase 1,
                but passed for future typed schema support).

        Returns:
            Parquet-encoded bytes with snappy compression.
        """
        if not rows:
            # Empty table — produce a valid empty Parquet file
            empty_table = pa.table({})
            buf = io.BytesIO()
            pq.write_table(empty_table, buf, compression="snappy")
            return buf.getvalue()

        arrow_table = self._rows_to_arrow_table(rows)
        buf = io.BytesIO()
        pq.write_table(arrow_table, buf, compression="snappy")
        return buf.getvalue()

    def _cursor_to_parquet(self, cur: Any) -> tuple[Any, int, int]:
        """Stream a cursor into a spooled Parquet file in row batches."""
        buf = tempfile.SpooledTemporaryFile(max_size=SPOOL_MAX_BYTES, mode="w+b")
        writer: Optional[pq.ParquetWriter] = None
        row_count = 0

        try:
            while True:
                batch_rows = [dict(row) for row in cur.fetchmany(SNAPSHOT_CHUNK_SIZE)]
                if not batch_rows:
                    break

                arrow_table = self._rows_to_arrow_table(batch_rows)
                if writer is None:
                    writer = pq.ParquetWriter(buf, arrow_table.schema, compression="snappy")
                writer.write_table(arrow_table)
                row_count += len(batch_rows)
        finally:
            if writer is not None:
                writer.close()

        if row_count == 0:
            empty_table = pa.table({})
            pq.write_table(empty_table, buf, compression="snappy")

        if row_count > MAX_INLINE_SNAPSHOT_ROWS:
            logger.warning(
                "[backstop] Snapshot captured %d rows (> MAX_INLINE_SNAPSHOT_ROWS=%d). "
                "DB reads are chunked; multipart S3 upload is a future optimization.",
                row_count, MAX_INLINE_SNAPSHOT_ROWS,
            )

        buf.seek(0, io.SEEK_END)
        size_bytes = buf.tell()
        buf.seek(0)
        return buf, row_count, size_bytes

    def _rows_to_arrow_table(self, rows: list[dict]) -> pa.Table:
        """Convert row dictionaries into an Arrow table."""
        clean_rows = []
        for row in rows:
            clean_row: dict[str, Any] = {}
            for k, v in row.items():
                if isinstance(v, memoryview):
                    clean_row[k] = bytes(v)
                elif type(v).__module__ == "uuid":
                    clean_row[k] = str(v)
                else:
                    clean_row[k] = v
            clean_rows.append(clean_row)
        return pa.Table.from_pylist(clean_rows)

    def _capture(
        self,
        conn: Any,
        table: str,
        query: str,
        operation: str,
        actor: Optional[str],
        select_sql: Any,
        select_params: Any,
        snapshot_scope: str,
        source_select_sql: Optional[str],
    ) -> SnapshotManifest:
        """Shared capture implementation for full-table and scoped snapshots."""
        snapshot_id = f"snap_{uuid4().hex[:8]}"
        timestamp = datetime.now(timezone.utc).isoformat()

        schema_ddl = self._get_table_ddl(conn, table)
        fk_constraints = self._get_fk_constraints(conn, table)
        indexes = self._get_indexes(conn, table)
        check_constraints = self._get_check_constraints(conn, table)
        db_name = "unknown"
        try:
            db_name = conn.get_dsn_parameters().get("dbname", "unknown")
        except Exception:
            pass

        with conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
            if select_params is not None:
                cur.execute(select_sql, select_params)
            else:
                cur.execute(select_sql)
            parquet_file, row_count, _size_bytes = self._cursor_to_parquet(cur)
            data_sha256 = _spooled_sha256(parquet_file)

        table_key = _safe_table_key(table)
        data_key = f"{self._prefix}/snapshots/{table_key}/{snapshot_id}/data.parquet"
        manifest_key = f"{self._prefix}/snapshots/{table_key}/{snapshot_id}/manifest.json"

        parquet_file.seek(0)
        try:
            self._s3.upload_fileobj(
                parquet_file,
                self._bucket,
                data_key,
                ExtraArgs={"ContentType": "application/octet-stream"},
                Config=self._transfer_config,
            )
        finally:
            parquet_file.close()

        manifest = SnapshotManifest(
            manifest_version=1,
            writer="python-sdk",
            db_name=db_name,
            schema_name="public",
            snapshot_id=snapshot_id,
            timestamp=timestamp,
            table_name=table,
            query=query,
            operation=operation,
            actor=actor,
            row_count=row_count,
            schema_ddl=schema_ddl,
            fk_constraints=fk_constraints,
            indexes=indexes,
            check_constraints=check_constraints,
            s3_bucket=self._bucket,
            s3_data_key=data_key,
            s3_manifest_key=manifest_key,
            data_sha256=data_sha256,
            snapshot_scope=snapshot_scope,
            source_select_sql=source_select_sql,
        )
        self._s3.put_object(
            Bucket=self._bucket,
            Key=manifest_key,
            Body=manifest.model_dump_json(indent=2).encode("utf-8"),
            ContentType="application/json",
        )

        logger.info(
            "[backstop] Snapshot complete: id=%s table=%r rows=%d operation=%s scope=%s",
            snapshot_id, table, row_count, operation, snapshot_scope,
        )
        return manifest


def _quote_ident(identifier: str) -> str:
    """Quote a simple PostgreSQL identifier for generated DDL."""
    return '"' + identifier.replace('"', '""') + '"'


def _safe_table_key(table: str) -> str:
    """Return a conservative S3 path component for a table name."""
    return re.sub(r"[^A-Za-z0-9_.-]", "_", table)


def _spooled_sha256(handle: Any) -> str:
    """Compute SHA-256 for a spooled file without changing its current position."""
    current = handle.tell()
    handle.seek(0)
    digest = hashlib.sha256()
    for chunk in iter(lambda: handle.read(1024 * 1024), b""):
        digest.update(chunk)
    handle.seek(current)
    return digest.hexdigest()

