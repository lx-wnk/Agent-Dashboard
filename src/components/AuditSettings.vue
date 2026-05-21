<script setup lang="ts">
import AuditLogTab from './AuditLogTab.vue'

defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()
</script>

<template>
  <Teleport to="body">
    <div
      v-if="open"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      @click.self="emit('close')"
    >
      <div class="bg-card rounded-xl shadow-2xl w-full max-w-3xl max-h-[80vh] flex flex-col overflow-hidden">
        <header class="px-5 py-3.5 border-b border-line flex items-center justify-between flex-shrink-0">
          <h2 class="text-sm font-semibold text-fg">
            Audit Log
          </h2>
          <button
            type="button"
            class="text-slate-400 hover:text-slate-600 dark:hover:text-slate-300 text-lg leading-none"
            @click="emit('close')"
          >
            &times;
          </button>
        </header>
        <div class="flex-1 overflow-y-auto p-5">
          <AuditLogTab :limit="100" />
        </div>
      </div>
    </div>
  </Teleport>
</template>
