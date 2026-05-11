"""Manifest compatibility tests for cross-component snapshot writers."""

from __future__ import annotations

import json

from backstop.snapshot import SnapshotManifest
from backstop.restore import RestoreEngine


def test_sidecar_manifest_contract_validates() -> None:
    raw = {
        "manifest_version": 1,
        "writer": "sync-sidecar",
        "db_name": "testdb",
        "schema_name": "public",
        "snapshot_id": "snap_1234abcd",
        "timestamp": "2026-05-01T00:00:00Z",
        "table_name": "users",
        "snapshot_scope": "table",
        "operation": "SYNC_SNAPSHOT",
        "query": "SYNC SNAPSHOT public.users",
        "actor": None,
        "row_count": 2,
        "schema_ddl": 'CREATE TABLE "users" ("id" INTEGER PRIMARY KEY)',
        "fk_constraints": [],
        "check_constraints": [],
        "indexes": [],
        "s3_bucket": "backstop-test",
        "s3_data_key": "backstop/snapshots/users/snap_1234abcd/data.parquet",
        "s3_manifest_key": "backstop/snapshots/users/snap_1234abcd/manifest.json",
    }

    manifest = SnapshotManifest.model_validate_json(json.dumps(raw))

    assert manifest.manifest_version == 1
    assert manifest.writer == "sync-sidecar"
    assert manifest.db_name == "testdb"
    assert manifest.schema_name == "public"
    assert manifest.snapshot_scope == "table"
    assert manifest.status == "valid"


def test_legacy_python_manifest_defaults_new_contract_fields() -> None:
    raw = {
        "snapshot_id": "snap_1234abcd",
        "timestamp": "2026-05-01T00:00:00Z",
        "table_name": "users",
        "query": "DROP TABLE users",
        "operation": "DROP TABLE",
        "actor": None,
        "row_count": 2,
        "schema_ddl": 'CREATE TABLE "users" ("id" INTEGER PRIMARY KEY)',
        "fk_constraints": [],
        "s3_bucket": "backstop-test",
        "s3_data_key": "backstop/snapshots/users/snap_1234abcd/data.parquet",
        "s3_manifest_key": "backstop/snapshots/users/snap_1234abcd/manifest.json",
    }

    manifest = SnapshotManifest.model_validate_json(json.dumps(raw))

    assert manifest.manifest_version == 1
    assert manifest.writer == "python-sdk"
    assert manifest.db_name == "unknown"
    assert manifest.schema_name == "public"
    assert manifest.snapshot_scope == "table"
    assert manifest.indexes == []
    assert manifest.check_constraints == []


def test_restore_rejects_unverifiable_manifest_contract() -> None:
    engine = RestoreEngine(s3_bucket="backstop-test", endpoint_url="http://localhost:9000")
    manifest = SnapshotManifest(
        snapshot_id="snap_bad",
        timestamp="2026-05-01T00:00:00Z",
        table_name="users",
        query="SYNC SNAPSHOT public.users",
        operation="SYNC_SNAPSHOT",
        actor=None,
        row_count=1,
        schema_ddl='CREATE TABLE "users" ("id" INTEGER PRIMARY KEY)',
        fk_constraints=[],
        s3_bucket="backstop-test",
        s3_data_key="backstop/snapshots/users/snap_bad/data.parquet",
        s3_manifest_key="backstop/snapshots/users/snap_bad/manifest.json",
        data_sha256=None,
        status="valid",
    )

    try:
        engine._validate_manifest_for_restore(manifest, "users")
    except RuntimeError as exc:
        assert "missing data_sha256" in str(exc)
    else:
        raise AssertionError("missing checksum manifest should be rejected")

    manifest.data_sha256 = "a" * 64
    manifest.status = "quarantined"
    try:
        engine._validate_manifest_for_restore(manifest, "users")
    except RuntimeError as exc:
        assert "not valid" in str(exc)
    else:
        raise AssertionError("quarantined manifest should be rejected")

