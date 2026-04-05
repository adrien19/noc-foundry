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

import test from "node:test";
import assert from "node:assert/strict";

import { clearAuthSession, saveAuthConfig, saveIDToken } from "./auth.js";
import { state } from "./state.js";
import { decodeAccessTokenClaims, isTokenExpired, logoutUI } from "./oidc.js";

function base64url(input) {
  return Buffer.from(JSON.stringify(input))
    .toString("base64url");
}

function makeJWT(payload) {
  return `${base64url({ alg: "none", typ: "JWT" })}.${base64url(payload)}.`;
}

test.beforeEach(() => {
  clearAuthSession();
  state.uiAuthEnabled = false;
  state.authStatus = "disabled";
  state.currentUser = null;
  state.authConfig = null;
  globalThis.document = {
    querySelector() {
      return null;
    },
    querySelectorAll() {
      return [];
    }
  };
  globalThis.window = {
    location: {
      origin: "http://127.0.0.1:5000",
      assign() {}
    }
  };
});

test("decodeAccessTokenClaims parses JWT payload", () => {
  const token = makeJWT({
    sub: "noc-operator",
    preferred_username: "noc",
    exp: Math.floor(Date.now() / 1000) + 300
  });

  const claims = decodeAccessTokenClaims(token);
  assert.equal(claims.sub, "noc-operator");
  assert.equal(claims.preferred_username, "noc");
});

test("isTokenExpired returns true for expired token", () => {
  const token = makeJWT({
    sub: "noc-operator",
    exp: Math.floor(Date.now() / 1000) - 60
  });

  assert.equal(isTokenExpired(token), true);
});

test("isTokenExpired returns false for token comfortably in the future", () => {
  const token = makeJWT({
    sub: "noc-operator",
    exp: Math.floor(Date.now() / 1000) + 600
  });

  assert.equal(isTokenExpired(token), false);
});

test("logoutUI redirects to the OIDC end-session endpoint when available", () => {
  let redirectedTo = "";
  globalThis.window = {
    location: {
      origin: "http://127.0.0.1:5000",
      assign(url) {
        redirectedTo = url;
      }
    }
  };

  const authConfig = {
    enabled: true,
    clientId: "noc-foundry-ui",
    endSessionEndpoint: "http://127.0.0.1:8180/realms/network-ops/protocol/openid-connect/logout"
  };
  saveAuthConfig(authConfig);
  saveIDToken("header.payload.signature");
  state.uiAuthEnabled = true;
  state.authStatus = "authenticated";
  state.currentUser = "noc-operator";
  state.authConfig = authConfig;

  logoutUI();

  assert.equal(
    redirectedTo,
    "http://127.0.0.1:8180/realms/network-ops/protocol/openid-connect/logout?client_id=noc-foundry-ui&post_logout_redirect_uri=http%3A%2F%2F127.0.0.1%3A5000%2Fui%2F&id_token_hint=header.payload.signature"
  );
});
