"""Structured event logging for backstop.

Emits structured JSON log lines via structlog and a Rich-formatted
summary line to stderr for CLI visibility.

Security rules:
- Never log full query text at INFO level.
- Never log database credentials.
- Never log actual row data.
"""

from __future__ import annotations

import logging
import sys
from typing import Optional

import structlog
from rich.console import Console

from .parser import RiskResult
from .snapshot import SnapshotManifest

# ── Module-level setup ───────────────────────────────────────────────────────

# structlog configuration — emit JSON-compatible key=value lines via stdlib
structlog.configure(
    processors=[
        structlog.stdlib.filter_by_level,
        structlog.stdlib.add_log_level,
        structlog.stdlib.add_logger_name,
        structlog.processors.TimeStamper(fmt="iso"),
        structlog.stdlib.PositionalArgumentsFormatter(),
        structlog.processors.StackInfoRenderer(),
        structlog.processors.format_exc_info,
        structlog.processors.UnicodeDecoder(),
        structlog.stdlib.ProcessorFormatter.wrap_for_formatter,
    ],
    context_class=dict,
    logger_factory=structlog.stdlib.LoggerFactory(),
    wrapper_class=structlog.stdlib.BoundLogger,
    cache_logger_on_first_use=True,
)

_struct_logger = structlog.get_logger("backstop.events")
_rich_console = Console(stderr=True, highlight=False)

# Risk-level to Rich colour mapping for terminal output
_LEVEL_COLOUR: dict[str, str] = {
    "safe": "green",
    "low": "yellow",
    "high": "orange3",
    "critical": "bold red",
}


def log_event(
    risk: RiskResult,
    query: str,
    actor: Optional[str] = None,
    manifest: Optional[SnapshotManifest] = None,
) -> None:
    """Log a backstop interception event.

    Emits a structured log record via structlog (INFO level) and prints a
    single Rich-formatted summary line to stderr.

    Args:
        risk: The :class:`~backstop.parser.RiskResult` from ``assess_risk()``.
        query: The SQL query that was assessed. Logged at DEBUG level only.
        actor: Optional actor identifier (user ID, agent ID).
        manifest: Optional :class:`~backstop.snapshot.SnapshotManifest` if a
            snapshot was taken. None for monitor-only events.
    """
    snapshot_id = manifest.snapshot_id if manifest else None
    row_count = manifest.row_count if manifest else None

    # Structured log (INFO) — safe fields only, no query text, no row data
    _struct_logger.info(
        "backstop.event",
        risk_level=risk.level.value,
        operation=risk.operation,
        table=risk.table,
        actor=actor,
        snapshot_id=snapshot_id,
        row_count=row_count,
        requires_snapshot=risk.requires_snapshot,
    )

    # Full query at DEBUG only (could contain sensitive literals)
    _struct_logger.debug("backstop.query", query=query[:500])

    # Rich one-liner to stderr
    colour = _LEVEL_COLOUR.get(risk.level.value, "white")
    level_tag = f"[{colour}]{risk.level.value.upper()}[/{colour}]"
    table_tag = f" table=[cyan]{risk.table}[/cyan]" if risk.table else ""
    actor_tag = f" actor=[dim]{actor}[/dim]" if actor else ""
    snap_tag = f" snapshot=[green]{snapshot_id}[/green] rows={row_count}" if snapshot_id else ""

    _rich_console.print(
        f"[backstop] {level_tag} {risk.operation}{table_tag}{actor_tag}{snap_tag}"
    )

