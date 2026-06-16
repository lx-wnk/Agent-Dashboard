<script setup lang="ts">
import AppCard from '../ui/AppCard.vue'
</script>

<template>
  <AppCard aria-hidden="true" surface="card" radius="lg" class="p-3 animate-pulse motion-reduce:animate-none">
    <div class="h-3.5 w-2/3 bg-raised rounded mb-2" />
    <div class="h-2.5 w-1/3 bg-raised rounded mb-4" />
    <div class="flex gap-2">
      <div class="h-4 w-12 bg-raised rounded" />
      <div class="h-4 w-16 bg-raised rounded" />
    </div>
  </AppCard>
</template>
