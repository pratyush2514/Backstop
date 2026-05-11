"""backstop — production-grade database safety platform.

Public API
----------
guard(conn, storage, actor, mode) → GuardedConnection
    Wrap a psycopg2 connection with backstop protection.

protect_engine(engine, storage, actor, mode)
    Attach backstop protection to a SQLAlchemy Engine.

RiskLevel
    Enum of Python SDK risk classifications: SAFE, LOW, HIGH, CRITICAL.

RiskResult
    Dataclass describing the result of a SQL risk assessment.

Example usage::

    import psycopg2
    import backstop

    raw_conn = psycopg2.connect(DATABASE_URL)
    db = backstop.guard(
        conn=raw_conn,
        storage="s3://my-bucket@http://localhost:9000",
        actor="gpt-agent-prod",
        mode="protect",
    )

    # All SQL executed through db.execute() is now intercepted and protected.
    db.execute("DROP TABLE users")  # Snapshots users → S3, then drops the table
    db.commit()
"""

from .guard import GuardedConnection, GuardedCursor, guard
from .parser import RiskLevel, RiskResult
from .sqlalchemy import protect_engine

__all__ = [
    "guard",
    "GuardedConnection",
    "GuardedCursor",
    "protect_engine",
    "RiskLevel",
    "RiskResult",
]

