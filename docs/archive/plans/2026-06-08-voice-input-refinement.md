# Voice Input for Task Refinement — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user dictate refinement messages by voice; two speech-to-text engines (local whisper, browser web-speech) ship as plugins with zero voice-specific code in core.

**Architecture:** Core gains a generic, voice-agnostic frontend extension seam — a named `<PluginSlot>` plus a dynamic plugin-JS loader that imports `addon.js` from `route_extension` plugins over the existing reverse proxy. Each voice engine is a separate `route_extension` plugin in `DASHBOARD_PLUGIN_DIR` that serves a vanilla-ESM `addon.js` mounting a mic button into the slot.

**Tech Stack:** Vue 3 + TypeScript + Vitest (core), Go (plugin processes), vanilla ESM (plugin `addon.js`), whisper.cpp + ffmpeg (whisper engine), browser `SpeechRecognition` (web-speech engine).

**Spec:** `docs/superpowers/specs/2026-06-08-voice-input-refinement-design.md`

---

## Plugin Authoring Contract (reference — verified against code)

- Manifest `plugin.json` per subdir of `DASHBOARD_PLUGIN_DIR`. Struct (`server/internal/plugin/types.go:5`):
  `{ id, version, capabilities[], addr ("127.0.0.1:<port>"), command[], env[] }`.
- `id` must match `^[a-z0-9][a-z0-9-]*$`; `addr` must be loopback or the plugin is skipped (`registry.go:89,104`).
- Process: `exec` `command` with `cwd=pluginDir`; only base env (`PATH,HOME,TMPDIR,TEMP,USER,LANG,LC_ALL`) + listed `env[]` names forwarded (`registry.go:115,376`).
- Health: dashboard polls `GET {addr}/health` → expects `200` within 5s (`registry.go:396`).
- Proxy: `/api/settings/plugins/{id}` is **StripPrefix**'d (`proxy.go:27`), so `/api/settings/plugins/voice-whisper/transcribe` reaches the plugin as `GET/POST /transcribe`. `Cookie` + `Authorization` are stripped toward the plugin (`proxy.go:24`). JWT auth is enforced at the dashboard layer BEFORE the proxy (`router.go:208`).
- Capability strings: `"route_extension"`, `"auth_provider"` (`types.go:29`).
- Example manifest: `plugins/github-oauth/plugin.json`. Full dev guide: `docs/plugin-guide.md`.
- **Build convention (verified):** plugins are standalone Go modules NOT listed in root `go.work` (module name `github.com/lx-wnk/agent-dashboard-plugin-<name>`). They MUST be built/tested with `GOWORK=off` (CI does `cd "$plugin" && GOWORK=off go build` — `.github/workflows/ci.yml:147`). Do NOT add plugin dirs to `go.work`. All `go build`/`go test` commands below run with `GOWORK=off`.
- Listing endpoint `GET /api/settings/plugins` returns `[{ id, capabilities }]` only (`api/plugins/handler.go:23`) — enough for the loader.

**Slot-targeting convention (this plan):** the loader does NOT learn the target slot from the manifest. It imports each `route_extension` plugin's `addon.js` and reads `module.default.slot`. A plugin with no `addon.js` (404 on import) is skipped silently. This keeps the listing endpoint unchanged.

**Chosen ports:** `voice-whisper` → `127.0.0.1:19010`; `voice-webspeech` → `127.0.0.1:19011`.

---

# Phase A — Core Seam (this repo, voice-agnostic)

Ships independently. With no voice plugin installed it is a no-op (empty slot, no mic button). Fully unit-tested.

## Task A1: Slot contract types

**Files:**
- Create: `src/utils/pluginSlot.ts`

- [ ] **Step 1: Write the types**

```ts
// src/utils/pluginSlot.ts
// Generic frontend plugin-slot contract. Voice-agnostic: core knows only that a
// plugin may mount UI into a named slot and gets these two callbacks.

export interface SlotContext {
  /** Insert text into the host input (e.g. the refinement textarea). */
  insertText: (text: string) => void
  /** Reflect a busy state (recording / transcribing) in the host UI. */
  setBusy: (busy: boolean) => void
}

export type UnmountFn = () => void

export interface SlotAddon {
  /** Which named slot this addon targets, e.g. "refinement-input-addon". */
  slot: string
  /** Mount the addon UI into `el`; return a teardown fn. */
  mount: (el: HTMLElement, ctx: SlotContext) => UnmountFn
}

export interface SlotAddonModule {
  default: SlotAddon
}
```

- [ ] **Step 2: Typecheck**

Run: `pnpm typecheck`
Expected: PASS (no errors).

- [ ] **Step 3: Commit**

```bash
git add src/utils/pluginSlot.ts
git commit -m "feat(plugins): add generic frontend slot contract types"
```

## Task A2: Plugin-slot addon loader

**Files:**
- Create: `src/composables/usePluginSlots.ts`
- Test: `src/composables/usePluginSlots.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// src/composables/usePluginSlots.test.ts
import { describe, expect, it, vi } from 'vitest'
import { loadSlotAddons } from './usePluginSlots'

const addonFor = (slot: string) => ({ default: { slot, mount: () => () => {} } })

describe('loadSlotAddons', () => {
  it('returns only addons whose module targets the requested slot', async () => {
    const fetchPlugins = vi.fn().mockResolvedValue([
      { id: 'voice-whisper', capabilities: ['route_extension'] },
      { id: 'other', capabilities: ['route_extension'] },
      { id: 'auth', capabilities: ['auth_provider'] },
    ])
    const importAddon = vi.fn(async (url: string) => {
      if (url === '/api/settings/plugins/voice-whisper/addon.js')
        return addonFor('refinement-input-addon')
      if (url === '/api/settings/plugins/other/addon.js')
        return addonFor('some-other-slot')
      throw new Error('404')
    })

    const addons = await loadSlotAddons('refinement-input-addon', { fetchPlugins, importAddon })

    expect(addons).toHaveLength(1)
    expect(addons[0].slot).toBe('refinement-input-addon')
    // auth_provider plugin is never imported
    expect(importAddon).not.toHaveBeenCalledWith('/api/settings/plugins/auth/addon.js')
  })

  it('skips plugins whose addon.js import fails', async () => {
    const fetchPlugins = vi.fn().mockResolvedValue([
      { id: 'no-addon', capabilities: ['route_extension'] },
    ])
    const importAddon = vi.fn().mockRejectedValue(new Error('404'))

    const addons = await loadSlotAddons('refinement-input-addon', { fetchPlugins, importAddon })

    expect(addons).toEqual([])
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test src/composables/usePluginSlots.test.ts`
Expected: FAIL — `loadSlotAddons` is not defined.

- [ ] **Step 3: Write minimal implementation**

```ts
// src/composables/usePluginSlots.ts
import type { SlotAddon, SlotAddonModule } from '../utils/pluginSlot'

interface PluginInfo {
  id: string
  capabilities: string[]
}

interface LoadDeps {
  fetchPlugins?: () => Promise<PluginInfo[]>
  importAddon?: (url: string) => Promise<SlotAddonModule>
}

async function defaultFetchPlugins(): Promise<PluginInfo[]> {
  const res = await fetch('/api/settings/plugins')
  if (!res.ok)
    return []
  return res.json()
}

// `@vite-ignore` keeps Vite from trying to resolve the plugin URL at build time —
// it is served at runtime by the plugin process via the dashboard reverse proxy.
function defaultImportAddon(url: string): Promise<SlotAddonModule> {
  return import(/* @vite-ignore */ url)
}

/**
 * Discover route_extension plugins that provide a FE addon for `slot`.
 * Security: only plugins enumerated by `/api/settings/plugins` (registry-discovered,
 * health-checked) are imported — never an arbitrary URL.
 */
export async function loadSlotAddons(slot: string, deps: LoadDeps = {}): Promise<SlotAddon[]> {
  const fetchPlugins = deps.fetchPlugins ?? defaultFetchPlugins
  const importAddon = deps.importAddon ?? defaultImportAddon

  const plugins = await fetchPlugins()
  const candidates = plugins.filter(p => p.capabilities.includes('route_extension'))

  const addons: SlotAddon[] = []
  for (const p of candidates) {
    try {
      const mod = await importAddon(`/api/settings/plugins/${p.id}/addon.js`)
      if (mod.default?.slot === slot)
        addons.push(mod.default)
    }
    catch {
      // No addon.js (404) or bad module — skip this plugin, others continue.
    }
  }
  return addons
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test src/composables/usePluginSlots.test.ts`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add src/composables/usePluginSlots.ts src/composables/usePluginSlots.test.ts
git commit -m "feat(plugins): add slot addon loader with injectable deps"
```

## Task A3: PluginSlot component

**Files:**
- Create: `src/components/PluginSlot.vue`
- Test: `src/components/PluginSlot.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// src/components/PluginSlot.test.ts
import type { SlotAddon, SlotContext } from '../utils/pluginSlot'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import PluginSlot from './PluginSlot.vue'

function fakeCtx(): SlotContext {
  return { insertText: vi.fn(), setBusy: vi.fn() }
}

describe('PluginSlot', () => {
  it('mounts each addon into its own host element and unmounts on teardown', async () => {
    const unmount = vi.fn()
    const mountFn = vi.fn((el: HTMLElement) => {
      el.textContent = 'mic'
      return unmount
    })
    const addon: SlotAddon = { slot: 'refinement-input-addon', mount: mountFn }
    const loader = vi.fn().mockResolvedValue([addon])

    const ctx = fakeCtx()
    const wrapper = mount(PluginSlot, {
      props: { name: 'refinement-input-addon', ctx, loader },
    })
    await flushPromises()

    expect(loader).toHaveBeenCalledWith('refinement-input-addon')
    expect(mountFn).toHaveBeenCalledTimes(1)
    // addon received a real host element and the slot context
    expect(mountFn.mock.calls[0][0]).toBeInstanceOf(HTMLElement)
    expect(mountFn.mock.calls[0][1]).toBe(ctx)
    expect(wrapper.text()).toContain('mic')

    wrapper.unmount()
    expect(unmount).toHaveBeenCalledTimes(1)
  })

  it('renders nothing when no addons target the slot', async () => {
    const loader = vi.fn().mockResolvedValue([])
    const wrapper = mount(PluginSlot, {
      props: { name: 'refinement-input-addon', ctx: fakeCtx(), loader },
    })
    await flushPromises()
    expect(wrapper.find('[data-addon-host]').exists()).toBe(false)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test src/components/PluginSlot.test.ts`
Expected: FAIL — cannot resolve `./PluginSlot.vue`.

- [ ] **Step 3: Write minimal implementation**

```vue
<!-- src/components/PluginSlot.vue -->
<script setup lang="ts">
import type { SlotAddon, SlotContext, UnmountFn } from '../utils/pluginSlot'
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { loadSlotAddons } from '../composables/usePluginSlots'

const props = withDefaults(defineProps<{
  name: string
  ctx: SlotContext
  // Injectable for tests; defaults to the real discovery loader.
  loader?: (slot: string) => Promise<SlotAddon[]>
}>(), {
  loader: loadSlotAddons,
})

const containerEl = ref<HTMLElement | null>(null)
const unmounts: UnmountFn[] = []

onMounted(async () => {
  const addons = await props.loader(props.name)
  const container = containerEl.value
  if (!container)
    return
  for (const addon of addons) {
    const host = document.createElement('div')
    host.setAttribute('data-addon-host', '')
    container.appendChild(host)
    unmounts.push(addon.mount(host, props.ctx))
  }
})

onBeforeUnmount(() => {
  for (const fn of unmounts) {
    try {
      fn()
    }
    catch {
      // Addon teardown failures must not break host unmount.
    }
  }
})
</script>

<template>
  <div ref="containerEl" class="contents" />
</template>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test src/components/PluginSlot.test.ts`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add src/components/PluginSlot.vue src/components/PluginSlot.test.ts
git commit -m "feat(plugins): add PluginSlot mount host component"
```

## Task A4: Wire the slot into RefinementChat

**Files:**
- Modify: `src/components/RefinementChat.vue` (script: after line 43; template: input bar at line 331)
- Test: `src/components/RefinementChat.slot.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// src/components/RefinementChat.slot.test.ts
import type { SlotAddon } from '../utils/pluginSlot'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import RefinementChat from './RefinementChat.vue'

// A fake addon that immediately drives the slot context, simulating a finished
// transcription writing into the refinement textarea.
function capturingAddon(text: string): SlotAddon {
  return {
    slot: 'refinement-input-addon',
    mount: (_el, ctx) => {
      ctx.insertText(text)
      return () => {}
    },
  }
}

describe('RefinementChat voice slot', () => {
  it('inserts addon text into the refinement textarea', async () => {
    const wrapper = mount(RefinementChat, {
      props: {
        open: true,
        task: { id: 't1', slug: 's', title: 'T', status: 'refinement' } as any,
        // RefinementChat forwards this to <PluginSlot :loader>
        slotLoader: async () => [capturingAddon('hello world')],
      },
    })
    await flushPromises()

    const textarea = wrapper.get('textarea[placeholder="Message..."]')
      .element as HTMLTextAreaElement
    expect(textarea.value).toContain('hello world')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test src/components/RefinementChat.slot.test.ts`
Expected: FAIL — `slotLoader` prop / slot not wired; textarea value empty.

- [ ] **Step 3: Add the slot wiring (script)**

In `src/components/RefinementChat.vue`, extend the props (currently line 8) and add `slotCtx` after the refs block (after line 43):

```ts
// Replace the existing defineProps (line 8) with:
const props = defineProps<{
  open: boolean
  task: PipelineTask | null
  // Optional: lets tests inject a fake slot loader. Production uses the default.
  slotLoader?: (slot: string) => Promise<import('../utils/pluginSlot').SlotAddon[]>
}>()
```

Add the import at the top alongside the other imports:

```ts
import PluginSlot from './PluginSlot.vue'
import type { SlotContext } from '../utils/pluginSlot'
```

Add after line 43 (`const pendingImages = ...`):

```ts
const slotBusy = ref(false)

// Voice-agnostic bridge handed to plugin addons. insertText appends into the same
// model the textarea is bound to (v-model="inputText"); setBusy drives a local flag.
const slotCtx: SlotContext = {
  insertText: (text: string) => {
    inputText.value += (inputText.value && !inputText.value.endsWith(' ') ? ' ' : '') + text
    void nextTick(autoResize)
  },
  setBusy: (busy: boolean) => {
    slotBusy.value = busy
  },
}
```

- [ ] **Step 4: Add the slot (template)**

In the input-bar `<div>` (line 331), insert the slot immediately after the attach-image `<button>` block (after line 339, before the file `<input>`):

```vue
        <PluginSlot
          name="refinement-input-addon"
          :ctx="slotCtx"
          :loader="props.slotLoader"
        />
```

> Note: `PluginSlot`'s `loader` prop has a default; passing `undefined` from `props.slotLoader` falls back to the real `loadSlotAddons`.

- [ ] **Step 5: Run test to verify it passes**

Run: `pnpm test src/components/RefinementChat.slot.test.ts`
Expected: PASS.

- [ ] **Step 6: Typecheck + full unit run**

Run: `pnpm typecheck && pnpm test`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add src/components/RefinementChat.vue src/components/RefinementChat.slot.test.ts
git commit -m "feat(refinement): mount refinement-input-addon plugin slot"
```

---

# Phase B — voice-whisper plugin (local, privacy-preserving)

Lives in `plugins/voice-whisper/`. Separate Go binary + vanilla `addon.js`. Depends on Phase A.

## Task B1: Manifest + Go module scaffold

**Files:**
- Create: `plugins/voice-whisper/plugin.json`
- Create: `plugins/voice-whisper/go.mod`

- [ ] **Step 1: Write the manifest**

```json
// plugins/voice-whisper/plugin.json
{
  "id": "voice-whisper",
  "version": "0.1.0",
  "capabilities": ["route_extension"],
  "addr": "127.0.0.1:19010",
  "command": ["./voice-whisper"],
  "env": ["VOICE_WHISPER_MODEL", "VOICE_WHISPER_BIN", "FFMPEG_BIN"]
}
```

- [ ] **Step 2: Init the Go module**

Run: `cd plugins/voice-whisper && GOWORK=off go mod init github.com/lx-wnk/agent-dashboard-plugin-voice-whisper`
Expected: creates `go.mod`. (Standalone module name matches the `plugins/github-oauth` convention; not added to `go.work`.)

- [ ] **Step 3: Commit**

```bash
git add plugins/voice-whisper/plugin.json plugins/voice-whisper/go.mod
git commit -m "feat(voice-whisper): add plugin manifest + go module"
```

## Task B2: HTTP server (health, addon.js, transcribe) with injectable transcriber

**Files:**
- Create: `plugins/voice-whisper/server.go`
- Create: `plugins/voice-whisper/addon.js`
- Test: `plugins/voice-whisper/server_test.go`

- [ ] **Step 1: Create a placeholder addon.js so embedding compiles**

```js
// plugins/voice-whisper/addon.js
// Replaced in Task B3. Placeholder so //go:embed has a target.
export default { slot: 'refinement-input-addon', mount() { return () => {} } }
```

- [ ] **Step 2: Write the failing test**

```go
// plugins/voice-whisper/server_test.go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeTranscriber struct{ text string }

func (f fakeTranscriber) Transcribe(_ context.Context, _ string) (string, error) {
	return f.text, nil
}

func TestHealth(t *testing.T) {
	srv := NewServer(fakeTranscriber{})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health = %d, want 200", rec.Code)
	}
}

func TestTranscribeReturnsText(t *testing.T) {
	srv := NewServer(fakeTranscriber{text: "hello world"})

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("audio", "clip.webm")
	fw.Write([]byte("fake-audio-bytes"))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/transcribe", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("transcribe = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Text != "hello world" {
		t.Fatalf("text = %q, want %q", out.Text, "hello world")
	}
}

func TestAddonJsServed(t *testing.T) {
	srv := NewServer(fakeTranscriber{})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/addon.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("addon.js = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/javascript" {
		t.Fatalf("content-type = %q, want text/javascript", ct)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd plugins/voice-whisper && go test ./...`
Expected: FAIL — `NewServer` undefined.

- [ ] **Step 4: Write the server**

```go
// plugins/voice-whisper/server.go
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"os"
)

//go:embed addon.js
var addonJS []byte

// Transcriber turns an audio file on disk into text. The real implementation
// (whisper.go) shells out to ffmpeg + whisper.cpp; tests inject a fake.
type Transcriber interface {
	Transcribe(ctx context.Context, audioPath string) (string, error)
}

func NewServer(t Transcriber) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/addon.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write(addonJS)
	})

	mux.HandleFunc("/transcribe", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		file, _, err := r.FormFile("audio")
		if err != nil {
			http.Error(w, "missing audio field", http.StatusBadRequest)
			return
		}
		defer file.Close()

		tmp, err := os.CreateTemp("", "voice-*.webm")
		if err != nil {
			http.Error(w, "temp file", http.StatusInternalServerError)
			return
		}
		defer os.Remove(tmp.Name())
		if _, err := io.Copy(tmp, file); err != nil {
			http.Error(w, "write audio", http.StatusInternalServerError)
			return
		}
		tmp.Close()

		text, err := t.Transcribe(r.Context(), tmp.Name())
		if err != nil {
			http.Error(w, "transcription failed", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"text": text})
	})

	return mux
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd plugins/voice-whisper && go test ./...`
Expected: PASS (3 tests).

- [ ] **Step 6: Commit**

```bash
git add plugins/voice-whisper/server.go plugins/voice-whisper/server_test.go plugins/voice-whisper/addon.js
git commit -m "feat(voice-whisper): http server with injectable transcriber"
```

## Task B3: Real transcriber (ffmpeg + whisper.cpp) + main entrypoint + real addon.js

**Files:**
- Create: `plugins/voice-whisper/whisper.go`
- Create: `plugins/voice-whisper/main.go`
- Modify: `plugins/voice-whisper/addon.js` (replace placeholder)

> The real transcriber shells out to external binaries; it is exercised manually /
> in integration, not in unit tests (the unit tests inject `fakeTranscriber`).

- [ ] **Step 1: Write the transcriber**

```go
// plugins/voice-whisper/whisper.go
package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// whisperCLI converts webm/opus → wav via ffmpeg, then runs whisper.cpp and reads
// the produced .txt. Binary + model paths come from env (see plugin.json env list).
type whisperCLI struct {
	ffmpegBin  string // default "ffmpeg"
	whisperBin string // VOICE_WHISPER_BIN
	modelPath  string // VOICE_WHISPER_MODEL
}

func newWhisperCLI() whisperCLI {
	return whisperCLI{
		ffmpegBin:  envOr("FFMPEG_BIN", "ffmpeg"),
		whisperBin: envOr("VOICE_WHISPER_BIN", "whisper-cli"),
		modelPath:  envOr("VOICE_WHISPER_MODEL", "models/ggml-base.en.bin"),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (c whisperCLI) Transcribe(ctx context.Context, audioPath string) (string, error) {
	wav := audioPath + ".wav"
	defer os.Remove(wav)
	// -ar 16000 -ac 1: whisper.cpp expects 16kHz mono.
	conv := exec.CommandContext(ctx, c.ffmpegBin, "-y", "-i", audioPath,
		"-ar", "16000", "-ac", "1", wav)
	if out, err := conv.CombinedOutput(); err != nil {
		return "", &cmdErr{"ffmpeg", out, err}
	}

	outBase := audioPath + ".out"
	// whisper.cpp: -otxt writes <outBase>.txt
	w := exec.CommandContext(ctx, c.whisperBin, "-m", c.modelPath, "-f", wav,
		"-otxt", "-of", outBase, "-nt")
	if out, err := w.CombinedOutput(); err != nil {
		return "", &cmdErr{"whisper", out, err}
	}
	txt, err := os.ReadFile(outBase + ".txt")
	os.Remove(outBase + ".txt")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(txt)), nil
}

type cmdErr struct {
	stage string
	out   []byte
	err   error
}

func (e *cmdErr) Error() string {
	return e.stage + ": " + e.err.Error() + ": " + string(e.out)
}
```

- [ ] **Step 2: Write main.go**

```go
// plugins/voice-whisper/main.go
package main

import (
	"log"
	"net/http"
)

func main() {
	srv := NewServer(newWhisperCLI())
	addr := "127.0.0.1:19010"
	log.Printf("voice-whisper listening on %s", addr)
	if err := http.ListenAndServe(addr, srv); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 3: Replace addon.js with the real mic UI**

```js
// plugins/voice-whisper/addon.js
// Vanilla ESM. Framework-neutral per the slot contract: exports default { slot, mount }.
const BASE = '/api/settings/plugins/voice-whisper'

export default {
  slot: 'refinement-input-addon',
  mount(el, ctx) {
    const btn = document.createElement('button')
    btn.type = 'button'
    btn.title = 'Dictate (local whisper)'
    btn.textContent = '🎙'
    btn.style.cssText = 'width:2.25rem;height:2.25rem;border-radius:0.75rem;cursor:pointer'

    let recorder = null
    let chunks = []

    async function start() {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      recorder = new MediaRecorder(stream)
      chunks = []
      recorder.ondataavailable = e => chunks.push(e.data)
      recorder.onstop = () => {
        stream.getTracks().forEach(t => t.stop())
        void send(new Blob(chunks, { type: 'audio/webm' }))
      }
      recorder.start()
      btn.textContent = '⏹'
      ctx.setBusy(true)
    }

    function stop() {
      recorder?.stop()
      recorder = null
      btn.textContent = '🎙'
    }

    async function send(blob) {
      try {
        const fd = new FormData()
        fd.append('audio', blob, 'clip.webm')
        const res = await fetch(`${BASE}/transcribe`, { method: 'POST', body: fd })
        if (!res.ok)
          throw new Error(`transcribe ${res.status}`)
        const { text } = await res.json()
        if (text)
          ctx.insertText(text)
      }
      catch (err) {
        btn.title = `Voice error: ${err.message}`
      }
      finally {
        ctx.setBusy(false)
      }
    }

    btn.addEventListener('click', () => (recorder ? stop() : void start()))
    el.appendChild(btn)

    return () => {
      stop()
      btn.remove()
    }
  },
}
```

- [ ] **Step 4: Build to verify it compiles**

Run: `cd plugins/voice-whisper && go build ./... && go test ./...`
Expected: build succeeds; 3 tests PASS (embedded addon.js now the real one).

- [ ] **Step 5: Commit**

```bash
git add plugins/voice-whisper/whisper.go plugins/voice-whisper/main.go plugins/voice-whisper/addon.js
git commit -m "feat(voice-whisper): ffmpeg+whisper transcriber, entrypoint, mic addon"
```

## Task B4: Setup README + end-to-end proxy verification

**Files:**
- Create: `plugins/voice-whisper/README.md`

- [ ] **Step 1: Write the README**

````markdown
# voice-whisper plugin

Local, on-device speech-to-text for the refinement chat. Audio never leaves the machine.

## Prerequisites
- `ffmpeg` on PATH (or set `FFMPEG_BIN`)
- whisper.cpp CLI (`whisper-cli`) on PATH (or set `VOICE_WHISPER_BIN`)
- A ggml model file; set `VOICE_WHISPER_MODEL` (default `models/ggml-base.en.bin`)

## Build
```bash
cd plugins/voice-whisper && go build -o voice-whisper .
```

## Install
Copy this directory into `DASHBOARD_PLUGIN_DIR` (each plugin = one subdir with
`plugin.json` + the built binary). Restart the dashboard. The mic button appears in
the refinement chat once the plugin's `/health` passes.

## Notes
- Listens on `127.0.0.1:19010` (loopback only — required by the registry).
- Reached by the dashboard via reverse proxy at `/api/settings/plugins/voice-whisper/*`.
````

- [ ] **Step 2: Manual end-to-end verification**

Run (after building both dashboard and plugin, with `DASHBOARD_PLUGIN_DIR` pointing at a dir containing `voice-whisper/`):

```bash
# 1. confirm dashboard discovered + proxies the plugin (JWT cookie required;
#    use the browser devtools network tab instead if curl auth is awkward)
curl -s -i http://127.0.0.1:13120/api/settings/plugins | grep voice-whisper
```

Expected: the plugin id `voice-whisper` appears in the JSON list. In the browser,
open a refinement chat → a 🎙 button is present next to the attach-image button →
clicking it records, and on stop the transcript lands in the textarea.

> **Contingency:** if the proxied path 404s even though the plugin is listed, the
> `route_extension` mount may be inert. Verify `server/internal/api/router.go:349-353`
> actually mounts the reverse proxy for `route_extension` plugins; if it is stubbed,
> wiring it is a prerequisite bug-fix task (mount `plugin.NewReverseProxy` under
> `/api/settings/plugins/{id}` inside the JWT-protected group).

- [ ] **Step 3: Commit**

```bash
git add plugins/voice-whisper/README.md
git commit -m "docs(voice-whisper): setup guide + e2e verification notes"
```

---

# Phase C — voice-webspeech plugin (browser STT)

Lives in `plugins/voice-webspeech/`. A trivial Go static server (serves `/health` +
`/addon.js`) because discovery is process-based; all transcription happens in the
browser. Depends on Phase A. Independent of Phase B.

## Task C1: Manifest + module

**Files:**
- Create: `plugins/voice-webspeech/plugin.json`
- Create: `plugins/voice-webspeech/go.mod`

- [ ] **Step 1: Write the manifest**

```json
// plugins/voice-webspeech/plugin.json
{
  "id": "voice-webspeech",
  "version": "0.1.0",
  "capabilities": ["route_extension"],
  "addr": "127.0.0.1:19011",
  "command": ["./voice-webspeech"],
  "env": []
}
```

- [ ] **Step 2: Init module**

Run: `cd plugins/voice-webspeech && GOWORK=off go mod init github.com/lx-wnk/agent-dashboard-plugin-voice-webspeech`
Expected: creates `go.mod`.

- [ ] **Step 3: Commit**

```bash
git add plugins/voice-webspeech/plugin.json plugins/voice-webspeech/go.mod
git commit -m "feat(voice-webspeech): add plugin manifest + go module"
```

## Task C2: Static server (health + addon.js)

**Files:**
- Create: `plugins/voice-webspeech/main.go`
- Create: `plugins/voice-webspeech/server.go`
- Create: `plugins/voice-webspeech/addon.js`
- Test: `plugins/voice-webspeech/server_test.go`

- [ ] **Step 1: Placeholder addon.js (so embed compiles)**

```js
// plugins/voice-webspeech/addon.js
// Replaced in Task C3. Placeholder for //go:embed.
export default { slot: 'refinement-input-addon', mount() { return () => {} } }
```

- [ ] **Step 2: Write the failing test**

```go
// plugins/voice-webspeech/server_test.go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	NewServer().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health = %d, want 200", rec.Code)
	}
}

func TestAddonJsServed(t *testing.T) {
	rec := httptest.NewRecorder()
	NewServer().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/addon.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("addon.js = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/javascript" {
		t.Fatalf("content-type = %q, want text/javascript", ct)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd plugins/voice-webspeech && go test ./...`
Expected: FAIL — `NewServer` undefined.

- [ ] **Step 4: Write the server**

```go
// plugins/voice-webspeech/server.go
package main

import (
	_ "embed"
	"net/http"
)

//go:embed addon.js
var addonJS []byte

func NewServer() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/addon.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write(addonJS)
	})
	return mux
}
```

```go
// plugins/voice-webspeech/main.go
package main

import (
	"log"
	"net/http"
)

func main() {
	addr := "127.0.0.1:19011"
	log.Printf("voice-webspeech listening on %s", addr)
	if err := http.ListenAndServe(addr, NewServer()); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd plugins/voice-webspeech && go test ./...`
Expected: PASS (2 tests).

- [ ] **Step 6: Commit**

```bash
git add plugins/voice-webspeech/server.go plugins/voice-webspeech/main.go plugins/voice-webspeech/server_test.go plugins/voice-webspeech/addon.js
git commit -m "feat(voice-webspeech): static server for health + addon.js"
```

## Task C3: Real addon.js (SpeechRecognition)

**Files:**
- Modify: `plugins/voice-webspeech/addon.js` (replace placeholder)

> Browser-API bound; no unit test. Verify manually in Chrome/Edge.

- [ ] **Step 1: Write the addon**

```js
// plugins/voice-webspeech/addon.js
// Vanilla ESM. Browser SpeechRecognition (Chrome/Edge). Audio is sent to the
// browser's speech engine (Google for Chrome) — off-device; labelled in the title.
export default {
  slot: 'refinement-input-addon',
  mount(el, ctx) {
    const SR = window.SpeechRecognition || window.webkitSpeechRecognition
    if (!SR)
      return () => {} // unsupported browser → render nothing

    const btn = document.createElement('button')
    btn.type = 'button'
    btn.title = 'Dictate (browser — audio sent to browser speech engine)'
    btn.textContent = '🎙'
    btn.style.cssText = 'width:2.25rem;height:2.25rem;border-radius:0.75rem;cursor:pointer'

    let rec = null

    function start() {
      rec = new SR()
      rec.interimResults = false
      rec.continuous = false
      rec.onresult = (e) => {
        const text = Array.from(e.results).map(r => r[0].transcript).join(' ').trim()
        if (text)
          ctx.insertText(text)
      }
      rec.onend = () => {
        rec = null
        btn.textContent = '🎙'
        ctx.setBusy(false)
      }
      rec.onerror = () => {
        btn.title = 'Voice error'
      }
      rec.start()
      btn.textContent = '⏹'
      ctx.setBusy(true)
    }

    function stop() {
      rec?.stop()
    }

    btn.addEventListener('click', () => (rec ? stop() : start()))
    el.appendChild(btn)

    return () => {
      stop()
      btn.remove()
    }
  },
}
```

- [ ] **Step 2: Build to confirm embed compiles**

Run: `cd plugins/voice-webspeech && go build ./... && go test ./...`
Expected: build succeeds; 2 tests PASS.

- [ ] **Step 3: Commit**

```bash
git add plugins/voice-webspeech/addon.js
git commit -m "feat(voice-webspeech): SpeechRecognition mic addon"
```

## Task C4: README

**Files:**
- Create: `plugins/voice-webspeech/README.md`

- [ ] **Step 1: Write the README**

````markdown
# voice-webspeech plugin

Browser-based speech-to-text for the refinement chat using the Web Speech API.

> **Privacy:** Chrome/Edge send captured audio to their cloud speech engine
> (Google for Chrome). Prefer `voice-whisper` if audio must stay on-device.

## Browser support
Chrome / Edge only. On unsupported browsers the mic button is not rendered.

## Build
```bash
cd plugins/voice-webspeech && go build -o voice-webspeech .
```

## Install
Copy this directory into `DASHBOARD_PLUGIN_DIR`, restart the dashboard. Listens on
`127.0.0.1:19011`; reached via `/api/settings/plugins/voice-webspeech/*`.
````

- [ ] **Step 2: Commit**

```bash
git add plugins/voice-webspeech/README.md
git commit -m "docs(voice-webspeech): setup guide"
```

---

## Final Verification

- [ ] `pnpm typecheck && pnpm test` — core seam green.
- [ ] `cd plugins/voice-whisper && go test ./... && go build ./...` — whisper green.
- [ ] `cd plugins/voice-webspeech && go test ./... && go build ./...` — webspeech green.
- [ ] Manual: install one plugin in `DASHBOARD_PLUGIN_DIR`, open a refinement chat, confirm the mic button appears and dictation fills the textarea; uninstall → button gone (core unaffected).
- [ ] Grep core for accidental voice coupling: `grep -ri "whisper\|speechrecognition\|mediarecorder\|voice" src/` should return ONLY generic slot infra (no engine-specific names). Expected: no matches in `src/`.
```
