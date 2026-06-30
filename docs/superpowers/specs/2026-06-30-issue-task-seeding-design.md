# Issue → Task Seeding (GitHub + Jira) — Design Spec

> Date: 2026-06-30 · Status: Approved · Branch: `feat/issue-task-seeding` (off `upcoming`)
> Competitor-gap B8 (from Conductor/Orca). Create a pipeline task pre-filled from a GitHub or Jira issue.

## Why

Today a task is created by hand (title/description typed into the New-Task form). Work often originates as a tracker issue. B8 lets the user paste an issue reference, fetch it, and pre-fill the task form — removing manual copy/paste and keeping a traceable link back to the source issue.

## Decisions (user-approved)

| # | Decision | Rationale |
|---|---|---|
| D1 | **Core** (a built-in `tracker` package), not a plugin | Simpler for a single-user local dashboard; no plugin install/auth surface needed. Tracker clients sit behind a `Tracker` interface so extraction later stays possible. |
| D2 | Support **GitHub Issues + Jira** | The two trackers the user actually uses. |
| D3 | **Fetch-only** endpoint `POST /api/tracker/fetch` returns a preview Issue; the existing create flow (`CreateTaskFromInput` + the New-Task form) does the actual creation | DRY — no parallel task-creation path. The tracker layer only fetches + maps. |
| D4 | Tokens stored **encrypted at rest** (reuse the existing AES-GCM secretbox used for plugin settings), surfaced **masked** in a settings panel | Tracker tokens are secrets; never store plaintext, never echo back. |

## Scope

In: `server/internal/tracker/` (`Tracker` interface + github + jira impls + ref-shape dispatch); `POST /api/tracker/fetch {ref}` → mapped `Issue`; settings keys for the tokens (encrypted) + a settings panel to manage them; a "Import from issue" affordance in the New-Task form that pre-fills title/description/source-link; issue→task field mapping into the existing create params.

Out: creating the task server-side from the issue (the frontend uses the existing create endpoint after preview); bidirectional sync / status write-back to the tracker; browsing/listing issues (paste a single ref); Linear (not selected); webhook-driven auto-seeding; new ent tables.

## Architecture

### Tracker package (`server/internal/tracker/`)
- `type Issue struct { Tracker, Key, Title, Body, URL string; Labels []string }`.
- `type Tracker interface { FetchIssue(ctx context.Context, ref string) (Issue, error) }`.
- `github.go`: parses a ref (a full `https://github.com/{owner}/{repo}/issues/{n}` URL, or `{owner}/{repo}#{n}`, or bare `#{n}`/`{n}` when a default repo is configured) → `GET https://api.github.com/repos/{owner}/{repo}/issues/{n}` with `Authorization: Bearer <token>` + `Accept: application/vnd.github+json`. Maps `title`→Title, `body`→Body, `html_url`→URL, `number`→Key (`#n`), labels[].name→Labels.
- `jira.go`: parses a ref (a `KEY-123` issue key, or a full Jira browse URL) → `GET {baseURL}/rest/api/3/issue/{KEY}?fields=summary,description,labels` with Basic auth (`email:token` base64). Maps `fields.summary`→Title, `fields.description`→Body (Atlassian Document Format → render to plain/markdown text; if ADF parsing is heavy, store the raw text rendering — keep it simple, a best-effort flattening), `KEY`→Key, the browse URL→URL, `fields.labels`→Labels.
- `Resolve(ref, settings) (Tracker, error)`: pick github vs jira by ref shape (a Jira `KEY-123` pattern vs a GitHub URL/`owner/repo#n`); return a configured client or an error if the matching tracker's token/config is missing.
- Each client takes its config (token, and for jira baseURL+email) injected — no global state. HTTP via the stdlib client with a timeout; non-2xx → a typed error (`ErrTrackerAuth` 401/403, `ErrIssueNotFound` 404, `ErrTrackerUpstream` other).

### Token storage (settings)
- New settings keys: `tracker.github.token`, `tracker.jira.baseUrl`, `tracker.jira.email`, `tracker.jira.token`. The two `*.token` values are **secret** — stored encrypted via the existing secretbox master key (the same at-rest mechanism plugin secret settings use), masked when read by the settings API. baseUrl/email are plain.
- Reuse the existing settings registry + the secret-handling pattern already used for plugin/provider secret settings (find the exact seam; if the settings registry lacks per-key secret encryption, store the two tokens via the `pluginsettings`/secretbox path or a small `tracker_setting` encrypted store — prefer reusing an existing encrypted-settings mechanism over a new table).

### API
- `POST /api/tracker/fetch {ref}` (JWT group, Origin-checked) → resolves the tracker, fetches, returns `Issue` JSON (or the typed error → 400 bad ref / 401-403 auth / 404 not found / 502 upstream). Read-only; no mutation.

### Frontend
- `useTrackerImport`: `fetchIssue(ref)` → `POST /api/tracker/fetch`; surfaces errors via `useToast`.
- New-Task form (`BacklogForm.vue` / the create dialog): an "Import from issue" input — paste a ref, click Fetch → pre-fills Title (issue title), Description (issue body + a `Source: <url>` line or the URL into task metadata), leaving project + other fields for the user. The user then creates via the normal flow (existing create endpoint).
- A tracker-tokens settings panel (sibling of the API-keys/provider settings): inputs for the GitHub token, Jira baseUrl/email/token; secrets masked, unchanged-secret preserved on save (reuse the existing masked-secret pattern).

### Field mapping (issue → CreateTaskParams)
- `title` ← Issue.Title
- `description` ← Issue.Body (and a source link `Source: {url}` appended, or the url stored in task metadata for traceability)
- task metadata source link: `{tracker, key, url}` (the task's existing metadata JSON — no schema change)
- project, cwd, priority, branches, budgets: user-chosen in the form (defaults as today).

## Data flow
```
user pastes ref → POST /api/tracker/fetch {ref}
  → Resolve(ref, settings) → github|jira client (decrypted token)
  → FetchIssue → Issue{title,body,url,key,labels}
  → frontend pre-fills New-Task form (title, body+source link)
  → user picks project + confirms → existing create endpoint → CreateTaskFromInput
settings panel → tracker tokens (encrypted at rest, masked in API)
```

## Error handling
- Unrecognized ref shape → 400 ("not a recognized GitHub or Jira issue reference").
- Matching tracker not configured / token missing → 400/401 ("configure the <tracker> token in Settings").
- Upstream 401/403 → 401/403 ("<tracker> rejected the token").
- Issue not found → 404.
- Upstream timeout / 5xx → 502.
- Jira ADF body that can't be flattened → fall back to an empty/plain description (don't fail the fetch on body rendering).
- Secrets never returned by the API (masked); errors never echo the token.

## Testing
- **github.FetchIssue:** `httptest` server returning a canned GitHub issue JSON → assert mapped Issue; ref parsing for the URL form, `owner/repo#n`, and bare `#n` with a default repo; 404/401 → typed errors.
- **jira.FetchIssue:** `httptest` server with a canned Jira issue (incl. an ADF description) → assert Title/Body(flattened)/Key/URL; KEY-123 + browse-URL ref parsing; auth header is Basic email:token; 404/401 typed errors.
- **Resolve:** github URL → github client; `KEY-123` → jira client; missing config → error.
- **Endpoint:** valid ref → 200 Issue; bad ref → 400; missing token → 400/401; not found → 404. Origin/auth enforced.
- **Frontend:** `useTrackerImport.fetchIssue` posts the ref and returns the issue; a fetch error calls `toast.error`; the form pre-fills title+description from a mocked issue.
- **Settings:** tokens round-trip masked (secret not echoed; unchanged-secret sentinel preserved on save).

## Risks / notes
- Token security: encrypt at rest + mask in API; do not log tokens. Same posture as plugin secret settings.
- GitHub bare `#n` needs a configured default `owner/repo` (a setting) — otherwise require the full URL/`owner/repo#n`. Keep the default-repo optional; full URL always works.
- Jira ADF (Atlassian Document Format) description is structured JSON; a full renderer is out of scope — flatten text content best-effort, never fail the fetch on it.
- This reuses the entire existing create path (form + `CreateTaskFromInput`) — the only new surface is fetch + token settings, keeping the blast radius small.
- Provider/tracker tokens are operator-supplied secrets for the operator's own accounts — same trust boundary as API keys already in the dashboard.
