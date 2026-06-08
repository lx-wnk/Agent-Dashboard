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
