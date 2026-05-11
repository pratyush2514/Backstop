#!/usr/bin/env sh
set -eu

BACKSTOP_REPO="${BACKSTOP_REPO:-pratyush2514/Backstop}"
BACKSTOP_REF="${BACKSTOP_REF:-main}"
BACKSTOP_INSTALL_DIR="${BACKSTOP_INSTALL_DIR:-$HOME/.backstop}"
BACKSTOP_RAW_BASE="${BACKSTOP_RAW_BASE:-https://raw.githubusercontent.com/$BACKSTOP_REPO/$BACKSTOP_REF}"

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "backstop installer: missing required command: $1" >&2
    exit 1
  fi
}

download() {
  src="$1"
  dst="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$src" -o "$dst"
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -q "$src" -O "$dst"
    return
  fi
  echo "backstop installer: install curl or wget" >&2
  exit 1
}

need docker

mkdir -p "$BACKSTOP_INSTALL_DIR/bin"
download "$BACKSTOP_RAW_BASE/deploy/docker-compose.oss.yml" "$BACKSTOP_INSTALL_DIR/docker-compose.yml"
download "$BACKSTOP_RAW_BASE/deploy/seed.sql" "$BACKSTOP_INSTALL_DIR/seed.sql"
download "$BACKSTOP_RAW_BASE/examples/gateway-auth-tokens.example.json" "$BACKSTOP_INSTALL_DIR/gateway-auth-tokens.json"

if [ ! -f "$BACKSTOP_INSTALL_DIR/backstop.env" ]; then
  cat > "$BACKSTOP_INSTALL_DIR/backstop.env" <<'EOF'
BACKSTOP_VERSION=latest
BACKSTOP_GATEWAY_IMAGE=ghcr.io/pratyush2514/backstop-gateway:latest
BACKSTOP_SYNC_IMAGE=ghcr.io/pratyush2514/backstop-sync:latest

BACKSTOP_ENVIRONMENT=local
BACKSTOP_CLUSTER_ID=local
BACKSTOP_BUCKET=backstop-test
BACKSTOP_STORAGE=s3://backstop-test@http://minio:9000

POSTGRES_DB=testdb
POSTGRES_USER=postgres
POSTGRES_PASSWORD=password
POSTGRES_PORT=5433

MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=minioadmin
MINIO_PORT=9000
MINIO_CONSOLE_PORT=9001

BACKSTOP_GATEWAY_PORT=8080
BACKSTOP_SYNC_METRICS_PORT=9091
EOF
fi

cat > "$BACKSTOP_INSTALL_DIR/bin/backstop-oss" <<'EOF'
#!/usr/bin/env sh
set -eu

BACKSTOP_HOME="${BACKSTOP_HOME:-$HOME/.backstop}"
COMPOSE="$BACKSTOP_HOME/docker-compose.yml"
ENV_FILE="$BACKSTOP_HOME/backstop.env"

compose() {
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE" -p backstop_oss "$@"
}

case "${1:-help}" in
  up)
    compose up -d
    ;;
  down)
    compose down
    ;;
  reset)
    compose down -v
    compose up -d
    ;;
  status)
    compose ps
    ;;
  logs)
    shift || true
    compose logs "$@"
    ;;
  seed)
    compose exec -T postgres psql -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-testdb}" -v ON_ERROR_STOP=1 -f /seed/seed.sql
    ;;
  doctor)
    docker version >/dev/null
    docker compose version >/dev/null
    echo "backstop doctor: docker and compose are available"
    ;;
  mcp-config)
    cat <<JSON
{
  "mcpServers": {
    "backstop": {
      "command": "npx",
      "args": ["@backstop/mcp-server"],
      "env": {
        "BACKSTOP_URL": "http://localhost:8080",
        "BACKSTOP_TOKEN": "replace-agent-token",
        "BACKSTOP_AGENT_ID": "local-ai-agent",
        "BACKSTOP_MCP_MODE": "agent"
      }
    }
  }
}
JSON
    ;;
  where)
    echo "$BACKSTOP_HOME"
    ;;
  help|*)
    cat <<HELP
backstop-oss commands:
  up          Start free OSS stack
  down        Stop stack
  reset       Stop stack, delete volumes, start fresh
  status      Show containers
  logs [svc]  Show logs
  seed        Load sample seed SQL into local Postgres
  doctor      Check Docker/Compose availability
  mcp-config  Print MCP config snippet
  where       Print install directory

Config files live in: $BACKSTOP_HOME
HELP
    ;;
esac
EOF
chmod +x "$BACKSTOP_INSTALL_DIR/bin/backstop-oss"

case ":$PATH:" in
  *":$BACKSTOP_INSTALL_DIR/bin:"*) ;;
  *)
    echo
    echo "Add backstop to PATH:"
    echo "  export PATH=\"$BACKSTOP_INSTALL_DIR/bin:\$PATH\""
    ;;
esac

echo
echo "backstop OSS installed in $BACKSTOP_INSTALL_DIR"
echo "Try:"
echo "  backstop-oss doctor"
echo "  backstop-oss up"
echo "  backstop-oss mcp-config"

