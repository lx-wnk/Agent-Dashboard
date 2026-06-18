<script setup lang="ts">
defineProps<{
  for?: string
}>()
</script>

<template>
  <label
    :for="$props.for"
    class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5"
  ><slot /></label>
</template>
