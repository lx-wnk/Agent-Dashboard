import { ref, watch } from 'vue'

export type Role = 'requester' | 'reviewer'

const stored = typeof localStorage !== 'undefined' ? localStorage.getItem('task-role') : null
const role = ref<Role>(stored === 'reviewer' ? 'reviewer' : 'requester')

watch(role, (val) => {
  if (typeof localStorage !== 'undefined')
    localStorage.setItem('task-role', val)
})

export function useRole() {
  function toggleRole() {
    role.value = role.value === 'requester' ? 'reviewer' : 'requester'
  }
  return { role, toggleRole }
}
