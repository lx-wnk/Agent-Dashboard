<template>
  <div class="app">
    <header class="app-header">
      <h1>Claude Agent Overview</h1>
    </header>
    <main>
      <p>Loading agents...</p>
    </main>
  </div>
</template>

<script setup lang="ts">
</script>

<style>
:root {
  --bg-primary: #0f172a;
  --bg-secondary: #1e293b;
  --bg-tertiary: #334155;
  --text-primary: #e2e8f0;
  --text-secondary: #94a3b8;
  --text-muted: #64748b;
  --accent-green: #4ade80;
  --accent-yellow: #facc15;
  --accent-gray: #64748b;
  --border: #1e293b;
}

* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  background: var(--bg-primary);
  color: var(--text-primary);
  min-height: 100vh;
}

.app-header {
  padding: 16px 24px;
  border-bottom: 1px solid var(--border);
}

.app-header h1 {
  font-size: 18px;
  font-weight: 600;
}

main {
  padding: 24px;
}
</style>
