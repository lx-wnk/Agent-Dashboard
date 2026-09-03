# Cockpit Shell and the GitHub Application

**Date:** 2026-09-03
**Status:** Approved design
**Parent:** `2026-08-27-agenticos-overview-design.md`
**Implements:** S1 (OS shell) and the second `reach` application, which is the MLP entry criterion

---

## 1. Purpose

Two things in one slice, because neither is worth much alone:

- A **cockpit** that shows the working day in one screen instead of the agent grid alone.
- A **GitHub application**, the system's second reach application.

The second one is why this is not just a UI change. The overview's MLP entry criterion reads: *two applications have run through the kernel without kernel changes specific to either.* Today it is one — Obsidian. GitHub is the test of whether the registry, the capability gate and the settings-secret path generalise, or whether they were shaped around a single case.

---

## 2. What exists today

Read first-hand.

| Piece | Where | State |
|---|---|---|
| Application pattern | `server/internal/apps/obsidian/app.go` | In-server module, `Register` writes the registry row plus capability rows, idempotent on every boot |
| Capability classes and their defaults | `server/internal/capability/decide.go:233-242` | `tool`, `reach`, `resource` → **ask**; `spend` → **deny**; unknown → deny |
| Encrypted setting | `app_setting.secret`/`nonce`, `settings.Definition{Secret: true}` | Landed with `obsidian.apiKey` in PR #414 |
| Registry read surface | `GET /api/resources?kind=` | `application`, `routine`, `skill`, `memory_space` |
| Routines as real rows | `task_schedule` projected into `kind=routine` | Landed in PR #421 |
| Views | `src/composables/useViewState.ts:5` — `'dashboard' \| 'workflows' \| 'pipeline' \| 'cost' \| 'schedules' \| 'eval'` | `App.vue` is 473 lines and carries the view branching inline |

---

## 3. Decisions

### D1 — GitHub is an in-server module, like Obsidian

Not an MCP client to a foreign server, not a plugin subprocess.

*Rejected:* the dashboard becoming an MCP client to the servers Claude Desktop uses. It would have brought Slack and Jira along for free, but it puts a new client role in the server, adopts foreign auth lifecycles, and — the disqualifying part — asks the gate to rule on tool names we do not control. A capability catalogue whose vocabulary a third party defines cannot be reasoned about.

*Rejected:* the plugin subprocess mechanism. It adds a hop without adding isolation, since plugins already run in the machine's trust domain, and it would need capabilities marshalled across a process boundary. Its real advantage — third-party connectors — is not a goal here.

*Consequence, and it is the point:* if GitHub needs a kernel change that Obsidian did not, the MLP entry criterion is not met and we will have learned exactly what the kernel was missing.

### D2 — Four capabilities, and `merge` is class `spend`

| Capability | Class | Default with no grant | Reversible |
|---|---|---|---|
| `github.read` | `reach` | ask | yes |
| `github.search` | `reach` | ask | yes |
| `github.comment` | `reach` | ask | no |
| `github.merge` | **`spend`** | **deny** | no |

**A correction to the design as first sketched, and it matters.** The earlier sketch claimed `spend` defaults to *ask*. It does not — `defaultEffect` (`decide.go:233-242`) sends `spend` to **deny**. That is the stronger behaviour and the reason to use it: a merge does not happen unless a human deliberately created a grant for it. There is no "hold and prompt" fallback to rely on, and relying on one would have been the weaker design.

`github.comment` carries `Reversible: false` for the same reason `obsidian.write` does: a comment is public the moment it posts, and deleting it afterwards does not unsend it.

### D3 — A fine-grained PAT in encrypted settings

`github.token` as a `Secret: true` definition, alongside `github.repos` (an allow-list) and `github.baseURL` (for GitHub Enterprise, defaulting to `api.github.com`).

*Rejected:* reusing the `gh` CLI's token. Zero setup, and the blast radius would be the whole account across every repository — unacceptable with a merge capability in play, and the application would have no revocable key of its own.

*Rejected:* a GitHub App with a device flow. The cleanest separation and the finest permissions, but it would put the project's first OAuth surface here rather than in the mail slice the spec planned it for.

### D4 — The repo allow-list is checked before the gate, not by it

`github.repos` is matched against the target repository before `Gate.Authorize` runs. A repository outside the list is refused without a capability question ever being asked.

This mirrors `obsidian_write`, which normalizes the note path *before* the gate and passes the same normalized string to both the check and the client — the lesson being that a value the gate rules on and a value the client acts on must be the same string. Here that string is `owner/name`.

### D5 — The cockpit becomes the landing view; the agent grid becomes one of its panels

`ActiveView` gains `'cockpit'` and it becomes the default. `'dashboard'` stays a view, so nothing that exists is deleted and the E2E specs that pin it keep passing.

*Consequence:* `App.vue` is 473 lines and already carries the view branching inline. The dashboard branch moves to `src/features/cockpit/` **before** any panel is added. Adding panels to the file as it stands would take it past 700 lines and nobody would touch it again.

### D6 — No placeholder panels

The first version shows Agents, Pipeline, Routines, Memory and GitHub. Slack, Jira, Mail and Calendar get no tile until they have an application behind them.

A panel that says "not connected yet" is a loading state rendered as an empty state — the exact defect this project fixed in the settings panels, and the one that produced the flaky E2E race in the spawn dialog. Two instances in one week is enough.

---

## 4. Design

### 4.1 The application

`server/internal/apps/github/` — `app.go` (slug, capability declarations, `Register`), `client.go` (the API calls, enforcing nothing), and their tests. The gate is called by the callers, not inside the client, exactly as decision D-A3 of the Obsidian slice established: `Client` takes no capability repos, and a caller reaching it directly bypasses the gate. That is a known property, stated so it is not rediscovered.

Settings, all applying only after a restart, as the Obsidian trio does:

| Key | Type | Note |
|---|---|---|
| `github.token` | secret | encrypted, masked on every read except `Service.Secret` |
| `github.repos` | string | comma-separated `owner/name`, empty means the application is off |
| `github.baseURL` | string | defaults to `https://api.github.com` |

Half-configuration fails the boot, naming the missing keys — the rule `buildObsidianClient` already follows, because a client that comes up looking healthy and 401s on every call is worse than one that refuses to start.

### 4.2 Reaching it

Two surfaces, both of which must be wired or neither is:

- **HTTP**, for the cockpit: `GET /api/github/summary` returns the panel's data in one request.
- **MCP tools**: `github_read`, `github_search`, `github_comment`, `github_merge`, each authorizing through `memory.Gate` with the caller contexts `CallerResolver` supplies.

That "both surfaces" rule is not a preference. This project has twice shipped a seam wired on one surface only — most recently the plan and concept approvals, where the HTTP handler revoked a credential and the MCP tool did not.

`github_merge` is registered like the others. Its class does the work: with no grant, `Decide` returns deny.

### 4.3 The cockpit

`src/features/cockpit/` — one `CockpitView.vue` composing panel components, each fetching its own data and owning its own five states (loading, not-asked, denied, confirmed-empty, failed). That five-state rule is this project's, established in the settings panels, and a panel that collapses any two of them is a defect.

Design tokens are taken from the Claude Desktop mock (`--bg`, `--line`, `--accent`, `--now`, IBM Plex Sans and Mono, light and dark). The RUBRIC layout's dense edge tiles and quiet centre are adopted; its ring of nodes is not — it carries no information a list would not carry better.

### 4.4 Order of work

1. Move the dashboard branch into `features/cockpit/`, no behaviour change, E2E still green.
2. The GitHub application: settings, client, `Register`, capability catalogue.
3. The HTTP summary route and the MCP tools, both gated.
4. The cockpit view and its five panels.
5. Docs, in the same change.

Step 1 first is deliberate: it is the only step that can break something that works today, so it lands alone, where a failure is unambiguous.

---

## 5. Explicitly out of scope

- **Slack, Jira, Mail, Calendar.** Each is its own slice, after GitHub has shown the kernel takes a second application unchanged.
- **The `agent_session` context level**, still without a producer.
- **Merging from an agent without a grant.** D2 makes it deny; nothing here adds a path around that.

---

## 6. Testing

| Test | Asserts |
|---|---|
| `github.merge` with no grant | denied, and the reason names the class default |
| `github.merge` with an explicit allow grant at `global` | allowed |
| A repository outside `github.repos` | refused before the gate is consulted |
| Same grant, MCP tool vs. HTTP route | both enforce; neither surface is open |
| `github.token` on any read endpoint or in any log line | never present |
| Half-configured settings | boot fails, naming the missing keys |
| Each cockpit panel | the five states are distinguishable, per panel |
| E2E after step 1 | the dashboard view still behaves as before the move |

---

## 7. Risks

| Risk | Mitigation |
|---|---|
| A merge fires without a human | `spend` class denies by default (D2); the repo allow-list narrows it further (D4) |
| The token's blast radius | fine-grained PAT, scoped to the repositories you list (D3) |
| The `App.vue` restructure breaks the working dashboard | It lands as its own step, before any panel exists (D5, §4.4) |
| The kernel needs a GitHub-specific change | That is the MLP criterion failing, and it is the finding, not a defeat — record what was missing |
