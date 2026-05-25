import { onMounted, ref } from 'vue'

export interface MemoryFile {
  path: string
  name: string
}

export function useMemory() {
  const files = ref<MemoryFile[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchFiles() {
    loading.value = true
    error.value = null
    try {
      const res = await fetch('/api/memory')
      if (res.ok) {
        const data = await res.json() as { files: MemoryFile[] }
        files.value = data.files
      }
      else {
        error.value = `Failed to load files (${res.status})`
      }
    }
    catch {
      error.value = 'Network error loading files.'
    }
    finally {
      loading.value = false
    }
  }

  async function fetchFileContent(path: string): Promise<string | null> {
    try {
      const res = await fetch(`/api/memory/${encodeURIComponent(path)}`)
      if (res.ok) {
        const data = await res.json() as { content: string }
        return data.content
      }
      error.value = `Failed to load file (${res.status})`
      return null
    }
    catch {
      error.value = 'Network error loading file.'
      return null
    }
  }

  async function saveFileContent(path: string, content: string): Promise<boolean> {
    try {
      const res = await fetch(`/api/memory/${encodeURIComponent(path)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content }),
      })
      if (res.ok)
        return true
      error.value = `Failed to save file (${res.status})`
      return false
    }
    catch {
      error.value = 'Network error saving file.'
      return false
    }
  }

  onMounted(fetchFiles)

  return {
    files,
    loading,
    error,
    refetch: fetchFiles,
    fetchFileContent,
    saveFileContent,
  }
}
