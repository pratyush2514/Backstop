"""backstop CLI - snapshot management and table restore.

Entry point: ``backstop`` (registered in pyproject.toml project.scripts).

Commands:
    backstop snapshots list  -- List all snapshots in S3
    backstop restore         -- Restore a table from a snapshot

Security:
    Database URLs are scrubbed (password replaced with ***) before any
    display or log output.
"""

from __future__ import annotations

import re
import json
import shutil
import sys
import tarfile
import time
import tempfile
from pathlib import Path
from typing import Optional
from uuid import uuid4

import click
import psycopg2
from rich.console import Console
from rich.panel import Panel
from rich.table import Table

from .restore import RestoreEngine
from .parser import assess_risk
from .native import LogicalBackupEngine, NativeBackupManifest, PhysicalBackupEngine, S3ArtifactStore, WALArchive, parse_storage as parse_native_storage, utc_now
from .snapshot import SnapshotEngine
from .metadata import MetadataStore

console = Console()
err_console = Console(stderr=True)


def _scrub_url(url: str) -> str:
    """Replace password in a connection URL with *** for safe display."""
    return re.sub(r"://([^:]+):[^@]+@", r"://\1:***@", url)


def _parse_storage(storage: str) -> tuple[str, Optional[str]]:
    """Parse a storage URL into (bucket, endpoint_url).

    Args:
        storage: A storage URL like ``s3://bucket`` or
            ``s3://bucket@http://localhost:9000``.

    Returns:
        Tuple of (bucket_name, endpoint_url_or_None).
    """
    raw = storage.removeprefix("s3://")
    parts = raw.split("@", 1)
    bucket = parts[0]
    endpoint_url = parts[1] if len(parts) > 1 else None
    return bucket, endpoint_url


def _emit_drill_result(payload: dict, as_json: bool) -> None:
    if as_json:
        sys.stdout.write(json.dumps(payload, indent=2) + "\n")
        return
    status = "[green]PASS[/green]" if payload.get("ok") else "[red]FAIL[/red]"
    console.print(Panel(json.dumps(payload, indent=2), title=f"backstop Drill {status}", border_style="green" if payload.get("ok") else "red"))


def _record_native_manifest(metadata_db: Optional[str], manifest) -> None:
    store = MetadataStore(metadata_db)
    try:
        store.record_native_backup(manifest)
    finally:
        store.close()


# ── CLI root ─────────────────────────────────────────────────────────────────

@click.group()
def cli() -> None:
    """backstop - production-grade database safety platform.

    Intercepts destructive SQL operations, snapshots affected tables,
    and enables instant recovery.
    """


# ── Launch validation commands ───────────────────────────────────────────────

@cli.group()
def doctor() -> None:
    """Validate local production dependencies."""


@doctor.command("native-tools")
@click.option("--json", "as_json", is_flag=True, help="Emit machine-readable JSON")
def doctor_native_tools(as_json: bool) -> None:
    """Check required PostgreSQL native backup tools."""
    tools = ["pg_dump", "pg_restore", "pg_basebackup"]
    result = {
        "ok": True,
        "checks": {},
        "missing": [],
    }
    for tool in tools:
        path = shutil.which(tool)
        result["checks"][tool] = {"found": path is not None, "path": path}
        if path is None:
            result["ok"] = False
            result["missing"].append(tool)

    _emit_drill_result(result, as_json)
    if not result["ok"]:
        sys.exit(1)


@doctor.command("storage-permissions")
@click.option("--storage", required=True, help="S3 storage URL")
@click.option("--metadata-db", default=None, help="Optional SQLite metadata DB path")
@click.option("--strict", is_flag=True, help="Fail when DeleteObject is allowed")
@click.option("--json", "as_json", is_flag=True, help="Emit machine-readable JSON")
def doctor_storage_permissions(storage: str, metadata_db: Optional[str], strict: bool, as_json: bool) -> None:
    """Check S3-compatible snapshot/WAL storage permissions."""
    cfg = parse_native_storage(storage)
    key = f"{cfg.prefix}/doctor/permissions/{uuid4().hex}.txt"
    payload = f"backstop storage doctor {uuid4().hex}\n".encode("utf-8")
    result = {
        "ok": False,
        "doctor": "storage-permissions",
        "bucket": cfg.bucket,
        "prefix": cfg.prefix,
        "test_key": key,
        "write_ok": False,
        "read_ok": False,
        "delete_allowed": None,
        "strict": strict,
        "warnings": [],
    }
    try:
        store = S3ArtifactStore(cfg)
        store.put_bytes(key, payload, "text/plain")
        result["write_ok"] = True
        result["read_ok"] = store.get_bytes(key) == payload
        try:
            store.delete_object(key)
            result["delete_allowed"] = True
            result["warnings"].append("DeleteObject is allowed for this credential. Use --strict in production checks to fail on mutable backup storage.")
        except Exception as exc:
            result["delete_allowed"] = False
            result["delete_error"] = str(exc)
        result["ok"] = bool(result["write_ok"] and result["read_ok"] and not (strict and result["delete_allowed"]))
    except Exception as exc:
        result.update({"ok": False, "error": str(exc)})

    store = MetadataStore(metadata_db)
    store.record_health("storage_permissions", "ok" if result["ok"] else "failed", result)
    store.close()
    _emit_drill_result(result, as_json)
    if not result["ok"]:
        sys.exit(1)


@cli.group()
def drill() -> None:
    """Run backend launch validation drills."""


@drill.command("wal-archive-fetch")
@click.option("--storage", required=True, help="S3 storage URL")
@click.option("--cluster-id", required=True, help="Stable PostgreSQL cluster identifier")
@click.option("--metadata-db", default=None, help="Optional SQLite metadata DB path")
@click.option("--json", "as_json", is_flag=True, help="Emit machine-readable JSON")
def drill_wal_archive_fetch(storage: str, cluster_id: str, metadata_db: Optional[str], as_json: bool) -> None:
    """Round-trip a WAL-shaped object through archive/fetch storage."""
    wal_name = "000000010000000000000001"
    payload = b"backstop-wal-drill"
    result = {"ok": False, "drill": "wal-archive-fetch", "wal_name": wal_name}
    try:
        with tempfile.TemporaryDirectory(prefix="backstop-wal-drill-") as tmp:
            source = Path(tmp) / wal_name
            fetched = Path(tmp) / "fetched" / wal_name
            source.write_bytes(payload)
            archive = WALArchive(storage, cluster_id)
            key = archive.archive(str(source), wal_name)
            archive.fetch(wal_name, str(fetched))
            result.update({"ok": fetched.read_bytes() == payload, "key": key})
        store = MetadataStore(metadata_db)
        store.record_health("wal_drill", "ok" if result["ok"] else "failed", result)
        store.close()
    except Exception as exc:
        result.update({"ok": False, "error": str(exc)})
        store = MetadataStore(metadata_db)
        store.record_health("wal_drill", "failed", result)
        store.close()
    _emit_drill_result(result, as_json)
    if not result["ok"]:
        sys.exit(1)


@drill.command("pitr-prepare")
@click.option("--storage", required=True, help="S3 storage URL")
@click.option("--cluster-id", required=True, help="Stable PostgreSQL cluster identifier")
@click.option("--backup-id", default=None, help="Physical base backup ID; auto-created when --simulate is used")
@click.option("--target-dir", required=True, help="Directory to prepare for restore")
@click.option("--target-time", default=None, help="Optional recovery_target_time")
@click.option("--simulate", is_flag=True, help="Create a tiny synthetic basebackup artifact before preparing restore")
@click.option("--metadata-db", default=None, help="Optional SQLite metadata DB path")
@click.option("--json", "as_json", is_flag=True, help="Emit machine-readable JSON")
def drill_pitr_prepare(
    storage: str,
    cluster_id: str,
    backup_id: Optional[str],
    target_dir: str,
    target_time: Optional[str],
    simulate: bool,
    metadata_db: Optional[str],
    as_json: bool,
) -> None:
    """Prepare a PITR restore directory and verify recovery files."""
    result = {"ok": False, "drill": "pitr-prepare", "target_dir": target_dir}
    try:
        engine = PhysicalBackupEngine(storage, cluster_id)
        effective_backup_id = backup_id or f"base_drill_{uuid4().hex[:8]}"
        if simulate:
            _create_simulated_basebackup(engine, storage, cluster_id, effective_backup_id)
        manifest = engine.prepare_restore(
            backup_id=effective_backup_id,
            target_dir=target_dir,
            storage=storage,
            target_time=target_time,
            force=True,
        )
        auto_conf = Path(target_dir) / "postgresql.auto.conf"
        recovery_signal = Path(target_dir) / "recovery.signal"
        auto_text = auto_conf.read_text(encoding="utf-8") if auto_conf.exists() else ""
        result.update(
            {
                "ok": recovery_signal.exists() and "restore_command" in auto_text and (target_time is None or "recovery_target_time" in auto_text),
                "backup_id": manifest.backup_id,
                "recovery_signal": recovery_signal.exists(),
                "restore_command": "restore_command" in auto_text,
                "recovery_target_time": target_time,
            }
        )
        _record_native_manifest(metadata_db, manifest)
    except Exception as exc:
        result.update({"ok": False, "error": str(exc)})
        store = MetadataStore(metadata_db)
        store.record_health("pitr_drill", "failed", result)
        store.close()
    _emit_drill_result(result, as_json)
    if not result["ok"]:
        sys.exit(1)


@drill.command("logical-backup-restore")
@click.option("--source-db", required=True, help="Source PostgreSQL URL")
@click.option("--target-db", required=True, help="Clean target PostgreSQL URL")
@click.option("--storage", required=True, help="S3 storage URL")
@click.option("--pg-dump-bin", default="pg_dump", show_default=True)
@click.option("--pg-restore-bin", default="pg_restore", show_default=True)
@click.option("--metadata-db", default=None, help="Optional SQLite metadata DB path")
@click.option("--json", "as_json", is_flag=True, help="Emit machine-readable JSON")
def drill_logical_backup_restore(
    source_db: str,
    target_db: str,
    storage: str,
    pg_dump_bin: str,
    pg_restore_bin: str,
    metadata_db: Optional[str],
    as_json: bool,
) -> None:
    """Seed, pg_dump, pg_restore, and verify a tiny logical backup."""
    table = "backstop_drill_seed"
    result = {"ok": False, "drill": "logical-backup-restore", "table": table}
    try:
        with psycopg2.connect(source_db) as conn:
            with conn.cursor() as cur:
                cur.execute(f"CREATE TABLE IF NOT EXISTS {table} (id integer PRIMARY KEY, note text)")
                cur.execute(f"INSERT INTO {table} (id, note) VALUES (1, 'launch drill') ON CONFLICT (id) DO NOTHING")

        engine = LogicalBackupEngine(storage)
        manifest = engine.create_backup(source_db, backup_id=f"dump_drill_{uuid4().hex[:8]}", pg_dump_bin=pg_dump_bin)
        engine.restore_backup(target_db, manifest.backup_id, clean=True, pg_restore_bin=pg_restore_bin)

        with psycopg2.connect(target_db) as conn:
            with conn.cursor() as cur:
                cur.execute(f"SELECT count(*) FROM {table}")
                row_count = cur.fetchone()[0]
        result.update({"ok": row_count >= 1, "backup_id": manifest.backup_id, "row_count": row_count})
        _record_native_manifest(metadata_db, manifest)
    except Exception as exc:
        result.update({"ok": False, "error": str(exc)})
        store = MetadataStore(metadata_db)
        store.record_health("logical_drill", "failed", result)
        store.close()
    _emit_drill_result(result, as_json)
    if not result["ok"]:
        sys.exit(1)


def _create_simulated_basebackup(engine: PhysicalBackupEngine, storage: str, cluster_id: str, backup_id: str) -> None:
    cfg = parse_native_storage(storage)
    base_key = f"{cfg.prefix}/native/physical/{cluster_id}/{backup_id}"
    archive_key = f"{base_key}/basebackup.tar.gz"
    manifest_key = f"{base_key}/manifest.json"
    with tempfile.TemporaryDirectory(prefix="backstop-pitr-sim-") as tmp:
        data_dir = Path(tmp) / "data"
        data_dir.mkdir()
        (data_dir / "PG_VERSION").write_text("16", encoding="utf-8")
        archive_path = Path(tmp) / "basebackup.tar.gz"
        with tarfile.open(archive_path, "w:gz") as tar:
            tar.add(data_dir, arcname=".")
        engine.store.put_file(archive_path, archive_key, "application/gzip")
    manifest = NativeBackupManifest(
        backup_id=backup_id,
        backup_type="physical_pg_basebackup",
        timestamp=utc_now(),
        db_name="simulated",
        cluster_id=cluster_id,
        pg_tool="simulated",
        pg_tool_args=[],
        format="tar.gz",
        s3_bucket=cfg.bucket,
        s3_prefix=cfg.prefix,
        artifacts={"basebackup": archive_key, "manifest": manifest_key},
        recovery_capabilities=["point_in_time_recovery_with_archived_wal"],
    )
    engine.store.put_json(manifest_key, manifest.model_dump())


# ── Snapshots group ───────────────────────────────────────────────────────────

@cli.group()
def snapshots() -> None:
    """Manage database snapshots."""


@snapshots.command("list")
@click.option("--db", required=True, help="PostgreSQL connection URL")
@click.option(
    "--storage",
    required=True,
    help="S3 storage URL (s3://bucket or s3://bucket@http://endpoint)",
)
@click.option("--table", default=None, help="Filter by table name")
def list_snapshots(db: str, storage: str, table: Optional[str]) -> None:
    """List all snapshots stored in S3.

    \b
    Examples:
        backstop snapshots list --db postgresql://... --storage s3://my-bucket
        backstop snapshots list --db postgresql://... --storage s3://my-bucket --table users
    """
    bucket, endpoint_url = _parse_storage(storage)

    try:
        engine = SnapshotEngine(
            s3_bucket=bucket,
            endpoint_url=endpoint_url,
        )
        manifests = engine.list_snapshots(table=table)
    except Exception as exc:
        err_console.print(
            Panel(
                f"[red]Failed to retrieve snapshots:[/red]\n{exc}",
                title="[red]Error[/red]",
                border_style="red",
            )
        )
        sys.exit(1)

    if not manifests:
        filter_desc = f" for table [cyan]{table}[/cyan]" if table else ""
        console.print(f"[dim]No snapshots found{filter_desc}.[/dim]")
        return

    rich_table = Table(
        title=f"backstop Snapshots{' - ' + table if table else ''}",
        show_header=True,
        header_style="bold cyan",
    )
    rich_table.add_column("Snapshot ID", style="green")
    rich_table.add_column("Table", style="cyan")
    rich_table.add_column("Operation", style="yellow")
    rich_table.add_column("Writer", style="magenta")
    rich_table.add_column("Scope", style="blue")
    rich_table.add_column("Actor", style="dim")
    rich_table.add_column("Timestamp", style="white")
    rich_table.add_column("Rows", justify="right")

    for m in manifests:
        rich_table.add_row(
            m.snapshot_id,
            m.table_name,
            m.operation,
            m.writer,
            m.snapshot_scope,
            m.actor or "-",
            m.timestamp,
            str(m.row_count),
        )

    console.print(rich_table)


# ── Restore command ───────────────────────────────────────────────────────────

@cli.command()
@click.option("--db", required=True, help="PostgreSQL connection URL")
@click.option(
    "--storage",
    required=True,
    help="S3 storage URL (s3://bucket or s3://bucket@http://endpoint)",
)
@click.option("--snapshot-id", required=True, help="Snapshot ID to restore from")
@click.option("--table", required=True, help="Original table name that was snapshotted")
@click.option(
    "--target-table",
    default=None,
    help="Restore into this table (default: {table}_recovered)",
)
@click.option(
    "--dry-run",
    is_flag=True,
    help="Preview the restore without modifying the database",
)
@click.option(
    "--conflict-policy",
    type=click.Choice(["skip", "overwrite", "fail"]),
    default="skip",
    show_default=True,
    help="How to handle rows that conflict on primary/unique keys",
)
@click.option("--metadata-db", default=None, help="Optional SQLite metadata DB path")
def restore(
    db: str,
    storage: str,
    snapshot_id: str,
    table: str,
    target_table: Optional[str],
    dry_run: bool,
    conflict_policy: str,
    metadata_db: Optional[str],
) -> None:
    """Restore a table from a snapshot.

    \b
    Examples:
        backstop restore --db postgresql://... --storage s3://bucket \\
            --snapshot-id snap_a3f91c2b --table users

        backstop restore --db postgresql://... --storage s3://bucket \\
            --snapshot-id snap_a3f91c2b --table users --target-table users
    """
    safe_db = _scrub_url(db)
    bucket, endpoint_url = _parse_storage(storage)
    effective_target = target_table or f"{table}_recovered"

    console.print(
        f"[cyan]Restoring[/cyan] snapshot [green]{snapshot_id}[/green] "
        f"-> table [cyan]{effective_target}[/cyan]"
    )

    conn = None
    try:
        conn = psycopg2.connect(db)
        conn.autocommit = False
    except Exception as exc:
        err_console.print(
            Panel(
                f"[red]Could not connect to database:[/red]\n{exc}\n\n"
                f"[dim]URL (scrubbed): {safe_db}[/dim]",
                title="[red]Connection Error[/red]",
                border_style="red",
            )
        )
        sys.exit(1)

    try:
        restore_engine = RestoreEngine(
            s3_bucket=bucket,
            endpoint_url=endpoint_url,
        )
        if dry_run:
            preview = restore_engine.preview_restore(
                conn=conn,
                snapshot_id=snapshot_id,
                table=table,
                target_table=target_table,
            )
            console.print(
                Panel(
                    f"[cyan]Restore preview only. No database changes were made.[/cyan]\n\n"
                    f"  Snapshot:       [green]{preview.snapshot_id}[/green]\n"
                    f"  Source table:   [cyan]{preview.source_table}[/cyan]\n"
                    f"  Target table:   [cyan]{preview.target_table}[/cyan]\n"
                    f"  Rows in snapshot: [bold]{preview.row_count}[/bold]\n"
                    f"  Snapshot scope: [yellow]{preview.snapshot_scope}[/yellow]\n"
                    f"  Target exists:  [bold]{preview.target_exists}[/bold]\n"
                    f"  Target rows:    [bold]{preview.target_row_count if preview.target_row_count is not None else 'n/a'}[/bold]\n"
                    f"  Will create table: [bold]{preview.will_create_table}[/bold]\n"
                    f"  Indexes to apply: [bold]{preview.will_apply_indexes}[/bold]\n"
                    f"  Constraints to apply: [bold]{preview.will_apply_constraints}[/bold]",
                    title="[cyan]Dry Run[/cyan]",
                    border_style="cyan",
                )
            )
            return

        with console.status(f"[cyan]Restoring {table} -> {effective_target}...[/cyan]"):
            row_count = restore_engine.restore_table(
                conn=conn,
                snapshot_id=snapshot_id,
                table=table,
                target_table=target_table,
                conflict_policy=conflict_policy,
            )
            store = MetadataStore(metadata_db)
            store.record_restore_event(
                snapshot_id,
                table,
                effective_target,
                "restored",
                {"snapshot_id": snapshot_id, "source_table": table, "target_table": effective_target, "rows_restored": row_count},
            )
            store.close()

        console.print(
            Panel(
                f"[green]Restore complete.[/green]\n\n"
                f"  Snapshot:     [green]{snapshot_id}[/green]\n"
                f"  Source table: [cyan]{table}[/cyan]\n"
                f"  Target table: [cyan]{effective_target}[/cyan]\n"
                f"  Rows restored: [bold]{row_count}[/bold]",
                title="[green]Success[/green]",
                border_style="green",
            )
        )
    except RuntimeError as exc:
        err_console.print(
            Panel(
                f"[red]Restore failed (rolled back):[/red]\n{exc}",
                title="[red]Restore Error[/red]",
                border_style="red",
            )
        )
        sys.exit(1)
    except Exception as exc:
        err_console.print(
            Panel(
                f"[red]Unexpected error during restore:[/red]\n{exc}",
                title="[red]Error[/red]",
                border_style="red",
            )
        )
        sys.exit(1)
    finally:
        if conn is not None:
            try:
                conn.close()
            except Exception:
                pass


@cli.command("restore-validate")
@click.option("--db", required=True, help="PostgreSQL connection URL")
@click.option("--storage", required=True, help="S3 storage URL")
@click.option("--snapshot-id", required=True, help="Snapshot ID that was restored")
@click.option("--table", required=True, help="Original table name")
@click.option("--target-table", default=None, help="Restored target table (default: {table}_recovered)")
@click.option("--metadata-db", default=None, help="Optional SQLite metadata DB path")
@click.option("--json", "as_json", is_flag=True, help="Emit machine-readable JSON")
def restore_validate(db: str, storage: str, snapshot_id: str, table: str, target_table: Optional[str], metadata_db: Optional[str], as_json: bool) -> None:
    """Validate a restored table against its snapshot manifest."""
    bucket, endpoint_url = _parse_storage(storage)
    target = target_table or f"{table}_recovered"
    payload = {"ok": False, "snapshot_id": snapshot_id, "source_table": table, "target_table": target}
    try:
        engine = RestoreEngine(s3_bucket=bucket, endpoint_url=endpoint_url)
        manifest = engine._snapshot_engine.get_manifest(snapshot_id=snapshot_id, table=table)
        with psycopg2.connect(db) as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_schema = 'public' AND table_name = %s)", (target,))
                target_exists = bool(cur.fetchone()[0])
                row_count = None
                if target_exists:
                    from psycopg2 import sql as pgsql
                    cur.execute(pgsql.SQL("SELECT COUNT(*) FROM {}").format(pgsql.Identifier(target)))
                    row_count = int(cur.fetchone()[0])
        payload.update(
            {
                "ok": target_exists and row_count == manifest.row_count,
                "target_exists": target_exists,
                "target_row_count": row_count,
                "manifest_row_count": manifest.row_count,
                "indexes": len(getattr(manifest, "indexes", [])),
                "constraints": len(manifest.fk_constraints) + len(getattr(manifest, "check_constraints", [])),
            }
        )
    except Exception as exc:
        payload.update({"ok": False, "error": str(exc)})
    store = MetadataStore(metadata_db)
    store.record_restore_event(snapshot_id, table, target, "ok" if payload["ok"] else "failed", payload)
    store.close()
    _emit_drill_result(payload, as_json)
    if not payload["ok"]:
        sys.exit(1)


@cli.command("restore-copyback-plan")
@click.option("--source-table", required=True, help="Restored table, usually {table}_recovered")
@click.option("--target-table", required=True, help="Original table to repair")
@click.option("--conflict-policy", type=click.Choice(["skip", "overwrite", "fail"]), default="skip", show_default=True)
def restore_copyback_plan(source_table: str, target_table: str, conflict_policy: str) -> None:
    """Print operator-reviewed SQL for copying recovered rows back."""
    if conflict_policy == "fail":
        insert = f'INSERT INTO "{target_table}" SELECT * FROM "{source_table}";'
    elif conflict_policy == "overwrite":
        insert = (
            f'-- Requires a matching primary key/unique constraint on "{target_table}".\n'
            f'INSERT INTO "{target_table}" SELECT * FROM "{source_table}"\n'
            "ON CONFLICT DO UPDATE SET -- fill explicit column assignments after review;"
        )
    else:
        insert = f'INSERT INTO "{target_table}" SELECT * FROM "{source_table}" ON CONFLICT DO NOTHING;'
    console.print(
        Panel(
            f"-- Review locks, triggers, FKs, and application downtime before running.\n"
            f"BEGIN;\nLOCK TABLE \"{target_table}\" IN SHARE ROW EXCLUSIVE MODE;\n{insert}\nCOMMIT;",
            title="[yellow]Copy-back plan[/yellow]",
            border_style="yellow",
        )
    )


# ── Audit command ─────────────────────────────────────────────────────────────

@cli.command()
@click.option(
    "--storage",
    required=True,
    help="S3 storage URL (s3://bucket or s3://bucket@http://endpoint)",
)
@click.option("--actor", default=None, help="Filter by actor ID")
@click.option("--table", default=None, help="Filter by table name")
@click.option(
    "--since",
    default=None,
    help='Filter events on or after this ISO timestamp (e.g. "2025-04-30")',
)
def audit(storage: str, actor: Optional[str], table: Optional[str], since: Optional[str]) -> None:
    """Show audit log of snapshot events.

    \b
    Examples:
        backstop audit --storage s3://bucket --actor gpt-agent-prod
        backstop audit --storage s3://bucket --table payments --since "2025-04-30"
    """
    bucket, endpoint_url = _parse_storage(storage)

    try:
        engine = SnapshotEngine(s3_bucket=bucket, endpoint_url=endpoint_url)
        manifests = engine.list_snapshots(table=table)
    except Exception as exc:
        err_console.print(
            Panel(
                f"[red]Failed to retrieve audit log:[/red]\n{exc}",
                title="[red]Error[/red]",
                border_style="red",
            )
        )
        sys.exit(1)

    # Apply filters
    if actor:
        manifests = [m for m in manifests if m.actor == actor]
    if since:
        manifests = [m for m in manifests if m.timestamp >= since]

    if not manifests:
        console.print("[dim]No audit events match the given filters.[/dim]")
        return

    rich_table = Table(
        title="backstop Audit Log",
        show_header=True,
        header_style="bold cyan",
    )
    rich_table.add_column("Snapshot ID", style="green")
    rich_table.add_column("Table", style="cyan")
    rich_table.add_column("Operation", style="yellow")
    rich_table.add_column("Actor", style="dim")
    rich_table.add_column("Timestamp")
    rich_table.add_column("Rows", justify="right")
    rich_table.add_column("Query", style="dim", max_width=60)

    for m in manifests:
        rich_table.add_row(
            m.snapshot_id,
            m.table_name,
            m.operation,
            m.actor or "-",
            m.timestamp,
            str(m.row_count),
            m.query[:60] + ("..." if len(m.query) > 60 else ""),
        )

    console.print(rich_table)


# ── Benchmark commands ───────────────────────────────────────────────────────

@cli.group()
def benchmark() -> None:
    """Run local backstop benchmarks."""


@benchmark.command("parser")
@click.option("--iterations", default=10_000, show_default=True, help="Number of parser iterations")
@click.option(
    "--sql",
    "sql_text",
    default="DELETE FROM users WHERE id = %s",
    show_default=True,
    help="SQL statement to classify repeatedly",
)
def benchmark_parser(iterations: int, sql_text: str) -> None:
    """Benchmark SQL risk-classification overhead."""
    if iterations <= 0:
        raise click.BadParameter("--iterations must be positive")

    started = time.perf_counter()
    result = None
    for _ in range(iterations):
        result = assess_risk(sql_text)
    elapsed = time.perf_counter() - started
    per_call_ms = (elapsed / iterations) * 1000

    console.print(
        Panel(
            f"SQL: [cyan]{sql_text}[/cyan]\n"
            f"Iterations: [bold]{iterations}[/bold]\n"
            f"Total: [bold]{elapsed:.4f}s[/bold]\n"
            f"Per call: [bold]{per_call_ms:.4f} ms[/bold]\n"
            f"Last result: [yellow]{result.level.value if result else 'n/a'}[/yellow]",
            title="[cyan]Parser Benchmark[/cyan]",
            border_style="cyan",
        )
    )


# ── PostgreSQL-native backup commands ────────────────────────────────────────

@cli.group()
def backup() -> None:
    """PostgreSQL-native full database backups."""


@backup.command("logical-create")
@click.option("--db", required=True, help="PostgreSQL connection URL")
@click.option("--storage", required=True, help="S3 storage URL")
@click.option("--backup-id", default=None, help="Optional stable backup ID")
@click.option("--pg-dump-bin", default="pg_dump", show_default=True, help="pg_dump executable")
@click.option("--metadata-db", default=None, help="Optional SQLite metadata DB path")
def logical_backup_create(db: str, storage: str, backup_id: Optional[str], pg_dump_bin: str, metadata_db: Optional[str]) -> None:
    """Create a full-fidelity logical backup using pg_dump custom format."""
    try:
        manifest = LogicalBackupEngine(storage).create_backup(
            db_url=db,
            backup_id=backup_id,
            pg_dump_bin=pg_dump_bin,
        )
        _record_native_manifest(metadata_db, manifest)
    except Exception as exc:
        err_console.print(Panel(f"[red]Logical backup failed:[/red]\n{exc}", title="[red]Error[/red]", border_style="red"))
        sys.exit(1)

    console.print(
        Panel(
            f"[green]Logical backup complete.[/green]\n\n"
            f"  Backup ID: [green]{manifest.backup_id}[/green]\n"
            f"  Dump:      [cyan]{manifest.artifacts['dump']}[/cyan]\n"
            f"  Manifest:  [cyan]{manifest.artifacts['manifest']}[/cyan]",
            title="[green]pg_dump[/green]",
            border_style="green",
        )
    )


@backup.command("logical-restore")
@click.option("--db", required=True, help="Target PostgreSQL connection URL")
@click.option("--storage", required=True, help="S3 storage URL")
@click.option("--backup-id", required=True, help="Logical backup ID")
@click.option("--clean/--no-clean", default=False, show_default=True, help="Run pg_restore --clean --if-exists")
@click.option("--jobs", default=1, show_default=True, help="pg_restore parallel jobs")
@click.option("--pg-restore-bin", default="pg_restore", show_default=True, help="pg_restore executable")
def logical_backup_restore(db: str, storage: str, backup_id: str, clean: bool, jobs: int, pg_restore_bin: str) -> None:
    """Restore a pg_dump custom-format backup with pg_restore."""
    try:
        manifest = LogicalBackupEngine(storage).restore_backup(
            db_url=db,
            backup_id=backup_id,
            clean=clean,
            jobs=jobs,
            pg_restore_bin=pg_restore_bin,
        )
    except Exception as exc:
        err_console.print(Panel(f"[red]Logical restore failed:[/red]\n{exc}", title="[red]Error[/red]", border_style="red"))
        sys.exit(1)

    console.print(
        Panel(
            f"[green]Logical restore complete.[/green]\n\n"
            f"  Backup ID: [green]{manifest.backup_id}[/green]\n"
            f"  Database:  [cyan]{manifest.db_name}[/cyan]",
            title="[green]pg_restore[/green]",
            border_style="green",
        )
    )


@cli.group()
def pitr() -> None:
    """Physical base backup and PITR restore preparation."""


@pitr.command("basebackup")
@click.option("--db", required=True, help="Replication-capable PostgreSQL connection URL")
@click.option("--storage", required=True, help="S3 storage URL")
@click.option("--cluster-id", required=True, help="Stable PostgreSQL cluster identifier")
@click.option("--backup-id", default=None, help="Optional stable base backup ID")
@click.option("--pg-basebackup-bin", default="pg_basebackup", show_default=True, help="pg_basebackup executable")
@click.option("--metadata-db", default=None, help="Optional SQLite metadata DB path")
def pitr_basebackup(db: str, storage: str, cluster_id: str, backup_id: Optional[str], pg_basebackup_bin: str, metadata_db: Optional[str]) -> None:
    """Create a physical base backup with pg_basebackup."""
    try:
        manifest = PhysicalBackupEngine(storage, cluster_id).create_basebackup(
            db_url=db,
            backup_id=backup_id,
            pg_basebackup_bin=pg_basebackup_bin,
        )
        _record_native_manifest(metadata_db, manifest)
    except Exception as exc:
        err_console.print(Panel(f"[red]Base backup failed:[/red]\n{exc}", title="[red]Error[/red]", border_style="red"))
        sys.exit(1)

    console.print(
        Panel(
            f"[green]Physical base backup complete.[/green]\n\n"
            f"  Cluster:   [cyan]{cluster_id}[/cyan]\n"
            f"  Backup ID: [green]{manifest.backup_id}[/green]\n"
            f"  Archive:   [cyan]{manifest.artifacts['basebackup']}[/cyan]",
            title="[green]pg_basebackup[/green]",
            border_style="green",
        )
    )


@pitr.command("prepare-restore")
@click.option("--storage", required=True, help="S3 storage URL")
@click.option("--cluster-id", required=True, help="Stable PostgreSQL cluster identifier")
@click.option("--backup-id", required=True, help="Physical base backup ID")
@click.option("--target-dir", required=True, help="Empty PostgreSQL data directory to prepare")
@click.option("--target-time", default=None, help="Optional recovery_target_time")
@click.option("--force", is_flag=True, help="Allow extracting into a non-empty target directory")
def pitr_prepare_restore(storage: str, cluster_id: str, backup_id: str, target_dir: str, target_time: Optional[str], force: bool) -> None:
    """Prepare a PostgreSQL data directory for PITR startup."""
    try:
        manifest = PhysicalBackupEngine(storage, cluster_id).prepare_restore(
            backup_id=backup_id,
            target_dir=target_dir,
            storage=storage,
            target_time=target_time,
            force=force,
        )
    except Exception as exc:
        err_console.print(Panel(f"[red]PITR restore preparation failed:[/red]\n{exc}", title="[red]Error[/red]", border_style="red"))
        sys.exit(1)

    console.print(
        Panel(
            f"[green]PITR restore directory prepared.[/green]\n\n"
            f"  Cluster:    [cyan]{cluster_id}[/cyan]\n"
            f"  Backup ID:  [green]{manifest.backup_id}[/green]\n"
            f"  Target dir: [cyan]{target_dir}[/cyan]\n"
            f"  Target time: [yellow]{target_time or 'latest available WAL'}[/yellow]",
            title="[green]PITR[/green]",
            border_style="green",
        )
    )


@cli.group()
def wal() -> None:
    """WAL archive_command and restore_command helpers."""


@wal.command("archive")
@click.option("--storage", required=True, help="S3 storage URL")
@click.option("--cluster-id", required=True, help="Stable PostgreSQL cluster identifier")
@click.option("--wal-file", required=True, help="Local WAL file path, usually archive_command %p")
@click.option("--wal-name", required=True, help="WAL file name, usually archive_command %f")
def wal_archive(storage: str, cluster_id: str, wal_file: str, wal_name: str) -> None:
    """Upload a WAL segment to S3. Intended for PostgreSQL archive_command."""
    try:
        key = WALArchive(storage, cluster_id).archive(wal_file=wal_file, wal_name=wal_name)
    except Exception as exc:
        err_console.print(Panel(f"[red]WAL archive failed:[/red]\n{exc}", title="[red]Error[/red]", border_style="red"))
        sys.exit(1)
    console.print(f"[green]Archived WAL[/green] [cyan]{wal_name}[/cyan] -> [cyan]{key}[/cyan]")


@wal.command("fetch")
@click.option("--storage", required=True, help="S3 storage URL")
@click.option("--cluster-id", required=True, help="Stable PostgreSQL cluster identifier")
@click.option("--wal-name", required=True, help="WAL file name, usually restore_command %f")
@click.option("--output", required=True, help="Output path, usually restore_command %p")
def wal_fetch(storage: str, cluster_id: str, wal_name: str, output: str) -> None:
    """Fetch a WAL segment from S3. Intended for PostgreSQL restore_command."""
    try:
        key = WALArchive(storage, cluster_id).fetch(wal_name=wal_name, output=output)
    except Exception as exc:
        err_console.print(Panel(f"[red]WAL fetch failed:[/red]\n{exc}", title="[red]Error[/red]", border_style="red"))
        sys.exit(1)
    console.print(f"[green]Fetched WAL[/green] [cyan]{key}[/cyan] -> [cyan]{output}[/cyan]")

