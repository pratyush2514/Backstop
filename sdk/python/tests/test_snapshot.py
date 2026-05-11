"""Integration tests for SnapshotEngine.

Requires:
    - PostgreSQL running on localhost:5432
    - MinIO running on localhost:9000

Mark: pytest.mark.integration — skipped automatically if services are unavailable.
"""

from __future__ import annotations

import io
from botocore.exceptions import ClientError

import pyarrow.parquet as pq
import pytest

from backstop.snapshot import SnapshotEngine

pytestmark = pytest.mark.integration

S3_BUCKET = "backstop-test"
S3_ENDPOINT = "http://localhost:9000"


@pytest.fixture
def engine(s3_client) -> SnapshotEngine:
    """SnapshotEngine pointed at local MinIO."""
    # Ensure the bucket exists before each test
    try:
        s3_client.create_bucket(Bucket=S3_BUCKET)
    except Exception:
        pass
    return SnapshotEngine(
        s3_bucket=S3_BUCKET,
        endpoint_url=S3_ENDPOINT,
    )


class TestCaptureTable:
    """Tests for SnapshotEngine.capture_table()."""

    def test_capture_returns_manifest(self, pg_conn, engine, users_table) -> None:
        """Capture must return a SnapshotManifest with correct metadata."""
        manifest = engine.capture_table(
            conn=pg_conn,
            table="users",
            query="DROP TABLE users",
            operation="DROP TABLE",
            actor="test-agent",
        )

        assert manifest.snapshot_id.startswith("snap_")
        assert len(manifest.snapshot_id) == 37  # "snap_" + 32 hex chars
        assert manifest.row_count == 5
        assert manifest.table_name == "users"
        assert manifest.operation == "DROP TABLE"
        assert manifest.actor == "test-agent"
        assert manifest.s3_bucket == S3_BUCKET

    def test_capture_schema_ddl_captured(self, pg_conn, engine, users_table) -> None:
        """Captured manifest must include a CREATE TABLE DDL."""
        manifest = engine.capture_table(
            conn=pg_conn,
            table="users",
            query="TRUNCATE users",
            operation="TRUNCATE TABLE",
        )

        assert "CREATE TABLE" in manifest.schema_ddl
        assert "users" in manifest.schema_ddl

    def test_capture_produces_valid_parquet(self, pg_conn, s3_client, engine, users_table) -> None:
        """The uploaded Parquet file must be readable and have the correct row count."""
        manifest = engine.capture_table(
            conn=pg_conn,
            table="users",
            query="DROP TABLE users",
            operation="DROP TABLE",
        )

        response = s3_client.get_object(Bucket=S3_BUCKET, Key=manifest.s3_data_key)
        data_bytes = response["Body"].read()

        arrow_table = pq.read_table(io.BytesIO(data_bytes))
        assert arrow_table.num_rows == 5

    def test_capture_uploads_manifest_json(self, pg_conn, s3_client, engine, users_table) -> None:
        """A manifest.json must be uploaded to S3 alongside the Parquet file."""
        manifest = engine.capture_table(
            conn=pg_conn,
            table="users",
            query="DELETE FROM users",
            operation="DELETE",
        )

        # Verify manifest.json exists in S3
        response = s3_client.get_object(Bucket=S3_BUCKET, Key=manifest.s3_manifest_key)
        raw = response["Body"].read()
        assert len(raw) > 0

        from backstop.snapshot import SnapshotManifest
        retrieved = SnapshotManifest.model_validate_json(raw)
        assert retrieved.snapshot_id == manifest.snapshot_id
        assert retrieved.row_count == 5

    def test_capture_s3_key_format(self, pg_conn, engine, users_table) -> None:
        """S3 keys must follow the expected pattern."""
        manifest = engine.capture_table(
            conn=pg_conn,
            table="users",
            query="DROP TABLE users",
            operation="DROP TABLE",
        )

        assert f"/snapshots/users/{manifest.snapshot_id}/data.parquet" in manifest.s3_data_key
        assert f"/snapshots/users/{manifest.snapshot_id}/manifest.json" in manifest.s3_manifest_key

    def test_capture_empty_table(self, pg_conn, engine, users_table) -> None:
        """Capturing an empty table must produce a manifest with row_count=0."""
        cur = pg_conn.cursor()
        cur.execute("DELETE FROM users")
        pg_conn.commit()

        manifest = engine.capture_table(
            conn=pg_conn,
            table="users",
            query="DROP TABLE users",
            operation="DROP TABLE",
        )

        assert manifest.row_count == 0

    def test_capture_cleans_up_data_when_manifest_write_fails(
        self, pg_conn, s3_client, engine, users_table, monkeypatch
    ) -> None:
        """Manifest write interruption must not leave eligible orphaned data."""
        original_put_object = engine._s3.put_object
        deleted: list[str] = []

        def fail_manifest_put(*args, **kwargs):
            key = kwargs.get("Key")
            if key and key.endswith("/manifest.json"):
                raise RuntimeError("injected manifest write failure")
            return original_put_object(*args, **kwargs)

        original_delete = engine._s3.delete_object

        def record_delete(*args, **kwargs):
            deleted.append(kwargs["Key"])
            return original_delete(*args, **kwargs)

        monkeypatch.setattr(engine._s3, "put_object", fail_manifest_put)
        monkeypatch.setattr(engine._s3, "delete_object", record_delete)

        with pytest.raises(RuntimeError, match="injected manifest write failure"):
            engine.capture_table(
                conn=pg_conn,
                table="users",
                query="DROP TABLE users",
                operation="DROP TABLE",
            )

        assert any(key.endswith("/data.parquet") for key in deleted)
        for key in deleted:
            with pytest.raises(ClientError):
                s3_client.get_object(Bucket=S3_BUCKET, Key=key)

    def test_capture_fails_closed_when_uploaded_data_verification_fails(
        self, pg_conn, engine, users_table, monkeypatch
    ) -> None:
        """Uploaded data corruption before manifest publication must abort capture."""
        monkeypatch.setattr(
            engine,
            "_verify_uploaded_data",
            lambda *_args, **_kwargs: (_ for _ in ()).throw(RuntimeError("injected checksum mismatch")),
        )

        with pytest.raises(RuntimeError, match="injected checksum mismatch"):
            engine.capture_table(
                conn=pg_conn,
                table="users",
                query="DROP TABLE users",
                operation="DROP TABLE",
            )

    def test_capture_records_secondary_indexes(self, pg_conn, engine, users_table) -> None:
        """Snapshot metadata should preserve non-primary index DDL."""
        cur = pg_conn.cursor()
        cur.execute("CREATE INDEX idx_users_name ON users (name)")
        pg_conn.commit()

        manifest = engine.capture_table(
            conn=pg_conn,
            table="users",
            query="DROP TABLE users",
            operation="DROP TABLE",
        )

        assert any("idx_users_name" in ddl for ddl in manifest.indexes)


class TestListSnapshots:
    """Tests for SnapshotEngine.list_snapshots()."""

    def test_list_returns_all_snapshots(self, pg_conn, engine, users_table) -> None:
        """list_snapshots() must return all captured manifests for a table."""
        engine.capture_table(conn=pg_conn, table="users", query="DROP TABLE users", operation="DROP TABLE")
        engine.capture_table(conn=pg_conn, table="users", query="TRUNCATE users", operation="TRUNCATE TABLE")

        snapshots = engine.list_snapshots(table="users")
        assert len(snapshots) >= 2

    def test_list_sorted_by_timestamp_desc(self, pg_conn, engine, users_table) -> None:
        """Snapshots must be returned most-recent-first."""
        engine.capture_table(conn=pg_conn, table="users", query="DROP TABLE users", operation="DROP TABLE")
        engine.capture_table(conn=pg_conn, table="users", query="TRUNCATE users", operation="TRUNCATE TABLE")

        snapshots = engine.list_snapshots(table="users")
        timestamps = [m.timestamp for m in snapshots]
        assert timestamps == sorted(timestamps, reverse=True)

    def test_list_filtered_by_table(self, pg_conn, engine, users_table) -> None:
        """list_snapshots(table='users') must only return snapshots for users."""
        engine.capture_table(conn=pg_conn, table="users", query="DROP TABLE users", operation="DROP TABLE")
        snapshots = engine.list_snapshots(table="users")
        for m in snapshots:
            assert m.table_name == "users"


class TestGetManifest:
    """Tests for SnapshotEngine.get_manifest()."""

    def test_get_manifest_returns_correct_manifest(self, pg_conn, engine, users_table) -> None:
        """get_manifest() must return the exact manifest that was captured."""
        created = engine.capture_table(
            conn=pg_conn, table="users", query="DROP TABLE users", operation="DROP TABLE"
        )
        retrieved = engine.get_manifest(snapshot_id=created.snapshot_id, table="users")
        assert retrieved.snapshot_id == created.snapshot_id
        assert retrieved.row_count == created.row_count

    def test_get_manifest_raises_keyerror_for_missing(self, engine) -> None:
        """get_manifest() must raise KeyError for a non-existent snapshot."""
        with pytest.raises(KeyError):
            engine.get_manifest(snapshot_id="snap_00000000", table="nonexistent_table")

