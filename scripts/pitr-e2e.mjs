#!/usr/bin/env node
import { spawn } from "node:child_process";

const options = parseArgs(process.argv.slice(2));
const restoreContainer = `${options.project}-pitr-restore`;
const restoreVolume = `${options.project}_pitr-restore`;
const backupId = "base_pitr_e2e";
const clusterId = "local";
const storage = "s3://backstop-test@http://minio:9000";
const sourceDb = "postgresql://postgres:password@localhost:5432/testdb?sslmode=disable";

const report = {
  ok: false,
  project: options.project,
  drill: "pitr-e2e",
  started_at: new Date().toISOString(),
  steps: [],
};

function addStep(name, status, detail = null) {
  report.steps.push({ name, status, detail, at: new Date().toISOString() });
}

async function main() {
  try {
    await run("docker", ["info"], { quiet: true });
    addStep("docker_engine", "ok");

    await runCompose(["config", "--quiet"]);
    addStep("compose_config", "ok");

    await cleanup();
    addStep("cleanup", "ok");

    await runCompose(["build", "postgres", "minio", "create-bucket"]);
    addStep("compose_build", "ok");

    await runCompose(["up", "-d", "--remove-orphans", "postgres", "minio", "create-bucket"]);
    addStep("compose_up", "ok");

    await waitSourceReady();
    addStep("source_ready", "ok");

    await execSourcePsql([
      "DROP TABLE IF EXISTS pitr_drill;",
      "CREATE TABLE pitr_drill(id integer PRIMARY KEY, marker text NOT NULL);",
    ].join(" "));
    await execSourcePsql("SELECT pg_switch_wal();");
    addStep("seed_schema", "ok");

    await execSource([
      "backstop", "pitr", "basebackup",
      "--db", sourceDb,
      "--storage", storage,
      "--cluster-id", clusterId,
      "--backup-id", backupId,
    ]);
    addStep("basebackup_uploaded", "ok", { backup_id: backupId });

    await execSourcePsql("INSERT INTO pitr_drill VALUES (1, 'before_target');");
    await execSourcePsql("SELECT pg_switch_wal();");
    await sleep(2_000);
    const targetTime = (await execSourcePsql("SELECT to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS.US+00');", { capture: true })).trim();
    await sleep(1_000);
    await execSourcePsql("INSERT INTO pitr_drill VALUES (2, 'after_target');");
    const afterWal = (await execSourcePsql("SELECT pg_walfile_name(pg_current_wal_lsn());", { capture: true })).trim();
    await execSourcePsql("SELECT pg_switch_wal();");
    await waitForArchivedWalObject(afterWal);
    addStep("target_window_created", "ok", { target_time: targetTime });

    await run("docker", ["volume", "create", restoreVolume], { quiet: true });
    await runCompose([
      "run", "--rm", "-T", "--no-deps",
      "-v", `${restoreVolume}:/restore-data`,
      "postgres",
      "bash", "-lc",
      [
        "rm -rf /restore-data/*",
        "&&",
        "backstop pitr prepare-restore",
        `--storage ${storage}`,
        `--cluster-id ${clusterId}`,
        `--backup-id ${backupId}`,
        "--target-dir /restore-data",
        `--target-time '${targetTime}'`,
        "--force",
      ].join(" "),
    ]);
    addStep("restore_directory_prepared", "ok");

    await run("docker", [
      "run", "-d",
      "--name", restoreContainer,
      "--network", `${options.project}_default`,
      "-e", "AWS_ACCESS_KEY_ID=minioadmin",
      "-e", "AWS_SECRET_ACCESS_KEY=minioadmin",
      "-e", "POSTGRES_PASSWORD=password",
      "-v", `${restoreVolume}:/var/lib/postgresql/data`,
      `${options.project}-postgres:latest`,
      "postgres",
    ], { quiet: true });
    addStep("restore_postgres_started", "ok");

    await waitRestoreReady();
    addStep("restore_ready", "ok");

    const normalizedRows = await waitForPitrRows();
    if (JSON.stringify(normalizedRows) !== JSON.stringify(["1:before_target"])) {
      throw new Error(`unexpected PITR rows after recovery: ${JSON.stringify(normalizedRows)}`);
    }
    const recoveryState = (await execRestorePsql("SELECT pg_is_in_recovery();", { capture: true })).trim();
    if (recoveryState !== "t") {
      throw new Error(`restored PostgreSQL is not in recovery at target time: pg_is_in_recovery=${recoveryState}`);
    }
    addStep("pitr_validation", "ok", { rows: normalizedRows, pg_is_in_recovery: true });

    report.ok = true;
  } catch (error) {
    addStep("failure", "failed", error instanceof Error ? error.message : String(error));
    report.ok = false;
    process.exitCode = 1;
  } finally {
    if (report.ok) {
      await cleanupRestoreOnly();
    }
    report.finished_at = new Date().toISOString();
    process.stdout.write(`${JSON.stringify(report, null, 2)}\n`);
  }
}

async function cleanup() {
  await cleanupRestoreOnly();
  await runCompose(["down", "-v", "--remove-orphans"], { quiet: true, allowFailure: true });
  await run("docker", ["volume", "rm", "-f", restoreVolume], { quiet: true, allowFailure: true });
}

async function cleanupRestoreOnly() {
  await run("docker", ["rm", "-f", restoreContainer], { quiet: true, allowFailure: true });
}

function execSource(args, opts = {}) {
  return runCompose(["exec", "-T", "postgres", ...args], opts);
}

function execSourcePsql(sql, opts = {}) {
  return execSource(["psql", "-U", "postgres", "-d", "testdb", "-v", "ON_ERROR_STOP=1", "-At", "-c", sql], opts);
}

function execRestorePsql(sql, opts = {}) {
  return run("docker", ["exec", "-i", restoreContainer, "psql", "-U", "postgres", "-d", "testdb", "-v", "ON_ERROR_STOP=1", "-At", "-c", sql], opts);
}

async function waitRestoreReady() {
  const deadline = Date.now() + options.timeoutSeconds * 1000;
  let lastError;
  while (Date.now() < deadline) {
    try {
      await run("docker", ["exec", "-i", restoreContainer, "pg_isready", "-U", "postgres", "-d", "testdb"], { quiet: true });
      return;
    } catch (error) {
      lastError = error;
      await sleep(2_000);
    }
  }
  throw new Error(`timed out waiting for restored PostgreSQL: ${lastError?.message || "unknown error"}`);
}

async function waitForPitrRows() {
  const deadline = Date.now() + options.timeoutSeconds * 1000;
  let lastRows = [];
  let lastError;
  while (Date.now() < deadline) {
    try {
      const rows = await execRestorePsql("SELECT id || ':' || marker FROM pitr_drill ORDER BY id;", { capture: true });
      lastRows = rows.trim().split(/\r?\n/).filter(Boolean);
      if (JSON.stringify(lastRows) === JSON.stringify(["1:before_target"])) {
        return lastRows;
      }
    } catch (error) {
      lastError = error;
    }
    await sleep(1_000);
  }
  throw new Error(`timed out waiting for PITR target rows: lastRows=${JSON.stringify(lastRows)} error=${lastError?.message || "none"}`);
}

async function waitSourceReady() {
  const deadline = Date.now() + options.timeoutSeconds * 1000;
  let lastError;
  while (Date.now() < deadline) {
    try {
      await execSourcePsql("SELECT 1;", { quiet: true });
      return;
    } catch (error) {
      lastError = error;
      await sleep(2_000);
    }
  }
  throw new Error(`timed out waiting for source PostgreSQL: ${lastError?.message || "unknown error"}`);
}

async function waitForArchivedWalCount(minDoneFiles) {
  const deadline = Date.now() + options.timeoutSeconds * 1000;
  let lastCount = 0;
  while (Date.now() < deadline) {
    const output = await execSource([
      "bash", "-lc",
      "find /var/lib/postgresql/data/pg_wal/archive_status -maxdepth 1 -name '*.done' -type f | wc -l",
    ], { capture: true });
    lastCount = Number(output.trim());
    if (Number.isFinite(lastCount) && lastCount >= minDoneFiles) {
      return;
    }
    await sleep(2_000);
  }
  throw new Error(`timed out waiting for WAL archive completion: got ${lastCount}, want at least ${minDoneFiles}`);
}

async function waitForArchivedWalObject(walName) {
  const deadline = Date.now() + options.timeoutSeconds * 1000;
  let seen = false;
  while (Date.now() < deadline) {
    const output = await execSource([
      "python3", "-c",
      "import boto3, sys\ns3=boto3.client('s3',endpoint_url='http://minio:9000',aws_access_key_id='minioadmin',aws_secret_access_key='minioadmin')\nkey=f'backstop/wal/local/{sys.argv[1]}'\ntry:\n    s3.head_object(Bucket='backstop-test', Key=key)\n    print('yes')\nexcept Exception:\n    print('no')\n",
      walName,
    ], { capture: true });
    seen = output.trim() === "yes";
    if (seen) {
      return;
    }
    await sleep(2_000);
  }
  throw new Error(`timed out waiting for archived WAL object ${walName}`);
}

function runCompose(args, opts = {}) {
  return run("docker", ["compose", "-f", options.composeFile, "-p", options.project, ...args], {
    ...opts,
    env: composeEnv(),
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

function run(command, args, opts = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd: process.cwd(),
      env: opts.env || process.env,
      stdio: opts.capture ? ["ignore", "pipe", "pipe"] : opts.quiet ? ["ignore", "pipe", "pipe"] : "inherit",
      shell: false,
    });
    let stdout = "";
    let stderr = "";
    if (child.stdout) child.stdout.on("data", (chunk) => { stdout += chunk.toString(); });
    if (child.stderr) child.stderr.on("data", (chunk) => { stderr += chunk.toString(); });
    child.on("error", reject);
    child.on("close", (code) => {
      if (code === 0 || opts.allowFailure) {
        resolve(opts.capture ? stdout : stdout);
        return;
      }
      reject(new Error(`${command} ${args.join(" ")} failed with exit ${code}: ${stderr || stdout}`));
    });
  });
}

function parseArgs(argv) {
  const parsed = {
    composeFile: process.env.BACKSTOP_E2E_COMPOSE_FILE || "deploy/docker-compose.yml",
    project: process.env.BACKSTOP_PITR_E2E_PROJECT || "backstop_pitr_e2e",
    timeoutSeconds: Number(process.env.BACKSTOP_E2E_TIMEOUT_SECONDS || 360),
    gatewayPort: Number(process.env.BACKSTOP_GATEWAY_HOST_PORT || 18088),
    postgresPort: Number(process.env.BACKSTOP_POSTGRES_HOST_PORT || 15441),
    minioPort: Number(process.env.BACKSTOP_MINIO_HOST_PORT || 19010),
    minioConsolePort: Number(process.env.BACKSTOP_MINIO_CONSOLE_HOST_PORT || 19011),
    syncPort: Number(process.env.BACKSTOP_SYNC_HOST_PORT || 19099),
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
        process.stdout.write("Usage: node scripts/pitr-e2e.mjs [--project name] [--timeout-seconds n] [port options]\n");
        process.exit(0);
      default:
        throw new Error(`unknown option: ${arg}`);
    }
  }
  return parsed;
}

function requireValue(name, value) {
  if (!value || value.startsWith("--")) {
    throw new Error(`${name} requires a value`);
  }
  return value;
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

main();
