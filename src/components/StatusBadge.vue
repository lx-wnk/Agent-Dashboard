<template>
  <span class="status-badge" :class="status">
    <span class="dot"></span>
    <span class="label">{{ status }}</span>
  </span>
</template>

<script setup lang="ts">
defineProps<{
  status: 'active' | 'waiting' | 'idle' | 'completed'
}>()
</script>

<style scoped>
.status-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  text-transform: capitalize;
}

.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.active .dot { background: var(--accent-green); }
.active .label { color: var(--accent-green); }

.waiting .dot { background: var(--accent-yellow); }
.waiting .label { color: var(--accent-yellow); }

.idle .dot { background: var(--accent-gray); }
.idle .label { color: var(--accent-gray); }

.completed .dot { background: var(--accent-gray); }
.completed .label { color: var(--accent-gray); }
</style>
