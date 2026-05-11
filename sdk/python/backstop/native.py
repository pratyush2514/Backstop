"""PostgreSQL-native backup and PITR primitives.

This module is intentionally built on PostgreSQL's own tools instead of
handmade DDL reconstruction. Table snapshots are useful for fast object-level
recovery, but launch-grade database disaster recovery and PostgreSQL fidelity
must use pg_dump/pg_restore for logical backups and pg_basebackup + WAL archive
for physical point-in-time recovery.
"""

from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import tarfile
import tempfile
from contextlib import contextmanager
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Literal, Optional
from urllib.parse import urlparse
from uuid import uuid4

import boto3
from pydantic import BaseModel, Field


DEFAULT_PREFIX = "backstop"
WAL_NAME_RE = re.compile(r"^(?:[A-Fa-f0-9]{24}(?:\.[A-Za-z0-9._-]+)?|[A-Fa-f0-9]{8}\.history)$")


@dataclass(frozen=True)
class StorageConfig:
    bucket: str
    endpoint_url: Optional[str]
    prefix: str = DEFAULT_PREFIX


class NativeBackupManifest(BaseModel):
    """Manifest for PostgreSQL-native backup artifacts."""

    manifest_version: int = 1
    writer: str = "python-native"
    backup_id: str
    backup_type: Literal["logical_pg_dump", "physical_pg_basebackup"]
    timestamp: str
    db_name: str
    cluster_id: Optional[str] = None
    pg_tool: str
    pg_tool_args: list[str] = Field(default_factory=list)
    format: str
    s3_bucket: str
    s3_prefix: str
    artifacts: dict[str, str]
    recovery_capabilities: list[str]


def parse_storage(storage: str, prefix: str = DEFAULT_PREFIX) -> StorageConfig:
    """Parse ``s3://bucket`` or ``s3://bucket@http://endpoint``."""
    if not storage.startswith("s3://"):
        raise ValueError("storage must start with s3://")
    raw = storage.removeprefix("s3://")
    bucket, sep, endpoint = raw.partition("@")
    bucket = bucket.strip()
    if not bucket:
        raise ValueError("storage bucket is required")
    return StorageConfig(bucket=bucket, endpoint_url=endpoint.strip() if sep else None, prefix=prefix.strip("/"))


def db_name_from_url(db_url: str) -> str:
    parsed = urlparse(db_url)
    if parsed.path and parsed.path != "/":
        return parsed.path.lstrip("/")
    return "unknown"


def scrub_url(url: str) -> str:
    return re.sub(r"://([^:]+):[^@]+@", r"://\1:***@", url)


def scrub_text(text: str) -> str:
    return re.sub(r"://([^:]+):[^@\s]+@", r"://\1:***@", text)


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat()


def temp_parent() -> Optional[str]:
    configured = os.getenv("BACKSTOP_TMPDIR")
    if not configured:
        return None
    Path(configured).mkdir(parents=True, exist_ok=True)
    return configured


@contextmanager
def managed_tempdir(prefix: str):
    configured = temp_parent()
    if configured is None:
        with tempfile.TemporaryDirectory(prefix=prefix) as tmp:
            yield tmp
        return

    path = Path(configured) / f"{prefix}{uuid4().hex}"
    path.mkdir(parents=True, exist_ok=False)
    try:
        yield str(path)
    finally:
        shutil.rmtree(path, ignore_errors=True)


class S3ArtifactStore:
    """Small S3 wrapper for native backup artifacts."""

    def __init__(self, storage: StorageConfig) -> None:
        self.storage = storage
        self.client = boto3.client(
            "s3",
            endpoint_url=storage.endpoint_url,
            region_name="us-east-1",
        )

    def put_file(self, local_path: Path, key: str, content_type: str) -> None:
        self.client.upload_file(
            str(local_path),
            self.storage.bucket,
            key,
            ExtraArgs={"ContentType": content_type},
        )

    def get_file(self, key: str, local_path: Path) -> None:
        local_path.parent.mkdir(parents=True, exist_ok=True)
        self.client.download_file(self.storage.bucket, key, str(local_path))

    def put_json(self, key: str, payload: dict[str, Any]) -> None:
        self.client.put_object(
            Bucket=self.storage.bucket,
            Key=key,
            Body=json.dumps(payload, indent=2).encode("utf-8"),
            ContentType="application/json",
        )

    def read_json(self, key: str) -> dict[str, Any]:
        response = self.client.get_object(Bucket=self.storage.bucket, Key=key)
        return json.loads(response["Body"].read())

    def put_bytes(self, key: str, payload: bytes, content_type: str = "application/octet-stream") -> None:
        self.client.put_object(
            Bucket=self.storage.bucket,
            Key=key,
            Body=payload,
            ContentType=content_type,
        )

    def get_bytes(self, key: str) -> bytes:
        response = self.client.get_object(Bucket=self.storage.bucket, Key=key)
        return response["Body"].read()

    def delete_object(self, key: str) -> None:
        self.client.delete_object(Bucket=self.storage.bucket, Key=key)


class CommandError(RuntimeError):
    """Raised when a PostgreSQL native tool fails."""


def run_command(args: list[str], timeout: Optional[int] = None) -> subprocess.CompletedProcess[str]:
    try:
        result = subprocess.run(
            args,
            text=True,
            capture_output=True,
            timeout=timeout,
            check=False,
        )
    except FileNotFoundError as exc:
        raise CommandError(f"required PostgreSQL tool not found: {args[0]}") from exc

    if result.returncode != 0:
        safe_args = [scrub_url(a) for a in args]
        raise CommandError(
            "command failed: "
            + " ".join(safe_args)
            + f"\nstdout:\n{scrub_text(result.stdout)}\nstderr:\n{scrub_text(result.stderr)}"
        )
    return result


class LogicalBackupEngine:
    """Full logical database backup and restore using pg_dump/pg_restore."""

    def __init__(self, storage: str, s3_prefix: str = DEFAULT_PREFIX) -> None:
        self.storage = parse_storage(storage, s3_prefix)
        self.store = S3ArtifactStore(self.storage)

    def create_backup(
        self,
        db_url: str,
        backup_id: Optional[str] = None,
        pg_dump_bin: str = "pg_dump",
        timeout: Optional[int] = None,
    ) -> NativeBackupManifest:
        backup_id = backup_id or f"dump_{uuid4().hex[:12]}"
        base_key = f"{self.storage.prefix}/native/logical/{backup_id}"
        dump_key = f"{base_key}/dump.pgcustom"
        manifest_key = f"{base_key}/manifest.json"

        with managed_tempdir("backstop-pgdump-") as tmp:
            dump_path = Path(tmp) / "dump.pgcustom"
            args = [
                pg_dump_bin,
                "--format=custom",
                "--blobs",
                "--verbose",
                "--dbname",
                db_url,
                "--file",
                str(dump_path),
            ]
            run_command(args, timeout=timeout)
            self.store.put_file(dump_path, dump_key, "application/octet-stream")

        manifest = NativeBackupManifest(
            backup_id=backup_id,
            backup_type="logical_pg_dump",
            timestamp=utc_now(),
            db_name=db_name_from_url(db_url),
            pg_tool=pg_dump_bin,
            pg_tool_args=["--format=custom", "--blobs", "--verbose"],
            format="pg_dump_custom",
            s3_bucket=self.storage.bucket,
            s3_prefix=self.storage.prefix,
            artifacts={"dump": dump_key, "manifest": manifest_key},
            recovery_capabilities=[
                "full_database_logical_restore",
                "non_public_schemas",
                "triggers",
                "rls_policies",
                "custom_types",
                "partitions",
                "materialized_views",
            ],
        )
        self.store.put_json(manifest_key, manifest.model_dump())
        return manifest

    def restore_backup(
        self,
        db_url: str,
        backup_id: str,
        clean: bool = False,
        pg_restore_bin: str = "pg_restore",
        jobs: int = 1,
        timeout: Optional[int] = None,
    ) -> NativeBackupManifest:
        manifest = self.get_manifest(backup_id)
        dump_key = manifest.artifacts["dump"]

        with managed_tempdir("backstop-pgrestore-") as tmp:
            dump_path = Path(tmp) / "dump.pgcustom"
            self.store.get_file(dump_key, dump_path)
            args = [pg_restore_bin, "--exit-on-error", "--dbname", db_url]
            if clean:
                args.extend(["--clean", "--if-exists"])
            if jobs > 1:
                args.extend(["--jobs", str(jobs)])
            args.append(str(dump_path))
            run_command(args, timeout=timeout)
        return manifest

    def get_manifest(self, backup_id: str) -> NativeBackupManifest:
        key = f"{self.storage.prefix}/native/logical/{backup_id}/manifest.json"
        return NativeBackupManifest.model_validate(self.store.read_json(key))


class PhysicalBackupEngine:
    """Physical base backups used for PostgreSQL PITR."""

    def __init__(self, storage: str, cluster_id: str, s3_prefix: str = DEFAULT_PREFIX) -> None:
        if not cluster_id:
            raise ValueError("cluster_id is required")
        self.storage = parse_storage(storage, s3_prefix)
        self.cluster_id = cluster_id
        self.store = S3ArtifactStore(self.storage)

    def create_basebackup(
        self,
        db_url: str,
        backup_id: Optional[str] = None,
        pg_basebackup_bin: str = "pg_basebackup",
        timeout: Optional[int] = None,
    ) -> NativeBackupManifest:
        backup_id = backup_id or f"base_{uuid4().hex[:12]}"
        base_key = f"{self.storage.prefix}/native/physical/{self.cluster_id}/{backup_id}"
        archive_key = f"{base_key}/basebackup.tar.gz"
        manifest_key = f"{base_key}/manifest.json"

        with managed_tempdir("backstop-basebackup-") as tmp:
            tmp_path = Path(tmp)
            data_dir = tmp_path / "data"
            archive_path = tmp_path / "basebackup.tar.gz"
            args = [
                pg_basebackup_bin,
                "--dbname",
                db_url,
                "--pgdata",
                str(data_dir),
                "--format",
                "plain",
                "--wal-method",
                "stream",
                "--checkpoint",
                "fast",
            ]
            run_command(args, timeout=timeout)
            with tarfile.open(archive_path, "w:gz") as tar:
                tar.add(data_dir, arcname=".")
            self.store.put_file(archive_path, archive_key, "application/gzip")

        manifest = NativeBackupManifest(
            backup_id=backup_id,
            backup_type="physical_pg_basebackup",
            timestamp=utc_now(),
            db_name=db_name_from_url(db_url),
            cluster_id=self.cluster_id,
            pg_tool=pg_basebackup_bin,
            pg_tool_args=["--format=plain", "--wal-method=stream", "--checkpoint=fast"],
            format="tar.gz",
            s3_bucket=self.storage.bucket,
            s3_prefix=self.storage.prefix,
            artifacts={"basebackup": archive_key, "manifest": manifest_key},
            recovery_capabilities=[
                "full_cluster_physical_restore",
                "point_in_time_recovery_with_archived_wal",
                "byte_level_postgresql_fidelity",
            ],
        )
        self.store.put_json(manifest_key, manifest.model_dump())
        return manifest

    def prepare_restore(
        self,
        backup_id: str,
        target_dir: str,
        storage: str,
        target_time: Optional[str] = None,
        force: bool = False,
    ) -> NativeBackupManifest:
        manifest = self.get_manifest(backup_id)
        target = Path(target_dir)
        if target.exists() and any(target.iterdir()) and not force:
            raise RuntimeError(f"target directory is not empty: {target}")
        target.mkdir(parents=True, exist_ok=True)

        with managed_tempdir("backstop-pitr-restore-") as tmp:
            archive_path = Path(tmp) / "basebackup.tar.gz"
            self.store.get_file(manifest.artifacts["basebackup"], archive_path)
            with tarfile.open(archive_path, "r:gz") as tar:
                safe_extract_tar(tar, target)

        restore_command = (
            f"backstop wal fetch --storage {storage} --cluster-id {self.cluster_id} "
            "--wal-name %f --output %p"
        )
        auto_conf = target / "postgresql.auto.conf"
        with auto_conf.open("a", encoding="utf-8") as fh:
            fh.write(f"\nrestore_command = '{restore_command}'\n")
            if target_time:
                fh.write(f"recovery_target_time = '{target_time}'\n")
        (target / "recovery.signal").touch()
        return manifest

    def get_manifest(self, backup_id: str) -> NativeBackupManifest:
        key = f"{self.storage.prefix}/native/physical/{self.cluster_id}/{backup_id}/manifest.json"
        return NativeBackupManifest.model_validate(self.store.read_json(key))


class WALArchive:
    """S3-backed archive_command/restore_command implementation."""

    def __init__(self, storage: str, cluster_id: str, s3_prefix: str = DEFAULT_PREFIX) -> None:
        if not cluster_id:
            raise ValueError("cluster_id is required")
        self.storage = parse_storage(storage, s3_prefix)
        self.cluster_id = cluster_id
        self.store = S3ArtifactStore(self.storage)

    def archive(self, wal_file: str, wal_name: str) -> str:
        self._validate_wal_name(wal_name)
        path = Path(wal_file)
        if not path.is_file():
            raise FileNotFoundError(wal_file)
        key = self._wal_key(wal_name)
        self.store.put_file(path, key, "application/octet-stream")
        return key

    def fetch(self, wal_name: str, output: str) -> str:
        self._validate_wal_name(wal_name)
        key = self._wal_key(wal_name)
        self.store.get_file(key, Path(output))
        return key

    def _wal_key(self, wal_name: str) -> str:
        return f"{self.storage.prefix}/wal/{self.cluster_id}/{wal_name}"

    @staticmethod
    def _validate_wal_name(wal_name: str) -> None:
        if not WAL_NAME_RE.match(wal_name):
            raise ValueError(f"invalid WAL file name: {wal_name}")


def safe_extract_tar(tar: tarfile.TarFile, target: Path) -> None:
    root = target.resolve()
    for member in tar.getmembers():
        destination = (target / member.name).resolve()
        if destination != root and root not in destination.parents:
            raise RuntimeError(f"unsafe path in base backup archive: {member.name}")
    tar.extractall(target, filter="data")

