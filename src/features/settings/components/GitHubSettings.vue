<script setup lang="ts">
import type { SelectOption } from '@/components/ui/selectOption'
import type { SettingView } from '@/features/settings/composables/useSettings'
import { computed, onMounted, ref, watch } from 'vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import { toast } from '@/composables/useToast'
import { useSettings } from '@/features/settings/composables/useSettings'
import { errorMessage } from '@/utils/errorMessage'

const KEY_TOKEN = 'github.token'
const KEY_REPOS = 'github.repos'
const KEY_BASE_URL = 'github.baseURL'
const KEY_TOKEN_SOURCE = 'github.tokenSource'

interface GitHubFormState {
  token: string
  repos: string
  baseURL: string
  tokenSource: string
}

const { items, loading, refetch, update } = useSettings()

const form = ref<GitHubFormState>({ token: '', repos: '', baseURL: '', tokenSource: 'setting' })
const saving = ref(false)

// Seeded exactly once from whatever the server first reports — items updates
// again after every save (useSettings.update patches the array in place),
// and re-seeding on that would clobber whatever the user is mid-typing.
const seeded = ref(false)
watch(items, (list: SettingView[]) => {
  if (seeded.value)
    return
  const byKey = new Map(list.map(i => [i.key, i]))
  const token = byKey.get(KEY_TOKEN)
  if (!token)
    return
  form.value = {
    token: token.value,
    repos: byKey.get(KEY_REPOS)?.value ?? '',
    baseURL: byKey.get(KEY_BASE_URL)?.value ?? '',
    tokenSource: byKey.get(KEY_TOKEN_SOURCE)?.value || 'setting',
  }
  seeded.value = true
}, { immediate: true })

onMounted(refetch)

// github.token and github.repos are a required PAIR server-side
// (serverapp.buildGitHubClient) — some-but-not-all set is a state the server
// refuses to BOOT with, and the dashboard is what dies, so the save is
// blocked rather than warned about. Both empty stays allowed: that is the
// working "off" switch. github.baseURL is NOT part of the pair — it carries a
// registry default, so it is never unset.
// The project replaced native selects deliberately (see CHANGELOG, custom
// listbox): AppSelect keeps the keyboard and Escape behaviour those lost.
const TOKEN_SOURCE_OPTIONS: SelectOption<string>[] = [
  { value: 'setting', label: 'Stored here (personal access token)' },
  { value: 'gh-cli', label: 'From the GitHub CLI (gh auth token)' },
]

const fromGhCLI = computed(() => form.value.tokenSource === 'gh-cli')

// With the token coming from the GitHub CLI it is not a setting at all, so it
// cannot be the missing half: repositories alone decide whether the
// integration is on, and every state boots.
const pairComplete = computed(() => {
  if (fromGhCLI.value)
    return true
  const setCount = [form.value.token, form.value.repos].filter(v => v !== '').length
  return setCount === 0 || setCount === 2
})

async function save() {
  if (!pairComplete.value)
    return
  saving.value = true
  try {
    // github.token always reads back as the mask sentinel once it is set;
    // sending it back untouched is how the server knows to leave it alone,
    // and sending an empty string is how the user clears it — which is the
    // only way the pair gets back to all-empty, since the field can never
    // show the real token to leave behind.
    const pairs: Array<[string, string]> = [
      [KEY_TOKEN, form.value.token],
      [KEY_REPOS, form.value.repos],
      [KEY_BASE_URL, form.value.baseURL],
      [KEY_TOKEN_SOURCE, form.value.tokenSource],
    ]

    let applied: 'live' | 'restart' = 'live'
    for (const [key, value] of pairs) {
      const result = await update(key, value)
      if (result === 'restart')
        applied = 'restart'
    }
    toast.success(applied === 'restart' ? 'Saved — applies after a server restart.' : 'Saved.')
  }
  catch (e) {
    toast.error(errorMessage(e, 'Failed to save GitHub settings'))
  }
  finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <div>
      <h3 class="text-[17px] font-bold text-fg mb-1">
        GitHub
      </h3>
      <p class="text-xs text-fg-mute">
        Connect a fine-grained personal access token and the repositories the dashboard may read. Token and repositories apply after a server restart.
      </p>
    </div>

    <div v-if="loading" class="text-center py-12 text-fg-mute text-sm">
      Loading...
    </div>

    <template v-else>
      <div
        v-if="!pairComplete"
        data-testid="github-pair-warning"
        class="text-xs rounded-md px-3 py-2 bg-warning-soft text-warning-text"
      >
        Token and repositories are a required pair. With only one of them set the server <strong>refuses to start</strong> at the next restart, so saving is blocked: fill both in, or clear both (the token field included) to turn GitHub off.
      </div>

      <div class="grid grid-cols-1 gap-3 max-w-md">
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1">Token source</label>
          <AppSelect
            v-model="form.tokenSource"
            :options="TOKEN_SOURCE_OPTIONS"
            aria-label="GitHub token source"
            data-testid="github-token-source"
            class="w-full"
          />
          <p class="text-[11px] text-fg-mute mt-1">
            <template v-if="fromGhCLI">
              Read from <code>gh</code> at each start, so no token is stored here. The server refuses to start if <code>gh</code> is missing or logged out.
            </template>
            <template v-else>
              Stored encrypted in the dashboard's database.
            </template>
          </p>
        </div>
        <div v-if="!fromGhCLI">
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="github-token">Token</label>
          <input
            id="github-token"
            v-model="form.token"
            data-testid="github-token"
            type="password"
            autocomplete="off"
            placeholder="Fine-grained personal access token"
            class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
          >
        </div>
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="github-repos">Repositories</label>
          <input
            id="github-repos"
            v-model="form.repos"
            data-testid="github-repos"
            type="text"
            placeholder="owner/repo, owner/other-repo"
            class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
          >
        </div>
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="github-baseurl">Base URL</label>
          <input
            id="github-baseurl"
            v-model="form.baseURL"
            data-testid="github-baseurl"
            type="url"
            placeholder="https://api.github.com"
            class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
          >
        </div>
      </div>

      <div>
        <AppButton variant="info" data-testid="github-save" :disabled="saving || !pairComplete" @click="save">
          {{ saving ? 'Saving…' : 'Save' }}
        </AppButton>
      </div>
    </template>
  </div>
</template>
