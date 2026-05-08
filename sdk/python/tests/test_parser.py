"""Unit tests for backstop.parser — SQL risk assessment.

These tests have zero external dependencies (no DB, no S3) and run offline.
Every test asserts on the RiskLevel returned by assess_risk().
"""

from __future__ import annotations

import pytest

from backstop.parser import (
    RiskLevel,
    RiskResult,
    assess_risk,
    build_before_image_query,
    select_before_image_params,
)


# ── Parametrized classification tests ────────────────────────────────────────

@pytest.mark.parametrize(
    "sql,expected_level",
    [
        # ── CRITICAL ────────────────────────────────────────────────────────
        ("DROP TABLE users", RiskLevel.CRITICAL),
        ("DROP TABLE IF EXISTS users", RiskLevel.CRITICAL),
        ("DROP DATABASE mydb", RiskLevel.CRITICAL),
        ("DROP SCHEMA public CASCADE", RiskLevel.CRITICAL),
        ("TRUNCATE TABLE orders", RiskLevel.CRITICAL),
        ("TRUNCATE orders", RiskLevel.CRITICAL),
        ("DELETE FROM users", RiskLevel.CRITICAL),
        ("UPDATE users SET balance = 0", RiskLevel.CRITICAL),
        ("UPDATE users SET name = 'x', email = 'y'", RiskLevel.CRITICAL),
        # Multi-statement: must detect CRITICAL even if wrapped in txn
        ("BEGIN; DROP TABLE users; COMMIT;", RiskLevel.CRITICAL),
        ("BEGIN; TRUNCATE orders; COMMIT;", RiskLevel.CRITICAL),
        ("BEGIN; DELETE FROM users; COMMIT;", RiskLevel.CRITICAL),
        # ── HIGH ────────────────────────────────────────────────────────────
        ("ALTER TABLE users DROP COLUMN email", RiskLevel.HIGH),
        ("ALTER TABLE orders DROP COLUMN amount", RiskLevel.HIGH),
        # ── LOW ─────────────────────────────────────────────────────────────
        ("DELETE FROM users WHERE id = 1", RiskLevel.LOW),
        ("DELETE FROM users WHERE email = 'alice@example.com'", RiskLevel.LOW),
        ("UPDATE users SET name = 'x' WHERE id = 1", RiskLevel.LOW),
        ("UPDATE orders SET status = 'closed' WHERE created_at < '2025-01-01'", RiskLevel.LOW),
        # ── SAFE ────────────────────────────────────────────────────────────
        ("SELECT * FROM users", RiskLevel.SAFE),
        ("SELECT id, name FROM users WHERE id > 5", RiskLevel.SAFE),
        ("INSERT INTO users (name, email) VALUES ('test', 'test@example.com')", RiskLevel.SAFE),
        ("CREATE TABLE orders (id SERIAL PRIMARY KEY)", RiskLevel.SAFE),
        ("CREATE INDEX idx_users_email ON users (email)", RiskLevel.SAFE),
        ("ALTER TABLE users ADD COLUMN phone VARCHAR(20)", RiskLevel.SAFE),
        ("BEGIN", RiskLevel.SAFE),
        ("COMMIT", RiskLevel.SAFE),
        ("ROLLBACK", RiskLevel.SAFE),
        ("EXPLAIN SELECT * FROM users", RiskLevel.SAFE),
        # ── Parse failure / edge cases ───────────────────────────────────────
        ("this is not sql !@#$", RiskLevel.SAFE),
        ("", RiskLevel.SAFE),
        ("   ", RiskLevel.SAFE),
        ("!!! not sql at all !!!", RiskLevel.SAFE),
    ],
)
def test_risk_classification(sql: str, expected_level: RiskLevel) -> None:
    """Every SQL pattern must map to the correct RiskLevel."""
    result = assess_risk(sql)
    assert result.level == expected_level, (
        f"SQL: {sql!r}\n"
        f"  Expected: {expected_level}\n"
        f"  Got:      {result.level}\n"
        f"  Reason:   {result.reason}"
    )


# ── Individual assertion tests ────────────────────────────────────────────────

def test_drop_table_extracts_table_name() -> None:
    """DROP TABLE must extract the table name into result.table."""
    result = assess_risk("DROP TABLE users")
    assert result.table is not None
    assert "users" in result.table.lower()


def test_drop_database_extracts_db_name() -> None:
    """DROP DATABASE must report operation='DROP DATABASE'."""
    result = assess_risk("DROP DATABASE mydb")
    assert result.level == RiskLevel.CRITICAL
    assert result.operation == "DROP DATABASE"


def test_truncate_extracts_table_name() -> None:
    """TRUNCATE must extract the table name."""
    result = assess_risk("TRUNCATE TABLE orders")
    assert result.table is not None
    assert "orders" in result.table.lower()


def test_delete_no_where_is_critical() -> None:
    """DELETE without WHERE must be CRITICAL and require a snapshot."""
    result = assess_risk("DELETE FROM orders")
    assert result.level == RiskLevel.CRITICAL
    assert result.requires_snapshot is True
    assert result.requires_approval is True


def test_delete_with_where_is_low() -> None:
    """DELETE with WHERE must be LOW but still capture a before-image snapshot."""
    result = assess_risk("DELETE FROM orders WHERE id = 42")
    assert result.level == RiskLevel.LOW
    assert result.requires_snapshot is True
    assert result.requires_approval is False


def test_update_no_where_is_critical() -> None:
    """UPDATE without WHERE must be CRITICAL."""
    result = assess_risk("UPDATE users SET active = false")
    assert result.level == RiskLevel.CRITICAL
    assert result.requires_snapshot is True


def test_update_with_where_is_low() -> None:
    """UPDATE with WHERE is LOW risk but still recoverable via before-image snapshot."""
    result = assess_risk("UPDATE users SET active = false WHERE id = 1")
    assert result.level == RiskLevel.LOW
    assert result.requires_snapshot is True


def test_alter_drop_column_is_high() -> None:
    """ALTER TABLE DROP COLUMN must be HIGH risk with snapshot required."""
    result = assess_risk("ALTER TABLE users DROP COLUMN email")
    assert result.level == RiskLevel.HIGH
    assert result.requires_snapshot is True
    assert result.requires_approval is False


def test_alter_add_column_is_safe() -> None:
    """ALTER TABLE ADD COLUMN must be SAFE."""
    result = assess_risk("ALTER TABLE users ADD COLUMN phone VARCHAR(20)")
    assert result.level == RiskLevel.SAFE
    assert result.requires_snapshot is False


def test_parse_failure_is_safe() -> None:
    """Parse failures must always return SAFE — never raise, never block."""
    result = assess_risk("!!! not sql at all !!!")
    assert result.level == RiskLevel.SAFE
    assert result.requires_snapshot is False
    assert result.requires_approval is False


def test_empty_string_is_safe() -> None:
    """Empty SQL must return SAFE."""
    result = assess_risk("")
    assert result.level == RiskLevel.SAFE


def test_whitespace_only_is_safe() -> None:
    """Whitespace-only SQL must return SAFE."""
    result = assess_risk("   \n\t  ")
    assert result.level == RiskLevel.SAFE


def test_multistatement_detects_critical() -> None:
    """Multi-statement SQL must return the highest risk level across all statements."""
    result = assess_risk("BEGIN; DROP TABLE users; COMMIT;")
    assert result.level == RiskLevel.CRITICAL


def test_multistatement_safe_plus_critical_is_critical() -> None:
    """A mix of SAFE and CRITICAL statements must resolve to CRITICAL."""
    result = assess_risk("SELECT 1; DELETE FROM orders;")
    assert result.level == RiskLevel.CRITICAL


def test_risk_result_is_dataclass() -> None:
    """RiskResult must be a dataclass with all required fields."""
    result = assess_risk("SELECT * FROM users")
    assert isinstance(result, RiskResult)
    assert hasattr(result, "level")
    assert hasattr(result, "operation")
    assert hasattr(result, "table")
    assert hasattr(result, "reason")
    assert hasattr(result, "requires_snapshot")
    assert hasattr(result, "requires_approval")


def test_safe_select_has_no_snapshot_requirement() -> None:
    """SELECT must never require a snapshot or approval."""
    result = assess_risk("SELECT * FROM users WHERE active = true")
    assert result.requires_snapshot is False
    assert result.requires_approval is False


def test_critical_requires_both_snapshot_and_approval() -> None:
    """CRITICAL operations must require both snapshot and approval."""
    result = assess_risk("DROP TABLE payments")
    assert result.requires_snapshot is True
    assert result.requires_approval is True


def test_update_before_image_uses_where_params_only() -> None:
    """UPDATE before-image SELECT must use only WHERE parameters."""
    before = build_before_image_query("UPDATE users SET name = %s WHERE id = %s")
    assert before is not None
    assert before.select_sql == "SELECT * FROM users WHERE id = %s"
    assert select_before_image_params(("New Name", 42), before) == (42,)


def test_delete_before_image_keeps_all_params() -> None:
    """DELETE before-image SELECT can reuse the original parameter tuple."""
    before = build_before_image_query("DELETE FROM users WHERE id = %s")
    assert before is not None
    assert before.select_sql == "SELECT * FROM users WHERE id = %s"
    assert select_before_image_params((42,), before) == (42,)

