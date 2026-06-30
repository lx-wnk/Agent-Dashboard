// UI timing constants (milliseconds).

// SpawnDialog auto-closes this long after a successful spawn if no error surfaces.
export const SPAWN_AUTOCLOSE_MS = 3_000

// Transient send-status badge ('sent' / 'error') clears itself after this delay.
export const SEND_STATUS_RESET_MS = 3_000

// Window to confirm a question answer was registered by the live session.
// Covers parser cache TTL (3s) + SSE broadcast interval (3s) with margin.
export const ANSWER_CONFIRM_MS = 9_000
