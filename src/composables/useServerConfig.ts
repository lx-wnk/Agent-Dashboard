import { readonly, ref } from 'vue'

const mcpServerName = ref('')
const mcpEndpoint = ref('')
const homedir = ref('')
const scriptPath = ref('')
const loaded = ref(false)

async function loadServerConfig(): Promise<void> {
  if (loaded.value)
    return
  try {
    const res = await fetch('/api/config')
    if (!res.ok)
      return
    const data = await res.json() as {
      mcpServerName?: string
      mcpEndpoint?: string
      homedir?: string
      scriptPath?: string
    }
    mcpServerName.value = data.mcpServerName ?? ''
    mcpEndpoint.value = data.mcpEndpoint ?? ''
    homedir.value = data.homedir ?? ''
    scriptPath.value = data.scriptPath ?? ''
  }
  catch {
    // leave refs at defaults — caller can retry or show degraded state
  }
  finally {
    loaded.value = true
  }
}

export function useServerConfig() {
  return {
    mcpServerName: readonly(mcpServerName),
    mcpEndpoint: readonly(mcpEndpoint),
    homedir: readonly(homedir),
    scriptPath: readonly(scriptPath),
    loaded: readonly(loaded),
    loadServerConfig,
  }
}
