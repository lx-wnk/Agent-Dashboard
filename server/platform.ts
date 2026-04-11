import { platform } from 'node:os'

export const IS_LINUX = platform() === 'linux'
