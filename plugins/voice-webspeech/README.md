# voice-webspeech plugin

Browser-based speech-to-text for the refinement chat using the Web Speech API.

> **Privacy:** Chrome/Edge send captured audio to their cloud speech engine
> (Google for Chrome). Prefer `voice-whisper` if audio must stay on-device.

## Browser support
Chrome / Edge only. On unsupported browsers the mic button is not rendered.

## Build
```bash
cd plugins/voice-webspeech && GOWORK=off go build -o voice-webspeech .
```

## Install
Copy this directory into `DASHBOARD_PLUGIN_DIR`, restart the dashboard. Listens on
`127.0.0.1:19011` (override via `VOICE_WEBSPEECH_ADDR`); reached via
`/api/settings/plugins/voice-webspeech/*`.

## How it works
The dashboard reverse-proxies `/api/settings/plugins/voice-webspeech/*` to this
process (prefix stripped → it serves `/health` + `/addon.js`). The browser loads
`addon.js`, which uses the Web Speech `SpeechRecognition` API to transcribe speech
client-side and inserts the result into the refinement textarea. No audio passes
through this plugin process — transcription is entirely browser-side.
