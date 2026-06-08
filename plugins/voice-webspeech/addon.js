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
