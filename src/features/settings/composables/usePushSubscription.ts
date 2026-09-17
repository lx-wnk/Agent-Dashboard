import { computed, ref } from 'vue'
import { errorMessage, readErrorMessage } from '@/utils/errorMessage'

export type PushState = 'unknown' | 'subscribed' | 'not-subscribed' | 'denied' | 'error'

// Web Push VAPID keys are base64url (RFC 4648 §5); atob needs standard base64.
function urlBase64ToUint8Array(base64Url: string): Uint8Array<ArrayBuffer> {
  const base64 = (base64Url + '='.repeat((4 - base64Url.length % 4) % 4))
    .replace(/-/g, '+')
    .replace(/_/g, '/')
  const raw = atob(base64)
  const bytes = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++)
    bytes[i] = raw.charCodeAt(i)
  return bytes
}

async function fetchVapidKey(): Promise<string> {
  const getRes = await fetch('/api/settings/webpush/vapid')
  if (getRes.ok)
    return (await getRes.json() as { publicKey: string }).publicKey

  const postRes = await fetch('/api/settings/webpush/vapid', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({}),
  })
  if (!postRes.ok)
    throw new Error(await readErrorMessage(postRes, `HTTP ${postRes.status}`))
  return (await postRes.json() as { publicKey: string }).publicKey
}

export function usePushSubscription() {
  const supported = computed(() =>
    typeof navigator !== 'undefined' && 'serviceWorker' in navigator && typeof window !== 'undefined' && 'PushManager' in window,
  )
  const state = ref<PushState>('unknown')
  const error = ref<string | null>(null)

  async function refresh(): Promise<void> {
    if (!supported.value)
      return
    try {
      const registration = await navigator.serviceWorker.ready
      const subscription = await registration.pushManager.getSubscription()
      state.value = subscription ? 'subscribed' : 'not-subscribed'
    }
    catch (e) {
      error.value = errorMessage(e, 'Failed to check push subscription')
      state.value = 'error'
    }
  }

  async function enable(): Promise<void> {
    if (!supported.value)
      return
    error.value = null
    const permission = await Notification.requestPermission()
    if (permission !== 'granted') {
      state.value = permission === 'denied' ? 'denied' : 'error'
      return
    }
    try {
      const publicKey = await fetchVapidKey()
      const registration = await navigator.serviceWorker.ready
      const subscription = await registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlBase64ToUint8Array(publicKey),
      })
      const json = subscription.toJSON()
      const res = await fetch('/api/settings/webpush/subscribe', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          endpoint: json.endpoint,
          keys: { p256dh: json.keys?.p256dh, auth: json.keys?.auth },
        }),
      })
      if (!res.ok)
        throw new Error(await readErrorMessage(res, `HTTP ${res.status}`))
      state.value = 'subscribed'
    }
    catch (e) {
      error.value = errorMessage(e, 'Failed to subscribe to push notifications')
      state.value = 'error'
    }
  }

  return { supported, state, error, refresh, enable }
}
