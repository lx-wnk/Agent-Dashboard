<script setup lang="ts">
import { WIDGETS } from '@/features/workspace'

const COCKPIT = ['agents', 'pipeline', 'routines', 'memory', 'github'] as const
</script>

<template>
  <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3" data-testid="cockpit">
    <component :is="WIDGETS[id].component" v-for="id in COCKPIT" :key="id" />
  </div>
</template>
