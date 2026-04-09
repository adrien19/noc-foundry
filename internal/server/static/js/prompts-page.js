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

import { fetchPrompts } from "./api.js";
import { $, clear, el } from "./dom.js";
import { bootstrapUIAuth, isAuthRequiredError, redirectToLogin, renderAuthControls } from "./oidc.js";
import { initSidebar } from "./sidebar.js";

const state = {
    prompts: [],
    filteredPrompts: [],
    selectedPrompt: null
};

function renderPromptList() {
    const container = $("#prompt-list");
    if (!container) return;
    clear(container);

    if (!state.filteredPrompts.length) {
        container.appendChild(el("div", "panel", "No prompts found."));
        return;
    }

    state.filteredPrompts.forEach((prompt) => {
        const item = el("button", "list-item");
        item.type = "button";
        if (state.selectedPrompt?.name === prompt.name) {
            item.classList.add("list-item--active");
        }
        item.append(
            el("div", "list-item__title", prompt.name),
            el("div", "list-item__meta", `${prompt.argumentCount || 0} argument(s)`)
        );
        item.addEventListener("click", () => {
            state.selectedPrompt = prompt;
            renderSelectedPrompt();
            renderPromptList();
        });
        container.appendChild(item);
    });
}

function renderSelectedPrompt() {
    const prompt = state.selectedPrompt;
    if (!prompt) return;

    $("#prompt-title").textContent = prompt.name;
    $("#prompt-subtitle").textContent = prompt.description || "No description available.";
    $("#prompt-name").textContent = prompt.name;
    $("#prompt-description").textContent = prompt.description || "No description available.";

    const argsContainer = $("#prompt-arguments");
    clear(argsContainer);

    const args = Array.isArray(prompt.arguments) ? prompt.arguments : [];
    if (!args.length) {
        argsContainer.appendChild(el("div", "muted", "This prompt has no arguments."));
        return;
    }

    args.forEach((arg) => {
        const item = el("div", "panel panel--subtle");
        const nameEl = el("div", "list-item__title", `${arg.name}${arg.required ? "" : " (optional)"}`);
        const descEl = el("div", "list-item__meta", arg.description || "No description.");
        item.append(nameEl, descEl);
        argsContainer.appendChild(item);
    });
}

function applyFilter(query) {
    const value = (query || "").trim().toLowerCase();
    if (!value) {
        state.filteredPrompts = [...state.prompts];
    } else {
        state.filteredPrompts = state.prompts.filter((prompt) => {
            const name = (prompt.name || "").toLowerCase();
            const desc = (prompt.description || "").toLowerCase();
            return name.includes(value) || desc.includes(value);
        });
    }
    renderPromptList();
}

async function init() {
    try {
        await bootstrapUIAuth();
        initSidebar();

        const searchInput = $("#prompt-search");
        searchInput?.addEventListener("input", (event) => {
            applyFilter(event.target.value);
        });

        state.prompts = await fetchPrompts();
        state.filteredPrompts = [...state.prompts];

        const url = new URL(window.location.href);
        const requestedPrompt = url.searchParams.get("prompt");
        if (requestedPrompt) {
            const byName = state.filteredPrompts.find((p) => p.name === requestedPrompt);
            if (byName) state.selectedPrompt = byName;
        }

        renderPromptList();

        if (!state.selectedPrompt && state.filteredPrompts.length > 0) {
            state.selectedPrompt = state.filteredPrompts[0];
        }
        if (state.selectedPrompt) {
            renderSelectedPrompt();
            renderPromptList();
        }
    } catch (error) {
        if (isAuthRequiredError(error) && state.uiAuthEnabled) {
            await redirectToLogin(window.location.href);
            return;
        }
        renderAuthControls(error.message);
        const container = $("#prompt-list");
        clear(container);
        container.appendChild(el("div", "panel", `Failed to load prompts: ${error.message}`));
    }
}

init();
