# Design: Office365 SSO Plugin

**Date:** 2026-05-19  
**Status:** Approved

---

## Scope

New standalone auth plugin `plugins/office365-oauth/` implementing the `auth_provider` capability for Microsoft single-tenant OAuth2/OIDC. Mirrors the `github-oauth` plugin pattern exactly. No changes to core.

Optional group restriction: if `OFFICE365_ALLOWED_GROUP_ID` is set, only members of that Azure AD group may log in.

---

## Architecture

Same standalone OAuth dance as `github-oauth`:

```
Browser          Core                     Plugin              Microsoft
  │                │                        │                    │
  │ GET /api/auth/github                     │                    │
  │───────────────►│                        │                    │
  │ 302 → /login   │                        │                    │
  │◄───────────────│                        │                    │
  │ GET /login?nonce=<jwt>                  │                    │
  │───────────────────────────────────────►│                    │
  │ 302 → Microsoft OAuth                   │                    │
  │◄───────────────────────────────────────│                    │
  │                                         Microsoft OAuth dance │
  │◄────────────────────────────────────────────────────────────│
  │ GET /callback?code=…                   │                    │
  │───────────────────────────────────────►│                    │
  │                │  POST /api/auth/session│                    │
  │                │◄───────────────────────│                    │
  │                │  Set-Cookie: auth_token│                    │
  │                │───────────────────────►│                    │
  │ 302 → /        │                        │                    │
  │◄───────────────────────────────────────│                    │
```

---

## OAuth Endpoints (Single-Tenant)

| Purpose | URL |
|---------|-----|
| Authorization | `https://login.microsoftonline.com/{AZURE_TENANT_ID}/oauth2/v2.0/authorize` |
| Token exchange | `https://login.microsoftonline.com/{AZURE_TENANT_ID}/oauth2/v2.0/token` |
| User profile | `https://graph.microsoft.com/v1.0/me` |
| Group membership | `https://graph.microsoft.com/v1.0/me/memberOf` |

**Default scopes:** `openid profile email User.Read`  
**With group restriction:** add `GroupMember.Read.All`

---

## User Profile Mapping

Microsoft Graph `/me` → `POST /api/auth/session` body:

| Graph field | Session field | Notes |
|-------------|--------------|-------|
| `id` | `github_id` | UUID string, stable across renames |
| `userPrincipalName` | `login` | e.g. `alex@contoso.com` |
| `displayName` | `display_name` | Full name |
| *(absent)* | `avatar_url` | Empty string — Graph returns binary JPEG, not a URL |

---

## Group Restriction

When `OFFICE365_ALLOWED_GROUP_ID` is set:

1. After token exchange, call `GET /v1.0/me/memberOf` with the access token.
2. Scan the response for a group whose `id` matches `OFFICE365_ALLOWED_GROUP_ID`.
3. If not found: respond with HTTP 403 and a human-readable error page, do **not** call `POST /api/auth/session`.
4. If found: proceed normally.

`/me/memberOf` returns direct group memberships only (not transitive). Transitive membership check (`/me/transitiveMemberOf`) is out of scope.

---

## Files

```
plugins/office365-oauth/
├── plugin.json   — capability descriptor, port 19002, command ./office365-oauth
├── go.mod        — standalone module (github.com/lx-wnk/agent-dashboard-plugin-office365-oauth)
└── main.go       — HTTP server implementing the OAuth dance
```

**Routes:**

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check — `{"ok":true}` |
| `GET` | `/login?nonce=<jwt>` | Start OAuth dance; core forwards here with nonce |
| `GET` | `/callback?code=&state=` | Exchange code, get user, check group, create session, redirect to `/` |

No legacy capability routes — this plugin is new and implements the standalone flow only.

---

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `AZURE_CLIENT_ID` | yes | OAuth app (application) client ID from Azure portal |
| `AZURE_CLIENT_SECRET` | yes | OAuth app client secret |
| `AZURE_TENANT_ID` | yes | Azure AD tenant ID (Directory ID) |
| `DASHBOARD_URL` | yes | Dashboard base URL, e.g. `http://127.0.0.1:13120` |
| `DASHBOARD_AUTH_PLUGIN_SECRET` | yes | Shared secret for `POST /api/auth/session` (≥32 chars) |
| `OFFICE365_ALLOWED_GROUP_ID` | no | Azure AD group object ID; if set, non-members get 403 |

`plugin.json` `env` array declares all six names so the dashboard forwards them to the process.

---

## `plugin.json`

```json
{
  "id": "office365-oauth",
  "version": "1.0.0",
  "capabilities": ["auth_provider"],
  "addr": "127.0.0.1:19002",
  "command": ["./office365-oauth"],
  "env": [
    "AZURE_CLIENT_ID",
    "AZURE_CLIENT_SECRET",
    "AZURE_TENANT_ID",
    "DASHBOARD_URL",
    "DASHBOARD_AUTH_PLUGIN_SECRET",
    "OFFICE365_ALLOWED_GROUP_ID"
  ]
}
```

---

## Security

- Plugin binds to `127.0.0.1:19002` only.
- CSRF: nonce embedded in OAuth `state` parameter, verified on callback.
- Group check runs before `POST /api/auth/session` — unauthorized users never get a session.
- `DASHBOARD_AUTH_PLUGIN_SECRET` validated with constant-time compare on the core side.
- Client secret never logged.

---

## Out of Scope

- Transitive group membership (`/me/transitiveMemberOf`)
- Multi-tenant support
- Role-based access (Azure AD app roles)
- Token refresh / session extension
- Avatar image fetch from Graph
