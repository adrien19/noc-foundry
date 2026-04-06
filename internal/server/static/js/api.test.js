// Copyright 2026 Adrien Ndikumana
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import test from "node:test";
import assert from "node:assert/strict";

import { clearAuthSession, saveBearerToken } from "./auth.js";
import { fetchTools, fetchToolset, runTool } from "./api.js";
import { AuthRequiredError } from "./oidc.js";

function response({ status = 200, statusText = "OK", body = {} } = {}) {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText,
    async text() {
      return typeof body === "string" ? body : JSON.stringify(body);
    }
  };
}

test.beforeEach(() => {
  clearAuthSession();
});

test("fetchTools preserves tool names from /api/tools", async () => {
  const calls = [];
  globalThis.fetch = async (url) => {
    calls.push(url);
    return response({
      body: [
        { name: "alpha", description: "A", parameters: [] },
        { name: "beta", description: "B", parameters: [] }
      ]
    });
  };

  const tools = await fetchTools();
  assert.deepEqual(calls, ["/api/tools"]);
  assert.deepEqual(
    tools.map((tool) => tool.name),
    ["alpha", "beta"]
  );
});

test("fetchTools falls back to /api/toolset on 404", async () => {
  const calls = [];
  globalThis.fetch = async (url) => {
    calls.push(url);
    if (url === "/api/tools") {
      return response({ status: 404, statusText: "Not Found", body: "not found" });
    }
    return response({
      body: {
        tools: {
          alpha: { description: "A", parameters: [] },
          beta: { description: "B", parameters: [] }
        }
      }
    });
  };

  const tools = await fetchTools();
  assert.deepEqual(calls, ["/api/tools", "/api/toolset"]);
  assert.deepEqual(
    tools.map((tool) => tool.name),
    ["alpha", "beta"]
  );
});

test("fetchToolset uses /api/toolset/{name} and parses tools map", async () => {
  const calls = [];
  globalThis.fetch = async (url) => {
    calls.push(url);
    return response({
      body: {
        serverVersion: "0.0.0",
        tools: {
          alpha: { description: "A", parameters: [] },
          beta: { description: "B", parameters: [] }
        }
      }
    });
  };

  const toolset = await fetchToolset("network_ops");
  assert.deepEqual(calls, ["/api/toolset/network_ops"]);
  assert.deepEqual(
    toolset.tools.map((tool) => tool.name),
    ["alpha", "beta"]
  );
});

test("runTool posts to /api/tool/{name}/invoke with bearer token", async () => {
  const calls = [];
  saveBearerToken("header.payload.signature");
  globalThis.fetch = async (url, options) => {
    calls.push({ url, options });
    return response({ status: 200, body: { result: "ok" } });
  };

  await runTool({ name: "alpha", body: { x: 1 } });
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, "/api/tool/alpha/invoke");
  assert.equal(calls[0].options.method, "POST");
  assert.equal(calls[0].options.headers.get("Authorization"), "Bearer header.payload.signature");
});

test("fetchTools throws AuthRequiredError on 401", async () => {
  globalThis.fetch = async () => response({ status: 401, statusText: "Unauthorized", body: "unauthorized" });

  await assert.rejects(
    () => fetchTools(),
    (error) => error instanceof AuthRequiredError
  );
});
