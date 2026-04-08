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
          <section class="current-action-section" v-if="agent.currentAction">
            <div class="current-action-badge">
              <span class="pulse-dot"></span>
              <span class="action-text">{{ agent.currentAction }}</span>
            </div>
          </section>

          <section class="section">
            <h4>Path</h4>
            <code class="path">{{ agent.projectPath }}</code>
          </section>

          <section class="section">
            <h4>Session</h4>
            <code class="session-id">{{ agent.sessionId }}</code>
          </section>

          <section class="section stats-grid">
            <div class="stat-item" v-if="agent.model">
              <span class="stat-label">Model</span>
              <span class="stat-value">{{ agent.model }}</span>
            </div>
            <div class="stat-item" v-if="agent.codeVersion">
              <span class="stat-label">Version</span>
              <span class="stat-value">{{ agent.codeVersion }}</span>
            </div>
            <div class="stat-item">
              <span class="stat-label">Turns</span>
              <span class="stat-value">{{ agent.conversationTurns }}</span>
            </div>
            <div class="stat-item">
              <span class="stat-label">Cost</span>
              <span class="stat-value cost">{{ formatCost(agent.costEstimate) }}</span>
            </div>
          </section>

          <section class="section" v-if="totalTokens > 0">
            <h4>Token Usage</h4>
            <div class="token-bar">
              <div class="bar-segment input" :style="{ width: pct(agent.tokenUsage.inputTokens) }"></div>
              <div class="bar-segment output" :style="{ width: pct(agent.tokenUsage.outputTokens) }"></div>
              <div class="bar-segment cache-read" :style="{ width: pct(agent.tokenUsage.cacheReadTokens) }"></div>
              <div class="bar-segment cache-create" :style="{ width: pct(agent.tokenUsage.cacheCreationTokens) }"></div>
            </div>
            <div class="token-legend">
              <span class="legend-item"><span class="dot input"></span>Input {{ formatTokens(agent.tokenUsage.inputTokens) }}</span>
              <span class="legend-item"><span class="dot output"></span>Output {{ formatTokens(agent.tokenUsage.outputTokens) }}</span>
              <span class="legend-item"><span class="dot cache-read"></span>Cache Read {{ formatTokens(agent.tokenUsage.cacheReadTokens) }}</span>
              <span class="legend-item"><span class="dot cache-create"></span>Cache Write {{ formatTokens(agent.tokenUsage.cacheCreationTokens) }}</span>
            </div>
          </section>

          <section class="section stats-grid" v-if="agent.meta">
            <div class="stat-item" v-if="agent.meta.linesAdded || agent.meta.linesRemoved">
              <span class="stat-label">Lines Changed</span>
              <span class="stat-value lines">
                <span class="added">+{{ agent.meta.linesAdded }}</span>
                <span class="removed">-{{ agent.meta.linesRemoved }}</span>
              </span>
            </div>
            <div class="stat-item" v-if="agent.meta.filesModified">
              <span class="stat-label">Files Modified</span>
              <span class="stat-value">{{ agent.meta.filesModified }}</span>
            </div>
            <div class="stat-item" v-if="agent.meta.gitCommits">
              <span class="stat-label">Git Commits</span>
              <span class="stat-value">{{ agent.meta.gitCommits }}</span>
            </div>
            <div class="stat-item" v-if="agent.meta.toolErrors">
              <span class="stat-label">Tool Errors</span>
              <span class="stat-value error-count">{{ agent.meta.toolErrors }}</span>
            </div>
          </section>

          <section class="section" v-if="agent.meta?.firstPrompt">
            <h4>First Prompt</h4>
            <p class="first-prompt">{{ agent.meta.firstPrompt }}</p>
          </section>

          <section class="section" v-if="agent">
            <ChannelPanel :agent="agent" />
          </section>

          <section class="section">
            <ToolTimeline :tools="agent.lastTools" />
          </section>

          <section class="section" v-if="topTools.length > 0">
            <h4>Tool Usage</h4>
            <div class="tool-counts">
              <div v-for="t in topTools" :key="t.name" class="tool-count-row">
                <span class="tool-count-name">{{ t.name }}</span>
                <div class="tool-count-bar-bg">
                  <div class="tool-count-bar" :style="{ width: (t.count / topTools[0].count * 100) + '%' }"></div>
                </div>
                <span class="tool-count-num">{{ t.count }}</span>
              </div>
            </div>
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
import { computed } from 'vue'
import type { Agent } from '../types'
import { formatTokens, formatCost } from '../utils/format'
import StatusBadge from './StatusBadge.vue'
import ToolTimeline from './ToolTimeline.vue'
import TaskList from './TaskList.vue'
import SubAgentList from './SubAgentList.vue'
import ChannelPanel from './ChannelPanel.vue'

const props = defineProps<{
  agent: Agent | null
}>()

defineEmits<{
  close: []
}>()

const totalTokens = computed(() => {
  if (!props.agent) return 0
  const u = props.agent.tokenUsage
  return u.inputTokens + u.outputTokens + u.cacheReadTokens + u.cacheCreationTokens
})

const topTools = computed(() => {
  if (!props.agent) return []
  return Object.entries(props.agent.toolCounts)
    .map(([name, count]) => ({ name, count }))
    .sort((a, b) => b.count - a.count)
    .slice(0, 10)
})

function pct(value: number): string {
  if (totalTokens.value === 0) return '0%'
  return (value / totalTokens.value * 100).toFixed(1) + '%'
}
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


.current-action-section {
  margin-bottom: 16px;
}

.current-action-badge {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  background: rgba(74, 222, 128, 0.08);
  border: 1px solid rgba(74, 222, 128, 0.2);
  border-radius: 6px;
  padding: 10px 12px;
}

.pulse-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--accent-green);
  flex-shrink: 0;
  margin-top: 4px;
  animation: pulse 2s ease-in-out infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 1; box-shadow: 0 0 0 0 rgba(74, 222, 128, 0.4); }
  50% { opacity: 0.7; box-shadow: 0 0 0 4px rgba(74, 222, 128, 0); }
}

.action-text {
  font-size: 13px;
  color: var(--text-primary);
  line-height: 1.4;
  word-break: break-word;
}

/* Stats grid */
.stats-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}

.stat-item {
  background: var(--bg-primary);
  border-radius: 6px;
  padding: 8px 10px;
}

.stat-label {
  display: block;
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted);
  margin-bottom: 2px;
}

.stat-value {
  font-size: 13px;
  color: var(--text-primary);
  font-family: monospace;
}

.stat-value.cost {
  color: var(--accent-green);
  font-weight: 600;
}

.stat-value.lines {
  display: flex;
  gap: 8px;
}

.stat-value .added { color: #4ade80; }
.stat-value .removed { color: #f87171; }

.stat-value.error-count { color: #f87171; }

.first-prompt {
  font-size: 12px;
  color: var(--text-secondary);
  line-height: 1.4;
  background: var(--bg-primary);
  padding: 8px 10px;
  border-radius: 4px;
  max-height: 80px;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
}

/* Token bar */
.token-bar {
  display: flex;
  height: 8px;
  border-radius: 4px;
  overflow: hidden;
  background: var(--bg-primary);
  margin-bottom: 8px;
}

.bar-segment { min-width: 2px; }
.bar-segment.input { background: #60a5fa; }
.bar-segment.output { background: #f472b6; }
.bar-segment.cache-read { background: #34d399; }
.bar-segment.cache-create { background: #fbbf24; }

.token-legend {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.legend-item {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  color: var(--text-muted);
}

.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.dot.input { background: #60a5fa; }
.dot.output { background: #f472b6; }
.dot.cache-read { background: #34d399; }
.dot.cache-create { background: #fbbf24; }

/* Tool counts */
.tool-count-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}

.tool-count-name {
  font-size: 11px;
  color: var(--text-secondary);
  font-family: monospace;
  width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex-shrink: 0;
}

.tool-count-bar-bg {
  flex: 1;
  height: 6px;
  background: var(--bg-primary);
  border-radius: 3px;
  overflow: hidden;
}

.tool-count-bar {
  height: 100%;
  background: #60a5fa;
  border-radius: 3px;
}

.tool-count-num {
  font-size: 11px;
  color: var(--text-muted);
  font-family: monospace;
  width: 30px;
  text-align: right;
  flex-shrink: 0;
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
