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

    await run("docker", ["compose", "-f", options.composeFile, "-p", options.project, "up", "-d", "--build"]);
    addStep("compose_up", "ok");

    await waitHttp("http://localhost:8080/health");
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
      "-f",
      "/seed/seed.sql",
    ]);
    addStep("seed_database", "ok");

    await sleep(40_000);
    let snapshots = await getJson("http://localhost:8080/metadata/snapshots?table=audit_logs", authHeaders());
    if (!snapshots.count || snapshots.count < 1) {
      throw new Error("No audit_logs snapshots found in metadata");
    }
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

    snapshots = await getJson("http://localhost:8080/metadata/snapshots?table=audit_logs", authHeaders());
    if (!snapshots.count || snapshots.count < 1) {
      throw new Error("No latest audit_logs snapshot found before critical query");
    }
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
    const pending = await getJson("http://localhost:8080/pending", authHeaders());
    if (!pending.pending || pending.pending.length < 1) {
      throw new Error("No pending critical approval found");
    }
    const approvalId = pending.pending[0].id;
    await postJson(`http://localhost:8080/approve/${encodeURIComponent(approvalId)}`, undefined, authHeaders());

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

    const metrics = await getText("http://localhost:8080/metrics", authHeaders());
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

function invokeMcp(body) {
  return postJson("http://localhost:8080/", body, {
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

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

await main();

