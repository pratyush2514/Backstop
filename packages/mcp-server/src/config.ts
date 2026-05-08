import os from "node:os";

export interface BackstopMcpConfig {
  backstopUrl?: string;
  backstopToken?: string;
  backstopPostgresUrl?: string;
  agentId: string;
  timeoutMs: number;
  enableApprovalTools: boolean;
  mode: BackstopMcpMode;
  profile?: string;
  runtimeVersion?: string;
  runtimeRepo?: string;
}

export type BackstopMcpMode = "readonly" | "agent" | "operator" | "admin";

export function loadConfig(env: NodeJS.ProcessEnv = process.env, argv: string[] = process.argv.slice(2)): BackstopMcpConfig {
  const args = parseArgs(argv);
  const backstopUrl = args.url || env.BACKSTOP_URL || env.BACKSTOP_GATEWAY_URL;
  const backstopToken = args.token || env.BACKSTOP_TOKEN || env.BACKSTOP_GATEWAY_TOKEN;
  const backstopPostgresUrl = args.postgresUrl || env.BACKSTOP_POSTGRES_URL || env.POSTGRES_URL;
  const agentId = resolveMcpAgentId(args.agentId || env.BACKSTOP_AGENT_ID || env.AGENT_ID);
  const timeoutMs = parsePositiveInt(args.timeoutMs || env.BACKSTOP_TIMEOUT_MS, 30_000);
  const enableApprovalTools = parseBool(args.enableApprovalTools || env.BACKSTOP_MCP_ENABLE_APPROVAL_TOOLS, false);
  const mode = parseMode(args.mode || env.BACKSTOP_MCP_MODE, enableApprovalTools);
  const profile = args.profile || env.BACKSTOP_PROFILE;
  const runtimeVersion = args.runtimeVersion || env.BACKSTOP_RUNTIME_VERSION;
  const runtimeRepo = args.runtimeRepo || env.BACKSTOP_RUNTIME_REPO;

  if (!backstopUrl && !backstopPostgresUrl) {
    throw new Error("set BACKSTOP_URL for an existing Backstop runtime or BACKSTOP_POSTGRES_URL for managed local mode");
  }

  return {
    backstopUrl,
    backstopToken,
    backstopPostgresUrl,
    agentId,
    timeoutMs,
    enableApprovalTools,
    mode,
    profile,
    runtimeVersion,
    runtimeRepo,
  };
}

export function resolveMcpAgentId(value?: string): string {
  if (value && value.trim()) {
    return value.trim();
  }
  const username = safeUsername();
  return username ? `backstop-mcp-${username}` : "backstop-mcp-local";
}

export function usage(): string {
  return [
    "backstop-mcp",
    "",
    "Environment:",
    "  BACKSTOP_URL                         Existing Backstop gateway URL (optional if managed local mode is used)",
    "  BACKSTOP_TOKEN                       Gateway bearer token for an existing runtime",
    "  BACKSTOP_POSTGRES_URL                PostgreSQL URL for managed local mode",
    "  BACKSTOP_AGENT_ID                    Stable audit identity for this MCP agent",
    "  BACKSTOP_TIMEOUT_MS                  Gateway request timeout, default 30000",
    "  BACKSTOP_MCP_MODE                    Tool profile: agent, operator, readonly, admin. Default agent",
    "  BACKSTOP_MCP_ENABLE_APPROVAL_TOOLS   Enable approve/deny tools, default false",
    "  BACKSTOP_PROFILE                     Local runtime profile name, default local",
    "  BACKSTOP_RUNTIME_VERSION             Backstop runtime release version override",
    "  BACKSTOP_RUNTIME_REPO                GitHub repo to download runtime binaries from",
    "",
    "Options:",
    "  --url <url>",
    "  --token <token>",
    "  --postgres-url <url>",
    "  --agent-id <id>",
    "  --timeout-ms <ms>",
    "  --mode <agent|operator|readonly|admin>",
    "  --profile <name>",
    "  --runtime-version <version>",
    "  --runtime-repo <owner/repo>",
    "  --enable-approval-tools",
    "  --help",
  ].join("\n");
}

function parseArgs(argv: string[]): Record<string, string> {
  const result: Record<string, string> = {};
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--help" || arg === "-h") {
      result.help = "true";
      continue;
    }
    if (arg === "--enable-approval-tools") {
      result.enableApprovalTools = "true";
      continue;
    }
    const next = argv[i + 1];
    switch (arg) {
      case "--url":
        result.url = requireValue(arg, next);
        i += 1;
        break;
      case "--token":
        result.token = requireValue(arg, next);
        i += 1;
        break;
      case "--postgres-url":
        result.postgresUrl = requireValue(arg, next);
        i += 1;
        break;
      case "--agent-id":
        result.agentId = requireValue(arg, next);
        i += 1;
        break;
      case "--timeout-ms":
        result.timeoutMs = requireValue(arg, next);
        i += 1;
        break;
      case "--mode":
        result.mode = requireValue(arg, next);
        i += 1;
        break;
      case "--profile":
        result.profile = requireValue(arg, next);
        i += 1;
        break;
      case "--runtime-version":
        result.runtimeVersion = requireValue(arg, next);
        i += 1;
        break;
      case "--runtime-repo":
        result.runtimeRepo = requireValue(arg, next);
        i += 1;
        break;
      default:
        throw new Error(`unknown backstop-mcp option: ${arg}`);
    }
  }
  return result;
}

function parseMode(value: string | undefined, enableApprovalTools: boolean): BackstopMcpMode {
  if (!value || !value.trim()) {
    return enableApprovalTools ? "operator" : "agent";
  }
  const mode = value.trim().toLowerCase();
  if (mode === "readonly" || mode === "agent" || mode === "operator" || mode === "admin") {
    return mode;
  }
  throw new Error(`unknown BACKSTOP_MCP_MODE: ${value}`);
}

function requireValue(name: string, value?: string): string {
  if (!value || value.startsWith("--")) {
    throw new Error(`${name} requires a value`);
  }
  return value;
}

function parsePositiveInt(value: string | undefined, fallback: number): number {
  if (!value) return fallback;
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    throw new Error(`expected a positive integer, got ${value}`);
  }
  return Math.floor(parsed);
}

function parseBool(value: string | undefined, fallback: boolean): boolean {
  if (value === undefined || value === "") return fallback;
  return ["1", "true", "yes", "on"].includes(value.toLowerCase());
}

function safeUsername(): string {
  try {
    return os.userInfo().username.replace(/[^a-zA-Z0-9_.-]/g, "-").slice(0, 64);
  } catch {
    return "";
  }
}

