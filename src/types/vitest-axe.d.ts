import type { AxeResults } from 'axe-core'
import 'vitest'

declare module 'vitest' {
  interface Assertion<T = any> {
    toHaveNoViolations: () => T
  }
  interface AsymmetricMatchersContaining {
    toHaveNoViolations: () => AxeResults
  }
}
