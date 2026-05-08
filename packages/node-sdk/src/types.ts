export type RiskLevel = "SAFE" | "LOW" | "HIGH" | "IMPACT_CRITICAL" | "CRITICAL" | string;

export type PolicyAction = "execute" | "approve" | "block" | string;

export interface BackstopClientOptions {
  url: string;
  token?: string;
  agentId?: string;
  timeoutMs?: number;
  fetchImpl?: typeof fetch;
  defaultHeaders?: Record<string, string>;
}

export interface BackstopLocalClientOptions {
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
  mode?: "agent" | "operator" | "readonly" | "admin";
  timeoutMs?: number;
  fetchImpl?: typeof fetch;
  defaultHeaders?: Record<string, string>;
}

export interface RequestOptions {
  timeoutMs?: number;
  signal?: AbortSignal;
  requestId?: string | number;
}

export interface ExecuteQueryOptions extends RequestOptions {
  agentId?: string;
  dbUrl?: string;
  snapshotId?: string;
  throwOnPolicyViolation?: boolean;
}

export interface AnalyzeQueryOptions extends RequestOptions {
  agentId?: string;
  dbUrl?: string;
}

export interface ListSnapshotsOptions extends RequestOptions {
  table?: string;
}

export interface AuditOptions extends RequestOptions {
  agentId?: string;
  risk?: RiskLevel;
}

export interface SafetyMetadata {
  risk_level?: RiskLevel;
  operation?: string;
  reason?: string;
  table?: string | null;
  schema?: string | null;
  table_recoverable?: boolean;
  recovery_required?: boolean;
  recovery_possible?: boolean;
  policy_action?: PolicyAction;
  policy_reason?: string;
  requires_approval?: boolean;
  parse_error_present?: boolean;
  parse_error?: string;
  estimated_affected_rows?: number;
  estimated_table_rows?: number;
  affected_percent?: number;
  protected_table?: boolean;
  protected_columns?: string[];
  impact_status?: string;
  [key: string]: unknown;
}

export interface ExecuteQueryResult {
  status: string;
  risk_level?: RiskLevel;
  safety_metadata?: SafetyMetadata;
  message?: string;
  approval_id?: string;
  snapshot_id?: string | null;
  result_type?: "rows" | "command" | string;
  columns?: string[];
  row_count?: number;
  preview?: Array<Record<string, unknown>>;
  rows_affected?: number;
  error?: string;
  [key: string]: unknown;
}

export interface AnalyzeQueryResult {
  status: "analyzed" | string;
  risk_level?: RiskLevel;
  safety_metadata?: SafetyMetadata;
  message?: string;
  [key: string]: unknown;
}

export interface JsonRpcResponse<T> {
  jsonrpc: "2.0";
  id: string | number | null;
  result?: T;
  error?: {
    code: number;
    message: string;
  };
}

export interface CollectionResponse<T = Record<string, unknown>> {
  items?: T[];
  entries?: T[];
  pending?: T[];
  count?: number;
  [key: string]: unknown;
}

