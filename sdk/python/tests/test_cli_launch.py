"""CLI launch-readiness command tests."""

from __future__ import annotations

import json
import shutil
from pathlib import Path
from uuid import uuid4

from click.testing import CliRunner

from backstop.cli import cli
import backstop.cli as cli_module
from backstop.metadata import MetadataStore


def test_doctor_native_tools_reports_missing(monkeypatch) -> None:
    monkeypatch.setattr(shutil, "which", lambda _: None)

    result = CliRunner().invoke(cli, ["doctor", "native-tools", "--json"])

    assert result.exit_code == 1
    payload = json.loads(result.output)
    assert payload["ok"] is False
    assert set(payload["missing"]) == {"pg_dump", "pg_restore", "pg_basebackup"}


def test_metadata_store_records_native_health() -> None:
    root = Path(__file__).resolve().parents[1] / ".tmp-native-tests" / f"backstop-cli-{uuid4().hex}"
    root.mkdir(parents=True, exist_ok=True)
    path = root / "backstop.db"
    store = MetadataStore(str(path))
    store.record_health("native_tools", "ok", {"tools": ["pg_dump"]})
    store.close()

    reopened = MetadataStore(str(path))
    row = reopened.conn.execute("SELECT status FROM health_checks WHERE component = ?", ("native_tools",)).fetchone()
    reopened.close()
    assert row[0] == "ok"


class _FakeStore:
    delete_allowed = True

    def __init__(self, storage) -> None:
        self.storage = storage
        self.objects = {}

    def put_bytes(self, key: str, payload: bytes, content_type: str = "application/octet-stream") -> None:
        self.objects[key] = payload

    def get_bytes(self, key: str) -> bytes:
        return self.objects[key]

    def delete_object(self, key: str) -> None:
        if not self.delete_allowed:
            raise PermissionError("delete denied")
        self.objects.pop(key, None)


def test_doctor_storage_permissions_warns_when_delete_allowed(monkeypatch) -> None:
    class Store(_FakeStore):
        delete_allowed = True

    monkeypatch.setattr(cli_module, "S3ArtifactStore", Store)

    result = CliRunner().invoke(cli, ["doctor", "storage-permissions", "--storage", "s3://bucket", "--json"])

    assert result.exit_code == 0
    payload = json.loads(result.output)
    assert payload["ok"] is True
    assert payload["delete_allowed"] is True
    assert payload["warnings"]


def test_doctor_storage_permissions_strict_fails_when_delete_allowed(monkeypatch) -> None:
    class Store(_FakeStore):
        delete_allowed = True

    monkeypatch.setattr(cli_module, "S3ArtifactStore", Store)

    result = CliRunner().invoke(cli, ["doctor", "storage-permissions", "--storage", "s3://bucket", "--strict", "--json"])

    assert result.exit_code == 1
    payload = json.loads(result.output)
    assert payload["ok"] is False
    assert payload["delete_allowed"] is True


def test_doctor_storage_permissions_passes_when_delete_denied(monkeypatch) -> None:
    class Store(_FakeStore):
        delete_allowed = False

    monkeypatch.setattr(cli_module, "S3ArtifactStore", Store)

    result = CliRunner().invoke(cli, ["doctor", "storage-permissions", "--storage", "s3://bucket", "--strict", "--json"])

    assert result.exit_code == 0
    payload = json.loads(result.output)
    assert payload["ok"] is True
    assert payload["delete_allowed"] is False


def test_doctor_launch_reports_degraded_when_optional_checks_are_skipped(monkeypatch) -> None:
    monkeypatch.setattr(shutil, "which", lambda tool: f"/usr/bin/{tool}")

    result = CliRunner().invoke(cli, ["doctor", "launch", "--json"])

    assert result.exit_code == 0
    payload = json.loads(result.output)
    assert payload["verdict"] == "degraded"
    assert payload["checks"]["native_tools"]["ok"] is True
    assert "storage_permissions" in payload["skipped"]


def test_recover_refuses_to_restore_over_original_table() -> None:
    result = CliRunner().invoke(
        cli,
        [
            "recover",
            "--db",
            "postgresql://user:secret@localhost/db",
            "--storage",
            "s3://bucket",
            "--table",
            "users",
            "--target-table",
            "users",
            "--yes",
        ],
    )

    assert result.exit_code == 1
    assert "Refusing to restore over the original table" in result.output


def test_recover_pitr_routes_to_native_recovery_instead_of_table_restore() -> None:
    result = CliRunner().invoke(
        cli,
        [
            "recover",
            "--type",
            "pitr",
            "--db",
            "postgresql://user:secret@localhost/db",
            "--storage",
            "s3://bucket",
        ],
    )

    assert result.exit_code == 0
    assert "PostgreSQL-native PITR" in result.output
    assert "npm run e2e:pitr" in result.output


def test_restore_copyback_plan_prints_reviewable_sql() -> None:
    result = CliRunner().invoke(
        cli,
        [
            "restore-copyback-plan",
            "--source-table",
            "users_recovered",
            "--target-table",
            "users",
        ],
    )

    assert result.exit_code == 0
    assert "LOCK TABLE" in result.output
    assert "ON CONFLICT DO NOTHING" in result.output

