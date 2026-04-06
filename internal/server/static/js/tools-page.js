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

import { fetchTools } from "./api.js";
import { $, clear, el } from "./dom.js";
import { initHeaderEditor } from "./header-editor.js";
import { bindModalDismiss } from "./modal.js";
import { bootstrapUIAuth, isAuthRequiredError, redirectToLogin, renderAuthControls } from "./oidc.js";
import { initSidebar } from "./sidebar.js";
import { state } from "./state.js";
import { initToolRunner } from "./tool-runner.js";

const LIST_COLLAPSE_STORAGE_KEY = "tools.listPaneCollapsed";

function renderToolList() {
  const container = $("#tool-list");
  if (!container) return;
  clear(container);

  if (!state.filteredTools.length) {
    const empty = el("div", "panel", "No tools found.");
    container.appendChild(empty);
    return;
  }

  state.filteredTools.forEach((tool) => {
    const item = el("button", "list-item");
    item.type = "button";

    if (state.selectedTool?.name === tool.name) {
      item.classList.add("list-item--active");
    }

    item.append(
      el("div", "list-item__title", tool.name)
    );

    item.addEventListener("click", () => {
      state.selectedTool = tool;
      renderSelectedTool();
      renderToolList();
    });

    container.appendChild(item);
  });
}

function renderParameterInput(name, schema) {
  const row = el("div", "panel panel--subtle");
  row.setAttribute("data-param-name", name);
  row.setAttribute("data-param-type", schema?.type || "string");
  if (schema?.items?.type) {
    row.setAttribute("data-param-item-type", schema.items.type);
  }

  const field = el("label", "field");
  const title = el("span", "field__label", `${name}${schema?.required ? "" : " (optional)"}`);
  field.appendChild(title);

  let input;
  if (Array.isArray(schema?.enum) && schema.enum.length > 0) {
    input = document.createElement("select");
    input.className = "input";
    input.setAttribute("data-param-input", "true");

    if (!schema.required) {
      const emptyOption = document.createElement("option");
      emptyOption.value = "";
      emptyOption.textContent = "Select value";
      input.appendChild(emptyOption);
    }

    schema.enum.forEach((value) => {
      const option = document.createElement("option");
      option.value = String(value);
      option.textContent = String(value);
      input.appendChild(option);
    });
  } else {
    input = document.createElement("input");
    input.className = "input";
    input.setAttribute("data-param-input", "true");
    input.type = "text";
    input.placeholder = schema?.description || "Enter a value";
    if (schema?.default !== undefined && schema?.default !== null) {
      input.value = typeof schema.default === "object" ? JSON.stringify(schema.default) : String(schema.default);
    }
  }

  field.appendChild(input);

  if (schema?.description) {
    field.appendChild(el("span", "muted small", schema.description));
  }

  const includeLabel = el("label", "checkbox-inline");
  const includeCheckbox = document.createElement("input");
  includeCheckbox.type = "checkbox";
  includeCheckbox.setAttribute("data-param-enabled", "true");
  includeCheckbox.checked = !!schema?.required;
  if (schema?.required) includeCheckbox.disabled = true;

  includeLabel.append(
    includeCheckbox,
    el("span", "", schema?.required ? "Required" : "Include parameter")
  );

  row.append(field, includeLabel);
  return row;
}

function renderParameters(parameters = {}) {
  const container = $("#tool-parameters");
  if (!container) return;
  clear(container);

  const entries = Object.entries(parameters || {});
  if (!entries.length) {
    container.appendChild(el("div", "muted", "This tool does not define parameters."));
    return;
  }

  entries.forEach(([name, schema]) => {
    container.appendChild(renderParameterInput(name, schema));
  });
}

function renderSelectedTool() {
  const tool = state.selectedTool;

  $("#tool-title").textContent = tool?.name || "Select a tool";
  $("#tool-subtitle").textContent = tool?.description || "Choose a tool from the left to inspect parameters and execute it.";
  $("#tool-name").textContent = tool?.name || "—";
  $("#tool-description").textContent = tool?.description || "Select a tool to view its description.";
  $("#run-tool-button").disabled = !tool;
  $("#tool-response").textContent = "Results will appear here...";

  renderParameters(tool?.parameters || {});
}

function applyFilter(query) {
  const value = (query || "").trim().toLowerCase();
  if (!value) {
    state.filteredTools = [...state.tools];
  } else {
    state.filteredTools = state.tools.filter((tool) => {
      return tool.name.toLowerCase().includes(value);
    });
  }

  if (state.selectedTool && !state.filteredTools.some((tool) => tool.name === state.selectedTool.name)) {
    state.selectedTool = null;
    renderSelectedTool();
  }

  renderToolList();
}

function syncListPaneState() {
  const shell = document.querySelector(".app-shell--workspace");
  const toggleButton = $("#toggle-list-pane-button");
  if (!shell || !toggleButton) return;

  const isCollapsed = shell.classList.contains("is-list-collapsed");
  toggleButton.textContent = isCollapsed ? "Show list" : "Hide list";
  toggleButton.setAttribute("aria-expanded", String(!isCollapsed));
}

function setListPaneCollapsed(collapsed) {
  const shell = document.querySelector(".app-shell--workspace");
  if (!shell) return;

  shell.classList.toggle("is-list-collapsed", collapsed);
  localStorage.setItem(LIST_COLLAPSE_STORAGE_KEY, collapsed ? "1" : "0");
  syncListPaneState();
}

function initListPaneToggle() {
  const toggleButton = $("#toggle-list-pane-button");
  if (!toggleButton) return;

  const shouldCollapse = localStorage.getItem(LIST_COLLAPSE_STORAGE_KEY) === "1";
  setListPaneCollapsed(shouldCollapse);

  toggleButton.addEventListener("click", () => {
    const shell = document.querySelector(".app-shell--workspace");
    if (!shell) return;
    setListPaneCollapsed(!shell.classList.contains("is-list-collapsed"));
  });
}

async function init() {
  try {
    await bootstrapUIAuth();
    bindModalDismiss("#header-modal");
    initSidebar();
    initHeaderEditor();
    initToolRunner();
    initListPaneToggle();

    const searchInput = $("#tool-search");
    searchInput?.addEventListener("input", (event) => {
      applyFilter(event.target.value);
    });

    state.tools = await fetchTools();
    state.filteredTools = [...state.tools];

    const url = new URL(window.location.href);
    const requestedTool = url.searchParams.get("tool");
    if (requestedTool) {
      const byName = state.filteredTools.find((tool) => tool.name === requestedTool);
      if (byName) {
        state.selectedTool = byName;
      }
    }

    renderToolList();

    if (!state.selectedTool && state.filteredTools.length > 0) {
      state.selectedTool = state.filteredTools[0];
    }
    if (state.selectedTool) {
      renderSelectedTool();
      renderToolList();
    }
  } catch (error) {
    if (isAuthRequiredError(error) && state.uiAuthEnabled) {
      await redirectToLogin(window.location.href);
      return;
    }
    renderAuthControls(error.message);
    const container = $("#tool-list");
    clear(container);
    container.appendChild(el("div", "panel", `Failed to load tools: ${error.message}`));
  }
}

init();
