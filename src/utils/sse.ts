export const SSE_RETRY_DELAY_MS = 30_000

// Safety-net poll cadence after a permanent SSE drop. SSE is the primary live
// channel; this catches missed events from short drops (HMR restarts, network
// blips) and backend paths that mutate without broadcasting.
export const SSE_FALLBACK_POLL_MS = 60_000

// Agent stream poll cadence — agents update frequently, so the fallback polls
// faster than the generic resource cadence.
export const AGENTS_POLL_MS = 3_000

export const SPAWN_STATUS_POLL_MS = 2_000
export const CHAT_REFRESH_MS = 5_000

// Cadence for polling /api/system/health while waiting for the server to come
// back after a restart (SP3 reconnect overlay).
export const RECONNECT_POLL_MS = 1_500
