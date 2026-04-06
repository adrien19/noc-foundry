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

const SIDEBAR_COLLAPSE_STORAGE_KEY = "ui.sidebarCollapsed";

function syncSidebarState(shell, toggleButton) {
  if (!shell || !toggleButton) return;

  const isCollapsed = shell.classList.contains("is-sidebar-collapsed");
  toggleButton.textContent = isCollapsed ? ">" : "<";
  toggleButton.setAttribute("aria-expanded", String(!isCollapsed));
  toggleButton.setAttribute("aria-label", isCollapsed ? "Expand sidebar" : "Collapse sidebar");
}

function setSidebarCollapsed(shell, toggleButton, collapsed) {
  if (!shell || !toggleButton) return;

  shell.classList.toggle("is-sidebar-collapsed", collapsed);
  localStorage.setItem(SIDEBAR_COLLAPSE_STORAGE_KEY, collapsed ? "1" : "0");
  syncSidebarState(shell, toggleButton);
}

export function initSidebar() {
  const shell = document.querySelector(".app-shell");
  const toggleButton = document.querySelector("#toggle-sidebar-button");
  if (!shell || !toggleButton) return;

  const shouldCollapse = localStorage.getItem(SIDEBAR_COLLAPSE_STORAGE_KEY) === "1";
  setSidebarCollapsed(shell, toggleButton, shouldCollapse);

  toggleButton.addEventListener("click", () => {
    const isCollapsed = shell.classList.contains("is-sidebar-collapsed");
    setSidebarCollapsed(shell, toggleButton, !isCollapsed);
  });
}
