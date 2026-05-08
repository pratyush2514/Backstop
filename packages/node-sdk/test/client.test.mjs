import assert from "node:assert/strict";
import test from "node:test";
import {
  BackstopAuthError,
  BackstopClient,
  BackstopPolicyBlockedError,
  resolveAgentId,
} from "../dist/index.js";

test("executeQuery builds JSON-RPC payload with bearer auth and agent id", async () => {
  let seen;
  const client = new BackstopClient({
    url: "http://localhost:8080/",
    token: "secret-token",
    agentId: "agent-from-test",
    fetchImpl: async (url, init) => {
      seen = { url, init, body: JSON.parse(init.body) };
      return jsonResponse({
        jsonrpc: "2.0",
        id: seen.body.id,
        result: { status: "executed", risk_level: "SAFE" },
      });
    },
  });

  const result = await client.executeQuery("SELECT 1");
  assert.equal(result.status, "executed");
  assert.equal(seen.url, "http://localhost:8080/");
  assert.equal(seen.init.headers.get("Authorization"), "Bearer secret-token");
  assert.equal(seen.body.params.name, "execute_query");
  assert.equal(seen.body.params.arguments.agent_id, "agent-from-test");
  assert.equal(seen.body.params.arguments.query, "SELECT 1");
});

test("analyzeQuery calls analyze_query without execution options", async () => {
  let toolName;
  const client = new BackstopClient({
    url: "http://localhost:8080",
    agentId: "agent-1",
    fetchImpl: async (_url, init) => {
      const body = JSON.parse(init.body);
      toolName = body.params.name;
      return jsonResponse({
        jsonrpc: "2.0",
        id: body.id,
        result: { status: "analyzed", risk_level: "CRITICAL" },
      });
    },
  });

  const result = await client.analyzeQuery("DROP DATABASE prod");
  assert.equal(toolName, "analyze_query");
  assert.equal(result.status, "analyzed");
  assert.equal(result.risk_level, "CRITICAL");
});

test("policy results can be promoted to typed errors", async () => {
  const client = new BackstopClient({
    url: "http://localhost:8080",
    agentId: "agent-1",
    fetchImpl: async (_url, init) => {
      const body = JSON.parse(init.body);
      return jsonResponse({
        jsonrpc: "2.0",
        id: body.id,
        result: { status: "blocked", message: "DROP DATABASE is blocked by policy" },
      });
    },
  });

  await assert.rejects(
    () => client.executeQuery("DROP DATABASE prod", { throwOnPolicyViolation: true }),
    BackstopPolicyBlockedError,
  );
});

test("401 responses become auth errors without leaking token", async () => {
  const client = new BackstopClient({
    url: "http://localhost:8080",
    token: "secret-token",
    agentId: "agent-1",
    fetchImpl: async () => new Response("Authorization: Bearer secret-token", { status: 401 }),
  });

  await assert.rejects(async () => {
    try {
      await client.getHealth();
    } catch (error) {
      assert(error instanceof BackstopAuthError);
      assert(!JSON.stringify(error).includes("secret-token"));
      throw error;
    }
  }, BackstopAuthError);
});

test("agent id resolves from explicit input, env, then local fallback", () => {
  const previous = process.env.BACKSTOP_AGENT_ID;
  process.env.BACKSTOP_AGENT_ID = "env-agent";
  assert.equal(resolveAgentId("explicit-agent"), "explicit-agent");
  assert.equal(resolveAgentId(), "env-agent");
  if (previous === undefined) {
    delete process.env.BACKSTOP_AGENT_ID;
  } else {
    process.env.BACKSTOP_AGENT_ID = previous;
  }
  assert.equal(resolveAgentId(), "backstop-node-agent");
});

function jsonResponse(body, init = {}) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
    ...init,
  });
}

