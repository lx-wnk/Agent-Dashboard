# Agent Skills

Skills are owned by the dashboard's resource registry and **materialized** onto disk — the
`SKILL.md` files under a config directory are a derived artifact, not the source of truth.

## Materializing

```bash
# Dry run: reports what would be written, and writes nothing
curl -X POST http://127.0.0.1:13120/api/skills/materialize \
  -H 'Content-Type: application/json' -d '{}'

# Write
curl -X POST http://127.0.0.1:13120/api/skills/materialize \
  -H 'Content-Type: application/json' -d '{"dryRun": false}'
```

Both forms answer a report listing every target considered — every Claude config directory on
this machine, plus every enabled provider — and, per target, one of:

| Outcome | Meaning |
|---|---|
| `created` | No file was there. One was written. |
| `unchanged` | The file already holds the registry's content. |
| `repaired` | The file was one we wrote and had fallen behind the registry. It was rewritten. |
| `conflict` | The file was one we wrote and a person has since edited it. **Nothing was written**, and nothing will be on a later run either. Resolve it by hand. |
| `foreign` | The file was not written by this dashboard. It is never touched. |
| `unsupported` | That runtime has no skill format. Nothing was written, and nothing was faked. |
| `failed` | That target could not be processed. The others still ran; the report is marked `partial`. |

## What it will and will not do

- It writes **only** `<config dir>/skills/<slug>/SKILL.md` and `<project>/.claude/skills/<slug>/SKILL.md`.
  The slug is validated before any path is built.
- It never follows a symlink below the config directory, and never writes over a file it did not
  write itself.
- Two dashboard instances on one machine cannot both write: the run takes a node lease first, and
  the one that does not get it reports what it would have done and names the holder.

## `skills-lock.json` (removed)

Earlier revisions of this guide described a `skills-lock.json` and a `jq` one-liner for installing
from it. Nothing in the codebase ever read that file, and the snippet could not work as written —
it read a `.name` key that did not exist and passed an `owner/repo` slug to `curl` as a URL. Both
are gone. A skill's provenance now lives on its registry row (`origin`, `origin_ref`).
