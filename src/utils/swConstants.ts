export const BACKGROUND_SYNC_TAG = 'replay-agent-messages'
export const SW_MSG_MESSAGES_REPLAYED = 'MESSAGES_REPLAYED'
// Posted by the page (usePWA.updateSW) to tell a waiting service worker to
// activate immediately; the SW must handle it or prompt-mode updates never apply.
export const SW_MSG_SKIP_WAITING = 'SKIP_WAITING'
