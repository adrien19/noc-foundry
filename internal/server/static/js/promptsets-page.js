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

import { fetchPromptset, fetchPromptsets } from "./api.js";
import { $, clear, el } from "./dom.js";
import { bootstrapUIAuth, isAuthRequiredError, redirectToLogin, renderAuthControls } from "./oidc.js";
import { initSidebar } from "./sidebar.js";

const state = {
    promptsets: [],
    filteredPromptsets: [],
    selectedPromptsetName: null
};

function renderPromptsetList() {
    const container = $("#promptset-list");
    if (!container) return;
    clear(container);

    if (!state.filteredPromptsets.length) {
        container.appendChild(el("div", "panel", "No promptsets found."));
        return;
    }

    state.filteredPromptsets.forEach((promptset) => {
        const item = el("button", "list-item");
        item.type = "button";
        if (state.selectedPromptsetName === promptset.name) {
            item.classList.add("list-item--active");
        }
        item.append(
            el("div", "list-item__title", promptset.displayName || promptset.name || "Unnamed promptset"),
            el("div", "list-item__meta", `${promptset.promptCount || 0} prompt(s)`)
        );
        item.addEventListener("click", () => {
            selectPromptset(promptset);
        });
        container.appendChild(item);
    });
}

function renderPromptset(promptset) {
    const displayName = promptset?.name || "Promptset not found";
    $("#promptset-title").textContent = displayName;
    $("#promptset-subtitle").textContent = `Server version: ${promptset?.serverVersion || "unknown"}`;
    $("#promptset-name").textContent = displayName;

    const promptsContainer = $("#promptset-prompts");
    clear(promptsContainer);

    if (!promptset?.prompts?.length) {
        promptsContainer.appendChild(el("div", "muted", "No prompts found in this promptset."));
        return;
    }

    promptset.prompts.forEach((prompt) => {
        const item = el("a", "list-item");
        item.href = `/ui/prompts?prompt=${encodeURIComponent(prompt.name || "")}`;
        item.append(
            el("div", "list-item__title", prompt.name || "Unnamed prompt"),
            el("div", "list-item__meta", prompt.description || "Prompt exposed by this promptset.")
        );
        promptsContainer.appendChild(item);
    });
}

function applyFilter(query) {
    const value = (query || "").trim().toLowerCase();
    if (!value) {
        state.filteredPromptsets = [...state.promptsets];
    } else {
        state.filteredPromptsets = state.promptsets.filter((promptset) => {
            const name = (promptset.name || "").toLowerCase();
            const displayName = (promptset.displayName || "").toLowerCase();
            return name.includes(value) || displayName.includes(value);
        });
    }
    renderPromptsetList();
}

async function selectPromptset(promptset) {
    try {
        const details = await fetchPromptset(promptset.name);
        state.selectedPromptsetName = promptset.name;
        renderPromptset(details);
        renderPromptsetList();
    } catch (error) {
        const container = $("#promptset-list");
        clear(container);
        container.appendChild(el("div", "panel", `Failed to load promptset: ${error.message}`));
    }
}

async function init() {
    try {
        await bootstrapUIAuth();
        initSidebar();

        state.promptsets = (await fetchPromptsets()).filter((promptset) => (promptset?.name || "") !== "");
        state.filteredPromptsets = [...state.promptsets];
        renderPromptsetList();

        const url = new URL(window.location.href);
        const requestedPromptset = url.searchParams.get("promptset");
        let initialSelection = state.filteredPromptsets[0];
        if (requestedPromptset) {
            const byName = state.filteredPromptsets.find((ps) => ps.name === requestedPromptset);
            if (byName) initialSelection = byName;
        }

        if (initialSelection) {
            await selectPromptset(initialSelection);
        } else {
            $("#promptset-title").textContent = "No promptsets configured";
            $("#promptset-name").textContent = "—";
            clear($("#promptset-prompts"));
            $("#promptset-prompts").appendChild(el("div", "muted", "No prompts found in this promptset."));
            $("#promptset-subtitle").textContent = "No explicit promptsets are defined in the current configuration.";
        }
    } catch (error) {
        if (isAuthRequiredError(error) && state.uiAuthEnabled) {
            await redirectToLogin(window.location.href);
            return;
        }
        renderAuthControls(error.message);
        const container = $("#promptset-list");
        clear(container);
        container.appendChild(el("div", "panel", `Failed to load promptsets: ${error.message}`));
    }
}

$("#promptset-search-button")?.addEventListener("click", () => {
    applyFilter($("#promptset-search")?.value || "");
});
$("#promptset-search")?.addEventListener("keydown", (event) => {
    if (event.key === "Enter") {
        applyFilter(event.target.value || "");
    }
});
$("#promptset-search")?.addEventListener("input", (event) => {
    applyFilter(event.target.value || "");
});

init();
