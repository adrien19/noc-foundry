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

import { clearAuthSession, loadSavedBearerToken } from "./auth.js";
import { AuthRequiredError } from "./oidc.js";

function readJSONSafe(text) {
  try {
    return JSON.parse(text);
  } catch {
    return null;
  }
}

async function apiFetch(url, options = {}) {
  const headers = new Headers(options.headers || {});
  const token = loadSavedBearerToken();
  if (token && !headers.has("Authorization")) {
    headers.set("Authorization", `Bearer ${token}`);
  }

  const response = await fetch(url, {
    ...options,
    headers
  });

  if (response.status === 401) {
    clearAuthSession();
    throw new AuthRequiredError(`Authentication required (${response.status})`, response.status);
  }

  return response;
}

function normalizeParameterArray(parameters = []) {
  const out = {};
  (parameters || []).forEach((param) => {
    if (!param?.name) return;
    out[param.name] = {
      name: param.name,
      type: param.type || "string",
      description: param.description || "",
      default: param.default,
      enum: Array.isArray(param.allowedValues) ? param.allowedValues : null,
      required: !!param.required,
      items: param.items && typeof param.items === "object"
        ? { type: param.items.type || "string" }
        : null
    };
  });
  return out;
}

function normalizeParameterObject(properties = {}, required = []) {
  const requiredSet = new Set(Array.isArray(required) ? required : []);
  const out = {};

  Object.entries(properties || {}).forEach(([name, schema]) => {
    out[name] = {
      name,
      type: schema?.type || "string",
      description: schema?.description || "",
      default: schema?.default,
      enum: Array.isArray(schema?.enum) ? schema.enum : null,
      required: requiredSet.has(name) || !!schema?.required,
      items: schema?.items && typeof schema.items === "object"
        ? { type: schema.items.type || "string" }
        : null
    };
  });

  return out;
}

function normalizeParameters(tool) {
  if (tool?.inputSchema?.properties) {
    return normalizeParameterObject(tool.inputSchema.properties, tool.inputSchema.required || []);
  }

  if (Array.isArray(tool?.parameters)) {
    return normalizeParameterArray(tool.parameters);
  }

  if (tool?.parameters && !Array.isArray(tool.parameters)) {
    const entries = Object.entries(tool.parameters);
    const normalized = {};

    entries.forEach(([name, value]) => {
      if (value && typeof value === "object" && !Array.isArray(value)) {
        normalized[name] = {
          name,
          type: value.type || "string",
          description: value.description || "",
          default: value.default,
          enum: Array.isArray(value.enum) ? value.enum : null,
          required: !!value.required,
          items: value.items && typeof value.items === "object"
            ? { type: value.items.type || "string" }
            : null
        };
      } else {
        normalized[name] = {
          name,
          type: "string",
          description: "",
          default: undefined,
          enum: null,
          required: false,
          items: null
        };
      }
    });

    return normalized;
  }

  return {};
}

function normalizeTool(tool) {
  return {
    name: tool?.name || tool?.id || tool?.toolName || "Unnamed tool",
    description: tool?.description || tool?.summary || tool?.title || "No description available.",
    parameters: normalizeParameters(tool),
    raw: tool
  };
}

function toolListFromMap(toolsMap = {}) {
  return Object.entries(toolsMap || {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([name, tool]) => normalizeTool({ ...tool, name }));
}

export async function fetchTools() {
  const res = await apiFetch("/api/tools", {
    headers: { Accept: "application/json" }
  });

  if (res.status === 404) {
    const fallback = await apiFetch("/api/toolset", {
      headers: { Accept: "application/json" }
    });

    if (!fallback.ok) {
      throw new Error(`Failed to load tools (${fallback.status})`);
    }

    const fallbackPayload = readJSONSafe(await fallback.text()) || {};
    return toolListFromMap(fallbackPayload?.tools || {});
  }

  if (!res.ok) {
    throw new Error(`Failed to load tools (${res.status})`);
  }

  const text = await res.text();
  const payload = readJSONSafe(text);
  const list = Array.isArray(payload) ? payload : [];
  return list.map(normalizeTool);
}

export async function fetchToolsets() {
  const res = await apiFetch("/api/toolsets", {
    headers: { Accept: "application/json" }
  });

  if (!res.ok) {
    throw new Error(`Failed to load toolsets (${res.status})`);
  }

  const payload = readJSONSafe(await res.text());
  return Array.isArray(payload) ? payload : [];
}

export async function fetchToolset(name) {
  const res = await apiFetch(`/api/toolset/${encodeURIComponent(name)}`, {
    headers: { Accept: "application/json" }
  });

  if (!res.ok) {
    throw new Error(`Failed to load toolset (${res.status})`);
  }

  const text = await res.text();
  const payload = readJSONSafe(text) || {};

  const tools = toolListFromMap(payload?.tools || {});

  return {
    name,
    description: payload?.description || payload?.summary || "Toolset details",
    promptset: payload?.promptset || "",
    tools,
    raw: payload
  };
}

export async function fetchPrompts() {
  const res = await apiFetch("/api/prompts", {
    headers: { Accept: "application/json" }
  });

  if (!res.ok) {
    throw new Error(`Failed to load prompts (${res.status})`);
  }

  const payload = readJSONSafe(await res.text());
  return Array.isArray(payload) ? payload : [];
}

export async function fetchPromptsets() {
  const res = await apiFetch("/api/promptsets", {
    headers: { Accept: "application/json" }
  });

  if (!res.ok) {
    throw new Error(`Failed to load promptsets (${res.status})`);
  }

  const payload = readJSONSafe(await res.text());
  return Array.isArray(payload) ? payload : [];
}

export async function fetchPromptset(name) {
  const res = await apiFetch(`/api/promptset/${encodeURIComponent(name)}`, {
    headers: { Accept: "application/json" }
  });

  if (!res.ok) {
    throw new Error(`Failed to load promptset (${res.status})`);
  }

  const text = await res.text();
  const payload = readJSONSafe(text) || {};

  const prompts = Object.entries(payload?.prompts || {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([pName, p]) => ({
      name: pName,
      description: p?.description || "No description available.",
      argumentCount: Array.isArray(p?.arguments) ? p.arguments.length : 0,
      arguments: p?.arguments || []
    }));

  return {
    name,
    serverVersion: payload?.serverVersion || "",
    prompts,
    raw: payload
  };
}

export async function runTool({ name, body, headers }) {
  const mergedHeaders = new Headers(headers || {});
  if (!mergedHeaders.has("Content-Type")) {
    mergedHeaders.set("Content-Type", "application/json");
  }

  const res = await apiFetch(`/api/tool/${encodeURIComponent(name)}/invoke`, {
    method: "POST",
    headers: mergedHeaders,
    body: JSON.stringify(body || {})
  });

  const text = await res.text();

  return {
    ok: res.ok,
    status: res.status,
    statusText: res.statusText,
    body: text,
    parsed: readJSONSafe(text)
  };
}
