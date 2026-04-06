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

import { $, $$ } from "./dom.js";
import { prettyJSON } from "./format.js";
import { isAuthRequiredError, redirectToLogin } from "./oidc.js";
import { state } from "./state.js";
import { runTool } from "./api.js";

let lastResult = null;

function normalizeType(type = "string") {
  const t = String(type || "string").toLowerCase();
  if (t === "float") return "number";
  return t;
}

function coerceScalar(raw, type) {
  const normalizedType = normalizeType(type);

  if (raw === "") return "";

  switch (normalizedType) {
    case "integer":
    case "number": {
      const n = Number(raw);
      if (Number.isNaN(n)) {
        throw new Error(`expected ${normalizedType}, got ${raw}`);
      }
      return n;
    }
    case "boolean": {
      const v = String(raw).trim().toLowerCase();
      if (v === "true") return true;
      if (v === "false") return false;
      throw new Error(`expected boolean ('true' or 'false'), got ${raw}`);
    }
    default:
      return raw;
  }
}

function coerceValue(raw, type, itemType) {
  const normalizedType = normalizeType(type);

  if (raw === "") return "";

  if (normalizedType.startsWith("array<") && normalizedType.endsWith(">")) {
    const inlineItemType = normalizedType.slice(6, -1);
    return coerceValue(raw, "array", inlineItemType);
  }

  switch (normalizedType) {
    case "integer":
    case "number": {
      return coerceScalar(raw, normalizedType);
    }
    case "boolean": {
      return coerceScalar(raw, "boolean");
    }
    case "map":
    case "object":
    case "array": {
      try {
        const parsed = JSON.parse(raw);

        if (normalizedType === "array") {
          if (!Array.isArray(parsed)) {
            throw new Error("expected a JSON array");
          }
          if (itemType) {
            return parsed.map((item) => coerceScalar(item, itemType));
          }
          return parsed;
        }

        if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
          throw new Error("expected a JSON object");
        }
        return parsed;
      } catch (error) {
        throw new Error(error?.message || `invalid JSON for ${normalizedType}`);
      }
    }
    default:
      return raw;
  }
}

function collectParameters() {
  const values = {};

  $$('[data-param-name]').forEach((row) => {
    const name = row.getAttribute('data-param-name');
    const type = row.getAttribute('data-param-type') || 'string';
    const itemType = row.getAttribute('data-param-item-type') || '';
    const checkbox = row.querySelector('[data-param-enabled]');
    const input = row.querySelector('[data-param-input]');

    if (!name || !input) return;
    if (checkbox && !checkbox.checked) return;

    const value = input.value;
    if (value === "" && checkbox && !checkbox.disabled) return;

    try {
      values[name] = coerceValue(value, type, itemType);
    } catch (error) {
      throw new Error(`Invalid value for ${name}: ${error.message}`);
    }
  });

  return values;
}

function extractPayload(result) {
  const parsedEnvelope = result?.parsed;

  if (parsedEnvelope && typeof parsedEnvelope === "object" && Object.prototype.hasOwnProperty.call(parsedEnvelope, "result")) {
    const nested = parsedEnvelope.result;
    if (typeof nested === "string") {
      try {
        return JSON.parse(nested);
      } catch {
        return nested;
      }
    }
    return nested;
  }

  if (parsedEnvelope !== null && parsedEnvelope !== undefined) {
    return parsedEnvelope;
  }

  const body = result?.body;
  if (typeof body === "string") {
    try {
      return JSON.parse(body);
    } catch {
      return body;
    }
  }
  return body;
}

function compactJSON(value) {
  if (typeof value === "string") {
    try {
      return JSON.stringify(JSON.parse(value));
    } catch {
      return value;
    }
  }

  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function effectiveHeaders() {
  return { ...(state.headers || {}) };
}

function renderResult(result) {
  const output = $("#tool-response");
  const prettify = $("#prettify-json");
  if (!output) return;

  const payload = extractPayload(result);
  const rendered = prettify?.checked
    ? prettyJSON(payload)
    : compactJSON(payload);

  output.textContent = `HTTP ${result.status} ${result.statusText}\n\n${rendered}`;
}

export function initToolRunner() {
  const button = $("#run-tool-button");
  const output = $("#tool-response");
  const prettifyCheckbox = $("#prettify-json");
  if (!button || !output) return;

  prettifyCheckbox?.addEventListener("change", () => {
    if (lastResult) {
      renderResult(lastResult);
    }
  });

  button.addEventListener("click", async () => {
    if (!state.selectedTool) return;

    button.disabled = true;
    output.textContent = "Running tool...";

    try {
      const result = await runTool({
        name: state.selectedTool.name,
        body: collectParameters(),
        headers: effectiveHeaders()
      });
      lastResult = result;
      renderResult(result);
    } catch (error) {
      if (isAuthRequiredError(error) && state.uiAuthEnabled) {
        await redirectToLogin(window.location.href);
        return;
      }
      lastResult = null;
      output.textContent = `Execution failed\n\n${error.message}`;
    } finally {
      button.disabled = false;
    }
  });
}
