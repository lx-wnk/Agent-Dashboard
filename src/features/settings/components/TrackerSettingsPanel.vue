<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { toast } from '@/composables/useToast'
import { errorMessage } from '@/utils/errorMessage'

const SECRET_SENTINEL = '********'

interface FieldDef {
  key: string
  label: string
  type: string
  secret: boolean
}

const schema = ref<FieldDef[]>([])
const model = reactive<Record<string, string>>({})
const initial = reactive<Record<string, string>>({})
const saving = ref(false)
const loading = ref(true)

onMounted(async () => {
  try {
    const res = await fetch('/api/tracker/settings', { credentials: 'same-origin' })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    const data = await res.json()
    schema.value = data.schema
    for (const f of data.schema) {
      model[f.key] = data.values[f.key] ?? ''
      initial[f.key] = data.values[f.key] ?? ''
    }
  }
  catch (e) {
    toast.error(errorMessage(e, 'Failed to load tracker settings'))
  }
  finally {
    loading.value = false
  }
})

async function save() {
  saving.value = true
  try {
    const changed: Record<string, string> = {}
    for (const f of schema.value) {
      if (f.secret && model[f.key] === SECRET_SENTINEL)
        continue
      if (model[f.key] !== initial[f.key])
        changed[f.key] = model[f.key]
    }
    if (Object.keys(changed).length === 0) {
      toast.info('No changes to save.')
      return
    }
    const res = await fetch('/api/tracker/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', 'Origin': globalThis.location.origin },
      body: JSON.stringify({ values: changed }),
    })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    for (const k of Object.keys(changed))
      initial[k] = model[k]
    toast.success('Tracker settings saved.')
  }
  catch (e) {
    toast.error(errorMessage(e, 'Failed to save tracker settings'))
  }
  finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="space-y-4">
    <p v-if="loading" class="text-fg-subtle text-sm">
      Loading…
    </p>
    <template v-else>
      <div v-for="f in schema" :key="f.key" class="flex flex-col gap-1">
        <label :for="`tracker-${f.key}`" class="text-sm font-medium text-fg">{{ f.label }}</label>
        <input
          :id="`tracker-${f.key}`"
          v-model="model[f.key]"
          :type="f.secret ? 'password' : (f.type === 'url' ? 'url' : 'text')"
          :data-field="f.key"
          class="w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2 focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
          autocomplete="off"
        >
      </div>
      <div class="flex justify-end">
        <button
          type="button"
          :disabled="saving"
          class="px-4 py-2 rounded bg-accent text-white text-sm disabled:opacity-50"
          data-action="save"
          @click="save"
        >
          {{ saving ? 'Saving…' : 'Save settings' }}
        </button>
      </div>
    </template>
  </div>
</template>
