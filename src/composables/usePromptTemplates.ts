import { ref } from 'vue'

export interface PromptTemplate {
  id: string
  name: string
  body: string
  createdAt: string
}

export function usePromptTemplates() {
  const templates = ref<PromptTemplate[]>([])

  async function fetch() {
    const res = await window.fetch('/api/prompt-templates')
    if (res.ok)
      templates.value = await res.json()
  }

  async function create(name: string, body: string) {
    const res = await window.fetch('/api/prompt-templates', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Origin': window.location.origin },
      body: JSON.stringify({ name, body }),
    })
    if (!res.ok)
      throw new Error(await res.text())
    await fetch()
  }

  async function remove(id: string) {
    await window.fetch(`/api/prompt-templates/${id}`, {
      method: 'DELETE',
      headers: { Origin: window.location.origin },
    })
    await fetch()
  }

  void fetch()

  return { templates, create, remove }
}
