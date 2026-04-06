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

import { $ } from "./dom.js";
import { state } from "./state.js";
import { loadSavedBearerToken, loadSavedHeaders, saveBearerToken, saveHeaders } from "./auth.js";
import { closeModal, openModal } from "./modal.js";

function stripAuthorizationHeaders(headers) {
  if (!headers || typeof headers !== "object") {
    return headers;
  }
  const next = { ...headers };
  delete next.Authorization;
  delete next.authorization;
  return next;
}

export function initHeaderEditor() {
  const openButton = $("#edit-headers-button");
  const saveButton = $("#save-headers-button");
  const tokenInput = $("#bearer-token");
  const jsonInput = $("#headers-json");
  const tokenPanel = $("#manual-token-panel");
  const managedAuthNote = $("#managed-auth-note");

  if (!openButton || !saveButton || !tokenInput || !jsonInput) return;

  state.headers = loadSavedHeaders();
  state.bearerToken = state.uiAuthEnabled ? "" : loadSavedBearerToken();

  if (state.uiAuthEnabled) {
    state.headers = stripAuthorizationHeaders(state.headers);
    saveHeaders(state.headers);
  }

  tokenPanel?.classList.toggle("hidden", state.uiAuthEnabled);
  managedAuthNote?.classList.toggle("hidden", !state.uiAuthEnabled);

  openButton.addEventListener("click", () => {
    tokenInput.value = state.uiAuthEnabled ? "" : state.bearerToken || "";
    jsonInput.value = JSON.stringify(state.headers || { "Content-Type": "application/json" }, null, 2);
    openModal("#header-modal");
  });

  saveButton.addEventListener("click", () => {
    let parsed;
    try {
      parsed = JSON.parse(jsonInput.value || "{}");
    } catch {
      window.alert("Headers JSON is invalid.");
      return;
    }

    state.headers = state.uiAuthEnabled ? stripAuthorizationHeaders(parsed) : parsed;

    saveHeaders(state.headers);
    if (!state.uiAuthEnabled) {
      state.bearerToken = tokenInput.value.trim();
      saveBearerToken(state.bearerToken);
    }
    closeModal("#header-modal");
  });
}
