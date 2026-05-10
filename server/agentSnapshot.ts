import type { Agent } from '../src/types.js'

let snapshot: Agent[] = []

export function getSnapshot(): Agent[] { return snapshot }
export function updateSnapshot(agents: Agent[]): void { snapshot = agents }
