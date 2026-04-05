// Copyright 2025 Google LLC
// Modifications Copyright 2026 Adrien Ndikumana
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

const HEADERS_KEY = "noc-foundry.headers";
const TOKEN_KEY = "noc-foundry.token";
const ID_TOKEN_KEY = "noc-foundry.id-token";
const AUTH_CONFIG_KEY = "noc-foundry.ui-auth-config";
const PKCE_TRANSACTION_KEY = "noc-foundry.pkce-transaction";
const SIGNED_OUT_KEY = "noc-foundry.signed-out";

const fallbackStorage = new Map();

function storage() {
  if (typeof sessionStorage !== "undefined") {
    return sessionStorage;
  }

  return {
    getItem(key) {
      return fallbackStorage.has(key) ? fallbackStorage.get(key) : null;
    },
    setItem(key, value) {
      fallbackStorage.set(key, String(value));
    },
    removeItem(key) {
      fallbackStorage.delete(key);
    }
  };
}

function loadJSON(key, fallback) {
  try {
    const raw = storage().getItem(key);
    return raw ? JSON.parse(raw) : fallback;
  } catch {
    return fallback;
  }
}

function saveJSON(key, value) {
  storage().setItem(key, JSON.stringify(value));
}

export function loadSavedHeaders() {
  return loadJSON(HEADERS_KEY, { "Content-Type": "application/json" });
}

export function saveHeaders(headers) {
  saveJSON(HEADERS_KEY, headers || { "Content-Type": "application/json" });
}

export function loadSavedBearerToken() {
  return storage().getItem(TOKEN_KEY) || "";
}

export function saveBearerToken(token) {
  storage().setItem(TOKEN_KEY, token || "");
}

export function clearBearerToken() {
  storage().removeItem(TOKEN_KEY);
}

export function loadSavedIDToken() {
  return storage().getItem(ID_TOKEN_KEY) || "";
}

export function saveIDToken(token) {
  storage().setItem(ID_TOKEN_KEY, token || "");
}

export function clearIDToken() {
  storage().removeItem(ID_TOKEN_KEY);
}

export function loadSavedAuthConfig() {
  return loadJSON(AUTH_CONFIG_KEY, null);
}

export function saveAuthConfig(config) {
  saveJSON(AUTH_CONFIG_KEY, config || null);
}

export function clearSavedAuthConfig() {
  storage().removeItem(AUTH_CONFIG_KEY);
}

export function loadPKCETransaction() {
  return loadJSON(PKCE_TRANSACTION_KEY, null);
}

export function savePKCETransaction(txn) {
  saveJSON(PKCE_TRANSACTION_KEY, txn || null);
}

export function clearPKCETransaction() {
  storage().removeItem(PKCE_TRANSACTION_KEY);
}

export function loadSignedOutMarker() {
  return storage().getItem(SIGNED_OUT_KEY) === "1";
}

export function saveSignedOutMarker() {
  storage().setItem(SIGNED_OUT_KEY, "1");
}

export function clearSignedOutMarker() {
  storage().removeItem(SIGNED_OUT_KEY);
}

export function clearAuthSession() {
  clearBearerToken();
  clearIDToken();
  clearSavedAuthConfig();
  clearPKCETransaction();
}
