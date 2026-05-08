"""End-to-end integration test for the complete backstop guard → snapshot → restore cycle.

Tests the full user workflow:
    1. Wrap a psycopg2 connection with backstop.guard()
    2. Execute a DROP TABLE through the guard (triggers automatic snapshot)
    3. Verify the table is gone
    4. Locate the snapshot
    5. Restore via RestoreEngine
    6. Verify data is back

Requires:
    - PostgreSQL running on localhost:5432
    - MinIO running on localhost:9000

Mark: pytest.mark.integration
"""

from __future__ import annotations

import pytest

import backstop
from backstop.restore import RestoreEngine
from backstop.snapshot import SnapshotEngine

pytestmark = pytest.mark.integration

S3_BUCKET = "backstop-test"
S3_ENDPOINT = "http://localhost:9000"
STORAGE_URL = f"s3://{S3_BUCKET}@{S3_ENDPOINT}"


class TestFullGuardDropRestoreCycle:
    """Full guard → DROP TABLE → restore cycle."""

    def test_guard_protect_mode_drop_and_restore(
        self, pg_conn, s3_client, users_table
    ) -> None:
        """Complete cycle: guard intercepts DROP TABLE, snapshots, drops, restores."""
        # Ensure bucket exists
        try:
            s3_client.create_bucket(Bucket=S3_BUCKET)
        except Exception:
            pass

        # Step 1: wrap the connection
        db = backstop.guard(
            conn=pg_conn,
            storage=STORAGE_URL,
            actor="test-agent",
            mode="protect",
        )

        # Step 2: execute DROP TABLE through the guard
        # This should: snapshot users → S3, then DROP the table
        db.execute("DROP TABLE users")
        db.commit()

        # Step 3: verify the table is gone
        cur = pg_conn.cursor()
        cur.execute("SELECT to_regclass('users')")
        result = cur.fetchone()[0]
        assert result is None, f"Expected users table to be gone, got: {result}"

        # Step 4: find the snapshot
        engine = SnapshotEngine(s3_bucket=S3_BUCKET, endpoint_url=S3_ENDPOINT)
        snapshots = engine.list_snapshots(table="users")
        assert len(snapshots) >= 1, "Expected at least one snapshot for users table"

        latest = snapshots[0]
        assert latest.snapshot_id.startswith("snap_")
        assert latest.row_count == 5
        assert latest.operation == "DROP TABLE"
        assert latest.actor == "test-agent"

        # Step 5: restore
        restore = RestoreEngine(s3_bucket=S3_BUCKET, endpoint_url=S3_ENDPOINT)
        row_count = restore.restore_table(
            conn=pg_conn,
            snapshot_id=latest.snapshot_id,
            table="users",
        )

        assert row_count == 5

        # Step 6: verify data is back
        cur.execute("SELECT COUNT(*) FROM users_recovered")
        assert cur.fetchone()[0] == 5

        cur.execute("SELECT name FROM users_recovered ORDER BY name")
        names = [row[0] for row in cur.fetchall()]
        assert names == ["Alice", "Bob", "Carol", "Dave", "Eve"]

    def test_cursor_execute_is_protected(
        self, pg_conn, s3_client, users_table
    ) -> None:
        """conn.cursor().execute() must be protected, not a raw bypass."""
        try:
            s3_client.create_bucket(Bucket=S3_BUCKET)
        except Exception:
            pass

        engine = SnapshotEngine(s3_bucket=S3_BUCKET, endpoint_url=S3_ENDPOINT)
        before_count = len(engine.list_snapshots(table="users"))

        db = backstop.guard(
            conn=pg_conn,
            storage=STORAGE_URL,
            actor="cursor-agent",
            mode="protect",
        )

        cur = db.cursor()
        cur.execute("DROP TABLE users")
        db.commit()

        after_count = len(engine.list_snapshots(table="users"))
        assert after_count == before_count + 1

        cur = pg_conn.cursor()
        cur.execute("SELECT to_regclass('users')")
        assert cur.fetchone()[0] is None

    def test_delete_where_captures_scoped_before_image(
        self, pg_conn, s3_client, users_table
    ) -> None:
        """Scoped DELETE must snapshot only affected rows before execution."""
        try:
            s3_client.create_bucket(Bucket=S3_BUCKET)
        except Exception:
            pass

        db = backstop.guard(
            conn=pg_conn,
            storage=STORAGE_URL,
            actor="scoped-delete-agent",
            mode="protect",
        )

        db.execute("DELETE FROM users WHERE id = %s", (1,))
        db.commit()

        cur = pg_conn.cursor()
        cur.execute("SELECT COUNT(*) FROM users")
        assert cur.fetchone()[0] == 4

        engine = SnapshotEngine(s3_bucket=S3_BUCKET, endpoint_url=S3_ENDPOINT)
        latest = engine.list_snapshots(table="users")[0]
        assert latest.operation == "DELETE"
        assert latest.snapshot_scope == "rows"
        assert latest.row_count == 1

        restore = RestoreEngine(s3_bucket=S3_BUCKET, endpoint_url=S3_ENDPOINT)
        restored = restore.restore_table(
            conn=pg_conn,
            snapshot_id=latest.snapshot_id,
            table="users",
        )
        assert restored == 1

        cur.execute("SELECT name FROM users_recovered")
        assert cur.fetchone()[0] == "Alice"

    def test_update_where_captures_scoped_before_image(
        self, pg_conn, s3_client, users_table
    ) -> None:
        """Scoped UPDATE must snapshot affected rows before mutation."""
        try:
            s3_client.create_bucket(Bucket=S3_BUCKET)
        except Exception:
            pass

        db = backstop.guard(
            conn=pg_conn,
            storage=STORAGE_URL,
            actor="scoped-update-agent",
            mode="protect",
        )

        db.execute("UPDATE users SET name = %s WHERE id = %s", ("Alicia", 1))
        db.commit()

        cur = pg_conn.cursor()
        cur.execute("SELECT name FROM users WHERE id = 1")
        assert cur.fetchone()[0] == "Alicia"

        engine = SnapshotEngine(s3_bucket=S3_BUCKET, endpoint_url=S3_ENDPOINT)
        latest = engine.list_snapshots(table="users")[0]
        assert latest.operation == "UPDATE"
        assert latest.snapshot_scope == "rows"
        assert latest.row_count == 1

        restore = RestoreEngine(s3_bucket=S3_BUCKET, endpoint_url=S3_ENDPOINT)
        restored = restore.restore_table(
            conn=pg_conn,
            snapshot_id=latest.snapshot_id,
            table="users",
        )
        assert restored == 1

        cur.execute("SELECT name FROM users_recovered")
        assert cur.fetchone()[0] == "Alice"

    def test_protect_mode_blocks_unrecoverable_drop_database(
        self, pg_conn, s3_client, users_table
    ) -> None:
        """Protect mode must not pretend DROP DATABASE is recoverable."""
        db = backstop.guard(
            conn=pg_conn,
            storage=STORAGE_URL,
            actor="db-drop-agent",
            mode="protect",
        )

        with pytest.raises(PermissionError, match="cannot be made recoverable"):
            db.execute("DROP DATABASE dangerous_prod")

    def test_sqlalchemy_engine_blocks_unrecoverable_drop_database(
        self, pg_conn, s3_client, users_table
    ) -> None:
        """SQLAlchemy engine integration must run backstop before execution."""
        sqlalchemy = pytest.importorskip("sqlalchemy")
        from sqlalchemy import create_engine, text

        from backstop.sqlalchemy import protect_engine
        from tests.conftest import DATABASE_URL

        engine = create_engine(DATABASE_URL)
        protect_engine(
            engine,
            storage=STORAGE_URL,
            actor="sqlalchemy-agent",
            mode="protect",
        )

        with engine.connect() as conn:
            with pytest.raises(PermissionError, match="cannot be made recoverable"):
                conn.execute(text("DROP DATABASE dangerous_prod"))

    def test_guard_monitor_mode_does_not_snapshot(
        self, pg_conn, s3_client, users_table
    ) -> None:
        """Monitor mode must log but NOT create a snapshot."""
        try:
            s3_client.create_bucket(Bucket=S3_BUCKET)
        except Exception:
            pass

        engine = SnapshotEngine(s3_bucket=S3_BUCKET, endpoint_url=S3_ENDPOINT)
        before_count = len(engine.list_snapshots(table="users"))

        db = backstop.guard(
            conn=pg_conn,
            storage=STORAGE_URL,
            actor="monitor-agent",
            mode="monitor",
        )

        # Execute a dangerous query in monitor mode — no snapshot should be taken
        db.execute("DELETE FROM users WHERE id = 1")
        db.commit()

        after_count = len(engine.list_snapshots(table="users"))
        assert after_count == before_count, (
            f"Monitor mode must not create snapshots, but count went from "
            f"{before_count} to {after_count}"
        )

    def test_guard_block_mode_raises_on_critical(
        self, pg_conn, s3_client, users_table
    ) -> None:
        """Block mode must raise PermissionError for CRITICAL operations."""
        try:
            s3_client.create_bucket(Bucket=S3_BUCKET)
        except Exception:
            pass

        db = backstop.guard(
            conn=pg_conn,
            storage=STORAGE_URL,
            actor="blocked-agent",
            mode="block",
        )

        with pytest.raises(PermissionError, match=r"\[backstop\] BLOCKED"):
            db.execute("DROP TABLE users")

        # Table must still exist — block prevented execution
        cur = pg_conn.cursor()
        cur.execute("SELECT to_regclass('users')")
        assert cur.fetchone()[0] is not None

    def test_guard_block_mode_allows_safe_queries(
        self, pg_conn, s3_client, users_table
    ) -> None:
        """Block mode must allow SAFE queries to execute normally."""
        try:
            s3_client.create_bucket(Bucket=S3_BUCKET)
        except Exception:
            pass

        db = backstop.guard(
            conn=pg_conn,
            storage=STORAGE_URL,
            mode="block",
        )

        cur = db.execute("SELECT COUNT(*) FROM users")
        count = cur.fetchone()[0]
        assert count == 5

    def test_guard_snapshot_failure_does_not_block_query(
        self, pg_conn, users_table
    ) -> None:
        """If the snapshot fails (bad S3 endpoint), the query must still execute."""
        # Use a bad endpoint to force snapshot failure
        db = backstop.guard(
            conn=pg_conn,
            storage="s3://backstop-test@http://localhost:19999",  # bad port
            actor="test-agent",
            mode="protect",
        )

        # The snapshot will fail (bad endpoint), but DELETE must still execute
        db.execute("DELETE FROM users WHERE id = 1")
        db.commit()

        cur = pg_conn.cursor()
        cur.execute("SELECT COUNT(*) FROM users")
        count = cur.fetchone()[0]
        # Row with id=1 should be gone despite snapshot failure
        assert count == 4

    def test_guard_passthrough_for_select(
        self, pg_conn, s3_client, users_table
    ) -> None:
        """SELECT statements must pass through unchanged with no snapshot."""
        try:
            s3_client.create_bucket(Bucket=S3_BUCKET)
        except Exception:
            pass

        db = backstop.guard(conn=pg_conn, storage=STORAGE_URL, mode="protect")

        cur = db.execute("SELECT name, email FROM users ORDER BY name")
        rows = cur.fetchall()
        assert len(rows) == 5
        assert rows[0][0] == "Alice"

    def test_guard_context_manager(
        self, pg_conn, s3_client, users_table
    ) -> None:
        """GuardedConnection must work as a context manager."""
        try:
            s3_client.create_bucket(Bucket=S3_BUCKET)
        except Exception:
            pass

        with backstop.guard(conn=pg_conn, storage=STORAGE_URL, mode="monitor") as db:
            cur = db.execute("SELECT COUNT(*) FROM users")
            count = cur.fetchone()[0]

        assert count == 5

    def test_guard_truncate_triggers_snapshot(
        self, pg_conn, s3_client, users_table
    ) -> None:
        """TRUNCATE TABLE must trigger a snapshot in protect mode."""
        try:
            s3_client.create_bucket(Bucket=S3_BUCKET)
        except Exception:
            pass

        engine = SnapshotEngine(s3_bucket=S3_BUCKET, endpoint_url=S3_ENDPOINT)
        before_count = len(engine.list_snapshots(table="users"))

        db = backstop.guard(conn=pg_conn, storage=STORAGE_URL, mode="protect")
        db.execute("TRUNCATE TABLE users")
        db.commit()

        after_count = len(engine.list_snapshots(table="users"))
        assert after_count == before_count + 1, (
            f"Expected one new snapshot after TRUNCATE, "
            f"before={before_count} after={after_count}"
        )

