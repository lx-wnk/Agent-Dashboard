<template>
  <div class="h-screen flex flex-col bg-app text-fg font-sans">
    <a
      href="#main-content"
      class="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-[9999] focus:px-4 focus:py-2 focus:bg-accent focus:text-white focus:rounded focus:text-sm focus:font-semibold"
    >Skip to main content</a>

    <div class="flex-1 flex min-h-0">
      <slot name="sidebar" />
      <div class="flex-1 flex flex-col min-w-0">
        <slot name="topbar" />
        <main id="main-content" tabindex="-1" class="flex-1 min-h-0 overflow-y-auto" style="scroll-margin-top: 48px">
          <slot />
        </main>
      </div>
    </div>

    <slot name="statusbar" />
  </div>
</template>
