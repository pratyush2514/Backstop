"""Manifest compatibility tests for cross-component snapshot writers."""

from __future__ import annotations

import json

from backstop.snapshot import SnapshotManifest


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

