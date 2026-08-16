# Agent "Working" Status Overlay — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an orthogonal `Working` flag to each agent (true when it's actively generating / owes a reply) so the badge can show an animated "Working" state distinct from the staleness status (active/waiting/idle/finished).

**Architecture:** `working = turnOpen(A) || recentOutput(B)`. A = parser `TurnOpen` (last JSONL entry is a `user` message — incl. tool_result — or `PendingToolUse` set). B = live output activity: pty broker stamps `lastOutputAt` in its discovery file; tmux sessions are probed via `tmux #{window_activity}`. The merger ORs them into `sdk.Agent.Working`; the frontend badge renders "Working" over the status.

**Tech Stack:** Go (parser, merger, channel/ptyhost), tygo (sdk→TS), Vue 3 + Vitest.

**Spec:** `docs/superpowers/specs/2026-06-22-agent-working-status-design.md`

---

## File structure
- `server/internal/parser/parser.go` — `SessionData.TurnOpen` + set it in the parse loop.
- `sdk/types.go` (+ regen `src/sdk.generated.ts`) — `Agent.Working bool`.
- `server/internal/merger/merger.go` — `buildAgent` sets `Working`; new `recentChannelOutput(pid)` helper + tmux-activity seam.
- `server/internal/channel/ptyhost.go` + `headlesspty.go` — record `lastOutputAt`, include in pty discovery JSON.
- `src/components/ui/AppBadge.vue` (+ `src/types.ts` Agent type already has fields via tygo) — "Working" rendering.
- Tests alongside.

---

## Task 1: Parser `TurnOpen`

**Files:** Modify `server/internal/parser/parser.go` (struct ~line 414; parse loop ~740-833); Test: `server/internal/parser/parser_test.go`

- [ ] **Step 1: Failing test** — add to parser_test.go (adapt to the package's existing fixture helper for writing a JSONL + calling `ParseSessionFile`):

```go
func TestParse_TurnOpen(t *testing.T) {
	dir := t.TempDir()
	// last entry is a completed assistant text turn → NOT open
	closed := filepath.Join(dir, "closed.jsonl")
	os.WriteFile(closed, []byte(
		`{"type":"user","message":{"role":"user","content":"hi"}}`+"\n"+
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`+"\n"), 0o644)
	d, err := ParseSessionFile(closed)
	if err != nil { t.Fatal(err) }
	if d.TurnOpen { t.Error("completed assistant turn → TurnOpen must be false") }

	// last entry is a user message → open (agent owes a reply)
	open := filepath.Join(dir, "open.jsonl")
	os.WriteFile(open, []byte(
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]}}`+"\n"+
		`{"type":"user","message":{"role":"user","content":"next task"}}`+"\n"), 0o644)
	d2, err := ParseSessionFile(open)
	if err != nil { t.Fatal(err) }
	if !d2.TurnOpen { t.Error("trailing user message → TurnOpen must be true") }
}
```
First READ the `entry` struct + the parse loop to confirm `entry.Type` values (`"user"`/`"assistant"`/`"message"`) and the `ParseSessionFile` signature. If the fixture line shape needs adjusting to parse, match an existing parser test fixture.

Run: `cd server && go test ./internal/parser/ -run TestParse_TurnOpen -v` → FAIL (TurnOpen undefined).

- [ ] **Step 2: Implement**
Add to `SessionData` (near line 414): `TurnOpen bool`.
In the parse loop, track the last entry's role. Just inside the `for` body, before the `if entry.Type != "assistant" && entry.Type != "message" { continue }` skip, record the type for user/assistant entries:
```go
		if entry.Type == "user" || entry.Type == "assistant" || entry.Type == "message" {
			lastEntryType = entry.Type
		}
```
(declare `var lastEntryType string` before the loop). After the `PendingToolUse` block (after ~line 833), add:
```go
	// TurnOpen: the agent owes the next step — a trailing user message (real
	// prompt or a tool_result, both type "user") or a tool_use awaiting its
	// result. A completed trailing assistant message closes the turn.
	data.TurnOpen = lastEntryType == "user" || data.PendingToolUse != nil
```

- [ ] **Step 3: Run → PASS.** `go test ./internal/parser/ -run TestParse_TurnOpen -v`, then `go test ./internal/parser/ -count=1`.
- [ ] **Step 4: Commit**
```bash
git add server/internal/parser/parser.go server/internal/parser/parser_test.go
git commit -S -m "feat: parser TurnOpen — agent owes the next turn"
```

---

## Task 2: `Agent.Working` + merger sets it from A

**Files:** Modify `sdk/types.go` (Agent struct), regen `src/sdk.generated.ts`; `server/internal/merger/merger.go` (`buildAgent` ~261-305); Test: `server/internal/merger/*_test.go`

- [ ] **Step 1: Failing test** — add a merger test (use the existing fixture/seam style in merger tests; `buildAgent` is unexported so test in package `merger`):

```go
func TestBuildAgent_WorkingFromTurnOpen(t *testing.T) {
	live := &parser.SessionData{SessionID: "s1", LastActivity: time.Now(), TurnOpen: true}
	a := buildAgent(scanner.ProcessInfo{PID: 1, CWD: "/p", Provider: sdk.ProviderClaude}, live, 0)
	if !a.Working { t.Error("TurnOpen session → agent.Working must be true") }

	done := &parser.SessionData{SessionID: "s2", LastActivity: time.Now(), TurnOpen: false}
	b := buildAgent(scanner.ProcessInfo{PID: 2, CWD: "/p", Provider: sdk.ProviderClaude}, done, 0)
	if b.Working { t.Error("closed turn → agent.Working must be false (no B signal)") }
}
```
Confirm `buildAgent`'s signature (`buildAgent(proc scanner.ProcessInfo, session *parser.SessionData, baselineCost float64) sdk.Agent`).

Run: `cd server && go test ./internal/merger/ -run TestBuildAgent_Working -v` → FAIL (Working undefined).

- [ ] **Step 2: Implement**
`sdk/types.go` Agent struct — add: `Working bool `+"`json:\"working\"`"+`` (place near `Status`/`ChannelAvailable`).
Run `task generate` (regenerates `src/sdk.generated.ts` — adds `working: boolean`).
`merger.go buildAgent` — add to the returned `sdk.Agent{...}`: `Working: session.TurnOpen,` (B is ORed in Task 4).
Also apply the same to `buildFinishedAgent` (stale.go) — a finished agent is never working: set `Working: false` (explicit, since finished agents have no live turn).

- [ ] **Step 3: Run → PASS.** `go test ./internal/merger/ -count=1`; `pnpm typecheck` (Agent type gains `working`).
- [ ] **Step 4: Commit**
```bash
git add sdk/types.go src/sdk.generated.ts server/internal/merger/merger.go server/internal/merger/stale.go server/internal/merger/<test>.go
git commit -S -m "feat: Agent.Working set from parser TurnOpen"
```

---

## Task 3: pty broker records `lastOutputAt`

**Files:** Modify `server/internal/channel/ptyhost.go` (`writePtyDiscovery`), `server/internal/channel/headlesspty.go` (drain); Test: `server/internal/channel/headlesspty_test.go`

- [ ] **Step 1: Failing test** — extend headlesspty_test.go: run a child that prints periodically; assert the pty discovery JSON gains a `lastOutputAt` that is recent after output:

```go
func TestRunHeadlessPTY_StampsLastOutputAt(t *testing.T) {
	home := t.TempDir(); t.Setenv("HOME", home)
	ctx, cancel := context.WithCancel(context.Background()); defer cancel()
	pidCh := make(chan int, 1)
	go func() { _ = RunHeadlessPTY(ctx, "sh", []string{"-c", "while true; do echo tick; sleep 0.2; done"}, nil, "", func(p int){ pidCh <- p }) }()
	pid := <-pidCh
	disc := filepath.Join(home, channelconfig.DiscoveryDir, strconv.Itoa(pid)+".pty.json")
	var lastOut string
	for i := 0; i < 100; i++ {
		if b, err := os.ReadFile(disc); err == nil {
			var m map[string]any
			if json.Unmarshal(b, &m) == nil {
				if v, ok := m["lastOutputAt"].(string); ok && v != "" { lastOut = v; break }
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if lastOut == "" { t.Fatal("lastOutputAt never stamped despite output") }
	ts, err := time.Parse(time.RFC3339, lastOut)
	if err != nil { t.Fatalf("bad lastOutputAt %q: %v", lastOut, err) }
	if time.Since(ts) > 10*time.Second { t.Errorf("lastOutputAt stale: %v", ts) }
}
```

Run: `cd server && go test ./internal/channel/ -run TestRunHeadlessPTY_StampsLastOutputAt -v` → FAIL (no lastOutputAt).

- [ ] **Step 2: Implement**
In `headlesspty.go`, replace the `io.Copy(io.Discard, ptmx)` drain with a writer that records the last-output time into a shared `atomic.Int64` (unix nanos):
```go
	var lastOut atomic.Int64
	lastOut.Store(time.Now().UnixNano())
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 { lastOut.Store(time.Now().UnixNano()) }
			if err != nil { return }
		}
	}()
```
Make `writePtyDiscovery` accept the lastOutputAt (or write it via a periodic refresh). Simplest: change the token-rotation rewrite to also pass `lastOut`, AND add a dedicated 1s ticker that rewrites the discovery file with the current `lastOut` when it changed. Update `writePtyDiscovery(childPid, port int, token string, lastOutputAt time.Time)` to include `"lastOutputAt": lastOutputAt.UTC().Format(time.RFC3339)` in the marshaled map. Update both call sites (initial write + rotation) and `RunPTY` (pass `time.Now()` / a similar tracker, or `time.Time{}` if RunPTY doesn't track — but for consistency add a tracker to RunPTY too, or pass the broker's last-output time). Keep `RunPTY` behavior unchanged except the extra field (it can pass its own lastOut tracker; if too invasive, pass `time.Now()` so interactive live always looks recent — acceptable since RunPTY is the foreground user terminal). Document the choice.
Add a 1s refresh ticker in `RunHeadlessPTY` that calls `writePtyDiscovery` with the current `lastOut` so the file stays fresh between token rotations.

- [ ] **Step 3: Run → PASS.** `go test ./internal/channel/ -run TestRunHeadlessPTY -v -count=1` and `-race`; `go test ./internal/channel/ -count=1`.
- [ ] **Step 4: Commit**
```bash
git add server/internal/channel/ptyhost.go server/internal/channel/headlesspty.go server/internal/channel/headlesspty_test.go
git commit -S -m "feat: pty broker stamps lastOutputAt in discovery file"
```

---

## Task 4: merger reads B (pty lastOutputAt + tmux activity) → OR into Working

**Files:** Modify `server/internal/merger/merger.go`; Test: `server/internal/merger/channel_internal_test.go` or a new merger test

- [ ] **Step 1: Failing test** — test a `recentChannelOutput`-style helper with the tmux seam + a pty discovery file with a recent `lastOutputAt`:

```go
func TestRecentChannelOutput(t *testing.T) {
	home := t.TempDir(); t.Setenv("HOME", home)
	dir := filepath.Join(home, channelconfig.DiscoveryDir)
	os.MkdirAll(dir, 0o755)
	// pty file with a recent lastOutputAt → working
	os.WriteFile(filepath.Join(dir, "10.pty.json"),
		[]byte(`{"port":1,"ptyInject":true,"lastOutputAt":"`+time.Now().UTC().Format(time.RFC3339)+`"}`), 0o644)
	if !recentChannelOutput(10) { t.Error("recent pty output → working") }
	// stale pty file → not working
	os.WriteFile(filepath.Join(dir, "11.pty.json"),
		[]byte(`{"port":1,"ptyInject":true,"lastOutputAt":"`+time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)+`"}`), 0o644)
	if recentChannelOutput(11) { t.Error("stale pty output → not working") }

	// tmux activity via seam
	prev := tmuxActivityFn
	tmuxActivityFn = func(pane string) (time.Time, bool) { return time.Now(), true }
	t.Cleanup(func(){ tmuxActivityFn = prev })
	os.WriteFile(filepath.Join(dir, "12.json"), []byte(`{"port":1,"tmuxPane":"%3"}`), 0o644)
	if !recentChannelOutput(12) { t.Error("recent tmux activity → working") }
}
```

Run: `cd server && go test ./internal/merger/ -run TestRecentChannelOutput -v` → FAIL (undefined).

- [ ] **Step 2: Implement**
Add an `outputThreshold = 5 * time.Second` const near `activeThreshold` in merger.go.
Add a tmux seam: `var tmuxActivityFn = realTmuxActivity` where
```go
// realTmuxActivity returns the pane's last-activity time via tmux.
func realTmuxActivity(pane string) (time.Time, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", pane, "#{window_activity}").Output()
	if err != nil { return time.Time{}, false }
	sec, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil { return time.Time{}, false }
	return time.Unix(sec, 0), true
}
```
Add `recentChannelOutput(pid int) bool`: read `channelconfig.DiscoveryPtyFile(home,pid)` → parse `lastOutputAt` → if `time.Since < outputThreshold` return true. Else read `channelconfig.DiscoveryFile(home,pid)` → if it has a non-empty `tmuxPane`, call `tmuxActivityFn(pane)` → if ok && `time.Since < outputThreshold` return true. Else false. (Mirror `channelDiscovery`'s home/read pattern; tolerate missing files.)
In `buildAgent`, OR it in: `Working: session.TurnOpen || recentChannelOutput(proc.PID),`.

- [ ] **Step 3: Run → PASS.** `go test ./internal/merger/ -count=1`; `go vet`.
- [ ] **Step 4: Commit**
```bash
git add server/internal/merger/merger.go server/internal/merger/<test>.go
git commit -S -m "feat: merge live output activity (pty + tmux) into Agent.Working"
```

---

## Task 5: Frontend "Working" badge

**Files:** Modify `src/components/ui/AppBadge.vue` + the agent header/badge sites that pass `agent.status`; Test: a vitest for the badge

- [ ] **Step 1: Failing test** — add/extend a badge or AgentModal spec:
```ts
it('renders Working when agent.working, overriding status', () => {
  const w = mount(AgentModalHeaderOrCard, { props: { agent: { ...baseAgent, status: 'waiting', working: true } } })
  expect(w.text()).toContain('Working')
})
it('renders the status label when not working', () => {
  const w = mount(..., { props: { agent: { ...baseAgent, status: 'waiting', working: false } } })
  expect(w.text()).toContain('Waiting')
})
```
Pick the component that actually renders the badge from `agent` (likely `AgentCard.vue` header / `AgentModal.vue` line 74 `<AppBadge :variant="agent.status" />`). Decide whether to compute the variant at the call site (`:variant="agent.working ? 'working' : agent.status"`) or pass `working` into `AppBadge`. Prefer computing at the call sites (AgentCard + AgentModal) so `AppBadge` stays dumb. Add a `working` variant to `AppBadge` (dot+label classes + `statusLabel('working')→'Working'`), an animated pulse class.

Run: `pnpm test <spec>` → FAIL.

- [ ] **Step 2: Implement**
- `src/utils/statusColors.ts`: `statusLabel` case `'working' → 'Working'`; `agentStatusTone` `'working' → 'info'` (or 'accent').
- `src/components/ui/AppBadge.vue`: add `'working'` to the `Variant` union + `dotClass`/`labelClass` (e.g. `bg-info-dot animate-pulse`, `text-info-text`).
- `AgentCard.vue` + `AgentModal.vue`: change `<AppBadge :variant="agent.status" />` → `<AppBadge :variant="agent.working ? 'working' : agent.status" />`.

- [ ] **Step 3: Run → PASS.** `pnpm test`, `pnpm lint`, `pnpm typecheck`.
- [ ] **Step 4: Commit**
```bash
git add src/utils/statusColors.ts src/components/ui/AppBadge.vue src/components/AgentCard.vue src/components/AgentModal.vue src/components/<spec>
git commit -S -m "feat: animated Working badge overriding staleness status"
```

---

## Task 6: Docs

**Files:** `CHANGELOG.md`, `README.md`

- [ ] **Step 1:** CHANGELOG Unreleased → Added: "Agents now show an animated **Working** badge while actively generating (derived from conversation turn-state plus live tmux/pty output), distinct from the idle/waiting staleness states."
- [ ] **Step 2:** README — in the agent-status section, document the Working overlay + what drives it (turn-state + live output).
- [ ] **Step 3:** `pnpm lint && pnpm typecheck && pnpm test` + `cd server && go build ./... && go test ./... -count=1` green.
- [ ] **Step 4:** `git commit -S -m "docs: document the Working status overlay"`

---

## Final verification
- [ ] `cd server && go build ./... && go test ./... -count=1` (+ `-race` on channel + merger) — PASS
- [ ] Repro the CI tmux condition for any tmux-seam test: tests must PIN `tmuxActivityFn` (never call real tmux) — confirm no test reads ambient tmux. (See lesson: env-probing seams must be pinned.)
- [ ] `pnpm lint && pnpm typecheck && pnpm test` — PASS
- [ ] Manual (optional): spawn a live agent, send a prompt → badge flips to "Working" while generating, back to Waiting when it asks for input.

---

## Notes
- Overlay flag (not a status enum value) — `Status` semantics unchanged.
- B-tmux + B-pty both feed `recentChannelOutput`; tmux seam pinned in tests (env-probing seam — see `lesson_tmux_seam_platform_gap`).
- `outputThreshold` = 5s; tmux query timeout 500ms (never block the tick).
- finished agents: `Working=false` explicitly.
