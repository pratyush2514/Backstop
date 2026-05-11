import assert from "node:assert/strict";
import test from "node:test";
import { agentIdFor, jsonToolResult, loadConfig, modeAllows, resolveMcpAgentId } from "../dist/index.js";

test("loadConfig resolves URL, token, agent id, timeout, and approval setting", () => {
  const config = loadConfig(
    {
      BACKSTOP_URL: "http://gateway:8080",
      BACKSTOP_TOKEN: "secret-token",
      BACKSTOP_AGENT_ID: "cursor-agent",
      BACKSTOP_TIMEOUT_MS: "1234",
      BACKSTOP_MCP_ENABLE_APPROVAL_TOOLS: "true",
    },
    [],
  );
  assert.equal(config.backstopUrl, "http://gateway:8080");
  assert.equal(config.backstopToken, "secret-token");
  assert.equal(config.agentId, "cursor-agent");
  assert.equal(config.timeoutMs, 1234);
  assert.equal(config.enableApprovalTools, true);
  assert.equal(config.mode, "operator");
});

test("managed local mode resolves postgres url without requiring a gateway url", () => {
  const config = loadConfig(
    {
      BACKSTOP_POSTGRES_URL: "postgresql://postgres:password@localhost:5432/app",
      BACKSTOP_AGENT_ID: "cursor-agent",
      BACKSTOP_PROFILE: "local-dev",
    },
    [],
  );
  assert.equal(config.backstopUrl, undefined);
  assert.equal(config.backstopPostgresUrl, "postgresql://postgres:password@localhost:5432/app");
  assert.equal(config.profile, "local-dev");
});

test("CLI args override env config", () => {
  const config = loadConfig(
    {
      BACKSTOP_URL: "http://gateway:8080",
      BACKSTOP_AGENT_ID: "env-agent",
    },
    ["--url", "http://localhost:8080", "--agent-id", "cli-agent", "--timeout-ms", "999"],
  );
  assert.equal(config.backstopUrl, "http://localhost:8080");
  assert.equal(config.agentId, "cli-agent");
  assert.equal(config.timeoutMs, 999);
});

test("agent id fallback is stable and overrideable", () => {
  const fallback = resolveMcpAgentId();
  assert.match(fallback, /^backstop-mcp-/);
  assert.equal(agentIdFor({ agentId: "configured", backstopUrl: "", timeoutMs: 1, enableApprovalTools: false, mode: "agent" }, "tool-agent"), "tool-agent");
  assert.equal(agentIdFor({ agentId: "configured", backstopUrl: "", timeoutMs: 1, enableApprovalTools: false, mode: "agent" }, undefined), "configured");
});

test("explicit MCP mode overrides legacy approval flag", () => {
  const config = loadConfig(
    {
      BACKSTOP_MCP_ENABLE_APPROVAL_TOOLS: "true",
      BACKSTOP_MCP_MODE: "readonly",
      BACKSTOP_POSTGRES_URL: "postgresql://postgres:password@localhost:5432/app",
    },
    [],
  );
  assert.equal(config.mode, "readonly");
});

test("MCP mode profiles split execution and approval capabilities", () => {
  assert.equal(modeAllows("agent", "execute"), true);
  assert.equal(modeAllows("agent", "approvalWrite"), false);
  assert.equal(modeAllows("agent", "restorePrepare"), false);
  assert.equal(modeAllows("operator", "execute"), false);
  assert.equal(modeAllows("operator", "approvalWrite"), true);
  assert.equal(modeAllows("operator", "restorePrepare"), true);
  assert.equal(modeAllows("readonly", "execute"), false);
  assert.equal(modeAllows("readonly", "approvalWrite"), false);
  assert.equal(modeAllows("readonly", "restorePrepare"), false);
  assert.equal(modeAllows("admin", "execute"), true);
  assert.equal(modeAllows("admin", "approvalWrite"), true);
  assert.equal(modeAllows("admin", "restorePrepare"), true);
});

test("jsonToolResult returns MCP text content with formatted JSON", () => {
  const result = jsonToolResult({ status: "ok" });
  assert.equal(result.content[0].type, "text");
  assert.match(result.content[0].text, /"status": "ok"/);
});

