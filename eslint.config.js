import antfu from '@antfu/eslint-config'

export default antfu({
  vue: true,
  typescript: true,
  ignores: ['docs/**'],
  rules: {
    // Prevent debug logging from leaking into the production bundle / user DevTools.
    // console.warn and console.error remain allowed for genuine diagnostics.
    'no-console': ['warn', { allow: ['warn', 'error'] }],
  },
})
