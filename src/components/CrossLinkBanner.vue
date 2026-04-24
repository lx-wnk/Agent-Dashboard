<script setup lang="ts">
defineProps<{
  label: string
  targetName: string
  buttonText?: string
}>()

const emit = defineEmits<{
  click: []
}>()
</script>

<template>
  <div class="task-link-banner">
    <span class="task-link-text">
      ⬡ {{ label }}
      <strong>{{ targetName }}</strong>
    </span>
    <button class="task-link-btn" @click="emit('click')">
      {{ buttonText ?? 'Open →' }}
    </button>
  </div>
</template>

<style scoped>
.task-link-banner {
  background: color-mix(in srgb, var(--accent-blue) 12%, transparent);
  border-bottom: 1px solid color-mix(in srgb, var(--accent-blue) 30%, transparent);
  padding: 5px 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
}
.task-link-text {
  font-size: 11px;
  color: var(--text-secondary);
}
.task-link-text strong { color: var(--accent-blue); }
.task-link-btn {
  background: none;
  border: none;
  color: var(--accent-blue);
  font-size: 11px;
  cursor: pointer;
  text-decoration: underline;
  padding: 0;
  white-space: nowrap;
}
.task-link-btn:hover { color: var(--text-primary); }
</style>
