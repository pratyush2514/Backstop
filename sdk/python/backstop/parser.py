"""SQL AST risk assessment for backstop.

Uses sqlglot to parse SQL and classify operations by risk level.
NEVER uses regex for SQL parsing — always AST-based.
Parse failures are treated as SAFE (passthrough).
"""

from __future__ import annotations

import logging
from dataclasses import dataclass
from enum import Enum
from typing import Optional, Sequence

import sqlglot
import sqlglot.expressions as exp

logger = logging.getLogger(__name__)


class RiskLevel(Enum):
    """Risk classification for SQL operations."""

    SAFE = "safe"
    LOW = "low"
    HIGH = "high"
    CRITICAL = "critical"


@dataclass
class RiskResult:
    """Result of SQL risk assessment.

    Attributes:
        level: The risk classification.
        operation: Human-readable operation name (e.g. "DROP TABLE").
        table: The primary table affected, or None if not applicable.
        reason: Human-readable explanation of the classification.
        requires_snapshot: Whether a pre-execution snapshot should be taken.
        requires_approval: Whether human approval is required before execution.
    """

    level: RiskLevel
    operation: str
    table: Optional[str]
    reason: str
    requires_snapshot: bool
    requires_approval: bool


@dataclass
class BeforeImageQuery:
    """SELECT query used to capture rows before a destructive statement."""

    table: str
    select_sql: str
    param_indexes: Optional[tuple[int, ...]] = None


# ── Risk level ordering for multi-statement comparison ──────────────────────

_LEVEL_ORDER: dict[RiskLevel, int] = {
    RiskLevel.SAFE: 0,
    RiskLevel.LOW: 1,
    RiskLevel.HIGH: 2,
    RiskLevel.CRITICAL: 3,
}


def _higher_risk(a: RiskResult, b: RiskResult) -> RiskResult:
    """Return whichever result represents higher risk."""
    return a if _LEVEL_ORDER[a.level] >= _LEVEL_ORDER[b.level] else b


# ── Safe passthrough sentinel ────────────────────────────────────────────────

def _safe(operation: str = "UNKNOWN", table: Optional[str] = None, reason: str = "Safe operation") -> RiskResult:
    return RiskResult(
        level=RiskLevel.SAFE,
        operation=operation,
        table=table,
        reason=reason,
        requires_snapshot=False,
        requires_approval=False,
    )


def _low(
    operation: str,
    table: Optional[str],
    reason: str,
    requires_snapshot: bool = False,
) -> RiskResult:
    return RiskResult(
        level=RiskLevel.LOW,
        operation=operation,
        table=table,
        reason=reason,
        requires_snapshot=requires_snapshot,
        requires_approval=False,
    )


def _high(operation: str, table: Optional[str], reason: str) -> RiskResult:
    return RiskResult(
        level=RiskLevel.HIGH,
        operation=operation,
        table=table,
        reason=reason,
        requires_snapshot=True,
        requires_approval=False,
    )


def _critical(operation: str, table: Optional[str], reason: str) -> RiskResult:
    return RiskResult(
        level=RiskLevel.CRITICAL,
        operation=operation,
        table=table,
        reason=reason,
        requires_snapshot=True,
        requires_approval=True,
    )


# ── Table name extraction ────────────────────────────────────────────────────

def _extract_table_name(statement: exp.Expression) -> Optional[str]:
    """Safely extract the primary table name from a parsed statement."""
    try:
        table_node = statement.find(exp.Table)
        if table_node is not None:
            return str(table_node.name) if table_node.name else str(table_node)
        return None
    except Exception:
        return None


# ── Single-statement risk assessment ────────────────────────────────────────

def _assess_statement(statement: exp.Expression) -> RiskResult:
    """Assess the risk level of a single parsed SQL statement."""

    # DROP TABLE / DROP DATABASE / DROP SCHEMA
    if isinstance(statement, exp.Drop):
        kind: str = (statement.args.get("kind") or "").upper()

        if kind == "TABLE":
            table = _extract_table_name(statement)
            return _critical(
                operation="DROP TABLE",
                table=table,
                reason="DROP TABLE permanently destroys the table and all its data",
            )

        if kind in ("DATABASE", "SCHEMA"):
            return _critical(
                operation=f"DROP {kind}",
                table=None,
                reason=f"DROP {kind} is not recoverable by a table-level SDK snapshot",
            )

        # Other DROP variants (VIEW, INDEX, etc.) — treat as SAFE
        table = _extract_table_name(statement)
        return _safe(operation=f"DROP {kind}", table=table, reason=f"DROP {kind} is not a data-destructive operation")

    # TRUNCATE
    if isinstance(statement, exp.TruncateTable):
        table = _extract_table_name(statement)
        return _critical(
            operation="TRUNCATE TABLE",
            table=table,
            reason="TRUNCATE TABLE removes all rows without a WHERE clause",
        )

    # DELETE
    if isinstance(statement, exp.Delete):
        has_where = statement.find(exp.Where) is not None
        table = _extract_table_name(statement)
        if not has_where:
            return _critical(
                operation="DELETE",
                table=table,
                reason="DELETE without WHERE clause destroys all rows in the table",
            )
        return _low(
            operation="DELETE",
            table=table,
            reason="DELETE with WHERE clause affects a scoped subset of rows",
            requires_snapshot=True,
        )

    # UPDATE
    if isinstance(statement, exp.Update):
        has_where = statement.find(exp.Where) is not None
        table = _extract_table_name(statement)
        if not has_where:
            return _critical(
                operation="UPDATE",
                table=table,
                reason="UPDATE without WHERE clause modifies all rows in the table",
            )
        return _low(
            operation="UPDATE",
            table=table,
            reason="UPDATE with WHERE clause affects a scoped subset of rows",
            requires_snapshot=True,
        )

    # ALTER TABLE (sqlglot uses exp.Alter with kind="TABLE", not exp.AlterTable)
    if isinstance(statement, exp.Alter) and (statement.args.get("kind") or "").upper() == "TABLE":
        table = _extract_table_name(statement)
        # Check if any action is a column drop
        for action in statement.args.get("actions", []):
            if isinstance(action, exp.Drop) and (action.args.get("kind") or "").upper() == "COLUMN":
                return _high(
                    operation="ALTER TABLE DROP COLUMN",
                    table=table,
                    reason="Dropping a column is irreversible and removes all data in that column",
                )
        # Other ALTER TABLE operations (ADD COLUMN, etc.) — safe
        return _safe(
            operation="ALTER TABLE",
            table=table,
            reason="ALTER TABLE without column drop is not data-destructive",
        )

    # Everything else is SAFE: SELECT, INSERT, CREATE, BEGIN, COMMIT, ROLLBACK, EXPLAIN, etc.
    operation = type(statement).__name__.upper()
    table = _extract_table_name(statement)
    return _safe(operation=operation, table=table, reason="Non-destructive operation")


# ── Public API ───────────────────────────────────────────────────────────────

def assess_risk(sql: str) -> RiskResult:
    """Assess the risk level of a SQL string.

    Handles multi-statement SQL by returning the highest risk level found
    across all statements.

    Args:
        sql: The SQL string to assess. May be empty, multi-statement, or invalid.

    Returns:
        RiskResult with the classification. Parse failures and empty input
        always return RiskLevel.SAFE — never raise exceptions.
    """
    if not sql or not sql.strip():
        return _safe(operation="EMPTY", reason="Empty SQL — safe passthrough")

    try:
        statements = sqlglot.parse(sql, dialect="postgres")
    except Exception as exc:
        logger.debug("[backstop] SQL parse failed (treated as SAFE): %s", exc)
        return _safe(operation="PARSE_FAILURE", reason="SQL parse failure — treated as safe passthrough")

    if not statements:
        return _safe(operation="EMPTY", reason="No parseable statements — safe passthrough")

    # Filter out None entries that sqlglot can emit for empty statements
    parsed = [s for s in statements if s is not None]
    if not parsed:
        return _safe(operation="EMPTY", reason="No parseable statements — safe passthrough")

    result = _safe()
    for statement in parsed:
        try:
            stmt_result = _assess_statement(statement)
        except Exception as exc:
            logger.debug("[backstop] Statement assessment failed (treated as SAFE): %s", exc)
            stmt_result = _safe(reason="Statement assessment error — treated as safe passthrough")
        result = _higher_risk(result, stmt_result)

    return result


def build_before_image_query(sql: str) -> Optional[BeforeImageQuery]:
    """Build a row-scoped SELECT for statements where this is mechanically safe.

    Returns a scoped before-image query for single-statement ``DELETE`` and
    ``UPDATE`` statements with a WHERE clause. For positional parameters in an
    UPDATE, ``param_indexes`` identifies the subset of parameters used by the
    generated SELECT because SET parameters appear before WHERE parameters in
    the original statement.

    Args:
        sql: The original SQL statement.

    Returns:
        A :class:`BeforeImageQuery` for scoped capture, or ``None`` when the
        guard should use the safer full-table snapshot path.
    """
    if not sql or not sql.strip():
        return None

    try:
        statements = [s for s in sqlglot.parse(sql, dialect="postgres") if s is not None]
    except Exception:
        return None

    if len(statements) != 1:
        return None

    statement = statements[0]
    if not isinstance(statement, (exp.Delete, exp.Update)):
        return None

    where = statement.args.get("where")
    table_node = statement.find(exp.Table)
    if where is None or table_node is None:
        return None

    table_name = table_node.name or str(table_node)
    table_sql = table_node.sql(dialect="postgres")
    where_sql = where.sql(dialect="postgres")
    param_indexes = _where_param_indexes(statement)
    return BeforeImageQuery(
        table=str(table_name),
        select_sql=f"SELECT * FROM {table_sql} {where_sql}",
        param_indexes=param_indexes,
    )


def select_before_image_params(params: object, before_image: BeforeImageQuery) -> object:
    """Return the params object that should be used for a before-image SELECT."""
    if before_image.param_indexes is None or params is None:
        return params

    if isinstance(params, Sequence) and not isinstance(params, (str, bytes, bytearray)):
        values = tuple(params)
        return tuple(values[i] for i in before_image.param_indexes if i < len(values))

    return params


def _where_param_indexes(statement: exp.Expression) -> Optional[tuple[int, ...]]:
    """Map WHERE placeholders to positional parameter indexes when possible."""
    where = statement.args.get("where")
    if where is None:
        return None

    if isinstance(statement, exp.Update):
        set_param_count = sum(
            1 for expression in statement.args.get("expressions", []) for _ in expression.find_all(exp.Placeholder)
        )
        where_param_count = sum(1 for _ in where.find_all(exp.Placeholder))
        if where_param_count:
            return tuple(range(set_param_count, set_param_count + where_param_count))

    placeholders = list(statement.find_all(exp.Placeholder))
    where_placeholders = set(where.find_all(exp.Placeholder))
    if not placeholders or not where_placeholders:
        return None

    indexes = tuple(i for i, node in enumerate(placeholders) if node in where_placeholders)
    return indexes or None

