<script setup lang="ts">
import { ref } from 'vue'
import { useRemotes } from '../composables/useRemotes'

const { remotes, error, addRemote, removeRemote } = useRemotes()

const form = ref({ url: '', name: '', bearerKey: '' })
const saving = ref(false)

async function add() {
  if (!form.value.url)
    return
  saving.value = true
  error.value = null
  try {
    await addRemote(form.value.url, form.value.name || null, form.value.bearerKey || null)
    form.value = { url: '', name: '', bearerKey: '' }
  }
  catch (e) {
    error.value = (e as Error).message
  }
  finally {
    saving.value = false
  }
}

async function remove(id: string) {
  await removeRemote(id)
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <h3 class="text-sm font-semibold text-text-primary">
      My Local Dashboard Instances
    </h3>
    <p class="text-xs text-text-muted">
      Register your local dashboard instance so your local Claude sessions show up here. The local instance must be reachable over the network.
    </p>

    <div v-if="remotes.length" class="flex flex-col gap-2">
      <div
        v-for="r in remotes"
        :key="r.id"
        class="flex items-center justify-between p-3 rounded-lg border border-border-subtle bg-bg-surface text-sm"
      >
        <div>
          <div class="font-medium text-text-primary">
            {{ r.name ?? r.url }}
          </div>
          <div v-if="r.name" class="text-xs text-text-muted">
            {{ r.url }}
          </div>
          <span
            v-if="r.connectionOk !== undefined"
            class="text-xs"
            :class="r.connectionOk ? 'text-green-500' : 'text-red-500'"
          >
            {{ r.connectionOk ? '● Connected' : '● Unreachable' }}
          </span>
        </div>
        <button
          class="text-xs text-red-400 hover:text-red-300 transition-colors"
          @click="remove(r.id)"
        >
          Remove
        </button>
      </div>
    </div>
    <p v-else class="text-xs text-text-muted">
      No registrations.
    </p>

    <form class="flex flex-col gap-2" @submit.prevent="add">
      <input
        v-model="form.url"
        type="url"
        placeholder="http://192.168.1.5:13120"
        required
        class="input-field text-sm"
      >
      <input
        v-model="form.name"
        type="text"
        placeholder="Name (e.g. MacBook)"
        class="input-field text-sm"
      >
      <input
        v-model="form.bearerKey"
        type="password"
        placeholder="DASHBOARD_API_TOKEN (optional)"
        class="input-field text-sm"
      >
      <p v-if="error" class="text-xs text-red-400">
        {{ error }}
      </p>
      <button type="submit" :disabled="saving" class="btn-primary text-sm self-start">
        {{ saving ? 'Saving…' : 'Add & test' }}
      </button>
    </form>
  </div>
</template>
