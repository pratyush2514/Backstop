import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import type { BackstopClient } from "@backstop/client";
import type { BackstopMcpConfig } from "./config.js";

const optionalAgentId = z
  .string()
  .min(1)
  .optional()
  .describe("Optional stable caller identity. Defaults to BACKSTOP_AGENT_ID from the MCP server config.");

export function createBackstopMcpServer(client: BackstopClient, config: BackstopMcpConfig): McpServer {
  const server = new McpServer(
    {
      name: "backstop",
      version: "0.1.0-alpha.2",
    },
    {
      instructions:
        "Use backstop for PostgreSQL access instead of connecting directly to the database. Analyze risky writes before execution. SAFE reads execute immediately; HIGH and CRITICAL SQL may require approval; DROP DATABASE and DROP SCHEMA are blocked by policy. Do not request or use raw PostgreSQL credentials.",
    },
  );

  if (modeAllows(config.mode, "execute")) {
    server.registerTool(
      "backstop_execute_query",
      {
        title: "Execute SQL Safely",
        description:
          "Execute SQL through the backstop gateway. SAFE reads execute immediately. Risky writes may require approval. CRITICAL table operations require a latest snapshot_id. DROP DATABASE and DROP SCHEMA are blocked.",
        inputSchema: {
          query: z.string().min(1).describe("SQL query to execute through backstop"),
          snapshot_id: z.string().min(1).optional().describe("Latest sidecar snapshot ID for approved CRITICAL table operations"),
          agent_id: optionalAgentId,
        },
      },
      async (args) =>
        jsonToolResult(
          await client.executeQuery(args.query, {
            snapshotId: args.snapshot_id,
            agentId: agentIdFor(config, args.agent_id),
          }),
        ),
    );
  }

  server.registerTool(
    "backstop_analyze_query",
    {
      title: "Analyze SQL Safety",
      description:
        "Analyze SQL safety without executing it. Use before UPDATE, DELETE, DROP, ALTER, TRUNCATE, migrations, or unclear agent-generated SQL.",
      inputSchema: {
        query: z.string().min(1).describe("SQL query to analyze without executing"),
        agent_id: optionalAgentId,
      },
    },
    async (args) =>
      jsonToolResult(
        await client.analyzeQuery(args.query, {
          agentId: agentIdFor(config, args.agent_id),
        }),
      ),
  );

  server.registerTool(
    "backstop_list_snapshots",
    {
      title: "List Recovery Snapshots",
      description: "List backstop snapshot metadata, optionally filtered by table name.",
      inputSchema: {
        table: z.string().min(1).optional().describe("Optional table name to filter snapshots"),
      },
    },
    async (args) => jsonToolResult(await client.listSnapshots({ table: args.table })),
  );

  if (modeAllows(config.mode, "restorePrepare")) {
    server.registerTool(
      "backstop_prepare_restore_snapshot",
      {
        title: "Prepare Snapshot Restore",
        description:
          "Prepare a secret-safe Backstop CLI restore plan for a snapshot. This never returns raw database credentials.",
        inputSchema: {
          snapshot_id: z.string().min(1).describe("Snapshot ID to restore"),
          table: z.string().min(1).describe("Original table name captured by the snapshot"),
          target_table: z.string().min(1).optional().describe("Optional target table. Defaults to {table}_recovered"),
          agent_id: optionalAgentId,
        },
      },
      async (args) =>
        jsonToolResult(
          await client.prepareRestoreSnapshot(args.snapshot_id, args.table, {
            targetTable: args.target_table,
            agentId: agentIdFor(config, args.agent_id),
          }),
        ),
    );
  }

  server.registerTool(
    "backstop_get_safety_status",
    {
      title: "Get Safety Status",
      description: "Get gateway/sidecar health, alerts, and pending approval status for operator visibility.",
      inputSchema: {},
    },
    async () => {
      const pendingPromise = modeAllows(config.mode, "approvalRead")
        ? client.getPendingApprovals()
        : Promise.resolve({ skipped: true, reason: `MCP mode ${config.mode} does not include approval read tools` });
      const [health, alerts, pending] = await Promise.allSettled([
        client.getHealth(),
        client.getAlerts(),
        pendingPromise,
      ]);
      return jsonToolResult({
        health: settledValue(health),
        alerts: settledValue(alerts),
        pending: settledValue(pending),
      });
    },
  );

  if (modeAllows(config.mode, "approvalRead")) {
    server.registerTool(
      "backstop_get_pending_approvals",
      {
        title: "Get Pending Approvals",
        description: "List currently pending backstop approval requests. This does not approve or deny anything.",
        inputSchema: {},
      },
      async () => jsonToolResult(await client.getPendingApprovals()),
    );
  }

  server.registerTool(
    "backstop_get_audit_events",
    {
      title: "Get Audit Events",
      description: "Read durable backstop audit events, optionally filtered by agent_id or risk.",
      inputSchema: {
        agent_id: z.string().min(1).optional().describe("Optional agent id filter"),
        risk: z.string().min(1).optional().describe("Optional risk filter such as SAFE, HIGH, CRITICAL"),
      },
    },
    async (args) => jsonToolResult(await client.getAudit({ agentId: args.agent_id, risk: args.risk })),
  );

  server.registerTool(
    "backstop_get_alerts",
    {
      title: "Get Alerts",
      description: "Read backstop alert metadata, including sidecar, storage, staleness, and bypass alerts.",
      inputSchema: {},
    },
    async () => jsonToolResult(await client.getAlerts()),
  );

  if (modeAllows(config.mode, "approvalWrite")) {
    server.registerTool(
      "backstop_approve_query",
      {
        title: "Approve Pending Query",
        description:
          "Approve a pending backstop query. Keep disabled for autonomous agents unless an operator intentionally delegates approvals to this MCP client.",
        inputSchema: {
          approval_id: z.string().min(1).describe("Approval ID returned by pending approvals"),
        },
      },
      async (args) => jsonToolResult(await client.approve(args.approval_id)),
    );

    server.registerTool(
      "backstop_deny_query",
      {
        title: "Deny Pending Query",
        description: "Deny a pending backstop query.",
        inputSchema: {
          approval_id: z.string().min(1).describe("Approval ID returned by pending approvals"),
        },
      },
      async (args) => jsonToolResult(await client.deny(args.approval_id)),
    );
  }

  if (config.mode === "admin") {
    server.registerTool(
      "backstop_get_admin_status",
      {
        title: "Get Admin Status",
        description: "Read gateway admin status including emergency pause state.",
        inputSchema: {},
      },
      async () => jsonToolResult(await client.getAdminStatus()),
    );

    server.registerTool(
      "backstop_pause_gateway",
      {
        title: "Emergency Pause Gateway",
        description: "Pause write/destructive query execution through the backstop gateway.",
        inputSchema: {
          reason: z.string().min(1).optional().describe("Operator-visible incident reason"),
        },
      },
      async (args) => jsonToolResult(await client.pause(args.reason)),
    );

    server.registerTool(
      "backstop_resume_gateway",
      {
        title: "Resume Gateway",
        description: "Resume query execution after an emergency pause.",
        inputSchema: {
          reason: z.string().min(1).optional().describe("Operator-visible reason for resuming"),
        },
      },
      async (args) => jsonToolResult(await client.resume(args.reason)),
    );
  }

  return server;
}

export type McpCapability = "execute" | "approvalRead" | "approvalWrite" | "restorePrepare";

export function modeAllows(mode: BackstopMcpConfig["mode"], capability: McpCapability): boolean {
  if (mode === "admin") return true;
  if (capability === "execute") return mode === "agent";
  if (capability === "approvalRead") return mode === "operator";
  if (capability === "approvalWrite") return mode === "operator";
  if (capability === "restorePrepare") return mode === "operator";
  return false;
}

export function agentIdFor(config: BackstopMcpConfig, override?: string): string {
  return override?.trim() || config.agentId;
}

export function jsonToolResult(value: unknown) {
  return {
    content: [
      {
        type: "text" as const,
        text: JSON.stringify(value, null, 2),
      },
    ],
  };
}

function settledValue(result: PromiseSettledResult<unknown>): unknown {
  if (result.status === "fulfilled") {
    return result.value;
  }
  return {
    error: result.reason instanceof Error ? result.reason.message : String(result.reason),
  };
}

