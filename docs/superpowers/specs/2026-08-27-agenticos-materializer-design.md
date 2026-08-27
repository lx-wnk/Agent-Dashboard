# AgenticOS — Materializer

**Date:** 2026-08-27
**Status:** Approved design
**Stage:** MLP (unit K4) — specified now so MVP decisions stay compatible
**Parent:** `2026-08-27-agenticos-overview-design.md`
**Implements:** decision D5 (the database owns Skills; files are derived)

---

## 1. Purpose

Produce, on each node, the files the agent runtimes actually read — from resources the system
owns. Skills first; hooks and settings later, if ever.

This is the only component in AgenticOS that writes into the user's own directories. It is
therefore the only one with real destructive potential, which is why it is deferred to MLP and why
this spec spends most of its length on ownership, conflicts and refusal.

---

## 2. The target is a cross product

Not "the filesystem". Not "`~/.claude`". The target set is **node × config dir × provider**, and
each axis is already real in the codebase.

### 2.1 Config dirs multiply

`CLAUDE_CONFIG_DIR` is read in eleven distinct places. Two matter here:

- **Scope resolution** — `cmdscope/scope.go:42-51` and `:62-81`. Precedence, documented at
  `scope.go:55-57`: spawner env `CLAUDE_CONFIG_DIR` (tilde-expanded, "since exec does not expand
  it") → process `CLAUDE_CONFIG_DIR` → `~/.claude`.
- **The parser's search set** — `parser/parser.go:127-162`, four tiers: an explicit
  `DASHBOARD_CLAUDE_CONFIG_DIRS` list, the process variable, `~/.claude`, and then *common custom
  variants that exist on disk*: `~/.claude-personal`, `~/.claude-work`, `~/.claude-dev`
  (`parser.go:157-162`).

That last tier is not hypothetical for this project's own author, who runs `~/.claude-personal`.
A materializer that writes to one config dir writes to the wrong one about half the time.

### 2.2 Providers multiply, and most have no skill concept

`provider.Registry.ConfigDirs()` (`provider/registry.go:179-204`) already enumerates config dirs
for enabled providers, with the env override winning over the default and non-existent
directories dropped:

| id | env | default | ships enabled |
|---|---|---|---|
| `codex` | `CODEX_HOME` | `~/.codex` | no |
| `gemini` | — | `~/.gemini` | no |
| `junie` | — | `~/.junie` | no |
| `pi` | `PI_CODING_AGENT_SESSION_DIR` | `~/.pi/agent/sessions` | no |

Claude is deliberately **not** a descriptor — it is the always-on built-in
(`provider/registry.go:131-133`).

None of the four has a `SKILL.md` equivalent. `cmdscope` already encodes this: a non-Claude
adapter short-circuits to `Supported: false` and enumerates nothing
(`cmdscope/scope.go:63-65`).

### 2.3 Consequence

The materializer resolves a **target list**, and each target names its format adapter. For a
target whose adapter is `none`, materialization is a **visible no-op** recorded against the
resource — never a silent gap. A user who authors a skill and sees "not materialized for Codex —
no skill format" has learned something true. A user who sees nothing has been misled.

---

## 3. Exact on-disk layout

Read from `cmdscope/enumerate.go`, written here literally because a spec that paraphrases a path
is a spec that ships the wrong path.

| Kind | Source | Path template |
|---|---|---|
| Skill | user | `<ConfigDir>/skills/<name>/SKILL.md` |
| Skill | project | `<ProjectCwd>/.claude/skills/<name>/SKILL.md` |
| Skill | plugin | `<ConfigDir>/plugins/cache/<marketplace>/<plugin>/**/SKILL.md` |
| Command | user | `<ConfigDir>/commands/*.md` |
| Command | project | `<ProjectCwd>/.claude/commands/*.md` |

Precedence, from `sourceRank` (`enumerate.go:111-122`): `builtin > project > user > plugin`. Only
`user` and `project` are writable — `IsEditableSource` (`enumerate.go:57-62`) exists precisely to
say so, and the materializer never targets the other two.

Skill name resolution: frontmatter `name:`, falling back to the directory name
(`enumerate.go:251-253`). The materializer writes both consistently so the fallback never has to
fire.

---

## 4. The blocker in the existing write path

`PUT /api/config/file` is the only code that writes into a config directory on a user's behalf,
and it is deliberately incapable of what a materializer needs.

**Authorization is enumeration** (`api/config/file.go:151-174`):

```go
// editableFiles builds the write allow-list for a scope: a map from the
// canonicalized absolute path of every editable skill/command/memory file to
// its source layer ("user" | "project"). Enumeration IS the allow-list.
```

**And enumeration requires existence** (`file.go:176-185`):

```go
// canonical resolves path to an absolute, symlink-free form. EvalSymlinks
// requires the file to exist, which enforces the v1 rule: only existing files
// may be read or written (no create).
```

So: **no existing code creates a file inside a scope-enumerated skills directory.** That is not an
oversight, it is a stated rule, and the materializer changes it. The change is therefore explicit,
narrow and separately authorized:

- The materializer does **not** go through `/api/config/file`. It is a server-side component with
  its own path construction, derived from `cmdscope`'s templates rather than from client input.
- The only paths it may create are `<ConfigDir>/skills/<slug>/SKILL.md` and
  `<ProjectCwd>/.claude/skills/<slug>/SKILL.md`, with `<slug>` validated by `validation.SlugRE`.
  A slug is not a path segment the caller chooses; it is a validated identifier.
- It never follows a symlink, matching the enumeration guardrails
  (`enumerate.go:307-353`, which reject symlinks and hidden entries so "a malicious entry cannot
  redirect a read outside the configured root").

Note one existing asymmetry worth recording: `SpawnPolicy` blacklists `~/.claude` as a *spawn cwd*
(`services/spawn_policy.go:204-213`), while the config write path targets `<ConfigDir>` directly —
`CwdPolicy` gates only the project layer (`api/config/handler.go:80-84`). The materializer inherits
the second posture, not the first, and does so deliberately: writing skills into the config dir is
its entire job.

---

## 5. Ownership — the model already exists, and it is good

`cmd/serve/hooks.go` solves almost exactly this problem for `settings.json`, and its rules are
adopted wholesale rather than reinvented.

**Refuse rather than overwrite** (`hooks.go:195-198`):

```go
// Refuse rather than overwrite: this file holds the user's own
// configuration and a parse failure is not a reason to discard it.
return nil, fmt.Errorf("%s is not valid JSON — fix or move it first: %w", path, err)
```

**A marker is not proof of ownership; the path is** (`hooks.go:259-263`). A hook entry carrying the
dashboard marker but living outside the owned directory is treated as *foreign* and left in place
(`hooks.go:428-431`); uninstall surfaces foreign entries instead of deleting them.

**Three-way outcome** — `unchanged | installed | repaired` (`hooks.go:265-271`). Not a boolean.
"Repaired" is the case where a previously written artefact drifted and was corrected, and it is
reported rather than performed silently.

**Atomic write with an explicit mode** (`hookscript.go:44-73`): temp file in the target directory,
`Sync`, `Close`, `Chmod`, `Rename`. Note that `api/config/file.go:187-210` does the same **without**
`tmp.Sync()`. The materializer uses the `hookscript` variant; a skill file that survives a rename
but not a power loss is not worth the saved syscall.

**Rewriting on every install is deliberate** (`hookscript.go:30-31`): it is how a newer version
replaces an artefact written by an older one.

### 5.1 Applied to skills

Every materialized skill file carries an ownership marker in its frontmatter, and the resource
records the path it wrote. On the next run:

| Situation | Outcome |
|---|---|
| File absent | `created` |
| File present, marker ours, content matches | `unchanged` |
| File present, marker ours, content differs from the resource | `repaired` — the database is the truth for a file we own |
| File present, marker ours, but the file was hand-edited since we wrote it | `conflict` — see §6 |
| File present, **no marker** | `foreign` — never touched, reported once |

---

## 6. Conflicts, and why they are not called drift

Hand edits are explicitly legitimate in this system. So the interesting case is not "the file
differs" but "the file differs **because a human changed it**".

Detection is the mtime-versus-recorded-write comparison the config API already uses for optimistic
concurrency (`api/config/file.go:110-115`), with its known granularity of whole seconds
(`file.go:19,27,33`). A same-second edit is undetectable; that is accepted, recorded here, and
mitigated by also comparing a content hash.

On conflict the materializer **stops for that resource**, records the conflict, and surfaces it.
It does not merge, does not overwrite, and does not queue a retry that will overwrite later.

> **Naming.** Do not call this drift. `drift_alert` already exists and means model-quality drift
> per `(spawner_id, model, stage, metric_key)`, with a partial unique index on open alerts
> (`schema/drift_alert.go:14-40`). The materializer's concept is
> `materialization_conflict`.

---

## 7. Node leases

Two dashboard instances on one machine would write the same files. The lease mechanism already
exists and needs no new infrastructure.

`coord_lock` (`schema/coord_lock.go:12-27`) is a lease keyed by `(namespace, key)` with
`owner_task_id`, `acquired_at`, `expires_at` and a unique index. `Acquire`
(`repo/coord_lock_repo.go:24-86`) retries up to ten times with linear backoff on SQLite busy
errors, steals an expired lease, and is re-entrant for the same owner. `Release` on a lease held by
someone else is an error, not a silent no-op (`:96-107`).

The materializer acquires `namespace = "materialize"`, `key = <node_id>`. Without the lease it is
**read-only**: it computes what it would write, reports it, and writes nothing.

Two known properties, both accepted:

- **Expiry is lazy.** There is no sweeper; an expired row persists until the same key is
  re-acquired (`ListActive` filters on `expires_at > now`, `:109-114`). Harmless for this use.
- **Reachability is not ownership.** This project already learned that once, when a second desktop
  instance adopted a foreign server because a health check returned 200. The lease is the
  ownership check; a successful HTTP call is not.

---

## 8. What replaces `skills-lock.json`

The file exists and records seven skills with `{source, sourceType, computedHash}`. Three findings
from reading the tree:

1. **Nothing in the codebase reads or writes it.** No Go, no TypeScript, no shell. Its only
   references are the file itself, `.gitignore`, and `docs/guides/agent-skills.md`.
2. **The documented install command cannot work as written.** The `jq` filter reads `.name` and
   treats `.source` as a URL, but the name is the map key and `source` is `lx-wnk/skills` — an
   `owner/repo` slug. `computedHash` is never verified by anything.
3. **The documentation contradicts the file.** The "Current skills" table lists `vue`, `vitest`,
   `vite`, `vueuse-functions`, `playwright-best-practices`; the lock file lists seven different
   entries.

The registry's `origin` and `origin_ref` columns replace this: a skill sourced from GitHub records
where it came from and its hash, and the materializer produces the files. The lock file and its
broken snippet are removed in the same change that lands skill materialization — leaving a
documented command that cannot work is worse than having no command.

---

## 9. Failure modes

| Situation | Behaviour |
|---|---|
| No lease | Read-only. Reports what it would write, with the current holder named |
| Target directory unwritable | That target fails, others proceed. A partial materialization is reported as partial |
| Provider has no skill format | Visible no-op recorded against the resource |
| Config dir does not exist | Skipped, not created. `ConfigDirs()` already drops non-existent dirs (`provider/registry.go:196`); inventing a config directory for a runtime the user has not set up is not the materializer's business |
| Foreign file at the target path | Never touched. Reported once, then remembered so it does not nag |
| Conflict | Stops for that resource. No merge, no retry-that-overwrites |
| Slug fails validation | Refused before any path is built. This is the path-traversal boundary |
| Rename fails after temp write | Temp file removed by the deferred cleanup (`hookscript.go:47-72` pattern); target untouched |

---

## 10. Testing

- **Target resolution** — golden test over the cross product: two config dirs, four providers,
  one node. The expected target list is written out literally, because this is where a wrong path
  is a wrong write.
- **Format adapters** — golden file per adapter; an explicit assertion that the `none` adapter
  produces a recorded no-op and touches no filesystem.
- **Ownership** — the five outcomes of §5.1, each as its own case, including a foreign file that
  must survive untouched.
- **Conflict** — a hand-edited target must produce a conflict and no write. A second run must not
  overwrite it either.
- **Lease contention** — two writers, one lease: the loser writes nothing and reports the holder.
- **Path safety** — traversal and symlink-escape attempts refused before any write, matching the
  stance the config API already proves in `api/config/file_test.go:50-76`.
- **Atomicity** — an interrupted write leaves either the old file or the new file, never a partial
  one.

---

## 11. Deferred

| Item | Why not now |
|---|---|
| Materializing hooks and settings | `cmd/serve/hooks.go` already owns that file with a working ownership model. Two owners for one file is worse than one |
| Cross-node materialization | V2, with the node registry |
| Two-way sync (adopting hand edits back into the database) | Needs a merge model and a conflict UI. Conflicts are reported first; adoption can follow once there is evidence about how often they happen |
| Emulating a skill format for providers that lack one | A fabricated format that the runtime ignores is worse than an honest no-op |
