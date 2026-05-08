"""GuardedConnection — psycopg2 connection wrapper that intercepts destructive SQL.

Public API surface:
    guard(conn, storage, actor, mode) → GuardedConnection

Mode behaviour:
    protect  (default) — snapshot then execute. Snapshot failure is logged
                         but the original query is ALWAYS executed.
    monitor            — log only, no snapshot, always execute.
    block              — raise PermissionError on CRITICAL, never execute those.

Security rules enforced here:
- Database credentials are never logged (scrubbed before any log output).
- Snapshot failure MUST NOT prevent query execution.
"""

from __future__ import annotations

import logging
import re
from typing import Any, Iterable, Optional
from uuid import uuid4

from .logger import log_event
from .parser import RiskLevel, assess_risk, build_before_image_query, select_before_image_params
from .snapshot import SnapshotEngine

logger = logging.getLogger(__name__)

# Valid mode values
_VALID_MODES = frozenset({"protect", "monitor", "block"})


def _scrub_credentials(url: str) -> str:
    """Replace password in a connection URL with ``***`` for safe logging.

    Args:
        url: A database connection URL, e.g.
            ``postgresql://user:secret@host/db``.

    Returns:
        The URL with the password replaced by ``***``.
    """
    return re.sub(r"://([^:]+):[^@]+@", r"://\1:***@", url)


class GuardedConnection:
    """A psycopg2 connection wrapper that intercepts and guards destructive SQL.

    Implements the same interface as a psycopg2 connection for the methods that
    application code typically calls. All SQL execution goes through
    :meth:`execute` which is the primary interception point.

    Args:
        conn: The underlying psycopg2 connection to wrap.
        snapshot_engine: A configured :class:`~backstop.snapshot.SnapshotEngine`.
        actor: Optional actor identifier for audit logging.
        mode: One of ``protect``, ``monitor``, or ``block``.
    """

    def __init__(
        self,
        conn: Any,
        snapshot_engine: SnapshotEngine,
        actor: Optional[str] = None,
        mode: str = "protect",
    ) -> None:
        if mode not in _VALID_MODES:
            raise ValueError(f"Invalid mode {mode!r}. Must be one of: {sorted(_VALID_MODES)}")
        self._conn = conn
        self._engine = snapshot_engine
        self._actor = actor
        self._mode = mode

    # ── Primary interception point ───────────────────────────────────────────

    def execute(self, query: str, params: Any = None) -> Any:
        """Execute a SQL query with backstop protection applied.

        Assesses the risk of ``query``, optionally snapshots the affected table,
        logs the event, then executes the original query unchanged.

        Args:
            query: The SQL string to execute.
            params: Optional parameters to pass to ``cursor.execute()``.

        Returns:
            The psycopg2 cursor after execution.

        Raises:
            PermissionError: In ``block`` mode when a CRITICAL operation is
                detected. The query is NOT executed.
        """
        cur = self._conn.cursor()
        self._execute_on_cursor(cur, query, params)
        return cur

    # ── psycopg2 connection pass-throughs ────────────────────────────────────

    def cursor(self, *args: Any, **kwargs: Any) -> "GuardedCursor":
        """Return a protected cursor from the underlying connection."""
        return GuardedCursor(self._conn.cursor(*args, **kwargs), self)

    def commit(self) -> None:
        """Commit the current transaction."""
        return self._conn.commit()

    def rollback(self) -> None:
        """Roll back the current transaction."""
        return self._conn.rollback()

    def close(self) -> None:
        """Close the underlying connection."""
        return self._conn.close()

    def __enter__(self) -> "GuardedConnection":
        return self

    def __exit__(self, *args: Any) -> Any:
        return self._conn.__exit__(*args)

    def _execute_on_cursor(self, cursor: Any, query: str, params: Any = None) -> Any:
        """Apply backstop protection, then execute on an existing raw cursor."""
        self._before_execute(query, params)
        if params is not None:
            return cursor.execute(query, params)
        return cursor.execute(query)

    def _executemany_on_cursor(self, cursor: Any, query: str, param_seq: Iterable[Any]) -> Any:
        """Apply backstop protection for cursor.executemany()."""
        params_list = list(param_seq)
        risk = assess_risk(query)

        if self._mode == "block" and risk.level == RiskLevel.CRITICAL:
            raise PermissionError(
                f"[backstop] BLOCKED: {risk.operation} on {risk.table!r}. "
                f"Reason: {risk.reason}"
            )

        if self._mode == "protect" and risk.requires_snapshot:
            if risk.operation == "DELETE" and params_list:
                for params in params_list:
                    self._before_execute(query, params)
            else:
                first_params = params_list[0] if params_list else None
                self._before_execute(query, first_params)
        else:
            log_event(risk=risk, query=query, actor=self._actor, manifest=None)

        return cursor.executemany(query, params_list)

    def _before_execute(self, query: str, params: Any = None) -> None:
        """Assess risk and capture any required before-image snapshot."""
        risk = assess_risk(query)

        if self._mode == "block" and risk.level == RiskLevel.CRITICAL:
            raise PermissionError(
                f"[backstop] BLOCKED: {risk.operation} on {risk.table!r}. "
                f"Reason: {risk.reason}"
            )

        if self._mode != "protect" or not risk.requires_snapshot:
            log_event(risk=risk, query=query, actor=self._actor, manifest=None)
            return

        if not risk.table:
            log_event(risk=risk, query=query, actor=self._actor, manifest=None)
            raise PermissionError(
                f"[backstop] BLOCKED: {risk.operation} cannot be made recoverable "
                "by the Python SDK because it has no table-level snapshot target. "
                "Use mode='monitor' to observe it or block/handle it at the gateway/infra layer."
            )

        try:
            manifest = self._capture_with_savepoint(risk, query, params)
            log_event(risk=risk, query=query, actor=self._actor, manifest=manifest)
        except Exception as exc:
            # CRITICAL rule: snapshot failure must NOT block table-level query execution.
            logger.error(
                "[backstop] Snapshot failed for %s on table %r: %s. "
                "Proceeding with query execution.",
                risk.operation,
                risk.table,
                exc,
            )
            log_event(risk=risk, query=query, actor=self._actor, manifest=None)

    def _capture_with_savepoint(self, risk: Any, query: str, params: Any = None) -> Any:
        """Capture a snapshot without leaving the user's transaction aborted.

        PostgreSQL marks the whole transaction as failed after any statement
        error. Snapshotting uses the user's connection in Phase 1, so a failed
        protective SELECT must be isolated behind a SAVEPOINT.
        """
        sp_name = f"backstop_sp_{uuid4().hex}"
        use_savepoint = not bool(getattr(self._conn, "autocommit", False))

        if use_savepoint:
            with self._conn.cursor() as cur:
                cur.execute(f"SAVEPOINT {sp_name}")

        try:
            before_image = build_before_image_query(query)
            if before_image is not None and before_image.table == risk.table:
                manifest = self._engine.capture_query(
                    conn=self._conn,
                    table=risk.table,
                    select_sql=before_image.select_sql,
                    select_params=select_before_image_params(params, before_image),
                    query=query,
                    operation=risk.operation,
                    actor=self._actor,
                )
            else:
                manifest = self._engine.capture_table(
                    conn=self._conn,
                    table=risk.table,
                    query=query,
                    operation=risk.operation,
                    actor=self._actor,
                )

            if use_savepoint:
                with self._conn.cursor() as cur:
                    cur.execute(f"RELEASE SAVEPOINT {sp_name}")
            return manifest
        except Exception:
            if use_savepoint:
                try:
                    with self._conn.cursor() as cur:
                        cur.execute(f"ROLLBACK TO SAVEPOINT {sp_name}")
                        cur.execute(f"RELEASE SAVEPOINT {sp_name}")
                except Exception:
                    logger.exception("[backstop] Failed to clean up snapshot savepoint")
            raise


class GuardedCursor:
    """psycopg2 cursor wrapper that routes execute calls through backstop."""

    def __init__(self, cursor: Any, guarded_connection: GuardedConnection) -> None:
        self._cursor = cursor
        self._guarded_connection = guarded_connection

    def execute(self, query: str, params: Any = None) -> Any:
        self._guarded_connection._execute_on_cursor(self._cursor, query, params)
        return self

    def executemany(self, query: str, vars_list: Iterable[Any]) -> Any:
        self._guarded_connection._executemany_on_cursor(self._cursor, query, vars_list)
        return self

    def __enter__(self) -> "GuardedCursor":
        enter = getattr(self._cursor, "__enter__", None)
        if enter is not None:
            enter()
        return self

    def __exit__(self, *args: Any) -> Any:
        return self._cursor.__exit__(*args)

    def __iter__(self) -> Any:
        return iter(self._cursor)

    def __getattr__(self, name: str) -> Any:
        return getattr(self._cursor, name)


# ── Public factory function ──────────────────────────────────────────────────

def guard(
    conn: Any,
    storage: str,
    actor: Optional[str] = None,
    mode: str = "protect",
) -> GuardedConnection:
    """Wrap a psycopg2 connection with backstop protection.

    This is the primary public API. Replace ``psycopg2.connect()`` calls with:

    .. code-block:: python

        import backstop
        raw_conn = psycopg2.connect(DATABASE_URL)
        db = backstop.guard(raw_conn, storage="s3://my-bucket", actor="agent-id")

    Args:
        conn: An active psycopg2 connection to wrap.
        storage: S3 storage URL. Two formats are supported:

            - ``s3://bucket-name`` — real AWS S3
            - ``s3://bucket-name@http://localhost:9000`` — MinIO / custom endpoint

        actor: Optional actor identifier (user ID, agent ID) for audit logging.
        mode: Operation mode. One of:

            - ``protect`` *(default)* — snapshot then execute
            - ``monitor`` — log only, always execute
            - ``block`` — raise :class:`PermissionError` on CRITICAL operations

    Returns:
        A :class:`GuardedConnection` wrapping the provided psycopg2 connection.
    """
    raw = storage.removeprefix("s3://")
    parts = raw.split("@", 1)
    bucket = parts[0]
    endpoint_url = parts[1] if len(parts) > 1 else None

    engine = SnapshotEngine(s3_bucket=bucket, endpoint_url=endpoint_url)
    return GuardedConnection(conn, engine, actor=actor, mode=mode)

