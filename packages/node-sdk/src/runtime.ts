import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { createWriteStream, existsSync } from "node:fs";
import { chmod, mkdir, readFile, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { CreateBucketCommand, HeadBucketCommand, S3Client } from "@aws-sdk/client-s3";
import { BackstopError, BackstopNetworkError, BackstopTimeoutError } from "./errors.js";

const DEFAULT_HOST = "127.0.0.1";
const DEFAULT_GATEWAY_PORT = 37291;
const DEFAULT_SYNC_METRICS_PORT = 37292;
const DEFAULT_MINIO_PORT = 37293;
const DEFAULT_MINIO_CONSOLE_PORT = 37294;
const DEFAULT_START_TIMEOUT_MS = 90_000;
const DEFAULT_RUNTIME_REPO = "pratyush2514/Backstop";

export type BackstopClientMode = "agent" | "operator" | "readonly" | "admin";

export interface BackstopLocalRuntimeOptions {
  postgresUrl?: string;
  agentId?: string;
  profile?: string;
  homeDir?: string;
  host?: string;
  gatewayPort?: number;
  syncMetricsPort?: number;
  minioPort?: number;
  minioConsolePort?: number;
  storageBucket?: string;
  startTimeoutMs?: number;
  runtimeVersion?: string;
  runtimeRepo?: string;
  mode?: BackstopClientMode;
}

export interface BackstopManagedRuntime {
  url: string;
  token: string;
  agentId: string;
  mode: BackstopClientMode;
  profile: string;
  homeDir: string;
  metadataDb: string;
  storageUrl: string;
  gatewayPort: number;
  syncMetricsPort: number;
  minioPort: number;
  minioConsolePort: number;
}

interface RuntimePaths {
  homeDir: string;
  profileDir: string;
  binDir: string;
  logDir: string;
  dataDir: string;
  minioDataDir: string;
  tokenFile: string;
  metadataDb: string;
  runtimeStateFile: string;
  gatewayLog: string;
  syncLog: string;
  minioLog: string;
}

interface RuntimeState {
  profile: string;
  host: string;
  gatewayPort: number;
  syncMetricsPort: number;
  minioPort: number;
  minioConsolePort: number;
  storageBucket: string;
  minioAccessKey: string;
  minioSecretKey: string;
  tokens: Record<string, string>;
  gatewayPid?: number;
  syncPid?: number;
  minioPid?: number;
}

interface ResolvedRuntimeBinary {
  path: string;
  argv: string[];
  env?: NodeJS.ProcessEnv;
}

interface HealthWaitOptions {
  serviceName: string;
  logFile: string;
  hint?: string;
}

export async function ensureLocalRuntime(options: BackstopLocalRuntimeOptions = {}): Promise<BackstopManagedRuntime> {
  const profile = sanitizeProfile(options.profile || process.env.BACKSTOP_PROFILE || "local");
  const paths = resolveRuntimePaths(options.homeDir, profile);
  await ensureRuntimeDirectories(paths);

  const host = options.host || process.env.BACKSTOP_RUNTIME_HOST || DEFAULT_HOST;
  const gatewayPort = parsePositiveInt(options.gatewayPort || process.env.BACKSTOP_GATEWAY_PORT, DEFAULT_GATEWAY_PORT);
  const syncMetricsPort = parsePositiveInt(options.syncMetricsPort || process.env.BACKSTOP_SYNC_METRICS_PORT, DEFAULT_SYNC_METRICS_PORT);
  const minioPort = parsePositiveInt(options.minioPort || process.env.BACKSTOP_MINIO_PORT, DEFAULT_MINIO_PORT);
  const minioConsolePort = parsePositiveInt(
    options.minioConsolePort || process.env.BACKSTOP_MINIO_CONSOLE_PORT,
    DEFAULT_MINIO_CONSOLE_PORT,
  );
  const storageBucket = sanitizeBucket(options.storageBucket || process.env.BACKSTOP_BUCKET || `backstop-${profile}`);
  const runtimeVersion = options.runtimeVersion || process.env.BACKSTOP_RUNTIME_VERSION || packageVersion();
  const runtimeRepo = options.runtimeRepo || process.env.BACKSTOP_RUNTIME_REPO || DEFAULT_RUNTIME_REPO;
  const postgresUrl = normalizeManagedPostgresUrl(
    options.postgresUrl || process.env.BACKSTOP_POSTGRES_URL || process.env.BACKSTOP_DB_URL,
  );
  const mode = options.mode || "agent";
  const agentId = (options.agentId || process.env.BACKSTOP_AGENT_ID || "backstop-local-agent").trim();
  const startTimeoutMs = parsePositiveInt(options.startTimeoutMs || process.env.BACKSTOP_RUNTIME_TIMEOUT_MS, DEFAULT_START_TIMEOUT_MS);

  if (!postgresUrl || !postgresUrl.trim()) {
    throw new BackstopError(
      "backstop local runtime requires BACKSTOP_POSTGRES_URL or postgresUrl so it can manage the gateway for you",
    );
  }

  const state = await loadOrCreateRuntimeState(paths.runtimeStateFile, {
    profile,
    host,
    gatewayPort,
    syncMetricsPort,
    minioPort,
    minioConsolePort,
    storageBucket,
  });
  const storageUrl = `s3://${state.storageBucket}@http://${host}:${state.minioPort}`;
  await writeTokenFile(paths.tokenFile, state.tokens);

  if (!(await minioHealthy(host, state.minioPort))) {
    const minio = await resolveMinioBinary(paths, runtimeVersion);
    state.minioPid = await launchProcess({
      name: "minio",
      binary: minio.path,
      args: [...minio.argv, "server", paths.minioDataDir, "--address", `${host}:${state.minioPort}`, "--console-address", `${host}:${state.minioConsolePort}`],
      logFile: paths.minioLog,
      env: {
        ...process.env,
        MINIO_ROOT_USER: state.minioAccessKey,
        MINIO_ROOT_PASSWORD: state.minioSecretKey,
      },
    });
    await saveRuntimeState(paths.runtimeStateFile, state);
    await waitForHealth(async () => minioHealthy(host, state.minioPort), startTimeoutMs, {
      serviceName: "local MinIO",
      logFile: paths.minioLog,
      hint: "Make sure the local runtime ports are free and the MinIO binary can start on this machine.",
    });
  }

  await ensureBucketExists({
    host,
    port: state.minioPort,
    accessKey: state.minioAccessKey,
    secretKey: state.minioSecretKey,
    bucket: state.storageBucket,
  });

  if (!(await gatewayHealthy(host, state.gatewayPort))) {
    const gateway = await resolveBackstopBinary("gateway", paths, runtimeVersion, runtimeRepo);
    state.gatewayPid = await launchProcess({
      name: "gateway",
      binary: gateway.path,
      args: [
        ...gateway.argv,
        "--listen",
        `${host}:${state.gatewayPort}`,
        "--db",
        postgresUrl,
        "--storage",
        storageUrl,
        "--auth-tokens-file",
        paths.tokenFile,
        "--metadata-db",
        paths.metadataDb,
        "--environment",
        "local",
        "--cluster-id",
        profile,
      ],
      logFile: paths.gatewayLog,
      env: {
        ...process.env,
        AWS_ACCESS_KEY_ID: state.minioAccessKey,
        AWS_SECRET_ACCESS_KEY: state.minioSecretKey,
      },
    });
    await saveRuntimeState(paths.runtimeStateFile, state);
    await waitForHealth(async () => gatewayHealthy(host, state.gatewayPort), startTimeoutMs, {
      serviceName: "backstop gateway",
      logFile: paths.gatewayLog,
      hint:
        "For local PostgreSQL, Backstop auto-adds sslmode=disable for localhost when you do not specify sslmode yourself. Verify the database exists and accepts local TCP connections.",
    });
  }

  if (!(await syncHealthy(host, state.syncMetricsPort))) {
    const sync = await resolveBackstopBinary("sync", paths, runtimeVersion, runtimeRepo);
    state.syncPid = await launchProcess({
      name: "sync",
      binary: sync.path,
      args: [
        ...sync.argv,
        "--db",
        postgresUrl,
        "--storage",
        storageUrl,
        "--metadata-db",
        paths.metadataDb,
        "--metrics-listen",
        `${host}:${state.syncMetricsPort}`,
        "--interval",
        "30",
        "--snapshot-on-start=true",
        "--max-snapshot-failures",
        "3",
        "--mode",
        "snapshot-and-detect",
      ],
      logFile: paths.syncLog,
      env: {
        ...process.env,
        AWS_ACCESS_KEY_ID: state.minioAccessKey,
        AWS_SECRET_ACCESS_KEY: state.minioSecretKey,
      },
    });
    await saveRuntimeState(paths.runtimeStateFile, state);
    await waitForHealth(async () => syncHealthy(host, state.syncMetricsPort), startTimeoutMs, {
      serviceName: "backstop sync",
      logFile: paths.syncLog,
      hint: "If the gateway started but sync did not, check the sync log for database/storage startup errors.",
    });
  }

  return {
    url: `http://${host}:${state.gatewayPort}`,
    token: tokenForMode(state.tokens, mode),
    agentId,
    mode,
    profile,
    homeDir: paths.profileDir,
    metadataDb: paths.metadataDb,
    storageUrl,
    gatewayPort: state.gatewayPort,
    syncMetricsPort: state.syncMetricsPort,
    minioPort: state.minioPort,
    minioConsolePort: state.minioConsolePort,
  };
}

export function defaultBackstopHome(): string {
  return process.env.BACKSTOP_HOME || path.join(os.homedir(), ".backstop");
}

export function sanitizeProfile(value: string): string {
  const trimmed = (value || "local").trim();
  return trimmed.replace(/[^a-zA-Z0-9_.-]/g, "-") || "local";
}

export function tokenForMode(tokens: Record<string, string>, mode: BackstopClientMode): string {
  switch (mode) {
    case "operator":
      return tokens.operator;
    case "readonly":
      return tokens.readonly;
    case "admin":
      return tokens.admin;
    case "agent":
    default:
      return tokens.agent;
  }
}

export function normalizeManagedPostgresUrl(value: string | undefined): string | undefined {
  if (!value || !value.trim()) {
    return value;
  }

  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    return value;
  }

  if (!/^postgres(ql)?:$/i.test(parsed.protocol)) {
    return value;
  }
  if (!isLocalDbHost(parsed.hostname)) {
    return value;
  }
  if (!parsed.searchParams.has("sslmode")) {
    parsed.searchParams.set("sslmode", "disable");
  }
  return parsed.toString();
}

export function runtimeAssetName(service: "gateway" | "sync", platform = process.platform, arch = process.arch): string {
  const ext = platform === "win32" ? ".exe" : "";
  return `backstop-${service}-${platformLabel(platform)}-${archLabel(arch)}${ext}`;
}

export function minioDownloadUrl(platform = process.platform, arch = process.arch): string {
  const base = "https://dl.min.io/server/minio/release";
  const mappedArch = minioArch(arch);
  switch (platform) {
    case "win32":
      return `${base}/windows-${mappedArch}/minio.exe`;
    case "darwin":
      return `${base}/darwin-${mappedArch}/minio`;
    case "linux":
      return `${base}/linux-${mappedArch}/minio`;
    default:
      throw new BackstopError(`unsupported platform for managed local runtime: ${platform}/${arch}`);
  }
}

function resolveRuntimePaths(homeDir: string | undefined, profile: string): RuntimePaths {
  const root = homeDir || defaultBackstopHome();
  const profileDir = path.join(root, "profiles", profile);
  return {
    homeDir: root,
    profileDir,
    binDir: path.join(root, "bin"),
    logDir: path.join(profileDir, "logs"),
    dataDir: path.join(profileDir, "data"),
    minioDataDir: path.join(profileDir, "data", "minio"),
    tokenFile: path.join(profileDir, "gateway-auth-tokens.json"),
    metadataDb: path.join(profileDir, "backstop.db"),
    runtimeStateFile: path.join(profileDir, "runtime-state.json"),
    gatewayLog: path.join(profileDir, "logs", "gateway.log"),
    syncLog: path.join(profileDir, "logs", "sync.log"),
    minioLog: path.join(profileDir, "logs", "minio.log"),
  };
}

async function ensureRuntimeDirectories(paths: RuntimePaths): Promise<void> {
  await Promise.all([
    mkdir(paths.profileDir, { recursive: true }),
    mkdir(paths.binDir, { recursive: true }),
    mkdir(paths.logDir, { recursive: true }),
    mkdir(paths.dataDir, { recursive: true }),
    mkdir(paths.minioDataDir, { recursive: true }),
  ]);
}

async function loadOrCreateRuntimeState(
  file: string,
  defaults: Pick<RuntimeState, "profile" | "host" | "gatewayPort" | "syncMetricsPort" | "minioPort" | "minioConsolePort" | "storageBucket">,
): Promise<RuntimeState> {
  if (existsSync(file)) {
    const raw = JSON.parse(await readFile(file, "utf8")) as RuntimeState;
    return {
      ...raw,
      profile: raw.profile || defaults.profile,
      host: raw.host || defaults.host,
      gatewayPort: raw.gatewayPort || defaults.gatewayPort,
      syncMetricsPort: raw.syncMetricsPort || defaults.syncMetricsPort,
      minioPort: raw.minioPort || defaults.minioPort,
      minioConsolePort: raw.minioConsolePort || defaults.minioConsolePort,
      storageBucket: raw.storageBucket || defaults.storageBucket,
      minioAccessKey: raw.minioAccessKey || "backstop",
      minioSecretKey: raw.minioSecretKey || randomToken(24),
      tokens: completeTokens(raw.tokens || {}),
    };
  }

  const state: RuntimeState = {
    ...defaults,
    minioAccessKey: "backstop",
    minioSecretKey: randomToken(24),
    tokens: completeTokens({}),
  };
  await saveRuntimeState(file, state);
  return state;
}

async function saveRuntimeState(file: string, state: RuntimeState): Promise<void> {
  await writeFile(file, `${JSON.stringify(state, null, 2)}\n`, "utf8");
}

async function writeTokenFile(file: string, tokens: Record<string, string>): Promise<void> {
  const payload = {
    tokens: [
      {
        name: "agent-local",
        token: tokens.agent,
        scopes: ["query:analyze", "query:execute", "metadata:read"],
      },
      {
        name: "operator-local",
        token: tokens.operator,
        scopes: ["approval:read", "approval:write", "metadata:read", "restore:prepare"],
      },
      {
        name: "readonly-local",
        token: tokens.readonly,
        scopes: ["query:analyze", "metadata:read"],
      },
      {
        name: "admin-local",
        token: tokens.admin,
        scopes: ["admin:*", "query:analyze", "query:execute", "approval:read", "approval:write", "metadata:read", "metrics:read", "restore:prepare"],
      },
    ],
  };
  await writeFile(file, `${JSON.stringify(payload, null, 2)}\n`, "utf8");
}

async function minioHealthy(host: string, port: number): Promise<boolean> {
  try {
    const response = await fetch(`http://${host}:${port}/minio/health/live`);
    return response.ok;
  } catch {
    return false;
  }
}

async function gatewayHealthy(host: string, port: number): Promise<boolean> {
  try {
    const response = await fetch(`http://${host}:${port}/health`);
    return response.ok;
  } catch {
    return false;
  }
}

async function syncHealthy(host: string, port: number): Promise<boolean> {
  try {
    const response = await fetch(`http://${host}:${port}/metrics`);
    return response.ok;
  } catch {
    return false;
  }
}

async function ensureBucketExists(config: {
  host: string;
  port: number;
  accessKey: string;
  secretKey: string;
  bucket: string;
}): Promise<void> {
  const client = new S3Client({
    region: "us-east-1",
    endpoint: `http://${config.host}:${config.port}`,
    forcePathStyle: true,
    credentials: {
      accessKeyId: config.accessKey,
      secretAccessKey: config.secretKey,
    },
  });
  try {
    await client.send(new HeadBucketCommand({ Bucket: config.bucket }));
    return;
  } catch {}
  await client.send(new CreateBucketCommand({ Bucket: config.bucket }));
}

async function resolveBackstopBinary(
  service: "gateway" | "sync",
  paths: RuntimePaths,
  version: string,
  repo: string,
): Promise<ResolvedRuntimeBinary> {
  const envVar = service === "gateway" ? process.env.BACKSTOP_GATEWAY_BIN : process.env.BACKSTOP_SYNC_BIN;
  if (envVar && existsSync(envVar)) {
    return { path: envVar, argv: [] };
  }

  const binaryName = runtimeAssetName(service);
  const cachedBinary = path.join(paths.binDir, binaryName);
  if (existsSync(cachedBinary)) {
    return { path: cachedBinary, argv: [] };
  }

  const sourceRoot = findRepoRoot();
  if (sourceRoot) {
    const output = path.join(paths.binDir, executableName(`backstop-${service}`));
    if (!existsSync(output)) {
      await buildGoBinary(path.join(sourceRoot, service), output);
    }
    return { path: output, argv: [] };
  }

  const releaseTag = version.startsWith("v") ? version : `v${version}`;
  const downloadUrl = `https://github.com/${repo}/releases/download/${releaseTag}/${binaryName}`;
  await downloadBinary(downloadUrl, cachedBinary);
  return { path: cachedBinary, argv: [] };
}

async function resolveMinioBinary(paths: RuntimePaths, _version: string): Promise<ResolvedRuntimeBinary> {
  const envVar = process.env.BACKSTOP_MINIO_BIN;
  if (envVar && existsSync(envVar)) {
    return { path: envVar, argv: [] };
  }
  const target = path.join(paths.binDir, executableName("minio"));
  if (!existsSync(target)) {
    await downloadBinary(minioDownloadUrl(), target);
  }
  return { path: target, argv: [] };
}

function findRepoRoot(): string | undefined {
  const currentFile = fileURLToPath(import.meta.url);
  const candidate = path.resolve(path.dirname(currentFile), "../../../../");
  if (existsSync(path.join(candidate, "gateway", "main.go")) && existsSync(path.join(candidate, "sync", "main.go"))) {
    return candidate;
  }
  return undefined;
}

async function buildGoBinary(sourceDir: string, outputFile: string): Promise<void> {
  await mkdir(path.dirname(outputFile), { recursive: true });
  const command = process.platform === "win32" ? "go.exe" : "go";
  await runCommand(command, ["build", "-o", outputFile, "."], {
    cwd: sourceDir,
    env: {
      ...process.env,
      CGO_ENABLED: "0",
    },
    label: `building ${path.basename(outputFile)}`,
  });
  await makeExecutable(outputFile);
}

async function downloadBinary(url: string, destination: string): Promise<void> {
  const response = await fetch(url);
  if (!response.ok || !response.body) {
    throw new BackstopNetworkError(`failed to download backstop runtime asset from ${url} (HTTP ${response.status})`);
  }
  await mkdir(path.dirname(destination), { recursive: true });
  const bytes = Buffer.from(await response.arrayBuffer());
  await writeFile(destination, bytes);
  await makeExecutable(destination);
}

async function launchProcess(options: {
  name: string;
  binary: string;
  args: string[];
  logFile: string;
  env?: NodeJS.ProcessEnv;
}): Promise<number> {
  await mkdir(path.dirname(options.logFile), { recursive: true });
  const out = createWriteStream(options.logFile, { flags: "a" });
  const child = spawn(options.binary, options.args, {
    env: options.env,
    detached: true,
    stdio: ["ignore", "pipe", "pipe"],
    windowsHide: true,
  });
  child.stdout?.pipe(out);
  child.stderr?.pipe(out);
  child.unref();
  if (!child.pid) {
    throw new BackstopError(`failed to start local ${options.name} runtime`);
  }
  return child.pid;
}

async function waitForHealth(check: () => Promise<boolean>, timeoutMs: number, options: HealthWaitOptions): Promise<void> {
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    if (await check()) {
      return;
    }
    await sleep(750);
  }
  throw new BackstopTimeoutError(await startupFailureMessage(options));
}

async function runCommand(
  binary: string,
  args: string[],
  options: { cwd?: string; env?: NodeJS.ProcessEnv; label: string },
): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const child = spawn(binary, args, {
      cwd: options.cwd,
      env: options.env,
      stdio: ["ignore", "pipe", "pipe"],
      windowsHide: true,
    });
    let stderr = "";
    child.stderr?.on("data", (chunk) => {
      stderr += chunk.toString();
    });
    child.on("error", reject);
    child.on("exit", (code) => {
      if (code === 0) {
        resolve();
      } else {
        reject(new BackstopError(`${options.label} failed`, { body: stderr.slice(-2000) }));
      }
    });
  });
}

function completeTokens(tokens: Partial<Record<"agent" | "operator" | "readonly" | "admin", string>>): Record<string, string> {
  return {
    agent: tokens.agent || randomToken(24),
    operator: tokens.operator || randomToken(24),
    readonly: tokens.readonly || randomToken(24),
    admin: tokens.admin || randomToken(24),
  };
}

function randomToken(bytes: number): string {
  return randomBytes(bytes).toString("hex");
}

function executableName(base: string): string {
  return process.platform === "win32" ? `${base}.exe` : base;
}

async function makeExecutable(file: string): Promise<void> {
  if (process.platform !== "win32") {
    await chmod(file, 0o755);
  }
}

function parsePositiveInt(value: string | number | undefined, fallback: number): number {
  if (value === undefined || value === null || value === "") {
    return fallback;
  }
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    throw new BackstopError(`expected a positive integer, got ${value}`);
  }
  return Math.floor(parsed);
}

function sanitizeBucket(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9.-]/g, "-").replace(/^-+|-+$/g, "") || "backstop-local";
}

function packageVersion(): string {
  return "0.1.0-alpha.1";
}

async function startupFailureMessage(options: HealthWaitOptions): Promise<string> {
  const details = [`timed out waiting for ${options.serviceName} to become healthy`];
  const logTail = await readLogTail(options.logFile);
  if (logTail) {
    details.push(`log tail from ${options.logFile}:\n${logTail}`);
  } else {
    details.push(`check ${options.logFile} for startup details`);
  }
  if (options.hint) {
    details.push(options.hint);
  }
  return details.join("\n\n");
}

function platformLabel(platform: NodeJS.Platform): string {
  switch (platform) {
    case "win32":
      return "windows";
    case "darwin":
      return "darwin";
    case "linux":
      return "linux";
    default:
      throw new BackstopError(`unsupported platform for managed local runtime: ${platform}`);
  }
}

function archLabel(arch: string): string {
  switch (arch) {
    case "x64":
      return "amd64";
    case "arm64":
      return "arm64";
    default:
      throw new BackstopError(`unsupported architecture for managed local runtime: ${arch}`);
  }
}

function minioArch(arch: string): string {
  switch (arch) {
    case "x64":
      return "amd64";
    case "arm64":
      return "arm64";
    default:
      throw new BackstopError(`unsupported architecture for managed local runtime: ${arch}`);
  }
}

function isLocalDbHost(hostname: string): boolean {
  const normalized = (hostname || "").trim().toLowerCase();
  return normalized === "localhost" || normalized === "127.0.0.1" || normalized === "::1";
}

async function readLogTail(file: string, maxChars = 4000): Promise<string> {
  try {
    const text = await readFile(file, "utf8");
    const trimmed = text.trim();
    return trimmed ? trimmed.slice(-maxChars) : "";
  } catch {
    return "";
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
