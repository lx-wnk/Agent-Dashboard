# Voice Input for Task Refinement — Design Spec

**Date:** 2026-06-08
**Status:** Approved (design), pending implementation plan
**Scope:** Option *a* — minimal frontend plugin seam + two voice plugins (whisper, web-speech)

> Generalization of the seam into an app-wide framework is **out of scope**, tracked
> in `docs/local/todos/2026-06-08-generic-frontend-plugin-framework.md`.

## Goal

Let a user dictate refinement messages by voice instead of typing into the refinement
chat textarea (`src/components/RefinementChat.vue:348`). Both speech-to-text engines
ship as **plugins**; **no voice-specific code lives in core**.

## Hard Constraints

1. **No voice-specific code in core.** Core gains only a generic, voice-agnostic
   extension seam (a named slot + a plugin-JS loader). The words "voice" / "whisper" /
   "speech" never appear in core.
2. **Privacy posture.** The dashboard binds `127.0.0.1` and reads sensitive Claude
   session data. The local whisper plugin keeps audio on-device. The web-speech plugin
   (Chrome → Google) is opt-in by installation only and must be clearly labelled as
   sending audio off-device.
3. **Reuse existing extension point.** Build on the `route_extension` plugin mechanism
   (`server/internal/plugin/registry.go`, `server/internal/api/router.go:349`,
   `server/internal/plugin/proxy.go:14`). Do not invent a parallel discovery path.

## Architecture

Three cleanly separated parts:

```
CORE (voice-agnostic)                    PLUGINS (DASHBOARD_PLUGIN_DIR)
┌─────────────────────────────┐          ┌──────────────────────────┐
│ RefinementChat.vue           │          │ voice-whisper/           │
│  └ <PluginSlot               │  loads   │  ├ process: /transcribe  │
│      name="refinement-       │◀────ESM──│  │   (audio→wav→whisper) │
│      input-addon"            │  import  │  └ serves: addon.js (mic)│
│      :ctx=slotCtx />         │          ├──────────────────────────┤
│ PluginSlot.vue (generic)     │          │ voice-webspeech/         │
│ usePluginSlots.ts (generic)  │          │  ├ process: static server│
│  └ discovers FE modules via  │          │  └ serves: addon.js      │
│    /api/settings/plugins     │          │     (SpeechRecognition)  │
└─────────────────────────────┘          └──────────────────────────┘
```

Core knows only: "Slot X exists; plugins may mount UI into it; here is the slot
context." It never knows what the mounted UI does.

## Components

### Core (generic, voice-agnostic)

**`src/components/PluginSlot.vue`** — a named mount point.
- Prop `name: string` (here always `"refinement-input-addon"`).
- Prop `ctx: SlotContext` — passed through to mounted plugin modules.
- Renders a host `<div>` per matching plugin; delegates mount/unmount to
  `usePluginSlots`.

**`src/composables/usePluginSlots.ts`** — the generic loader.
- Reads the plugin list from `/api/settings/plugins` (existing endpoint; consumed
  today by `src/composables/usePlugins.ts`).
- For each plugin advertising a FE addon for the requested slot, dynamically imports
  its module: `import(/* @vite-ignore */ '/api/settings/plugins/{id}/addon.js')`.
- Calls `mod.mount(hostEl, ctx)` and retains the returned unmount fn.
- On plugin removal / unmount / component teardown, calls each unmount fn.
- **Security:** only loads from registry-discovered, health-checked plugins routed
  through the existing proxy. Never loads from an arbitrary URL.

**Core touch in `RefinementChat.vue`:** add
`<PluginSlot name="refinement-input-addon" :ctx="slotCtx" />` near the textarea, where
`slotCtx` wires `insertText` to the existing textarea model and `setBusy` to a local
busy flag. This is the ONLY edit to an existing core file.

### Slot Contract (the single core interface)

```ts
// src/types.ts (or a small new src/utils/pluginSlot.ts)
export interface SlotContext {
  insertText: (text: string) => void   // append/insert into the refinement textarea
  setBusy: (busy: boolean) => void      // reflect recording / transcribing state
}

// Plugin addon module shape (loaded via ESM):
export interface SlotAddonModule {
  default: {
    mount: (el: HTMLElement, ctx: SlotContext) => UnmountFn
  }
}
type UnmountFn = () => void
```

The `mount(el, ctx)` contract is framework-neutral — a plugin addon may be built with
Vue, vanilla JS, or anything else. Core never depends on the plugin's build.

### Plugins (in `DASHBOARD_PLUGIN_DIR`)

Both ship as `route_extension` plugins with a running process (decision *i*: uniform
single load path).

**`voice-whisper/`**
- Process: exposes `POST /transcribe` (multipart audio in → `{ text }` out). Internally:
  `webm/opus → ffmpeg → wav → whisper.cpp → text`.
- Serves `addon.js`: renders a mic button; uses `MediaRecorder` to capture audio,
  POSTs the blob to its own `/transcribe` route (reached via
  `/api/settings/plugins/voice-whisper/transcribe`), then calls `ctx.insertText(text)`.
- Privacy: audio never leaves the machine.
- Dependencies: `whisper.cpp` (or a Go/Python binding) + `ffmpeg`. Model stored under a
  plugin-local dir; download/setup documented in the plugin README.

**`voice-webspeech/`**
- Process: a trivial static server that only serves `addon.js` (no transcription — that
  happens in the browser). Required because plugin discovery is process-based.
- Serves `addon.js`: renders a mic button; uses the browser `SpeechRecognition`
  (`webkitSpeechRecognition`) API for live interim transcript, calls `ctx.insertText`.
- Privacy: Chrome/Edge send audio to Google. The addon UI must label this clearly.
- Browser support: Chrome/Edge only; degrades to a disabled/hidden control elsewhere.

## Data Flow

**Whisper:**
```
Mic click → MediaRecorder.start → stop → blob
  → POST /api/settings/plugins/voice-whisper/transcribe
  → dashboard reverse proxy → plugin process
  → ffmpeg → whisper.cpp → { text }
  → ctx.insertText(text) → textarea
```

**Web-speech:**
```
Mic click → SpeechRecognition.start → interim/final results
  → ctx.insertText(transcript) (live) → textarea
```

## Error Handling

- **No voice plugin installed** → slot renders nothing → no mic button. Clean degrade;
  typing is unaffected.
- **Mic permission denied** → addon shows an inline error in its own UI; core untouched.
- **whisper process down** → health check fails → plugin not loaded → button absent.
- **Transcription error** (ffmpeg/whisper failure, network) → addon shows error +
  `ctx.setBusy(false)`; textarea content preserved.
- **Loader import failure** → `usePluginSlots` logs + skips that plugin; other slots and
  core continue.
- **Secure context:** `MediaRecorder` / `getUserMedia` require a secure context;
  `127.0.0.1` qualifies. Document that custom binds must remain loopback or HTTPS.

## Testing Strategy

- **Core (Vitest):**
  - `usePluginSlots` — mock `/api/settings/plugins` + dynamic `import`; assert mount is
    called with a correct `ctx`, unmount on teardown, failures isolated per plugin.
  - `PluginSlot.vue` — renders one host per addon; passes `ctx` through.
  - `RefinementChat.vue` — `slotCtx.insertText` writes to the textarea model;
    `setBusy` toggles the busy flag. No voice-specific assertions.
- **voice-whisper process (Go/lang test):** `/transcribe` with an audio fixture →
  asserts non-empty `{ text }`; ffmpeg-missing path returns a clear error.
- **voice-webspeech addon:** browser-API bound → manual / Playwright E2E only.

## Decomposition / Build Order

1. **Core seam** — `SlotContext` type, `usePluginSlots.ts`, `PluginSlot.vue`, the single
   `RefinementChat.vue` edit. Ships independently; with no plugins installed it is a
   no-op. Fully unit-tested.
2. **voice-whisper plugin** — process (`/transcribe`, ffmpeg+whisper) + `addon.js` +
   README/setup. Built on the seam from step 1.
3. **voice-webspeech plugin** — static-serve process + `addon.js`. Built on the seam.

Steps 2 and 3 are independent of each other; both depend on step 1.

## Out of Scope

- App-wide multi-slot plugin framework → `docs/local/todos/2026-06-08-generic-frontend-plugin-framework.md`.
- Streaming/partial transcription for whisper (batch only in v1).
- Speaker diarization, language auto-detect tuning, custom vocab.

## Key References

| Concern | Location |
|---|---|
| Refinement textarea (core touch) | `src/components/RefinementChat.vue:348` |
| Refinement composable | `src/composables/useRefinementChat.ts` |
| Plugin list endpoint | `GET /api/settings/plugins` — `server/internal/api/plugins/handler.go:23` |
| Plugin registry / discovery / health | `server/internal/plugin/registry.go:61,146` |
| route_extension mount | `server/internal/api/router.go:349` |
| Reverse proxy (strips Cookie/Authorization) | `server/internal/plugin/proxy.go:14` |
| Plugin capability enum | `server/internal/plugin/types.go:28` |
| Existing plugin viewer | `src/composables/usePlugins.ts`, `src/components/PluginSettings.vue` |
