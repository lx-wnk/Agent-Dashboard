/**
 * The states a cockpit panel can be in. Exactly one is rendered at a time, by
 * CockpitPanel.vue, which is their single owner — a panel that collapsed two
 * of them (an unanswered request drawn as an empty list, a refusal drawn as
 * "nothing here") is the defect this type exists to prevent.
 *
 * - loading  — a request is in flight
 * - notAsked — no request was made, and the panel knows why (nothing configured)
 * - denied   — the server refused (HTTP 403), and named a reason
 * - empty    — the server answered, and the answer was nothing
 * - failed   — the request errored, which is not the same as being refused
 * - ready    — the content slot renders
 */
export const PANEL_STATES = ['loading', 'notAsked', 'denied', 'empty', 'failed', 'ready'] as const
export type PanelState = typeof PANEL_STATES[number]
