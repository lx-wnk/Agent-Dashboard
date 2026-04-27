<script setup lang="ts">
import { onMounted, ref } from 'vue'

interface Remote { id: string; url: string; name: string | null; createdAt: string; connectionOk?: boolean }

const remotes = ref<Remote[]>([])
const form = ref({ url: '', name: '', bearerKey: '' })
const saving = ref(false)
const error = ref<string | null>(null)

async function load() {
  try {
    const res = await fetch('/api/remotes')
    if (res.ok)
      remotes.value = await res.json()
  }
  catch {}
}

async function add() {
  if (!form.value.url)
    return
  saving.value = true
  error.value = null
  try {
    const res = await fetch('/api/remotes', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        url: form.value.url,
        name: form.value.name || null,
        bearerKey: form.value.bearerKey || null,
      }),
    })
    if (!res.ok) {
      const data = await res.json() as { error: string }
      error.value = data.error
      return
    }
    const reg = await res.json() as Remote
    remotes.value.push(reg)
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
  await fetch(`/api/remotes/${id}`, { method: 'DELETE' })
  remotes.value = remotes.value.filter(r => r.id !== id)
}

onMounted(load)
</script>

<template>
  <div class="flex flex-col gap-4">
    <h3 class="text-sm font-semibold text-text-primary">
      Meine lokalen Dashboard-Instanzen
    </h3>
    <p class="text-xs text-text-muted">
      Registriere deine lokale Dashboard-Instanz, damit deine lokalen Claude-Sessions hier angezeigt werden. Die lokale Instanz muss über Netzwerk erreichbar sein.
    </p>

    <div v-if="remotes.length" class="flex flex-col gap-2">
      <div
        v-for="r in remotes"
        :key="r.id"
        class="flex items-center justify-between p-3 rounded-lg border border-border-subtle bg-bg-surface text-sm"
      >
        <div>
          <div class="font-medium text-text-primary">{{ r.name ?? r.url }}</div>
          <div v-if="r.name" class="text-xs text-text-muted">{{ r.url }}</div>
          <span
            v-if="r.connectionOk !== undefined"
            class="text-xs"
            :class="r.connectionOk ? 'text-green-500' : 'text-red-500'"
          >
            {{ r.connectionOk ? '● Verbunden' : '● Nicht erreichbar' }}
          </span>
        </div>
        <button
          class="text-xs text-red-400 hover:text-red-300 transition-colors"
          @click="remove(r.id)"
        >
          Entfernen
        </button>
      </div>
    </div>
    <p v-else class="text-xs text-text-muted">
      Keine Registrierungen.
    </p>

    <form class="flex flex-col gap-2" @submit.prevent="add">
      <input
        v-model="form.url"
        type="url"
        placeholder="http://192.168.1.5:13120"
        required
        class="input-field text-sm"
      />
      <input
        v-model="form.name"
        type="text"
        placeholder="Name (z.B. MacBook)"
        class="input-field text-sm"
      />
      <input
        v-model="form.bearerKey"
        type="password"
        placeholder="DASHBOARD_API_TOKEN (optional)"
        class="input-field text-sm"
      />
      <p v-if="error" class="text-xs text-red-400">{{ error }}</p>
      <button type="submit" :disabled="saving" class="btn-primary text-sm self-start">
        {{ saving ? 'Wird gespeichert…' : 'Hinzufügen & testen' }}
      </button>
    </form>
  </div>
</template>
