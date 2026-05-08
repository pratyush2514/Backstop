"""SQLite metadata helpers shared by backstop CLI launch drills."""

from __future__ import annotations

import json
import sqlite3
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Optional


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat()


class MetadataStore:
    """Small stdlib SQLite store compatible with the Go gateway/sidecar schema."""

    def __init__(self, path: Optional[str]) -> None:
        self.path = path
        self.conn: Optional[sqlite3.Connection] = None
        if path:
            Path(path).parent.mkdir(parents=True, exist_ok=True)
            self.conn = sqlite3.connect(path)
            self._init()

    def close(self) -> None:
        if self.conn is not None:
            self.conn.close()

    def _init(self) -> None:
        assert self.conn is not None
        self.conn.execute("PRAGMA journal_mode=WAL")
        self.conn.execute("PRAGMA busy_timeout=5000")
        self.conn.executescript(
            """
            CREATE TABLE IF NOT EXISTS native_backups (
                backup_id TEXT PRIMARY KEY,
                backup_type TEXT NOT NULL,
                db_name TEXT,
                cluster_id TEXT,
                timestamp TEXT NOT NULL,
                payload_json TEXT
            );
            CREATE TABLE IF NOT EXISTS health_checks (
                component TEXT PRIMARY KEY,
                status TEXT NOT NULL,
                checked_at TEXT NOT NULL,
                detail_json TEXT
            );
            CREATE TABLE IF NOT EXISTS restore_events (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                timestamp TEXT NOT NULL,
                snapshot_id TEXT,
                source_table TEXT,
                target_table TEXT,
                status TEXT NOT NULL,
                payload_json TEXT
            );
            """
        )
        self.conn.commit()

    def record_native_backup(self, manifest: Any) -> None:
        if self.conn is None:
            return
        payload = manifest.model_dump() if hasattr(manifest, "model_dump") else dict(manifest)
        self.conn.execute(
            """
            INSERT OR REPLACE INTO native_backups
              (backup_id, backup_type, db_name, cluster_id, timestamp, payload_json)
            VALUES (?, ?, ?, ?, ?, ?)
            """,
            (
                payload.get("backup_id"),
                payload.get("backup_type"),
                payload.get("db_name"),
                payload.get("cluster_id"),
                payload.get("timestamp") or utc_now(),
                json.dumps(payload),
            ),
        )
        self.conn.commit()

    def record_health(self, component: str, status: str, detail: dict[str, Any]) -> None:
        if self.conn is None:
            return
        self.conn.execute(
            """
            INSERT OR REPLACE INTO health_checks
              (component, status, checked_at, detail_json)
            VALUES (?, ?, ?, ?)
            """,
            (component, status, utc_now(), json.dumps(detail)),
        )
        self.conn.commit()

    def record_restore_event(self, snapshot_id: str, source_table: str, target_table: str, status: str, payload: dict[str, Any]) -> None:
        if self.conn is None:
            return
        self.conn.execute(
            """
            INSERT INTO restore_events
              (timestamp, snapshot_id, source_table, target_table, status, payload_json)
            VALUES (?, ?, ?, ?, ?, ?)
            """,
            (utc_now(), snapshot_id, source_table, target_table, status, json.dumps(payload)),
        )
        self.conn.commit()

