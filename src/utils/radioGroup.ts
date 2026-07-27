let seq = 0

/**
 * Returns a document-unique `name` for a radio group.
 *
 * A radio `name` scopes a browser-native radio group across the WHOLE document,
 * not the component. Two component instances sharing a hard-coded name form one
 * group, so checking a radio in one silently unchecks the other's — and Vue
 * never repairs it, because the other component's bound `:checked` value did not
 * change, so there is nothing for it to patch. The selection is visually gone
 * while the component still believes it is set.
 *
 * Note this cannot live in `<script setup>`: that block is the setup function
 * body and runs per instance, so a counter declared there restarts at 0 for
 * every component. It also deliberately does not use Vue's `useId()`, whose
 * counter is per app instance — this page can host more than one app (plugin UI
 * slots mount their own).
 */
export function nextRadioGroupName(prefix: string): string {
  seq += 1
  return `${prefix}-${seq}`
}
