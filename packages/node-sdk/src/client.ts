import {
  BackstopApprovalRequiredError,
  BackstopAuthError,
  BackstopError,
  BackstopJsonRpcError,
  BackstopNetworkError,
  BackstopPolicyBlockedError,
  BackstopRecoveryNotReadyError,
  BackstopTimeoutError,
  scrubSecrets,
} from "./errors.js";
import type {
  AnalyzeQueryOptions,
  AnalyzeQueryResult,
  AuditOptions,
  BackstopLocalClientOptions,
  CollectionResponse,
  BackstopClientOptions,
  ExecuteQueryOptions,
  ExecuteQueryResult,
  JsonRpcResponse,
  ListSnapshotsOptions,
  RequestOptions,
  RestoreSnapshotOptions,
  RestoreSnapshotPlan,
} from "./types.js";
import { ensureLocalRuntime } from "./runtime.js";

const DEFAULT_TIMEOUT_MS = 30_000;

export class BackstopClient {
  readonly url: string;
  readonly token?: string;
  readonly agentId: string;
  readonly timeoutMs: number;

  private readonly fetchImpl: typeof fetch;
  private readonly defaultHeaders: Record<string, string>;

  static async local(options: BackstopLocalClientOptions = {}): Promise<BackstopClient> {
    const runtime = await ensureLocalRuntime({
      postgresUrl: options.postgresUrl,
      agentId: options.agentId,
      profile: options.profile,
      homeDir: options.homeDir,
      host: options.host,
      gatewayPort: options.gatewayPort,
      syncMetricsPort: options.syncMetricsPort,
      minioPort: options.minioPort,
      minioConsolePort: options.minioConsolePort,
      storageBucket: options.storageBucket,
      startTimeoutMs: options.startTimeoutMs,
      runtimeVersion: options.runtimeVersion,
      runtimeRepo: options.runtimeRepo,
      mode: options.mode || "agent",
    });
    return new BackstopClient({
      url: runtime.url,
      token: runtime.token,
      agentId: runtime.agentId,
      timeoutMs: options.timeoutMs,
      fetchImpl: options.fetchImpl,
      defaultHeaders: options.defaultHeaders,
    });
  }

  constructor(options: BackstopClientOptions) {
    if (!options.url || !options.url.trim()) {
      throw new BackstopError("backstop url is required");
    }
    this.url = normalizeBaseUrl(options.url);
    this.token = options.token || process.env.BACKSTOP_TOKEN || process.env.BACKSTOP_GATEWAY_TOKEN;
    this.agentId = resolveAgentId(options.agentId);
    this.timeoutMs = options.timeoutMs ?? Number(process.env.BACKSTOP_TIMEOUT_MS || DEFAULT_TIMEOUT_MS);
    this.fetchImpl = options.fetchImpl ?? globalThis.fetch;
    this.defaultHeaders = options.defaultHeaders ?? {};
    if (typeof this.fetchImpl !== "function") {
      throw new BackstopError("global fetch is unavailable; use Node 18+ or pass fetchImpl");
    }
  }

  query(sql: string, options: ExecuteQueryOptions = {}): Promise<ExecuteQueryResult> {
    return this.executeQuery(sql, options);
  }

  async executeQuery(sql: string, options: ExecuteQueryOptions = {}): Promise<ExecuteQueryResult> {
    const result = await this.rpcCall<ExecuteQueryResult>(
      "execute_query",
      {
        query: requireSql(sql),
        agent_id: options.agentId || this.agentId,
        db_url: options.dbUrl,
        snapshot_id: options.snapshotId,
      },
      options,
    );
    if (options.throwOnPolicyViolation) {
      throwForPolicyResult(result);
    }
    return result;
  }

  analyzeQuery(sql: string, options: AnalyzeQueryOptions = {}): Promise<AnalyzeQueryResult> {
    return this.rpcCall<AnalyzeQueryResult>(
      "analyze_query",
      {
        query: requireSql(sql),
        agent_id: options.agentId || this.agentId,
        db_url: options.dbUrl,
      },
      options,
    );
  }

  async listSnapshots(options: ListSnapshotsOptions = {}): Promise<CollectionResponse> {
    const params = new URLSearchParams();
    if (options.table) params.set("table", options.table);
    return this.getJson<CollectionResponse>(`/metadata/snapshots${queryString(params)}`, options);
  }

  prepareRestoreSnapshot(snapshotId: string, table: string, options: RestoreSnapshotOptions = {}): Promise<RestoreSnapshotPlan> {
    if (!snapshotId || !snapshotId.trim()) throw new BackstopError("snapshot id is required");
    if (!table || !table.trim()) throw new BackstopError("table is required");
    return this.rpcCall<RestoreSnapshotPlan>(
      "prepare_restore_snapshot",
      {
        snapshot_id: snapshotId,
        table,
        agent_id: options.agentId || this.agentId,
        target_table: options.targetTable,
      },
      options,
    );
  }

  async getAudit(options: AuditOptions = {}): Promise<CollectionResponse> {
    const params = new URLSearchParams();
    if (options.agentId) params.set("agent_id", options.agentId);
    if (options.risk) params.set("risk", options.risk);
    return this.getJson<CollectionResponse>(`/metadata/audit${queryString(params)}`, options);
  }

  getAlerts(options: RequestOptions = {}): Promise<CollectionResponse> {
    return this.getJson<CollectionResponse>("/metadata/alerts", options);
  }

  getHealth(options: RequestOptions = {}): Promise<CollectionResponse> {
    return this.getJson<CollectionResponse>("/metadata/health", options);
  }

  getPendingApprovals(options: RequestOptions = {}): Promise<CollectionResponse> {
    return this.getJson<CollectionResponse>("/pending", options);
  }

  async approve(id: string, options: RequestOptions = {}): Promise<Record<string, unknown>> {
    if (!id || !id.trim()) throw new BackstopError("approval id is required");
    return this.postJson<Record<string, unknown>>(`/approve/${encodeURIComponent(id)}`, undefined, options);
  }

  async deny(id: string, options: RequestOptions = {}): Promise<Record<string, unknown>> {
    if (!id || !id.trim()) throw new BackstopError("approval id is required");
    return this.postJson<Record<string, unknown>>(`/deny/${encodeURIComponent(id)}`, undefined, options);
  }

  getMetrics(options: RequestOptions = {}): Promise<string> {
    return this.requestText("/metrics", { method: "GET" }, options);
  }

  getAdminStatus(options: RequestOptions = {}): Promise<Record<string, unknown>> {
    return this.getJson<Record<string, unknown>>("/admin/status", options);
  }

  pause(reason?: string, options: RequestOptions = {}): Promise<Record<string, unknown>> {
    return this.postJson<Record<string, unknown>>("/admin/pause", reason ? { reason } : undefined, options);
  }

  resume(reason?: string, options: RequestOptions = {}): Promise<Record<string, unknown>> {
    return this.postJson<Record<string, unknown>>("/admin/resume", reason ? { reason } : undefined, options);
  }

  async rpcCall<T>(toolName: string, args: Record<string, unknown>, options: RequestOptions = {}): Promise<T> {
    const response = await this.postJson<JsonRpcResponse<T>>(
      "/",
      {
        jsonrpc: "2.0",
        id: options.requestId ?? randomRequestId(),
        method: "tools/call",
        params: {
          name: toolName,
          arguments: stripUndefined(args),
        },
      },
      options,
    );
    if (response.error) {
      throw new BackstopJsonRpcError(response.error.message, {
        code: response.error.code,
        body: response,
      });
    }
    if (response.result === undefined) {
      throw new BackstopJsonRpcError("backstop JSON-RPC response did not include result", { body: response });
    }
    return response.result;
  }

  private getJson<T>(path: string, options: RequestOptions): Promise<T> {
    return this.requestJson<T>(path, { method: "GET" }, options);
  }

  private postJson<T>(path: string, body: unknown, options: RequestOptions): Promise<T> {
    return this.requestJson<T>(
      path,
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: body === undefined ? undefined : JSON.stringify(body),
      },
      options,
    );
  }

  private async requestJson<T>(path: string, init: RequestInit, options: RequestOptions): Promise<T> {
    const text = await this.requestText(path, init, options);
    try {
      return JSON.parse(text) as T;
    } catch (error) {
      throw new BackstopError("backstop returned non-JSON response", {
        body: scrubSecrets(text.slice(0, 1000)),
        cause: error,
      });
    }
  }

  private async requestText(path: string, init: RequestInit, options: RequestOptions): Promise<string> {
    const controller = new AbortController();
    const timeoutMs = options.timeoutMs ?? this.timeoutMs;
    const timer = timeoutMs > 0 ? setTimeout(() => controller.abort(new Error("timeout")), timeoutMs) : undefined;
    const abortListener = () => controller.abort(options.signal?.reason);
    if (options.signal) {
      if (options.signal.aborted) {
        controller.abort(options.signal.reason);
      } else {
        options.signal.addEventListener("abort", abortListener, { once: true });
      }
    }
    try {
      const response = await this.fetchImpl(this.makeUrl(path), {
        ...init,
        headers: this.makeHeaders(init.headers),
        signal: controller.signal,
      });
      const text = await response.text();
      if (!response.ok) {
        if (response.status === 401) {
          throw new BackstopAuthError("backstop authentication failed", { status: response.status, body: text });
        }
        throw new BackstopError(`backstop request failed with HTTP ${response.status}`, {
          status: response.status,
          body: text,
        });
      }
      return text;
    } catch (error) {
      if (error instanceof BackstopError) throw error;
      if (controller.signal.aborted) {
        throw new BackstopTimeoutError("backstop request timed out", { cause: error });
      }
      throw new BackstopNetworkError("backstop network request failed", { cause: error });
    } finally {
      if (timer) clearTimeout(timer);
      if (options.signal) {
        options.signal.removeEventListener("abort", abortListener);
      }
    }
  }

  private makeUrl(path: string): string {
    if (/^https?:\/\//i.test(path)) return path;
    return `${this.url}${path.startsWith("/") ? path : `/${path}`}`;
  }

  private makeHeaders(initHeaders?: HeadersInit): Headers {
    const headers = new Headers(this.defaultHeaders);
    if (this.token) {
      headers.set("Authorization", `Bearer ${this.token}`);
    }
    if (initHeaders) {
      new Headers(initHeaders).forEach((value, key) => headers.set(key, value));
    }
    return headers;
  }
}

export function resolveAgentId(agentId?: string): string {
  const candidate =
    agentId ||
    process.env.BACKSTOP_AGENT_ID ||
    process.env.AGENT_ID ||
    process.env.MCP_SERVER_NAME ||
    "backstop-node-agent";
  return candidate.trim() || "backstop-node-agent";
}

function throwForPolicyResult(result: ExecuteQueryResult): void {
  if (result.status === "blocked") {
    const reason = String(result.message || result.safety_metadata?.policy_reason || "");
    if (/recovery/i.test(reason)) {
      throw new BackstopRecoveryNotReadyError(result);
    }
    throw new BackstopPolicyBlockedError(result);
  }
  if (result.status === "denied" || result.status === "approval_required" || result.status === "pending_approval") {
    throw new BackstopApprovalRequiredError(result);
  }
  if (result.status === "failed") {
    throw new BackstopError(result.error || result.message || "backstop query execution failed", { body: result });
  }
}

function normalizeBaseUrl(url: string): string {
  return url.trim().replace(/\/+$/, "");
}

function requireSql(sql: string): string {
  if (!sql || !sql.trim()) {
    throw new BackstopError("SQL query is required");
  }
  return sql;
}

function randomRequestId(): string {
  if (globalThis.crypto?.randomUUID) {
    return globalThis.crypto.randomUUID();
  }
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function stripUndefined(input: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(Object.entries(input).filter(([, value]) => value !== undefined && value !== ""));
}

function queryString(params: URLSearchParams): string {
  const value = params.toString();
  return value ? `?${value}` : "";
}

