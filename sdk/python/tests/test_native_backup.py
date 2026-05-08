"""Tests for PostgreSQL-native backup/PITR primitives."""

from __future__ import annotations

import subprocess
import tarfile
from pathlib import Path
from uuid import uuid4

import pytest

from backstop.native import (
    LogicalBackupEngine,
    PhysicalBackupEngine,
    WALArchive,
    parse_storage,
)


STORAGE = "s3://backstop-test"


@pytest.fixture(autouse=True)
def native_tmp(monkeypatch) -> None:
    root = Path(__file__).resolve().parents[1] / ".tmp-native-tests" / f"backstop-native-tests-{uuid4().hex}"
    root.mkdir(parents=True, exist_ok=True)
    monkeypatch.setenv("BACKSTOP_TMPDIR", str(root))


@pytest.fixture
def local_tmp() -> Path:
    root = Path(__file__).resolve().parents[1] / ".tmp-native-tests" / f"backstop-native-local-{uuid4().hex}"
    root.mkdir(parents=True, exist_ok=True)
    return root


def test_parse_storage_accepts_endpoint() -> None:
    cfg = parse_storage("s3://bucket@http://localhost:9000", prefix="/custom/")
    assert cfg.bucket == "bucket"
    assert cfg.endpoint_url == "http://localhost:9000"
    assert cfg.prefix == "custom"


def test_logical_backup_uses_pg_dump_and_uploads_manifest(monkeypatch, moto_s3) -> None:
    calls: list[list[str]] = []

    def fake_run(args, text, capture_output, timeout, check):
        calls.append(args)
        dump_path = Path(args[args.index("--file") + 1])
        dump_path.write_bytes(b"pgdump")
        return subprocess.CompletedProcess(args, 0, stdout="", stderr="")

    monkeypatch.setattr("backstop.native.subprocess.run", fake_run)

    manifest = LogicalBackupEngine(STORAGE).create_backup(
        db_url="postgresql://postgres:password@localhost:5433/testdb",
        backup_id="dump_test",
    )

    assert calls[0][0] == "pg_dump"
    assert manifest.backup_type == "logical_pg_dump"
    assert manifest.db_name == "testdb"
    assert "triggers" in manifest.recovery_capabilities
    raw = moto_s3.get_object(Bucket="backstop-test", Key=manifest.artifacts["dump"])["Body"].read()
    assert raw == b"pgdump"


def test_logical_restore_uses_pg_restore(monkeypatch, moto_s3) -> None:
    args_seen: list[list[str]] = []
    moto_s3.put_object(
        Bucket="backstop-test",
        Key="backstop/native/logical/dump_test/dump.pgcustom",
        Body=b"pgdump",
    )
    moto_s3.put_object(
        Bucket="backstop-test",
        Key="backstop/native/logical/dump_test/manifest.json",
        Body=b"""{
          "manifest_version": 1,
          "writer": "python-native",
          "backup_id": "dump_test",
          "backup_type": "logical_pg_dump",
          "timestamp": "2026-05-01T00:00:00Z",
          "db_name": "testdb",
          "pg_tool": "pg_dump",
          "pg_tool_args": [],
          "format": "pg_dump_custom",
          "s3_bucket": "backstop-test",
          "s3_prefix": "backstop",
          "artifacts": {
            "dump": "backstop/native/logical/dump_test/dump.pgcustom",
            "manifest": "backstop/native/logical/dump_test/manifest.json"
          },
          "recovery_capabilities": ["full_database_logical_restore"]
        }""",
    )

    def fake_run(args, text, capture_output, timeout, check):
        args_seen.append(args)
        return subprocess.CompletedProcess(args, 0, stdout="", stderr="")

    monkeypatch.setattr("backstop.native.subprocess.run", fake_run)

    manifest = LogicalBackupEngine(STORAGE).restore_backup(
        db_url="postgresql://postgres:password@localhost:5433/restoredb",
        backup_id="dump_test",
        clean=True,
        jobs=2,
    )

    assert manifest.backup_id == "dump_test"
    assert args_seen[0][0] == "pg_restore"
    assert "--clean" in args_seen[0]
    assert "--if-exists" in args_seen[0]
    assert "--jobs" in args_seen[0]


def test_wal_archive_and_fetch_round_trip(local_tmp, moto_s3) -> None:
    wal_name = "000000010000000000000001"
    wal_file = local_tmp / wal_name
    wal_file.write_bytes(b"wal")
    output = local_tmp / "out" / wal_name

    archive = WALArchive(STORAGE, cluster_id="cluster-a")
    key = archive.archive(str(wal_file), wal_name)
    assert key == f"backstop/wal/cluster-a/{wal_name}"

    archive.fetch(wal_name, str(output))
    assert output.read_bytes() == b"wal"


def test_physical_basebackup_and_prepare_restore(monkeypatch, local_tmp, moto_s3) -> None:
    def fake_run(args, text, capture_output, timeout, check):
        data_dir = Path(args[args.index("--pgdata") + 1])
        data_dir.mkdir(parents=True)
        (data_dir / "PG_VERSION").write_text("16", encoding="utf-8")
        return subprocess.CompletedProcess(args, 0, stdout="", stderr="")

    monkeypatch.setattr("backstop.native.subprocess.run", fake_run)

    engine = PhysicalBackupEngine(STORAGE, cluster_id="cluster-a")
    manifest = engine.create_basebackup(
        db_url="postgresql://postgres:password@localhost:5433/testdb",
        backup_id="base_test",
    )

    archive = moto_s3.get_object(Bucket="backstop-test", Key=manifest.artifacts["basebackup"])["Body"].read()
    archive_path = local_tmp / "basebackup.tar.gz"
    archive_path.write_bytes(archive)
    with tarfile.open(archive_path, "r:gz") as tar:
        assert any(name.endswith("PG_VERSION") for name in tar.getnames())

    target = local_tmp / "restore"
    engine.prepare_restore(
        backup_id="base_test",
        target_dir=str(target),
        storage=STORAGE,
        target_time="2026-05-01 00:00:00+00",
    )
    assert (target / "PG_VERSION").read_text(encoding="utf-8") == "16"
    assert (target / "recovery.signal").exists()
    auto_conf = (target / "postgresql.auto.conf").read_text(encoding="utf-8")
    assert "restore_command" in auto_conf
    assert "recovery_target_time" in auto_conf


def test_wal_rejects_bad_name(local_tmp, moto_s3) -> None:
    wal_file = local_tmp / "wal"
    wal_file.write_bytes(b"wal")
    with pytest.raises(ValueError):
        WALArchive(STORAGE, cluster_id="cluster-a").archive(str(wal_file), "../bad")

