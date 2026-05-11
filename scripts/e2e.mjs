#!/usr/bin/env node
import { spawn } from "node:child_process";

const options = parseArgs(process.argv.slice(2));
const report = {
  ok: false,
  project: options.project,
  started_at: new Date().toISOString(),
  steps: [],
};

function addStep(name, status, detail = null) {
  report.steps.push({
    name,
    status,
    detail,
    at: new Date().toISOString(),
  });
}

async function main() {
  try {
    await run("docker", ["info"], { quiet: true });
    addStep("docker_engine", "ok");

    await run("docker", ["compose", "-f", options.composeFile, "-p", options.project, "config", "--quiet"]);
    addStep("compose_config", "ok");

    await run("docker", ["compose", "-f", options.composeFile, "-p", options.project, "down", "-v", "--remove-orphans"], { quiet: true });
    addStep("compose_clean", "ok");

    await run("docker", ["compose", "-f", options.composeFile, "-p", options.project, "build"]);
    addStep("compose_build", "ok");

    await run("docker", ["compose", "-f", options.composeFile, "-p", options.project, "up", "-d", "--remove-orphans"]);
    addStep("compose_up", "ok");

    await waitHttp(gatewayUrl("/health"));
    addStep("gateway_health", "ok");

    await run("docker", [
      "compose",
      "-f",
      options.composeFile,
      "-p",
      options.project,
      "exec",
      "-T",
      "postgres",
      "psql",
      "-U",
      "postgres",
      "-d",
      "testdb",
      "-v",
      "ON_ERROR_STOP=1",
      "-c",
      "SELECT count(*) FROM users;",
    ]);
    addStep("seed_database_loaded", "ok");

    let snapshots = await waitForLatestValidSnapshot("audit_logs");
    let snapshotId = snapshots.items[0].snapshot_id;
    addStep("snapshot_metadata", "ok", { snapshot_id: snapshotId });

    const safe = await invokeMcp({
      jsonrpc: "2.0",
      id: 1,
      method: "tools/call",
      params: {
        name: "execute_query",
        arguments: {
          agent_id: "oss-e2e",
          query: "SELECT count(*) AS users FROM users",
        },
      },
    });
    addStep("safe_query", "ok", safe.result?.status);

    const blocked = await invokeMcp({
      jsonrpc: "2.0",
      id: 2,
      method: "tools/call",
      params: {
        name: "execute_query",
        arguments: {
          agent_id: "oss-e2e",
          query: "DROP DATABASE testdb",
        },
      },
    });
    if (blocked.result?.status !== "blocked") {
      throw new Error("DROP DATABASE was not blocked");
    }
    addStep("blocked_drop_database", "ok");

    snapshots = await waitForLatestValidSnapshot("audit_logs");
    snapshotId = snapshots.items[0].snapshot_id;
    addStep("critical_latest_snapshot", "ok", { snapshot_id: snapshotId });

    const criticalPromise = invokeMcp({
      jsonrpc: "2.0",
      id: 3,
      method: "tools/call",
      params: {
        name: "execute_query",
        arguments: {
          agent_id: "oss-e2e",
          query: "DROP TABLE audit_logs",
          snapshot_id: snapshotId,
        },
      },
    });

    await sleep(2_000);
    const pending = await getJson(gatewayUrl("/pending"), authHeaders());
    if (!pending.pending || pending.pending.length < 1) {
      throw new Error("No pending critical approval found");
    }
    const approvalId = pending.pending[0].id;
    await postJson(gatewayUrl(`/approve/${encodeURIComponent(approvalId)}`), undefined, authHeaders());

    const critical = await criticalPromise;
    if (critical.result?.status !== "executed") {
      throw new Error("DROP TABLE did not execute after approval");
    }
    addStep("critical_drop_table_approved", "ok", { approval_id: approvalId });

    await run("docker", [
      "compose",
      "-f",
      options.composeFile,
      "-p",
      options.project,
      "exec",
      "-T",
      "postgres",
      "backstop",
      "restore",
      "--db",
      "postgresql://postgres:password@localhost:5432/testdb",
      "--storage",
      "s3://backstop-test@http://minio:9000",
      "--snapshot-id",
      snapshotId,
      "--table",
      "audit_logs",
      "--metadata-db",
      "/metadata/backstop.db",
    ]);
    addStep("restore_audit_logs", "ok");

    await run("docker", [
      "compose",
      "-f",
      options.composeFile,
      "-p",
      options.project,
      "exec",
      "-T",
      "postgres",
      "backstop",
      "restore-validate",
      "--db",
      "postgresql://postgres:password@localhost:5432/testdb",
      "--storage",
      "s3://backstop-test@http://minio:9000",
      "--snapshot-id",
      snapshotId,
      "--table",
      "audit_logs",
      "--metadata-db",
      "/metadata/backstop.db",
      "--json",
    ]);
    addStep("restore_validate_audit_logs", "ok");

    const metrics = await getText(gatewayUrl("/metrics"), authHeaders());
    if (!metrics.includes("backstop_gateway_queries_total")) {
      throw new Error("Gateway metrics missing query counter");
    }
    addStep("gateway_metrics", "ok");

    report.ok = true;
  } catch (error) {
    addStep("failure", "failed", error instanceof Error ? error.message : String(error));
    report.ok = false;
    process.exitCode = 1;
  } finally {
    report.finished_at = new Date().toISOString();
    process.stdout.write(`${JSON.stringify(report, null, 2)}\n`);
  }
}

function parseArgs(argv) {
  const parsed = {
    composeFile: process.env.BACKSTOP_E2E_COMPOSE_FILE || "deploy/docker-compose.yml",
    project: process.env.BACKSTOP_E2E_PROJECT || "backstop_oss_e2e",
    token: process.env.BACKSTOP_TOKEN || "dev-token",
    timeoutSeconds: Number(process.env.BACKSTOP_E2E_TIMEOUT_SECONDS || 180),
    gatewayPort: Number(process.env.BACKSTOP_GATEWAY_HOST_PORT || 8080),
    postgresPort: Number(process.env.BACKSTOP_POSTGRES_HOST_PORT || 5433),
    minioPort: Number(process.env.BACKSTOP_MINIO_HOST_PORT || 9000),
    minioConsolePort: Number(process.env.BACKSTOP_MINIO_CONSOLE_HOST_PORT || 9001),
    syncPort: Number(process.env.BACKSTOP_SYNC_HOST_PORT || 9091),
  };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    switch (arg) {
      case "--compose-file":
        parsed.composeFile = requireValue(arg, argv[++i]);
        break;
      case "--project":
        parsed.project = requireValue(arg, argv[++i]);
        break;
      case "--token":
        parsed.token = requireValue(arg, argv[++i]);
        break;
      case "--timeout-seconds":
        parsed.timeoutSeconds = Number(requireValue(arg, argv[++i]));
        break;
      case "--gateway-port":
        parsed.gatewayPort = Number(requireValue(arg, argv[++i]));
        break;
      case "--postgres-port":
        parsed.postgresPort = Number(requireValue(arg, argv[++i]));
        break;
      case "--minio-port":
        parsed.minioPort = Number(requireValue(arg, argv[++i]));
        break;
      case "--minio-console-port":
        parsed.minioConsolePort = Number(requireValue(arg, argv[++i]));
        break;
      case "--sync-port":
        parsed.syncPort = Number(requireValue(arg, argv[++i]));
        break;
      case "--help":
      case "-h":
        process.stdout.write(usage());
        process.exit(0);
      default:
        throw new Error(`unknown option: ${arg}`);
    }
  }
  if (!Number.isFinite(parsed.timeoutSeconds) || parsed.timeoutSeconds <= 0) {
    throw new Error("timeout must be a positive number");
  }
  for (const key of ["gatewayPort", "postgresPort", "minioPort", "minioConsolePort", "syncPort"]) {
    if (!Number.isInteger(parsed[key]) || parsed[key] <= 0 || parsed[key] > 65535) {
      throw new Error(`${key} must be a TCP port number`);
    }
  }
  return parsed;
}

function usage() {
  return [
    "Usage: node scripts/e2e.mjs [options]",
    "",
    "Options:",
    "  --compose-file <path>       Docker Compose file, default deploy/docker-compose.yml",
    "  --project <name>            Compose project name, default backstop_oss_e2e",
    "  --token <token>             Gateway token, default BACKSTOP_TOKEN or dev-token",
    "  --timeout-seconds <seconds> HTTP wait timeout, default 180",
    "  --gateway-port <port>       Host port for gateway, default 8080",
    "  --postgres-port <port>      Host port for PostgreSQL, default 5433",
    "  --minio-port <port>         Host port for MinIO API, default 9000",
    "  --minio-console-port <port> Host port for MinIO console, default 9001",
    "  --sync-port <port>          Host port for sync metrics/health, default 9091",
    "  --help",
    "",
  ].join("\n");
}

function requireValue(name, value) {
  if (!value || value.startsWith("--")) {
    throw new Error(`${name} requires a value`);
  }
  return value;
}

function gatewayUrl(path) {
  return `http://localhost:${options.gatewayPort}${path}`;
}

async function waitHttp(url) {
  const deadline = Date.now() + options.timeoutSeconds * 1000;
  let lastError;
  while (Date.now() < deadline) {
    try {
      await getText(url);
      return;
    } catch (error) {
      lastError = error;
      await sleep(2_000);
    }
  }
  throw new Error(`Timed out waiting for ${url}: ${lastError?.message || "unknown error"}`);
}

async function waitForLatestValidSnapshot(table) {
  const deadline = Date.now() + options.timeoutSeconds * 1000;
  let lastError;
  const url = gatewayUrl(`/metadata/snapshots?table=${encodeURIComponent(table)}&latest_valid=true`);
  while (Date.now() < deadline) {
    try {
      const snapshots = await getJson(url, authHeaders());
      if (snapshots.count && snapshots.count >= 1) {
        return snapshots;
      }
      lastError = new Error(`metadata returned ${snapshots.count || 0} valid snapshots`);
    } catch (error) {
      lastError = error;
    }
    await sleep(2_000);
  }
  throw new Error(`Timed out waiting for latest valid snapshot for ${table}: ${lastError?.message || "unknown error"}`);
}

function invokeMcp(body) {
  return postJson(gatewayUrl("/"), body, {
    ...authHeaders(),
    "Content-Type": "application/json",
  });
}

function authHeaders() {
  return { Authorization: `Bearer ${options.token}` };
}

async function getJson(url, headers = {}) {
  const text = await getText(url, headers);
  return JSON.parse(text);
}

async function postJson(url, body, headers = {}) {
  const response = await fetch(url, {
    method: "POST",
    headers: {
      ...headers,
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  if (!response.ok) {
    throw new Error(`POST ${url} failed with HTTP ${response.status}: ${text}`);
  }
  return text ? JSON.parse(text) : {};
}

async function getText(url, headers = {}) {
  const response = await fetch(url, { headers });
  const text = await response.text();
  if (!response.ok) {
    throw new Error(`GET ${url} failed with HTTP ${response.status}: ${text}`);
  }
  return text;
}

function run(command, args, { quiet = false } = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      stdio: quiet ? ["ignore", "ignore", "pipe"] : "inherit",
      shell: false,
      windowsHide: true,
      env: composeEnv(),
    });
    let stderr = "";
    if (quiet && child.stderr) {
      child.stderr.on("data", (chunk) => {
        stderr += chunk.toString();
      });
    }
    child.on("error", reject);
    child.on("exit", (code) => {
      if (code === 0) {
        resolve();
        return;
      }
      reject(new Error(`${command} ${args.join(" ")} failed with exit code ${code}${stderr ? `: ${stderr}` : ""}`));
    });
  });
}

function composeEnv() {
  return {
    ...process.env,
    BACKSTOP_GATEWAY_HOST_PORT: String(options.gatewayPort),
    BACKSTOP_POSTGRES_HOST_PORT: String(options.postgresPort),
    BACKSTOP_MINIO_HOST_PORT: String(options.minioPort),
    BACKSTOP_MINIO_CONSOLE_HOST_PORT: String(options.minioConsolePort),
    BACKSTOP_SYNC_HOST_PORT: String(options.syncPort),
  };
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

await main();

