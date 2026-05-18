import { onUnmounted, ref } from 'vue'

/**
 * useInstallPrompt — wraps the browser's beforeinstallprompt event.
 *
 * PWA-12: Captures the install prompt so we can show a custom "Install app"
 * button in the header rather than relying on the browser's ambient UI.
 *
 * canInstall is true when the browser has fired beforeinstallprompt and the
 * user has not yet dismissed or accepted it.
 *
 * promptInstall() shows the browser's native install dialog. Returns a
 * promise that resolves with the user's choice ('accepted' | 'dismissed').
 */
export function useInstallPrompt() {
  // The deferred prompt event captured from beforeinstallprompt.
  const deferredPrompt = ref<BeforeInstallPromptEvent | null>(null)
  const canInstall = ref(false)
  const installed = ref(false)

  function onBeforeInstallPrompt(e: Event) {
    // Prevent the mini-infobar from appearing on mobile.
    e.preventDefault()
    deferredPrompt.value = e as BeforeInstallPromptEvent
    canInstall.value = true
  }

  function onAppInstalled() {
    canInstall.value = false
    installed.value = true
    deferredPrompt.value = null
  }

  if (typeof window !== 'undefined') {
    window.addEventListener('beforeinstallprompt', onBeforeInstallPrompt)
    window.addEventListener('appinstalled', onAppInstalled)
  }

  onUnmounted(() => {
    if (typeof window !== 'undefined') {
      window.removeEventListener('beforeinstallprompt', onBeforeInstallPrompt)
      window.removeEventListener('appinstalled', onAppInstalled)
    }
  })

  async function promptInstall(): Promise<'accepted' | 'dismissed'> {
    const prompt = deferredPrompt.value
    if (!prompt)
      return 'dismissed'
    await prompt.prompt()
    const { outcome } = await prompt.userChoice
    deferredPrompt.value = null
    canInstall.value = false
    return outcome
  }

  return { canInstall, installed, promptInstall }
}

/**
 * BeforeInstallPromptEvent is not in the standard TypeScript lib —
 * declare it here so the file is self-contained.
 */
interface BeforeInstallPromptEvent extends Event {
  prompt(): Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}
