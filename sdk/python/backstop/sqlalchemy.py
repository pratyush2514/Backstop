"""SQLAlchemy integration for backstop.

Use ``protect_engine(engine, storage=...)`` to attach backstop protection to a
SQLAlchemy Engine. The integration hooks SQLAlchemy's ``before_cursor_execute``
event, so existing ``connection.execute(...)`` and ORM Session work that routes
through the Engine is assessed before the DBAPI cursor executes.
"""

from __future__ import annotations

from typing import Any, Optional

from .guard import GuardedConnection
from .snapshot import SnapshotEngine


def protect_engine(
    engine: Any,
    storage: str,
    actor: Optional[str] = None,
    mode: str = "protect",
) -> Any:
    """Attach backstop protection to a SQLAlchemy Engine.

    Args:
        engine: A SQLAlchemy Engine.
        storage: S3 storage URL, same format accepted by ``backstop.guard``.
        actor: Optional actor identifier for audit logging.
        mode: ``protect``, ``monitor``, or ``block``.

    Returns:
        The same Engine, for fluent setup.

    Raises:
        ImportError: If SQLAlchemy is not installed.
    """
    try:
        from sqlalchemy import event
    except ImportError as exc:
        raise ImportError("backstop SQLAlchemy integration requires SQLAlchemy") from exc

    raw = storage.removeprefix("s3://")
    parts = raw.split("@", 1)
    bucket = parts[0]
    endpoint_url = parts[1] if len(parts) > 1 else None
    snapshot_engine = SnapshotEngine(s3_bucket=bucket, endpoint_url=endpoint_url)

    @event.listens_for(engine, "before_cursor_execute")
    def _backstop_before_cursor_execute(
        conn: Any,
        cursor: Any,
        statement: str,
        parameters: Any,
        context: Any,
        executemany: bool,
    ) -> None:
        raw_conn = _raw_dbapi_connection(conn)
        guarded = GuardedConnection(
            conn=raw_conn,
            snapshot_engine=snapshot_engine,
            actor=actor,
            mode=mode,
        )
        if executemany:
            guarded._before_execute(statement, parameters[0] if parameters else None)
        else:
            guarded._before_execute(statement, parameters)

    return engine


def _raw_dbapi_connection(sqlalchemy_connection: Any) -> Any:
    """Best-effort extraction of the underlying DBAPI connection."""
    proxied = sqlalchemy_connection.connection
    driver_connection = getattr(proxied, "driver_connection", None)
    if driver_connection is not None:
        return driver_connection
    dbapi_connection = getattr(proxied, "dbapi_connection", None)
    if dbapi_connection is not None:
        return dbapi_connection
    connection = getattr(proxied, "connection", None)
    if connection is not None:
        return connection
    return proxied

