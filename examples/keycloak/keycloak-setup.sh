#!/usr/bin/env bash
# keycloak-setup.sh — idempotent bootstrap of the local network-ops Keycloak realm.
# Run after `docker compose -f docker/docker-compose.keycloak.yaml up -d`.
set -euo pipefail

KC_URL="${KC_URL:-http://localhost:8180}"
REALM="network-ops"
CLIENT_ID="noc-foundry"
UI_CLIENT_ID="${KEYCLOAK_UI_CLIENT_ID:-noc-foundry-ui}"
NOCFOUNDRY_BASE_URL="${NOCFOUNDRY_BASE_URL:-http://127.0.0.1:5000}"
API_AUDIENCE="${NOCFOUNDRY_BASE_URL}/api"
TEST_USER="noc-operator"
TEST_PASSWORD="${TEST_PASSWORD:-changeme}"

# ── helpers ──────────────────────────────────────────────────────────────────
kc_token() {
  curl -sf -X POST "${KC_URL}/realms/master/protocol/openid-connect/token" \
    -d "grant_type=password" \
    -d "client_id=admin-cli" \
    -d "username=admin" \
    -d "password=admin" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4
}

kc() {
  local method="$1" path="$2"; shift 2
  curl -sf -X "${method}" "${KC_URL}/admin/realms${path}" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Content-Type: application/json" \
    "$@"
}

# ── wait for Keycloak ─────────────────────────────────────────────────────────
echo "Waiting for Keycloak at ${KC_URL} ..."
until curl -sf "${KC_URL}/realms/master" > /dev/null 2>&1; do sleep 2; done
echo "Keycloak is up."

TOKEN="$(kc_token)"

# ── realm ─────────────────────────────────────────────────────────────────────
if ! kc GET "/${REALM}" > /dev/null 2>&1; then
  echo "Creating realm '${REALM}' ..."
  kc POST "" -d "{\"realm\":\"${REALM}\",\"enabled\":true}"
else
  echo "Realm '${REALM}' already exists."
fi

# ── client ───────────────────────────────────────────────────────────────────
CLIENT_JSON=$(kc GET "/${REALM}/clients?clientId=${CLIENT_ID}" || echo "[]")
CLIENT_COUNT=$(echo "${CLIENT_JSON}" | grep -c '"id"' || true)
if [[ "${CLIENT_COUNT}" -eq 0 ]]; then
  echo "Creating client '${CLIENT_ID}' ..."
  kc POST "/${REALM}/clients" -d "$(cat <<EOF
{
  "clientId": "${CLIENT_ID}",
  "enabled": true,
  "publicClient": true,
  "directAccessGrantsEnabled": true,
  "standardFlowEnabled": true,
  "redirectUris": [
    "http://localhost:*",
    "http://127.0.0.1:*"
  ],
  "webOrigins": ["+"]
}
EOF
)"
else
  echo "Client '${CLIENT_ID}' already exists — ensuring standard flow and redirect URIs are configured ..."
  CLIENT_UUID=$(echo "${CLIENT_JSON}" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
  kc PUT "/${REALM}/clients/${CLIENT_UUID}" -d "$(cat <<EOF
{
  "clientId": "${CLIENT_ID}",
  "enabled": true,
  "publicClient": true,
  "directAccessGrantsEnabled": true,
  "standardFlowEnabled": true,
  "redirectUris": [
    "http://localhost:*",
    "http://127.0.0.1:*"
  ],
  "webOrigins": ["+"]
}
EOF
)"
fi

# ── test user ────────────────────────────────────────────────────────────────
USER_JSON=$(kc GET "/${REALM}/users?username=${TEST_USER}" || echo "[]")
USER_COUNT=$(echo "${USER_JSON}" | grep -c '"id"' || true)
if [[ "${USER_COUNT}" -eq 0 ]]; then
  echo "Creating user '${TEST_USER}' ..."
  # Note: 'credentials' in the creation body is ignored by Keycloak's REST API
  # (returns 201 + Location header with no body).  Password is always set via
  # reset-password below. email/firstName/lastName + emailVerified=true are
  # required to avoid the "Account is not fully set up" grant error.
  curl -sf -X POST "${KC_URL}/admin/realms/${REALM}/users" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{
      \"username\":\"${TEST_USER}\",
      \"enabled\":true,
      \"emailVerified\":true,
      \"email\":\"${TEST_USER}@lab.local\",
      \"firstName\":\"NOC\",
      \"lastName\":\"Operator\",
      \"requiredActions\":[]
    }"
  echo "User '${TEST_USER}' created."
  # Re-fetch to get the generated id
  USER_JSON=$(kc GET "/${REALM}/users?username=${TEST_USER}" || echo "[]")
fi

USER_ID=$(echo "${USER_JSON}" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
if [[ -z "${USER_ID}" ]]; then
  echo "ERROR: could not determine user id for '${TEST_USER}'" >&2
  exit 1
fi

echo "Ensuring user id=${USER_ID} is fully set up ..."
kc PUT "/${REALM}/users/${USER_ID}" -d '{"emailVerified":true,"requiredActions":[]}'

echo "Setting password for '${TEST_USER}' ..."
kc PUT "/${REALM}/users/${USER_ID}/reset-password" \
  -d "{\"type\":\"password\",\"value\":\"${TEST_PASSWORD}\",\"temporary\":false}"
echo "Password set."

# ── UI client for browser PKCE login ─────────────────────────────────────────
UI_CLIENT_JSON=$(kc GET "/${REALM}/clients?clientId=${UI_CLIENT_ID}" || echo "[]")
UI_CLIENT_COUNT=$(echo "${UI_CLIENT_JSON}" | grep -c '"id"' || true)
if [[ "${UI_CLIENT_COUNT}" -eq 0 ]]; then
  echo "Creating UI client '${UI_CLIENT_ID}' ..."
  kc POST "/${REALM}/clients" -d "$(cat <<EOF
{
  "clientId": "${UI_CLIENT_ID}",
  "enabled": true,
  "publicClient": true,
  "directAccessGrantsEnabled": false,
  "standardFlowEnabled": true,
  "redirectUris": [
    "http://localhost:5000/ui/",
    "http://127.0.0.1:5000/ui/",
    "http://localhost:5000/ui/auth/callback",
    "http://127.0.0.1:5000/ui/auth/callback",
    "http://localhost:*/ui/",
    "http://127.0.0.1:*/ui/",
    "http://localhost:*/ui/auth/callback",
    "http://127.0.0.1:*/ui/auth/callback"
  ],
  "webOrigins": ["+"],
  "attributes": {
    "pkce.code.challenge.method": "S256",
    "post.logout.redirect.uris": "+"
  }
}
EOF
)"
else
  echo "UI client '${UI_CLIENT_ID}' already exists — ensuring PKCE redirect URIs are configured ..."
  UI_CLIENT_UUID=$(echo "${UI_CLIENT_JSON}" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
  kc PUT "/${REALM}/clients/${UI_CLIENT_UUID}" -d "$(cat <<EOF
{
  "clientId": "${UI_CLIENT_ID}",
  "enabled": true,
  "publicClient": true,
  "directAccessGrantsEnabled": false,
  "standardFlowEnabled": true,
  "redirectUris": [
    "http://localhost:5000/ui/",
    "http://127.0.0.1:5000/ui/",
    "http://localhost:5000/ui/auth/callback",
    "http://127.0.0.1:5000/ui/auth/callback",
    "http://localhost:*/ui/",
    "http://127.0.0.1:*/ui/",
    "http://localhost:*/ui/auth/callback",
    "http://127.0.0.1:*/ui/auth/callback"
  ],
  "webOrigins": ["+"],
  "attributes": {
    "pkce.code.challenge.method": "S256",
    "post.logout.redirect.uris": "+"
  }
}
EOF
)"
fi

UI_CLIENT_JSON=$(kc GET "/${REALM}/clients?clientId=${UI_CLIENT_ID}" || echo "[]")
UI_CLIENT_UUID=$(echo "${UI_CLIENT_JSON}" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
if [[ -z "${UI_CLIENT_UUID}" ]]; then
  echo "ERROR: could not determine client id for '${UI_CLIENT_ID}'" >&2
  exit 1
fi

echo "Ensuring UI client '${UI_CLIENT_ID}' adds API audience '${API_AUDIENCE}' ..."
UI_MAPPERS_JSON=$(kc GET "/${REALM}/clients/${UI_CLIENT_UUID}/protocol-mappers/models" || echo "[]")
AUDIENCE_MAPPER_ID=$(echo "${UI_MAPPERS_JSON}" | python3 -c "
import sys, json
for mapper in json.load(sys.stdin):
    if mapper.get('name') == 'noc-foundry-api-audience':
        print(mapper['id']); break
" 2>/dev/null || true)

AUDIENCE_MAPPER_PAYLOAD="$(cat <<EOF
{
  "name": "noc-foundry-api-audience",
  "protocol": "openid-connect",
  "protocolMapper": "oidc-audience-mapper",
  "consentRequired": false,
  "config": {
    "included.custom.audience": "${API_AUDIENCE}",
    "id.token.claim": "false",
    "access.token.claim": "true",
    "userinfo.token.claim": "false"
  }
}
EOF
)"

if [[ -n "${AUDIENCE_MAPPER_ID}" ]]; then
  curl -sf -X PUT "${KC_URL}/admin/realms/${REALM}/clients/${UI_CLIENT_UUID}/protocol-mappers/models/${AUDIENCE_MAPPER_ID}" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Content-Type: application/json" \
    -d "${AUDIENCE_MAPPER_PAYLOAD}" > /dev/null
else
  curl -sf -X POST "${KC_URL}/admin/realms/${REALM}/clients/${UI_CLIENT_UUID}/protocol-mappers/models" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Content-Type: application/json" \
    -d "${AUDIENCE_MAPPER_PAYLOAD}" > /dev/null
fi

# ── Anonymous client registration (DCR / RFC 7591) ────────────────────────────
# VS Code MCP uses Dynamic Client Registration to auto-register its OAuth client.
# Keycloak's default "Trusted Hosts" anonymous policy blocks all DCR.
# We relax it: disable sender-host check, keep redirect-URI validation,
# and allow localhost + vscode.dev as trusted redirect origins.
echo "Configuring anonymous DCR (Trusted Hosts policy) ..."
POLICIES_JSON=$(kc GET "/${REALM}/components?type=org.keycloak.services.clientregistration.policy.ClientRegistrationPolicy" || echo "[]")
TRUSTED_ID=$(echo "${POLICIES_JSON}" | python3 -c "
import sys, json
for c in json.load(sys.stdin):
    if c.get('subType') == 'anonymous' and 'Trusted' in c.get('name',''):
        print(c['id']); break
" 2>/dev/null || true)

if [[ -n "${TRUSTED_ID}" ]]; then
  kc PUT "/${REALM}/components/${TRUSTED_ID}" -d "$(cat <<EOF
{
  "id": "${TRUSTED_ID}",
  "name": "Trusted Hosts",
  "providerId": "trusted-hosts",
  "providerType": "org.keycloak.services.clientregistration.policy.ClientRegistrationPolicy",
  "parentId": "${REALM}",
  "subType": "anonymous",
  "config": {
    "host-sending-registration-request-must-match": ["false"],
    "client-uris-must-match": ["true"],
    "trusted-hosts": ["localhost", "127.0.0.1", "vscode.dev"]
  }
}
EOF
)"
  echo "Trusted Hosts policy updated — anonymous DCR enabled."
else
  echo "WARNING: could not find Trusted Hosts policy component; skipping DCR config."
fi

echo ""
echo "Setup complete. To obtain a test token:"
printf "  curl -s -X POST %s/realms/%s/protocol/openid-connect/token" "${KC_URL}" "${REALM}"
printf " -d 'grant_type=password&client_id=%s&username=%s&password=%s'\n" \
       "${CLIENT_ID}" "${TEST_USER}" "${TEST_PASSWORD}"
echo ""
echo "NOCFoundry authServices config:"
cat <<EOF
---
kind: authServices
name: noc-keycloak
type: oidc
issuerUrl: ${KC_URL}/realms/${REALM}
clientId: ${CLIENT_ID}
EOF
echo ""
echo "NOCFoundry server UI auth config:"
cat <<EOF
auth:
  ui:
    enabled: true
    authService: noc-keycloak
    clientId: ${UI_CLIENT_ID}
    scopes: ["openid", "profile", "email"]
    redirectPath: /ui/auth/callback
EOF
