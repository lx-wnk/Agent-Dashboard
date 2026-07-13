<script setup lang="ts">
import { ref } from 'vue'

interface Column {
  key: string
  label: string
}

const props = defineProps<{
  caption: string
  columns: Column[]
  rows: Record<string, string | number>[]
}>()

const visible = ref(false)

function toggle() {
  visible.value = !visible.value
}
</script>

<template>
  <div class="chart-data-table">
    <button
      type="button"
      class="text-xs font-medium text-fg-mute hover:text-fg-soft underline underline-offset-2 focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:outline-none rounded"
      :aria-expanded="visible"
      @click="toggle"
    >
      {{ visible ? 'Hide data table' : 'Show data table' }}
    </button>
    <table :class="visible ? 'mt-2 text-xs w-full text-left' : 'sr-only'">
      <caption class="sr-only">
        {{ props.caption }}
      </caption>
      <thead>
        <tr>
          <th v-for="col in props.columns" :key="col.key" scope="col" class="pr-4 py-1 font-semibold text-fg-mute">
            {{ col.label }}
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="props.rows.length === 0">
          <td :colspan="props.columns.length" class="py-1 text-fg-mute">
            No data
          </td>
        </tr>
        <tr v-for="(row, index) in props.rows" :key="index">
          <td v-for="col in props.columns" :key="col.key" class="pr-4 py-1 text-fg">
            {{ row[col.key] }}
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
