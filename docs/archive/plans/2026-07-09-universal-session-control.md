# Universal Session Control — Implementation Plan

> Spec: `docs/superpowers/specs/2026-07-09-universal-session-control-design.md` (Approved, D1–D4)
> Branch: `feat/universal-session-control` off `upcoming`. TDD, no placeholders. PR(s) → `upcoming`.

## Slice 1 — Overlay copy-drift fix

**Already built as PR #260** (`fix/askquestion-copy-drift`). Merge as part of this bundle. No new work.

## Slice 2 — Server-side question detection + main-tab surfacing + answer delivery

The hard part: the needs-you band must show an *answerable* AskUserQuestion for any injectable session **without an open terminal**. The client gets the rendered rows free from xterm; the server does not. Decision D1: detect **where the raw pty stream already lives — the broker subprocess** (`pty-host`/`live`), which tees output into the hub. Add a minimal screen model there, run a Go port of `detectQuestion`, and expose the current `DetectedQuestion` at `GET /question`. The main server polls it (D2) and attaches it to the `Agent` payload; the band renders `QuestionCard`; answering posts to a typed endpoint (D3) that derives keystrokes server-side.

### T1 — Go `detectQuestion` port (parity-gated)
- New pkg `server/internal/askq` (Go). Port `detectQuestion` from `src/utils/askQuestionScreen.ts` **including the #260 `metaLabelMatches` normalization** (trailing-punct/prefix), the contiguity gate, multi-select decision, and description extraction.
- Test-first: create shared fixtures `server/internal/askq/testdata/*.txt` copied byte-for-byte from `src/utils/__tests__/fixtures/` PLUS the captured v2.1.205 render. Same fixtures drive the TS tests → parity contract. Table test asserts header/question/options/indices/multiSelect per fixture, matching the TS expectations exactly.
- SSOT note in both files cross-referencing each other (TS ↔ Go hand-parity, like `validation.ts` ↔ `validation/slug.go`).

### T2 — Minimal VT screen model (broker-side)
- The broker sees raw pty bytes (ANSI). To produce "visible rows" for `detectQuestion` we need a small screen grid. Decision inside T2 (cheapest that passes): a **minimal emulator** handling the sequences the AskUserQuestion modal actually uses — CR, LF, `ESC[2J` (clear), `ESC[H`/`ESC[<row>;<col>H` (cursor), `ESC[K` (erase line), SGR ignored — sufficient to reconstruct the visible rows. If a captured real stream needs more, extend minimally. NOT a full terminal.
- Test-first: feed the **recorded raw pty capture** of a v2.1.205 AskUserQuestion (capture via the smoke rig `scratchpad/dump_screen.py`, saved as a fixture) into the emulator → assert the reconstructed rows contain the contiguous options + both meta-rows, and that `askq.detectQuestion(rows)` returns the expected question. This is the make-or-break test — front-loaded.
- Lives in `server/internal/channel` (broker side) since it consumes the pty stream.

### T3 — Broker `GET /question` endpoint
- In `ptyMux` (`ptyhost.go`), tee pty output through the T2 emulator (alongside the existing hub tee) and maintain a current `*DetectedQuestion` (recompute on output, debounced). Add `GET /question` (bearer-gated like `/ws`) returning the current detection or `204`/null.
- Test: drive the hub/emulator with the recorded stream, GET `/question` → expect the parsed question; after an "answered/cleared" frame → null.

### T4 — tmux detection path
- For tmux sessions (no pty broker), detection uses `tmux capture-pane -p -t <pane>` → rows → `askq.detectQuestion`. Add to the merger/scan path guarded by the tmux discovery (`{pid}.json` tmuxPane).
- Test: fake `tmux` on PATH (per the `lookTmuxPath` seam / `tmux seam platform gap` lesson) returning a captured pane; assert detection.

### T5 — `Agent.PendingQuestion` field + poller wiring
- Re-add a typed `PendingQuestion *DetectedQuestion` to `sdk.Agent` (TS + Go, tygo parity — hand-add per `tygo regen` lesson). Sourced from render (T3/T4), NOT JSONL.
- In the merger/scan tick (D2), for each injectable session: pty → GET broker `/question`; tmux → capture-pane. Attach to the Agent. Bounded to injectable sessions; failures are silent (nil).
- Test: merger test with a stub broker `/question` + stub tmux → Agent carries the question.

### T6 — Answer endpoint (server-side keystrokes) — D3
- Resurrect `POST /api/agents/{pid}/answer-question` (typed body: selected option index(es) / custom text / chat). Server derives keystrokes via a Go port of `src/utils/answerKeys.ts` (single=digit; multi=digits→Tab→Enter; custom=digit→text→Enter; chat=digit→text→Enter — `[[lesson_askuserquestion_tui_keys]]`), delivered to:
  - pty broker: POST the raw keystroke bytes to the broker (extend the broker with a keystroke write, or reuse the still-present `SendAnswerKeys` machinery #257 left unreferenced).
  - tmux: `send-keys` for the same tokens.
- Test-first: Go `answerKeys` parity tests mirroring the TS `answerKeys` tests (same cases/bytes). Handler test: stub delivery, assert the derived byte sequence per mode.

### T7 — Needs-you band question card (frontend)
- When an Agent carries `pendingQuestion`, render an answerable card in the needs-you band reusing `QuestionCard` (detected-mode, same component as the overlay). Submit → `POST /api/agents/{pid}/answer-question`.
- Reconcile with the existing permission-fallback: an AskUserQuestion must surface as this answerable card, not as an "Allow AskUserQuestion" permission item.
- Test: band renders the card from a stubbed agent question; answering calls the endpoint with the right payload; the "Allow AskUserQuestion" permission path no longer shows for a detected question.

### T8 — Integration + docs
- CHANGELOG (Fixed: questions surface in the main tab again, now render-sourced not JSONL); README/docs mention of the two surfaces (band + terminal overlay).
- Full verify: `go build`/`go test` (restore `ent` after), `pnpm lint`/`typecheck`/`test`.

## Slice 3 — `live` on-ramp (docs only — D4)

`agent-dashboard live` already auto-wires the MCP + injectable transport (verified `live.go`). No code change.

### T9 — Onboarding docs + connect-dialog one-liner
- `docs/guides/mcp.md` (+ Quickstart): lead with `agent-dashboard live` as *the* way to start a controllable session; explain bare `claude` is monitor-only (no input path). Document the boundary (running bare sessions can't be retrofitted).
- Connect/key dialog (frontend): add a copyable `agent-dashboard live` one-liner alongside the existing `claude mcp add` command (from #253).
- CHANGELOG.
- Test: frontend test for the dialog one-liner render/copy.

## Sequencing & delivery

- Front-load **T1→T2→T3** (detection is the risk; T2 make-or-break test gates the whole approach — if the minimal emulator can't reconstruct the modal from a real capture, revisit before building UI).
- T4–T6 backend, then T7 frontend band, then T9 docs.
- Slices are largely file-disjoint; can ship as one PR or split (slice 2 as its own PR, slice 3 docs as another). #260 merges alongside.
- OFD: main-thread-orchestrated, foreground subagents in the worktree, per-task TDD, 2-stage review, `--no-gpg-sign`, restore `ent` after `go test`, `pnpm i` in the worktree, lint 0.
