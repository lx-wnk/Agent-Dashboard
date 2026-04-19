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
  background: #1e3a5f;
  border-bottom: 1px solid #1e4080;
  padding: 5px 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
}
.task-link-text {
  font-size: 11px;
  color: #93c5fd;
}
.task-link-text strong { color: #60a5fa; }
.task-link-btn {
  background: none;
  border: none;
  color: #60a5fa;
  font-size: 11px;
  cursor: pointer;
  text-decoration: underline;
  padding: 0;
  white-space: nowrap;
}
.task-link-btn:hover { color: #93c5fd; }
</style>
