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

import {
  clearAuthSession,
  clearPKCETransaction,
  loadSavedIDToken,
  clearSignedOutMarker,
  loadPKCETransaction,
  loadSavedAuthConfig,
  loadSavedBearerToken,
  loadSignedOutMarker,
  saveAuthConfig,
  saveBearerToken,
  saveIDToken,
  savePKCETransaction,
  saveSignedOutMarker
} from "./auth.js";
import { $, $$ } from "./dom.js";
import { state } from "./state.js";

const TOKEN_EXPIRY_SKEW_MS = 30_000;

export class AuthRequiredError extends Error {
  constructor(message = "Authentication required", status = 401) {
    super(message);
    this.name = "AuthRequiredError";
    this.status = status;
  }
}

function base64UrlEncode(bytes) {
  const binary = Array.from(bytes, (byte) => String.fromCharCode(byte)).join("");
  const encoded = typeof btoa === "function"
    ? btoa(binary)
    : Buffer.from(binary, "binary").toString("base64");
  return encoded.replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

function decodeBase64Url(value) {
  const normalized = value.replace(/-/g, "+").replace(/_/g, "/");
  const padded = normalized + "=".repeat((4 - (normalized.length % 4 || 4)) % 4);
  if (typeof atob === "function") {
    return atob(padded);
  }
  return Buffer.from(padded, "base64").toString("binary");
}

export function decodeAccessTokenClaims(token) {
  if (!token || typeof token !== "string") return null;
  const parts = token.split(".");
  if (parts.length < 2) return null;
  try {
    return JSON.parse(decodeBase64Url(parts[1]));
  } catch {
    return null;
  }
}

export function isTokenExpired(token, nowMs = Date.now()) {
  const claims = decodeAccessTokenClaims(token);
  if (!claims || typeof claims.exp !== "number") return true;
  return claims.exp * 1000 <= nowMs + TOKEN_EXPIRY_SKEW_MS;
}

function getCurrentUser(claims) {
  if (!claims || typeof claims !== "object") {
    return null;
  }
  return claims.preferred_username || claims.email || claims.name || claims.sub || null;
}

async function sha256(input) {
  const bytes = new TextEncoder().encode(input);
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return new Uint8Array(digest);
}

function randomString(byteLength = 32) {
  const bytes = new Uint8Array(byteLength);
  crypto.getRandomValues(bytes);
  return base64UrlEncode(bytes);
}

async function buildPkceTransaction(returnUrl) {
  const verifier = randomString(64);
  const challenge = base64UrlEncode(await sha256(verifier));
  return {
    verifier,
    challenge,
    state: randomString(32),
    returnUrl
  };
}

async function fetchUIAuthConfig() {
  const res = await fetch("/ui/auth/config", {
    headers: { Accept: "application/json" }
  });
  if (!res.ok) {
    throw new Error(`Failed to load UI auth config (${res.status})`);
  }
  const config = await res.json();
  saveAuthConfig(config);
  return config;
}

export async function getUIAuthConfig() {
  if (state.authConfig) {
    return state.authConfig;
  }
  const cached = loadSavedAuthConfig();
  if (cached) {
    state.authConfig = cached;
    state.uiAuthEnabled = !!cached.enabled;
    return cached;
  }
  const config = await fetchUIAuthConfig();
  state.authConfig = config;
  state.uiAuthEnabled = !!config.enabled;
  return config;
}

function applyAuthState(config, token) {
  state.authConfig = config || null;
  state.uiAuthEnabled = !!config?.enabled;

  if (!token || isTokenExpired(token)) {
    state.authStatus = state.uiAuthEnabled ? (loadSignedOutMarker() ? "signed_out" : "unauthenticated") : "disabled";
    state.currentUser = null;
    return;
  }

  const claims = decodeAccessTokenClaims(token);
  state.authStatus = "authenticated";
  state.currentUser = getCurrentUser(claims);
}

export function renderAuthControls(message = "") {
  const status = $("#auth-session-status");
  const user = $("#auth-session-user");
  const loginButton = $("#auth-login-button");
  const logoutButton = $("#auth-logout-button");
  const banner = $("#auth-banner");

  if (status) {
    if (!state.uiAuthEnabled) {
      status.textContent = "UI login disabled";
    } else if (state.authStatus === "authenticated") {
      status.textContent = "Signed in";
    } else if (state.authStatus === "signed_out") {
      status.textContent = "Signed out";
    } else {
      status.textContent = "Authentication required";
    }
  }

  if (user) {
    user.textContent = state.currentUser || "";
    user.classList.toggle("hidden", !state.currentUser);
  }

  if (loginButton) {
    loginButton.classList.toggle("hidden", !state.uiAuthEnabled || state.authStatus === "authenticated");
    loginButton.title = state.uiAuthEnabled ? "Sign in to the UI session" : "UI login disabled";
  }
  if (logoutButton) {
    logoutButton.classList.toggle("hidden", !state.uiAuthEnabled || state.authStatus !== "authenticated");
    logoutButton.title = state.currentUser ? `Sign out ${state.currentUser}` : "Sign out";
  }
  if (banner) {
    banner.textContent = message || (state.uiAuthEnabled && state.authStatus !== "authenticated" ? "Sign in to access protected API-backed UI features." : "");
    banner.classList.toggle("hidden", !banner.textContent);
  }

  $$("[data-managed-auth-only]").forEach((node) => {
    node.classList.toggle("hidden", !state.uiAuthEnabled);
  });
}

function bindAuthButtons() {
  const loginButton = $("#auth-login-button");
  const logoutButton = $("#auth-logout-button");

  if (loginButton && !loginButton.dataset.bound) {
    loginButton.dataset.bound = "true";
    loginButton.addEventListener("click", async () => {
      await redirectToLogin(window.location.href);
    });
  }

  if (logoutButton && !logoutButton.dataset.bound) {
    logoutButton.dataset.bound = "true";
    logoutButton.addEventListener("click", () => {
      logoutUI();
    });
  }
}

export async function redirectToLogin(returnUrl) {
  const config = await getUIAuthConfig();
  if (!config?.enabled) {
    return;
  }

  clearSignedOutMarker();
  const txn = await buildPkceTransaction(returnUrl || window.location.href);
  savePKCETransaction(txn);

  const authURL = new URL(config.authorizationEndpoint);
  authURL.searchParams.set("response_type", "code");
  authURL.searchParams.set("client_id", config.clientId);
  authURL.searchParams.set("redirect_uri", config.redirectUri);
  authURL.searchParams.set("scope", (config.scopes || []).join(" "));
  authURL.searchParams.set("state", txn.state);
  authURL.searchParams.set("code_challenge", txn.challenge);
  authURL.searchParams.set("code_challenge_method", "S256");

  window.location.assign(authURL.toString());
}

export async function bootstrapUIAuth() {
  const config = await getUIAuthConfig();
  bindAuthButtons();
  const token = loadSavedBearerToken();
  applyAuthState(config, token);
  renderAuthControls();

  if (!config?.enabled) {
    return config;
  }

  const callbackPath = new URL(config.redirectUri).pathname;
  if (window.location.pathname === callbackPath) {
    return config;
  }

  if (state.authStatus === "authenticated") {
    return config;
  }

  if (state.authStatus === "signed_out") {
    return config;
  }

  await redirectToLogin(window.location.href);
  return config;
}

export async function handleAuthCallback() {
  const status = $("#auth-callback-status");
  const errorNode = $("#auth-callback-error");
  const setStatus = (text) => {
    if (status) status.textContent = text;
  };
  const setError = (text) => {
    if (errorNode) {
      errorNode.textContent = text;
      errorNode.classList.toggle("hidden", !text);
    }
  };

  try {
    const config = await fetchUIAuthConfig();
    const url = new URL(window.location.href);
    const oauthError = url.searchParams.get("error");
    if (oauthError) {
      throw new Error(url.searchParams.get("error_description") || oauthError);
    }

    const code = url.searchParams.get("code");
    const returnedState = url.searchParams.get("state");
    const txn = loadPKCETransaction();
    if (!code || !returnedState || !txn || returnedState !== txn.state) {
      throw new Error("The login response could not be validated.");
    }

    setStatus("Exchanging authorization code...");
    const body = new URLSearchParams({
      grant_type: "authorization_code",
      client_id: config.clientId,
      code,
      redirect_uri: config.redirectUri,
      code_verifier: txn.verifier
    });

    const res = await fetch(config.tokenEndpoint, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: body.toString()
    });
    if (!res.ok) {
      const text = await res.text();
      throw new Error(`Token exchange failed (${res.status}): ${text}`);
    }

    const tokenPayload = await res.json();
    if (!tokenPayload?.access_token) {
      throw new Error("The identity provider did not return an access token.");
    }

    saveBearerToken(tokenPayload.access_token);
    saveIDToken(tokenPayload.id_token || "");
    saveAuthConfig(config);
    clearPKCETransaction();
    clearSignedOutMarker();
    setStatus("Login complete. Redirecting...");
    // Validate return URL inline so CodeQL can trace the sanitisation:
    // new URL().pathname breaks the taint from sessionStorage.
    let redirectTo = "/ui/tools";
    try {
      const parsed = new URL(txn.returnUrl, window.location.origin);
      const path = parsed.pathname + parsed.search + parsed.hash;
      if (parsed.origin === window.location.origin
        && (parsed.protocol === "https:" || parsed.protocol === "http:")
        && path.startsWith("/") && !path.startsWith("//")) {
        redirectTo = path;
      }
    } catch { /* malformed URL — use default */ }
    window.location.replace(redirectTo);
  } catch (error) {
    clearAuthSession();
    saveSignedOutMarker();
    setStatus("Sign-in failed.");
    setError(error?.message || "Unable to complete sign-in.");
  }
}

export function logoutUI() {
  const authConfig = state.authConfig || loadSavedAuthConfig();
  const idTokenHint = loadSavedIDToken();
  clearAuthSession();
  saveSignedOutMarker();
  state.authStatus = "signed_out";
  state.currentUser = null;
  renderAuthControls();

  if (authConfig?.enabled && authConfig?.endSessionEndpoint) {
    const logoutURL = new URL(authConfig.endSessionEndpoint);
    logoutURL.searchParams.set("client_id", authConfig.clientId);
    logoutURL.searchParams.set("post_logout_redirect_uri", `${window.location.origin}/ui/`);
    if (idTokenHint) {
      logoutURL.searchParams.set("id_token_hint", idTokenHint);
    }
    window.location.assign(logoutURL.toString());
    return;
  }

  window.location.assign("/ui/");
}

export function isAuthRequiredError(error) {
  return error instanceof AuthRequiredError;
}
