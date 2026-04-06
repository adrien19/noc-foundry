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
import { bootstrapUIAuth, renderAuthControls } from "./oidc.js";
import { initSidebar } from "./sidebar.js";

async function init() {
  initSidebar();
  try {
    await bootstrapUIAuth();
  } catch (error) {
    const banner = $("#auth-banner");
    if (banner) {
      banner.textContent = error?.message || "Unable to initialize UI authentication.";
      banner.classList.remove("hidden");
    }
    renderAuthControls(error?.message || "Unable to initialize UI authentication.");
  }
}

init();
