// The cockpit's public surface. The panels are exported because they are
// placements of a shared contract (panelState.ts) rather than cockpit-only
// screens: mission control puts the same two in its right rail, and a second
// implementation of either would be a second set of five states to keep in
// step. Everything else in this feature stays internal.
export { default as GitHubPanel } from './components/GitHubPanel.vue'
export { default as MemoryPanel } from './components/MemoryPanel.vue'
export * from './panelState'
