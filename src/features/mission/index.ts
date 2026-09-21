// LiveWorkWidget is exported because the workspace widget registry places it
// as a cross-feature widget, same as the cockpit panels.
export { default as LiveWorkWidget } from './components/LiveWorkWidget.vue'
export { default as NeedsYouQueue } from './components/NeedsYouQueue.vue'
export { useNeedsYouCount } from './composables/useNeedsYouCount'
