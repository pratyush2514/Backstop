"""Integration tests for RestoreEngine.

Requires:
    - PostgreSQL running on localhost:5432
    - MinIO running on localhost:9000

Mark: pytest.mark.integration
"""

from __future__ import annotations

import io
import json
import hashlib
from concurrent.futures import ThreadPoolExecutor

import pyarrow as pa
import pyarrow.parquet as pq
import pytest
import psycopg2

from backstop.restore import RestoreEngine
from backstop.snapshot import SnapshotEngine
from tests.conftest import DATABASE_URL

pytestmark = pytest.mark.integration

S3_BUCKET = "backstop-test"
S3_ENDPOINT = "http://localhost:9000"


@pytest.fixture
def snap_engine(s3_client) -> SnapshotEngine:
    """SnapshotEngine pointed at local MinIO."""
    try:
        s3_client.create_bucket(Bucket=S3_BUCKET)
    except Exception:
        pass
    return SnapshotEngine(s3_bucket=S3_BUCKET, endpoint_url=S3_ENDPOINT)


@pytest.fixture
def restore_engine() -> RestoreEngine:
    """RestoreEngine pointed at local MinIO."""
    return RestoreEngine(s3_bucket=S3_BUCKET, endpoint_url=S3_ENDPOINT)


class TestRestoreTable:
    """Tests for RestoreEngine.restore_table()."""

    def test_restore_creates_recovered_table(
        self, pg_conn, snap_engine, restore_engine, users_table
    ) -> None:
        """Restore must create a ``users_recovered`` table with all rows."""
        manifest = snap_engine.capture_table(
            conn=pg_conn,
            table="users",
            query="DROP TABLE users",
            operation="DROP TABLE",
        )

        # Simulate DROP TABLE
        cur = pg_conn.cursor()
        cur.execute("DROP TABLE users CASCADE")
        pg_conn.commit()

        # Restore
        row_count = restore_engine.restore_table(
            conn=pg_conn,
            snapshot_id=manifest.snapshot_id,
            table="users",
        )

        assert row_count == 5

        # Verify recovered table exists and has correct row count
        cur.execute("SELECT COUNT(*) FROM users_recovered")
        assert cur.fetchone()[0] == 5

    def test_restore_recovers_correct_data(
        self, pg_conn, snap_engine, restore_engine, users_table
    ) -> None:
        """Restored rows must match original data."""
        manifest = snap_engine.capture_table(
            conn=pg_conn,
            table="users",
            query="DROP TABLE users",
            operation="DROP TABLE",
        )

        cur = pg_conn.cursor()
        cur.execute("DROP TABLE users CASCADE")
        pg_conn.commit()

        restore_engine.restore_table(
            conn=pg_conn,
            snapshot_id=manifest.snapshot_id,
            table="users",
        )

        cur.execute("SELECT name FROM users_recovered ORDER BY name")
        names = [row[0] for row in cur.fetchall()]
        assert "Alice" in names
        assert "Bob" in names
        assert "Carol" in names
        assert "Dave" in names
        assert "Eve" in names

    def test_restore_default_target_is_recovered_suffix(
        self, pg_conn, snap_engine, restore_engine, users_table
    ) -> None:
        """When no target_table is given, restore target must be ``{table}_recovered``."""
        manifest = snap_engine.capture_table(
            conn=pg_conn,
            table="users",
            query="DROP TABLE users",
            operation="DROP TABLE",
        )
        cur = pg_conn.cursor()
        cur.execute("DROP TABLE users CASCADE")
        pg_conn.commit()

        restore_engine.restore_table(
            conn=pg_conn,
            snapshot_id=manifest.snapshot_id,
            table="users",
            # target_table intentionally omitted
        )

        # users_recovered must exist
        cur.execute("SELECT to_regclass('users_recovered')")
        assert cur.fetchone()[0] is not None

        # Original users table must NOT exist
        cur.execute("SELECT to_regclass('users')")
        assert cur.fetchone()[0] is None

    def test_restore_explicit_target_table(
        self, pg_conn, snap_engine, restore_engine, users_table
    ) -> None:
        """Restore with explicit target_table must use that table name."""
        manifest = snap_engine.capture_table(
            conn=pg_conn,
            table="users",
            query="DROP TABLE users",
            operation="DROP TABLE",
        )
        cur = pg_conn.cursor()
        cur.execute("DROP TABLE users CASCADE")
        pg_conn.commit()

        row_count = restore_engine.restore_table(
            conn=pg_conn,
            snapshot_id=manifest.snapshot_id,
            table="users",
            target_table="users_backup",
        )

        assert row_count == 5

        cur.execute("SELECT COUNT(*) FROM users_backup")
        assert cur.fetchone()[0] == 5

        # Clean up the extra table
        cur.execute("DROP TABLE IF EXISTS users_backup CASCADE")
        pg_conn.commit()

    def test_restore_is_idempotent(
        self, pg_conn, snap_engine, restore_engine, users_table
    ) -> None:
        """Running restore twice must not produce duplicate rows."""
        manifest = snap_engine.capture_table(
            conn=pg_conn,
            table="users",
            query="DROP TABLE users",
            operation="DROP TABLE",
        )
        cur = pg_conn.cursor()
        cur.execute("DROP TABLE users CASCADE")
        pg_conn.commit()

        restore_engine.restore_table(
            conn=pg_conn,
            snapshot_id=manifest.snapshot_id,
            table="users",
        )

        # Run restore a second time — must not raise, must not duplicate rows
        row_count = restore_engine.restore_table(
            conn=pg_conn,
            snapshot_id=manifest.snapshot_id,
            table="users",
        )

        cur.execute("SELECT COUNT(*) FROM users_recovered")
        total = cur.fetchone()[0]
        assert total == 5, f"Expected 5 rows after idempotent restore, got {total}"

    def test_restore_raises_on_missing_snapshot(
        self, pg_conn, restore_engine
    ) -> None:
        """Restore must raise RuntimeError when the snapshot does not exist."""
        with pytest.raises(RuntimeError, match="Restore failed"):
            restore_engine.restore_table(
                conn=pg_conn,
                snapshot_id="snap_00000000",
                table="nonexistent_table",
            )

    def test_restore_returns_correct_row_count(
        self, pg_conn, snap_engine, restore_engine, users_table
    ) -> None:
        """restore_table() must return the number of rows restored."""
        manifest = snap_engine.capture_table(
            conn=pg_conn,
            table="users",
            query="DROP TABLE users",
            operation="DROP TABLE",
        )
        cur = pg_conn.cursor()
        cur.execute("DROP TABLE users CASCADE")
        pg_conn.commit()

        row_count = restore_engine.restore_table(
            conn=pg_conn,
            snapshot_id=manifest.snapshot_id,
            table="users",
        )

        assert row_count == 5

    def test_preview_restore_does_not_create_table(
        self, pg_conn, snap_engine, restore_engine, users_table
    ) -> None:
        """preview_restore() must report restore effects without mutation."""
        manifest = snap_engine.capture_table(
            conn=pg_conn,
            table="users",
            query="DROP TABLE users",
            operation="DROP TABLE",
        )

        cur = pg_conn.cursor()
        cur.execute("DROP TABLE users CASCADE")
        pg_conn.commit()

        preview = restore_engine.preview_restore(
            conn=pg_conn,
            snapshot_id=manifest.snapshot_id,
            table="users",
        )

        assert preview.snapshot_id == manifest.snapshot_id
        assert preview.target_table == "users_recovered"
        assert preview.row_count == 5
        assert preview.will_create_table is True

        cur.execute("SELECT to_regclass('users_recovered')")
        assert cur.fetchone()[0] is None

    def test_restore_resets_existing_table_sequence(
        self, pg_conn, snap_engine, restore_engine, users_table
    ) -> None:
        """Restoring explicit IDs into an existing table must advance its sequence."""
        manifest = snap_engine.capture_query(
            conn=pg_conn,
            table="users",
            select_sql="SELECT * FROM users WHERE id = %s",
            select_params=(5,),
            query="DELETE FROM users WHERE id = %s",
            operation="DELETE",
            actor="test-agent",
        )

        cur = pg_conn.cursor()
        cur.execute("DELETE FROM users WHERE id = 5")
        pg_conn.commit()

        restored = restore_engine.restore_table(
            conn=pg_conn,
            snapshot_id=manifest.snapshot_id,
            table="users",
            target_table="users",
        )
        assert restored == 1

        cur.execute("INSERT INTO users (name, email) VALUES (%s, %s) RETURNING id", ("Frank", "frank@example.com"))
        new_id = cur.fetchone()[0]
        assert new_id > 5

    def test_restore_conflict_policy_fail_raises(
        self, pg_conn, snap_engine, restore_engine, users_table
    ) -> None:
        """conflict_policy='fail' should surface duplicate key conflicts."""
        manifest = snap_engine.capture_query(
            conn=pg_conn,
            table="users",
            select_sql="SELECT * FROM users WHERE id = %s",
            select_params=(1,),
            query="DELETE FROM users WHERE id = %s",
            operation="DELETE",
        )

        with pytest.raises(RuntimeError, match="duplicate key"):
            restore_engine.restore_table(
                conn=pg_conn,
                snapshot_id=manifest.snapshot_id,
                table="users",
                target_table="users",
                conflict_policy="fail",
            )

    def test_restore_conflict_policy_overwrite_updates_existing_row(
        self, pg_conn, snap_engine, restore_engine, users_table
    ) -> None:
        """conflict_policy='overwrite' should restore before-image values."""
        manifest = snap_engine.capture_query(
            conn=pg_conn,
            table="users",
            select_sql="SELECT * FROM users WHERE id = %s",
            select_params=(1,),
            query="UPDATE users SET name = %s WHERE id = %s",
            operation="UPDATE",
        )

        cur = pg_conn.cursor()
        cur.execute("UPDATE users SET name = %s WHERE id = %s", ("Alicia", 1))
        pg_conn.commit()

        restored = restore_engine.restore_table(
            conn=pg_conn,
            snapshot_id=manifest.snapshot_id,
            table="users",
            target_table="users",
            conflict_policy="overwrite",
        )
        assert restored == 1

        cur.execute("SELECT name FROM users WHERE id = 1")
        assert cur.fetchone()[0] == "Alice"

    def test_restore_accepts_sidecar_written_manifest(
        self, pg_conn, s3_client, restore_engine, users_table
    ) -> None:
        """Python restore must consume the Phase 2 sidecar manifest contract."""
        snapshot_id = "snap_sidecar1"
        data_key = f"backstop/snapshots/users/{snapshot_id}/data.parquet"
        manifest_key = f"backstop/snapshots/users/{snapshot_id}/manifest.json"

        table = pa.table(
            {
                "id": ["1", "2"],
                "name": ["Alice", "Bob"],
                "email": ["alice@example.com", "bob@example.com"],
            }
        )
        buf = io.BytesIO()
        pq.write_table(table, buf)
        data = buf.getvalue()
        s3_client.put_object(
            Bucket=S3_BUCKET,
            Key=data_key,
            Body=data,
            ContentType="application/octet-stream",
        )

        manifest = {
            "manifest_version": 1,
            "writer": "sync-sidecar",
            "db_name": "testdb",
            "schema_name": "public",
            "snapshot_id": snapshot_id,
            "timestamp": "2026-05-01T00:00:00Z",
            "table_name": "users",
            "snapshot_scope": "table",
            "operation": "SYNC_SNAPSHOT",
            "query": "SYNC SNAPSHOT public.users",
            "actor": None,
            "row_count": 2,
            "schema_ddl": (
                'CREATE TABLE "users" ('
                '"id" INTEGER PRIMARY KEY, '
                '"name" VARCHAR(100) NOT NULL, '
                '"email" VARCHAR(255) UNIQUE NOT NULL)'
            ),
            "fk_constraints": [],
            "check_constraints": [],
            "indexes": [],
            "s3_bucket": S3_BUCKET,
            "s3_data_key": data_key,
            "s3_manifest_key": manifest_key,
            "data_sha256": hashlib.sha256(data).hexdigest(),
            "status": "valid",
        }
        s3_client.put_object(
            Bucket=S3_BUCKET,
            Key=manifest_key,
            Body=json.dumps(manifest).encode("utf-8"),
            ContentType="application/json",
        )

        cur = pg_conn.cursor()
        cur.execute("DROP TABLE users CASCADE")
        pg_conn.commit()

        restored = restore_engine.restore_table(
            conn=pg_conn,
            snapshot_id=snapshot_id,
            table="users",
        )

        assert restored == 2
        cur.execute("SELECT id, name, email FROM users_recovered ORDER BY id")
        assert cur.fetchall() == [
            (1, "Alice", "alice@example.com"),
            (2, "Bob", "bob@example.com"),
        ]

    def test_restore_rejects_corrupt_snapshot_object(
        self, pg_conn, s3_client, snap_engine, restore_engine, users_table
    ) -> None:
        """Restore must fail closed when the snapshot object checksum does not match."""
        manifest = snap_engine.capture_table(
            conn=pg_conn,
            table="users",
            query="DROP TABLE users",
            operation="DROP TABLE",
        )
        s3_client.put_object(
            Bucket=S3_BUCKET,
            Key=manifest.s3_data_key,
            Body=b"not the captured parquet",
            ContentType="application/octet-stream",
        )

        with pytest.raises(RuntimeError, match="SHA256 mismatch"):
            restore_engine.restore_table(
                conn=pg_conn,
                snapshot_id=manifest.snapshot_id,
                table="users",
            )

        cur = pg_conn.cursor()
        cur.execute("SELECT to_regclass('users_recovered')")
        assert cur.fetchone()[0] is None

    def test_restore_fails_closed_when_snapshot_object_is_missing(
        self, pg_conn, s3_client, snap_engine, restore_engine, users_table
    ) -> None:
        """Restore-time object read failure must roll back the restore transaction."""
        manifest = snap_engine.capture_table(
            conn=pg_conn,
            table="users",
            query="DROP TABLE users",
            operation="DROP TABLE",
        )
        s3_client.delete_object(Bucket=S3_BUCKET, Key=manifest.s3_data_key)

        with pytest.raises(RuntimeError, match="Restore failed and was rolled back"):
            restore_engine.restore_table(
                conn=pg_conn,
                snapshot_id=manifest.snapshot_id,
                table="users",
            )

        cur = pg_conn.cursor()
        cur.execute("SELECT to_regclass('users_recovered')")
        assert cur.fetchone()[0] is None

    def test_concurrent_restore_same_target_serializes_without_duplicates(
        self, pg_conn, snap_engine, restore_engine, users_table
    ) -> None:
        """Concurrent restores into one target should be serialized by advisory lock."""
        with pg_conn.cursor() as cur:
            cur.execute("DROP TABLE IF EXISTS users_concurrent_restore CASCADE")
        pg_conn.commit()

        manifest = snap_engine.capture_table(
            conn=pg_conn,
            table="users",
            query="DROP TABLE users",
            operation="DROP TABLE",
        )
        pg_conn.commit()

        def restore_once() -> int:
            conn = psycopg2.connect(DATABASE_URL)
            conn.autocommit = False
            try:
                engine = RestoreEngine(s3_bucket=S3_BUCKET, endpoint_url=S3_ENDPOINT)
                return engine.restore_table(
                    conn=conn,
                    snapshot_id=manifest.snapshot_id,
                    table="users",
                    target_table="users_concurrent_restore",
                    conflict_policy="skip",
                )
            finally:
                conn.close()

        with ThreadPoolExecutor(max_workers=2) as pool:
            results = list(pool.map(lambda _: restore_once(), range(2)))

        try:
            assert sorted(results) == [0, 5]
            with pg_conn.cursor() as cur:
                cur.execute("SELECT COUNT(*) FROM users_concurrent_restore")
                assert cur.fetchone()[0] == 5
        finally:
            with pg_conn.cursor() as cur:
                cur.execute("DROP TABLE IF EXISTS users_concurrent_restore CASCADE")
            pg_conn.commit()

