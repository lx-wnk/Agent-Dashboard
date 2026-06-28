<script setup lang="ts">
import type { SettingField } from '../composables/usePluginSettings'
import { onMounted, reactive, ref } from 'vue'
import { errorMessage } from '../utils/errorMessage'

const props = defineProps<{
  pluginId: string
  getSettings: (id: string) => Promise<{ schema: SettingField[], values: Record<string, string> }>
  putSettings: (id: string, values: Record<string, string>) => Promise<void>
}>()

const SECRET_SENTINEL = '********'
const schema = ref<SettingField[]>([])
const model = reactive<Record<string, string>>({})
const initial = reactive<Record<string, string>>({})
const error = ref<string | null>(null)
const saving = ref(false)

onMounted(async () => {
  try {
    const { schema: s, values } = await props.getSettings(props.pluginId)
    schema.value = s
    for (const f of s) {
      model[f.key] = values[f.key] ?? ''
      initial[f.key] = values[f.key] ?? ''
    }
  }
  catch (e) {
    error.value = errorMessage(e, 'Failed to load settings')
  }
})

async function save() {
  saving.value = true
  error.value = null
  try {
    const changed: Record<string, string> = {}
    for (const f of schema.value) {
      // Skip secrets the user did not touch (still the masked sentinel).
      if (f.secret && model[f.key] === SECRET_SENTINEL)
        continue
      if (model[f.key] !== initial[f.key])
        changed[f.key] = model[f.key]
    }
    await props.putSettings(props.pluginId, changed)
    for (const k of Object.keys(changed))
      initial[k] = model[k]
  }
  catch (e) {
    error.value = errorMessage(e, 'Failed to save settings')
  }
  finally {
    saving.value = false
  }
}
</script>

<template>
  <form class="plugin-settings-form" @submit.prevent="save">
    <p v-if="error" class="plugin-settings-form__error">
      {{ error }}
    </p>
    <div v-for="f in schema" :key="f.key" class="plugin-settings-form__field">
      <label :for="`pf-${pluginId}-${f.key}`">{{ f.label }}</label>
      <select v-if="f.type === 'enum'" :id="`pf-${pluginId}-${f.key}`" v-model="model[f.key]" :data-field="f.key">
        <option v-for="opt in f.enum ?? []" :key="opt" :value="opt">
          {{ opt }}
        </option>
      </select>
      <input
        v-else-if="f.type === 'bool'"
        :id="`pf-${pluginId}-${f.key}`"
        v-model="model[f.key]"
        type="checkbox"
        :data-field="f.key"
        true-value="true"
        false-value="false"
      >
      <input
        v-else-if="f.type === 'int'"
        :id="`pf-${pluginId}-${f.key}`"
        v-model="model[f.key]"
        type="number"
        :data-field="f.key"
      >
      <input
        v-else
        :id="`pf-${pluginId}-${f.key}`"
        v-model="model[f.key]"
        :type="f.secret ? 'password' : 'text'"
        :data-field="f.key"
      >
    </div>
    <button type="button" :disabled="saving" data-action="save" @click="save">
      {{ saving ? 'Saving…' : 'Save' }}
    </button>
  </form>
</template>
