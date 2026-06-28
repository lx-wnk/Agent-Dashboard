# voice-whisper plugin

Local, on-device speech-to-text for the refinement chat. Audio never leaves the machine.

## Prerequisites
- `ffmpeg` on PATH (or set `FFMPEG_BIN`)
- whisper.cpp CLI (`whisper-cli`) on PATH (or set `VOICE_WHISPER_BIN`)
- A ggml model file; set `VOICE_WHISPER_MODEL`. **For any non-English language use a
  multilingual model** (e.g. `ggml-small.bin`, `ggml-medium.bin`) — the `*.en` models
  are English-only and will mistranscribe other languages. With `VOICE_WHISPER_LANG=auto`
  (default) whisper detects the language per clip.

## Build
```bash
cd plugins/voice-whisper && GOWORK=off go build -o voice-whisper .
```

## Install
Copy this directory into `DASHBOARD_PLUGIN_DIR` (each plugin = one subdir with
`plugin.json` + the built binary). Activate it via the Plugins settings panel (or
`POST /api/plugins/voice-whisper/activate`) — no restart required. The mic button
appears in the refinement chat once the plugin's `/health` passes.

## Environment
| Var | Default | Purpose |
|---|---|---|
| `VOICE_WHISPER_ADDR` | `127.0.0.1:19010` | Listen address (loopback only) |
| `FFMPEG_BIN` | `ffmpeg` | ffmpeg binary |
| `VOICE_WHISPER_BIN` | `whisper-cli` | whisper.cpp CLI binary |
| `VOICE_WHISPER_MODEL` | `models/ggml-base.en.bin` | ggml model path (use a multilingual model for non-English) |
| `VOICE_WHISPER_LANG` | `auto` | spoken-language hint passed to whisper `-l` (`auto` = detect; or `de`, `en`, …) |

## How it works
The dashboard reverse-proxies `/api/plugins/voice-whisper/proxy/*` to this process
(prefix stripped → the plugin sees `/health`, `/addon.js`, `/transcribe`). The browser
records audio via MediaRecorder, POSTs it to `/transcribe`; the plugin runs
ffmpeg→whisper.cpp locally and returns `{ "text": ... }`, which the mic addon inserts
into the refinement textarea.

## Notes
- Listens on loopback only (required by the plugin registry).
- Audio is processed entirely on-device; nothing is sent to any external service.
