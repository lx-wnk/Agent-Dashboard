import antfu from '@antfu/eslint-config'

// Cross-feature imports must go through a feature's public barrel
// (`@/features/<name>` → its index.ts). Deep-importing another feature's
// internals (`@/features/<name>/components|composables/...`) is forbidden;
// same-feature imports and tests are exempt. Kept as a tiny local rule to avoid
// pulling in a full import-resolver plugin for a single boundary check.
const FEATURE_RE = /\/src\/features\/([^/]+)\//
const IMPORT_RE = /^@\/features\/([^/]+)(?:\/(.+))?$/

const featureBoundaryRule = {
  meta: {
    type: 'problem',
    docs: { description: 'Forbid deep imports across feature boundaries' },
    schema: [],
    messages: {
      deep: 'Cross-feature import: use the public barrel \'@/features/{{target}}\' instead of reaching into its internals.',
    },
  },
  create(context) {
    const filename = context.filename ?? context.getFilename()
    const own = filename.match(FEATURE_RE)?.[1]
    if (!own)
      return {}

    function check(node) {
      if (!node || node.type !== 'Literal' || typeof node.value !== 'string')
        return
      const m = node.value.match(IMPORT_RE)
      if (!m)
        return
      const [, target, rest] = m
      if (target === own)
        return // same feature — any depth allowed
      if (rest && rest !== 'index' && rest !== 'index.ts')
        context.report({ node, messageId: 'deep', data: { target } })
    }

    return {
      ImportDeclaration: node => check(node.source),
      ImportExpression: node => check(node.source),
      ExportNamedDeclaration: node => check(node.source),
      ExportAllDeclaration: node => check(node.source),
    }
  },
}

const featureBoundary = {
  name: 'boundary/feature-internals',
  files: ['src/features/**/*.{ts,mts,vue}'],
  ignores: ['**/*.test.ts', '**/*.spec.ts', '**/__tests__/**'],
  plugins: { boundary: { rules: { 'feature-internals': featureBoundaryRule } } },
  rules: { 'boundary/feature-internals': 'error' },
}

export default antfu({
  vue: true,
  typescript: true,
  ignores: ['docs/**'],
  rules: {
    // Prevent debug logging from leaking into the production bundle / user DevTools.
    // console.warn and console.error remain allowed for genuine diagnostics.
    'no-console': ['warn', { allow: ['warn', 'error'] }],
  },
}, featureBoundary)
