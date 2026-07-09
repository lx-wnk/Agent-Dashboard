# Universal Session Control — Design Spec

> Date: 2026-07-09 · Status: Draft (awaiting user review) · Branch: `feat/universal-session-control` (off `upcoming`)
> User initiative: **every** Claude session that carries the dashboard MCP must be fully controllable from the dashboard — answer questions, inject prompts, drive the agent — with the strongest guarantee for sessions the user spawns *from* the dashboard. "Wie" is open; the requirement is a 100% control path.

## Why

Control of a live Claude TUI requires an **input path into its pty/tty**. The dashboard already has the whole machinery, but coverage and surfacing regressed:

- **Injection foundation exists and is shared.** `ptyMux` (`server/internal/channel/ptyhost.go`) serves `/health` + `/message` (HTTP prompt inject) + `/ws` (live terminal stream) and is used by **both** on-ramps: `RunHeadlessPTY` (dashboard-spawned, `pty-host`) and `RunPTY` (user-started, `agent-dashboard live`, which also proxies the real terminal). Both write `{pid}.pty.json` (`ptyInject:true`) → `liveInjectable=true`. tmux is a third path (`{pid}.json` with `tmuxPane` → `send-keys`).
- **The MCP alone does NOT make a session controllable.** The `dashboard-channel` MCP (`{pid}.json`) is a stdio child of Claude — it gives `channelAvailable` (replies, permission requests) but cannot inject input into Claude's TUI (documented limit). Input needs pty-broker OR tmux ownership of the tty.
- **Gap 1 — question overlay broken by copy drift.** The web-terminal `QuestionOverlay` never appears on current Claude Code (v2.1.205): `detectQuestion` matched the meta-row copy exactly, and v2.1.205 renders `Type something.` with a trailing period → detection returns null. (Fix already built: PR #260.)
- **Gap 2 — questions no longer surface in the main tab.** PR #257 (`e9dbf64a`) removed the JSONL-based `pendingQuestion` surfacing (the needs-you band question card + inline answer from #255/#256), citing JSONL timing-unreliability, and moved answering into the terminal-tab overlay only. Result: an AskUserQuestion now falls back to a generic PendingToolUse → shows as an "Allow AskUserQuestion" **permission** item, not an answerable question. For any session whose terminal tab isn't open, the question is invisible.
- **Gap 3 — user-started sessions are usually not injectable.** A session the user launches as bare `claude` (not via `live`, not in tmux) has the MCP but no input path → uncontrollable. Only `live`/tmux/dashboard-spawn sessions are injectable.

These are one problem with one foundation: **make every MCP session ride the pty-broker (or tmux) path, and expose one uniform control + question surface over it.** Building the three slices together is cheaper than separately because they share that foundation rather than duplicating it.

## Constraints / invariants

- A **running** bare-`claude` session cannot retroactively gain a broker — pty ownership is established at launch. Coverage is forward-looking (future sessions started the right way). This is physical, not a feature gap.
- Server binds `127.0.0.1` only; brokers are loopback + rotating-bearer gated (unchanged).
- SSOT: any question-detection logic that must run on both client (TS) and server (Go) has no shared module — parity is maintained by hand (`src/utils/validation.ts` ↔ `server/internal/validation` precedent).

## Slices

### Slice 1 — Overlay copy-drift fix (BUILT: PR #260)

`detectQuestion` matches the injected meta-rows via `metaLabelMatches` (lower-case, strip trailing punctuation, prefix) instead of exact equality, so cosmetic TUI-copy tweaks no longer disable detection. Regression test built from a captured v2.1.205 render. Kept open as slice 1 of this bundle.

### Slice 2 — Restore main-tab question surfacing (uniform, reliable)

**Goal:** an AskUserQuestion on any injectable session appears as an *answerable* card in the needs-you band (where the user expects it), regardless of whether the terminal tab is open, and answering it drives the session's injection path.

**Detection — server-side, from the rendered screen (not JSONL).** #257 was right that the JSONL tail is timing-unreliable. Instead, detect from the **actual rendered modal**, which the broker already holds:
- For pty-broker sessions: the server reads the broker's scrollback (the same `/ws` replay the terminal proxy uses) and runs question detection on the visible rows.
- For tmux sessions: `tmux capture-pane -p` yields the visible screen; run the same detection.
- Detection logic is ported to Go (`detectQuestion` parity with `src/utils/askQuestionScreen.ts`) and kept in sync by hand per SSOT. A shared fixture set (the captured real renders, incl. the v2.1.205 one) pins Go↔TS parity.
- A lightweight per-injectable-session poller (reuses the existing scan cadence) refreshes the detected question; the detected `DetectedQuestion` (header/question/options/multiSelect) is attached to the `Agent` payload (re-introducing a typed field, e.g. `Agent.PendingQuestion`, that #257 removed — now sourced from the render, not the JSONL).

**Surfacing:** the needs-you band renders the answerable question card (radio/checkbox from the detected options), reusing `QuestionCard` (which #257 already keeps in detected-mode). This is the same component the overlay uses — one card, two mount points (band + terminal overlay).

**Answer delivery — server-side keystrokes to the injection path.** Re-introduce a server endpoint (the removed `POST /api/agents/{pid}/answer-question` shape) that encodes the answer to AskUserQuestion keystrokes (the proven model in `[[lesson_askuserquestion_tui_keys]]`: digit for single-select; digits→Tab→Enter for multi; digit→text→Enter for custom) and delivers them via:
- pty broker: the broker `/message`-style keystroke write (raw bytes to `ptmx`), and/or the existing `SendAnswerKeys` machinery that #257 left in place unreferenced.
- tmux: `send-keys` for the same tokens.
The client keystroke encoder (`src/utils/answerKeys.ts`) is the TS SSOT; the Go side mirrors it (parity by hand). The terminal overlay keeps sending keystrokes directly over `/ws` (unchanged); the band answer goes through the server endpoint. Both converge on the same key model.

### Slice 3 — `live` on-ramp for user sessions

**Goal:** a user's own session becomes a pty-broker (injectable) session with one obvious action, so "every MCP session" is actually controllable — not just dashboard-spawned ones.

`agent-dashboard live -- claude …` already delivers exactly this (pty broker + real-terminal proxy + `/ws` + MCP). The work is ergonomics + discoverability:
- The MCP-connect / onboarding flow (`docs/guides/mcp.md`, #253) recommends and documents launching via `agent-dashboard live` (composing the `--mcp-config` for `dashboard-channel`), so a connected session is an injectable one by construction.
- Optionally: a copyable one-liner / shell alias (`ad-claude`) surfaced in the same key/connect dialog that starts `live` with the MCP wired.
- Verify `live` + the channel MCP compose cleanly (MCP config passed through to the wrapped `claude`; `{pid}.json` and `{pid}.pty.json` coexist per the two-file model).
- Document the boundary: already-running bare sessions stay uncontrollable; relaunch via `live` to gain control.

## Scope

**In:** #260 (slice 1); server-side question detection (Go `detectQuestion` parity + scrollback/tmux capture + poller) and re-added `Agent.PendingQuestion` typed field; needs-you band answerable question card (reuse `QuestionCard`); server-side answer endpoint + keystroke delivery (pty `/message`/`SendAnswerKeys` + tmux `send-keys`); `live` on-ramp docs + connect-dialog one-liner; CHANGELOG/README/docs.

**Out:** retroactively injecting into already-running bare sessions (physically impossible); Windows; any change to the pty-broker wire protocol; auto-launching `live` on the user's behalf (the user runs it).

## Testing

- Go: `detectQuestion` parity tests over the shared fixtures (incl. v2.1.205 trailing-period render); answer-keystroke encoding tests (single/multi/custom/chat) mirroring the TS `answerKeys` tests; tmux `capture-pane` + pty-scrollback detection with recorded frames.
- TS: existing `askQuestionScreen` + `answerKeys` + `QuestionOverlay` suites, plus a needs-you-band question-card render/answer test.
- E2E/live smoke: relaunch a session via `live`, trigger an AskUserQuestion, confirm it surfaces in BOTH the band and the terminal overlay, and that answering from the band drives the pty (the manual leg that surfaced #260).

## Open questions (for review)

1. **Go↔TS `detectQuestion` parity** — accept the hand-maintained duplication (SSOT precedent) with a shared fixture set as the parity gate? Or keep detection client-only and have the band consume a client-pushed detection (weaker — needs a live client)? *Recommendation: server-side Go port, fixture-gated — it's the only way the band works without an open terminal.*
2. **Detection cadence/cost** — poll every injectable session's scrollback on the existing scan tick, or only on-demand when the band is visible? *Recommendation: on the scan tick, bounded to injectable sessions.*
3. **Answer endpoint shape** — resurrect `POST /api/agents/{pid}/answer-question` verbatim, or fold into a more general keystroke-inject endpoint? *Recommendation: dedicated answer endpoint (typed to the question), keystrokes derived server-side.*
4. **Slice independence / order** — #260 (slice 1) merges first; slice 2 (surfacing) and slice 3 (on-ramp) are largely file-disjoint and can land in either order. Confirm slice 3 stays docs+ergonomics (no `live` code change) or whether `live` needs a flag to auto-wire the MCP config.
