"""pytest fixtures for backstop integration tests.

Requires Docker services for integration tests:
    - PostgreSQL 16 on localhost:5432 (password: password, db: testdb)
    - MinIO on localhost:9000 (user: minioadmin, password: minioadmin)

Tests marked with @pytest.mark.integration are automatically skipped when
the required services are not available.

Environment variables (loaded from .env):
    DATABASE_URL
    S3_BUCKET
    S3_ENDPOINT
    AWS_ACCESS_KEY_ID
    AWS_SECRET_ACCESS_KEY
"""

from __future__ import annotations

import os

import boto3
import psycopg2
import psycopg2.extras
import pytest
from dotenv import load_dotenv

load_dotenv()

# Ensure MinIO credentials are in the environment so boto3 picks them up
# automatically in any SnapshotEngine / RestoreEngine instantiated during tests.
os.environ.setdefault("AWS_ACCESS_KEY_ID", "minioadmin")
os.environ.setdefault("AWS_SECRET_ACCESS_KEY", "minioadmin")

DATABASE_URL: str = os.getenv(
    "DATABASE_URL", "postgresql://postgres:password@localhost:5433/testdb"
)
S3_BUCKET: str = os.getenv("S3_BUCKET", "backstop-test")
S3_ENDPOINT: str = os.getenv("S3_ENDPOINT", "http://localhost:9000")
AWS_KEY: str = os.getenv("AWS_ACCESS_KEY_ID", "minioadmin")
AWS_SECRET: str = os.getenv("AWS_SECRET_ACCESS_KEY", "minioadmin")


# ── Service availability detection ──────────────────────────────────────────

def _postgres_available() -> bool:
    try:
        conn = psycopg2.connect(DATABASE_URL, connect_timeout=3)
        conn.close()
        return True
    except Exception:
        return False


def _minio_available() -> bool:
    try:
        client = boto3.client(
            "s3",
            endpoint_url=S3_ENDPOINT,
            aws_access_key_id=AWS_KEY,
            aws_secret_access_key=AWS_SECRET,
            region_name="us-east-1",
        )
        client.list_buckets()
        return True
    except Exception:
        return False


# Check once at collection time — avoids repeated connection attempts per test
_POSTGRES_UP = _postgres_available()
_MINIO_UP = _minio_available()

requires_postgres = pytest.mark.skipif(
    not _POSTGRES_UP,
    reason="PostgreSQL not available (start with: docker run --name backstop-postgres -e POSTGRES_PASSWORD=password -e POSTGRES_DB=testdb -p 5433:5432 -d postgres:16)",
)
requires_minio = pytest.mark.skipif(
    not _MINIO_UP,
    reason="MinIO not available (start with: docker run --name backstop-minio -p 9000:9000 -p 9001:9001 -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin quay.io/minio/minio server /data --console-address ':9001')",
)
requires_services = pytest.mark.skipif(
    not (_POSTGRES_UP and _MINIO_UP),
    reason="Integration services (Postgres + MinIO) not available. Start Docker services first.",
)


# ── Auto-skip hook for integration tests ────────────────────────────────────

def pytest_runtest_setup(item: pytest.Item) -> None:
    """Skip integration tests when required services are unavailable."""
    if "integration" in item.keywords:
        if not (_POSTGRES_UP and _MINIO_UP):
            reason_parts = []
            if not _POSTGRES_UP:
                reason_parts.append("PostgreSQL")
            if not _MINIO_UP:
                reason_parts.append("MinIO")
            pytest.skip(
                f"Integration services not available: {', '.join(reason_parts)}. "
                "Start Docker containers to run these tests."
            )


# ── Live service fixtures ────────────────────────────────────────────────────

@pytest.fixture
def pg_conn():
    """Fresh psycopg2 connection to the test database.

    Yields a connection with autocommit disabled. Any uncommitted changes
    are rolled back after each test.
    """
    conn = psycopg2.connect(DATABASE_URL)
    conn.autocommit = False
    yield conn
    try:
        conn.rollback()
    except Exception:
        pass
    try:
        conn.close()
    except Exception:
        pass


@pytest.fixture
def s3_client():
    """boto3 S3 client pointed at the local MinIO instance.

    Creates the test bucket if it does not already exist.
    """
    client = boto3.client(
        "s3",
        endpoint_url=S3_ENDPOINT,
        aws_access_key_id=AWS_KEY,
        aws_secret_access_key=AWS_SECRET,
        region_name="us-east-1",
    )
    try:
        client.create_bucket(Bucket=S3_BUCKET)
    except client.exceptions.BucketAlreadyOwnedByYou:
        pass
    except Exception:
        pass
    return client


@pytest.fixture
def users_table(pg_conn):
    """Create a ``users`` table with 5 seed rows for testing.

    Drops and recreates the table on setup; drops both ``users`` and
    ``users_recovered`` on teardown.
    """
    cur = pg_conn.cursor()
    cur.execute("DROP TABLE IF EXISTS users CASCADE")
    cur.execute("DROP TABLE IF EXISTS users_recovered CASCADE")
    cur.execute("DROP TABLE IF EXISTS users_backup CASCADE")
    cur.execute(
        """
        CREATE TABLE users (
            id SERIAL PRIMARY KEY,
            name VARCHAR(100) NOT NULL,
            email VARCHAR(255) UNIQUE NOT NULL,
            created_at TIMESTAMP DEFAULT NOW()
        )
        """
    )

    seed_rows = [
        ("Alice", "alice@example.com"),
        ("Bob", "bob@example.com"),
        ("Carol", "carol@example.com"),
        ("Dave", "dave@example.com"),
        ("Eve", "eve@example.com"),
    ]
    psycopg2.extras.execute_values(
        cur,
        "INSERT INTO users (name, email) VALUES %s",
        seed_rows,
    )
    pg_conn.commit()

    yield

    cur2 = pg_conn.cursor()
    cur2.execute("DROP TABLE IF EXISTS users CASCADE")
    cur2.execute("DROP TABLE IF EXISTS users_recovered CASCADE")
    cur2.execute("DROP TABLE IF EXISTS users_backup CASCADE")
    pg_conn.commit()


# ── Moto-based offline S3 fixture ────────────────────────────────────────────

@pytest.fixture
def moto_s3():
    """Mocked S3 client via moto — no MinIO required.

    Creates an in-memory S3 bucket ``backstop-test`` for unit testing snapshot
    and restore logic without external services.
    """
    try:
        from moto import mock_aws
    except ImportError:
        pytest.skip("moto not installed — run: pip install 'backstop[dev]'")

    with mock_aws():
        client = boto3.client(
            "s3",
            region_name="us-east-1",
            aws_access_key_id="test",
            aws_secret_access_key="test",
        )
        client.create_bucket(Bucket="backstop-test")
        yield client

