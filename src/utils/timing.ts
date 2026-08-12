// UI timing constants (milliseconds).

// SpawnDialog auto-closes this long after a successful spawn if no error surfaces.
export const SPAWN_AUTOCLOSE_MS = 3_000

// Transient send-status badge ('sent' / 'error') clears itself after this delay.
export const SEND_STATUS_RESET_MS = 3_000

// `navigator.serviceWorker.ready` never settles on a host that registers no
// worker, so background-sync registration gives up after this long.
export const SW_READY_TIMEOUT_MS = 2_000
