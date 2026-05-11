import type { ExecuteQueryResult } from "./types.js";

export interface BackstopErrorOptions {
  status?: number;
  code?: number;
  body?: unknown;
  cause?: unknown;
}

export class BackstopError extends Error {
  readonly status?: number;
  readonly code?: number;
  readonly body?: unknown;

  constructor(message: string, options: BackstopErrorOptions = {}) {
    super(scrubSecrets(message));
    this.name = "BackstopError";
    this.status = options.status;
    this.code = options.code;
    this.body = scrubBody(options.body);
    if (options.cause !== undefined) {
      this.cause = options.cause;
    }
  }
}

export class BackstopAuthError extends BackstopError {
  constructor(message = "backstop authentication failed", options: BackstopErrorOptions = {}) {
    super(message, options);
    this.name = "BackstopAuthError";
  }
}

export class BackstopNetworkError extends BackstopError {
  constructor(message: string, options: BackstopErrorOptions = {}) {
    super(message, options);
    this.name = "BackstopNetworkError";
  }
}

export class BackstopTimeoutError extends BackstopNetworkError {
  constructor(message = "backstop request timed out", options: BackstopErrorOptions = {}) {
    super(message, options);
    this.name = "BackstopTimeoutError";
  }
}

export class BackstopJsonRpcError extends BackstopError {
  constructor(message: string, options: BackstopErrorOptions = {}) {
    super(message, options);
    this.name = "BackstopJsonRpcError";
  }
}

export class BackstopPolicyBlockedError extends BackstopError {
  readonly result: ExecuteQueryResult;

  constructor(result: ExecuteQueryResult) {
    super(result.message || "backstop policy blocked the query", { body: result });
    this.name = "BackstopPolicyBlockedError";
    this.result = result;
  }
}

export class BackstopApprovalRequiredError extends BackstopError {
  readonly result: ExecuteQueryResult;

  constructor(result: ExecuteQueryResult) {
    super(result.message || "backstop requires approval before executing the query", { body: result });
    this.name = "BackstopApprovalRequiredError";
    this.result = result;
  }
}

export class BackstopRecoveryNotReadyError extends BackstopError {
  readonly result: ExecuteQueryResult;

  constructor(result: ExecuteQueryResult) {
    super(result.message || "backstop recovery readiness check failed", { body: result });
    this.name = "BackstopRecoveryNotReadyError";
    this.result = result;
  }
}

export function scrubSecrets(value: string): string {
  return value
    .replace(/(authorization:\s*bearer\s+)[^\s,;]+/gi, "$1[REDACTED]")
    .replace(/(x-backstop-token:\s*)[^\s,;]+/gi, "$1[REDACTED]")
    .replace(/(token=)[^&\s]+/gi, "$1[REDACTED]")
    .replace(/(password=)[^&\s]+/gi, "$1[REDACTED]")
    .replace(/:\/\/([^:\s/@]+):([^@\s/]+)@/g, "://$1:[REDACTED]@");
}

function scrubBody(body: unknown): unknown {
  if (typeof body === "string") {
    return scrubSecrets(body);
  }
  if (body == null || typeof body !== "object") {
    return body;
  }
  try {
    return JSON.parse(scrubSecrets(JSON.stringify(body)));
  } catch {
    return "[unserializable]";
  }
}

