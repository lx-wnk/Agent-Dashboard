<script setup lang="ts">
withDefaults(defineProps<{
  title: string
  id?: string
  showClose?: boolean
}>(), { showClose: true })

const emit = defineEmits<{ close: [] }>()
</script>

<template>
  <header class="flex items-center gap-2.5 px-5 py-4 border-b border-line shrink-0">
    <h2
      :id="id"
      class="text-xl font-semibold text-fg m-0 whitespace-nowrap"
    >
      {{ title }}
    </h2>
    <slot />
    <button
      v-if="showClose"
      type="button"
      aria-label="Close"
      class="ml-auto bg-transparent border-0 text-fg-mute cursor-pointer text-xl leading-none p-0.5 rounded hover:text-fg transition-colors"
      @click="emit('close')"
    >
      &#x2715;
    </button>
  </header>
</template>
