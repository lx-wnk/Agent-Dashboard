<template>
  <Transition name="slide">
    <div v-if="agent" class="offcanvas-backdrop" @click.self="$emit('close')">
      <aside class="offcanvas">
        <header class="detail-header">
          <div class="header-top">
            <h2>{{ agent.projectName }}</h2>
            <button class="close-btn" @click="$emit('close')">&times;</button>
          </div>
          <div class="header-meta">
            <StatusBadge :status="agent.status" />
            <span class="meta-item">PID {{ agent.pid }}</span>
            <span class="meta-item badge" :class="agent.entrypoint">{{ agent.entrypoint }}</span>
          </div>
        </header>

        <div class="detail-body">
          <section class="section">
            <h4>Path</h4>
            <code class="path">{{ agent.projectPath }}</code>
          </section>

          <section class="section">
            <h4>Session</h4>
            <code class="session-id">{{ agent.sessionId }}</code>
          </section>

          <section class="section" v-if="agent.currentAction">
            <h4>Current Action</h4>
            <p class="current-action">{{ agent.currentAction }}</p>
          </section>

          <section class="section">
            <ToolTimeline :tools="agent.lastTools" />
          </section>

          <section class="section">
            <TaskList :tasks="agent.tasks" />
          </section>

          <section class="section">
            <SubAgentList :subagents="agent.subagents" />
          </section>
        </div>
      </aside>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import type { Agent } from '../types'
import StatusBadge from './StatusBadge.vue'
import ToolTimeline from './ToolTimeline.vue'
import TaskList from './TaskList.vue'
import SubAgentList from './SubAgentList.vue'

defineProps<{
  agent: Agent | null
}>()

defineEmits<{
  close: []
}>()
</script>

<style scoped>
.offcanvas-backdrop {
  position: fixed;
  inset: 0;
  z-index: 100;
}

.offcanvas {
  position: fixed;
  top: 0;
  right: 0;
  width: 420px;
  max-width: 90vw;
  height: 100vh;
  background: var(--bg-secondary);
  border-left: 1px solid var(--border);
  overflow-y: auto;
  box-shadow: -4px 0 24px rgba(0, 0, 0, 0.3);
}

.detail-header {
  padding: 16px 20px;
  border-bottom: 1px solid var(--border);
  position: sticky;
  top: 0;
  background: var(--bg-secondary);
  z-index: 1;
}

.header-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header-top h2 {
  font-size: 18px;
  font-weight: 600;
}

.close-btn {
  background: none;
  border: none;
  color: var(--text-muted);
  font-size: 24px;
  cursor: pointer;
  padding: 0 4px;
  line-height: 1;
}

.close-btn:hover {
  color: var(--text-primary);
}

.header-meta {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 8px;
}

.meta-item {
  font-size: 12px;
  color: var(--text-muted);
  font-family: monospace;
}

.badge {
  padding: 1px 6px;
  border-radius: 4px;
  background: var(--bg-tertiary);
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.badge.cli { color: var(--accent-green); }
.badge.desktop { color: #60a5fa; }

.detail-body {
  padding: 16px 20px;
}

.section {
  margin-bottom: 20px;
}

.section h4 {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted);
  margin-bottom: 6px;
}

.path, .session-id {
  font-size: 12px;
  color: var(--text-secondary);
  word-break: break-all;
  display: block;
  background: var(--bg-primary);
  padding: 6px 10px;
  border-radius: 4px;
}

.current-action {
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.4;
}

/* Slide transition */
.slide-enter-active,
.slide-leave-active {
  transition: opacity 0.2s ease;
}

.slide-enter-active .offcanvas,
.slide-leave-active .offcanvas {
  transition: transform 0.25s ease;
}

.slide-enter-from,
.slide-leave-to {
  opacity: 0;
}

.slide-enter-from .offcanvas,
.slide-leave-to .offcanvas {
  transform: translateX(100%);
}
</style>
