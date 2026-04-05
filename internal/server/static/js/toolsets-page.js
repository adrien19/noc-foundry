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

import { fetchToolset, fetchToolsets } from "./api.js";
import { $, clear, el } from "./dom.js";
import { bootstrapUIAuth, isAuthRequiredError, redirectToLogin, renderAuthControls } from "./oidc.js";
import { initSidebar } from "./sidebar.js";

const state = {
  toolsets: [],
  filteredToolsets: [],
  selectedToolsetName: null
};

function renderToolsetList() {
  const container = $("#toolset-list");
  if (!container) return;
  clear(container);

  if (!state.filteredToolsets.length) {
    container.appendChild(el("div", "panel", "No toolsets found."));
    return;
  }

  state.filteredToolsets.forEach((toolset) => {
    const item = el("button", "list-item");
    item.type = "button";
    if (state.selectedToolsetName === toolset.name) {
      item.classList.add("list-item--active");
    }
    item.append(
      el("div", "list-item__title", toolset.displayName || toolset.name || "Unnamed toolset"),
      el("div", "list-item__meta", `${toolset.toolCount || 0} tool(s)`)
    );
    item.addEventListener("click", () => {
      selectToolset(toolset);
    });
    container.appendChild(item);
  });
}

function renderToolset(toolset) {
  const displayName = toolset?.name || "Toolset not found";
  $("#toolset-title").textContent = displayName;
  $("#toolset-subtitle").textContent = toolset?.description || "Inspect tools available in this toolset.";
  $("#toolset-name").textContent = displayName;

  const toolsContainer = $("#toolset-tools");
  clear(toolsContainer);

  if (!toolset?.tools?.length) {
    toolsContainer.appendChild(el("div", "muted", "No tools found in this toolset."));
    return;
  }

  toolset.tools.forEach((tool) => {
    const item = el("a", "list-item");
    item.href = `/ui/tools?tool=${encodeURIComponent(tool.name || "")}`;
    item.append(
      el("div", "list-item__title", tool.name || "Unnamed tool"),
      el("div", "list-item__meta", tool.description || "Tool exposed by this toolset.")
    );
    toolsContainer.appendChild(item);
  });
}

function applyFilter(query) {
  const value = (query || "").trim().toLowerCase();
  if (!value) {
    state.filteredToolsets = [...state.toolsets];
  } else {
    state.filteredToolsets = state.toolsets.filter((toolset) => {
      const name = (toolset.name || "").toLowerCase();
      const displayName = (toolset.displayName || "").toLowerCase();
      return name.includes(value) || displayName.includes(value);
    });
  }
  renderToolsetList();
}

async function selectToolset(toolset) {
  try {
    const details = await fetchToolset(toolset.name);
    state.selectedToolsetName = toolset.name;
    renderToolset(details);
    renderToolsetList();
  } catch (error) {
    const container = $("#toolset-list");
    clear(container);
    container.appendChild(el("div", "panel", `Failed to load toolset: ${error.message}`));
  }
}

async function init() {
  try {
    await bootstrapUIAuth();
    initSidebar();

    state.toolsets = (await fetchToolsets()).filter((toolset) => (toolset?.name || "") !== "");
    state.filteredToolsets = [...state.toolsets];
    renderToolsetList();

    if (state.filteredToolsets.length > 0) {
      await selectToolset(state.filteredToolsets[0]);
    } else {
      $("#toolset-title").textContent = "No toolsets configured";
      $("#toolset-name").textContent = "—";
      clear($("#toolset-tools"));
      $("#toolset-tools").appendChild(el("div", "muted", "No tools found in this toolset."));
      $("#toolset-subtitle").textContent = "No explicit toolsets are defined in the current configuration.";
    }
  } catch (error) {
    if (isAuthRequiredError(error) && state.uiAuthEnabled) {
      await redirectToLogin(window.location.href);
      return;
    }
    renderAuthControls(error.message);
    const container = $("#toolset-list");
    clear(container);
    container.appendChild(el("div", "panel", `Failed to load toolsets: ${error.message}`));
  }
}

$("#toolset-search-button")?.addEventListener("click", () => {
  applyFilter($("#toolset-search")?.value || "");
});
$("#toolset-search")?.addEventListener("keydown", (event) => {
  if (event.key === "Enter") {
    applyFilter(event.target.value || "");
  }
});
$("#toolset-search")?.addEventListener("input", (event) => {
  applyFilter(event.target.value || "");
});

init();
