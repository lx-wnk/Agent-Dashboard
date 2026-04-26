# UI/UX Tailwind Refactor — Design Spec

**Date:** 2026-04-25  
**Branch:** feat/ui_ux-improvement  
**Status:** Approved

---

## Overview

Vollständige Migration des Frontends von handgeschriebenem CSS mit CSS-Custom-Properties auf **Tailwind CSS v4** mit einer einheitlichen GitHub-Style-Dark-Ästhetik und vollständigem Light/Dark-Mode-Support.

**Scope:** Alle 27 Vue-Komponenten + App.vue werden in einem Big-Bang-Refactor umgeschrieben. Keine neuen Features — nur Design-Konsistenz und Code-Qualität.

---

## Design Direction

**Stil:** GitHub-Style Dark — poliert, developer-native, klare visuelle Hierarchie. Subtile Gradienten auf Cards, abgerundete Status-Badges, hoher Kontrast.

**Anforderung:** Vollständiger Light- und Dark-Mode mit manuellem Toggle (bestehende `useTheme.ts`-Logik bleibt).

---

## Design Tokens & Farbsystem

### Basis: Tailwind's Slate-Palette (keine Custom-Colors nötig)

**Dark Mode:**
| Token | Tailwind-Klasse | Hex |
|-------|----------------|-----|
| bg-base | `bg-slate-950` | #020817 |
| bg-surface | `bg-slate-900` | #0f172a |
| bg-elevated | `bg-slate-800` | #1e293b |
| bg-muted | `bg-slate-700` | #334155 |
| border | `border-slate-700/50` | — |
| card | `bg-gradient-to-br from-slate-800 to-slate-900` | — |
| text-primary | `text-slate-100` | #f1f5f9 |
| text-secondary | `text-slate-400` | #94a3b8 |
| text-muted | `text-slate-600` | #475569 |

**Light Mode (via `dark:` prefix invertiert):**
| Token | Tailwind-Klasse |
|-------|----------------|
| bg-base | `bg-slate-50` |
| bg-surface | `bg-white` |
| bg-elevated | `bg-slate-100` |
| bg-muted | `bg-slate-200` |
| border | `border-slate-200` |
| card | `bg-white shadow-sm border border-slate-200` |
| text-primary | `text-slate-900` |
| text-secondary | `text-slate-500` |
| text-muted | `text-slate-400` |

**Semantische Akzentfarben (Dark + Light identisch):**
| Bedeutung | Text | Background | Border |
|-----------|------|-----------|--------|
| active/success | `text-green-400` | `bg-green-950` | `border-green-800` |
| waiting/warning | `text-yellow-400` | `bg-yellow-950` | `border-yellow-800` |
| info/accent | `text-blue-400` | `bg-blue-950` | `border-blue-800` |
| error/danger | `text-red-400` | `bg-red-950` | `border-red-800` |
| idle/neutral | `text-slate-400` | `bg-slate-800` | `border-slate-700` |

---

## Tailwind v4 Setup

### Installation
```bash
pnpm add -D tailwindcss @tailwindcss/vite
```

### vite.config.ts
```ts
import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [tailwindcss(), vue()],
  // ... bestehende Proxy-Config bleibt
})
```

### src/styles/main.css (neu)
```css
@import "tailwindcss";

/* Dark mode via class="dark" auf <html> */
@custom-variant dark (&:where(.dark, .dark *));

@theme {
  --font-mono: 'SF Mono', 'Fira Code', 'Cascadia Code', 'Menlo', monospace;
  --radius-card: 0.75rem;
  --radius-badge: 9999px;
}

/* Basis-Reset (bleibt minimal — Tailwind's Preflight übernimmt den Rest) */
:root {
  scrollbar-width: thin;
  scrollbar-color: var(--color-slate-700) transparent;
}
```

### src/main.ts
```ts
import './styles/main.css'  // ersetzt alle CSS-Variablen in App.vue
```

### Dark Mode: useTheme.ts (minimale Änderung)

Nur die `applyTheme`-Funktion ändert sich:
```ts
// VORHER:
function applyTheme(t: Theme) {
  document.documentElement.setAttribute('data-theme', t)
}

// NACHHER:
function applyTheme(t: Theme) {
  document.documentElement.classList.toggle('dark', t === 'dark')
}
```

Die FOUC-Prevention in `index.html` wird entsprechend angepasst (deckt `system`-Default ab):
```html
<script>
  (function() {
    var stored = localStorage.getItem('agent-theme');
    var isDark = stored === 'dark'
      || (stored !== 'light' && !window.matchMedia('(prefers-color-scheme: light)').matches);
    if (isDark) document.documentElement.classList.add('dark');
  })();
</script>
```

---

## Komponenten-Architektur

### Ansatz: Vue Wrapper Components (Option C)

Primitive UI-Bausteine in `src/components/ui/` kapseln **nur Aussehen** (keine Business-Logik, kein State). Feature-Komponenten in `src/components/` kapseln **nur Logik** — kein `<style>`-Block mehr.

Kein `@apply` — alle Tailwind-Klassen leben im Template der jeweiligen UI-Komponente.

### Neue Dateistruktur
```
src/
  styles/
    main.css              ← @import tailwindcss + @theme + @custom-variant
  components/
    ui/                   ← NEU: primitive Wrapper
      AppBadge.vue
      AppButton.vue
      AppCard.vue
      AppModal.vue
      AppInput.vue
    AgentCard.vue         ← bestehend, wird auf AppCard/AppBadge umgestellt
    AgentRow.vue          ← bestehend, Style-Block wird entfernt
    ... (alle 25 weiteren bestehenden Komponenten)
```

### Die 5 primitiven Komponenten

**AppBadge.vue** — Status-Badges mit `variant` Prop
- Varianten: `active | waiting | idle | error | info`
- Jede Variante hat passende `text-*`, `bg-*`, `border-*` Klassen für Dark + Light
- Ersetzt: `StatusBadge.vue` (wird nach Migration gelöscht)

**AppButton.vue** — Buttons mit `variant` und `size` Props
- Varianten: `primary` (grün), `secondary` (slate-800 mit Border), `ghost` (transparent)
- Größen: `sm`, `md` (default)
- Kein Icon-Slot nötig — bei reinen Icon-Buttons wird `ghost` + Emoji/Zeichen verwendet

**AppCard.vue** — Card-Wrapper mit Slot
- Dark: `bg-gradient-to-br from-slate-800 to-slate-900 border border-slate-700/50 rounded-xl`
- Light: `bg-white border border-slate-200 rounded-xl shadow-sm`
- Optionaler `padding` Prop (default: `p-4`)

**AppModal.vue** — Modal-Wrapper (ersetzt BaseModal.vue)
- Backdrop: `fixed inset-0 bg-black/60 backdrop-blur-sm`
- Dialog: `bg-slate-900 dark:bg-slate-950 border border-slate-700 rounded-xl`
- Nutzt Vue `<Teleport to="body">`
- Emits: `close`
- Slots: `header`, `default`, `footer`

**AppInput.vue** — Textfeld und Textarea
- Typen: `input` (default), `textarea`
- Dark: `bg-slate-900 border-slate-700 text-slate-100 placeholder:text-slate-500`
- Light: `bg-white border-slate-200 text-slate-900 placeholder:text-slate-400`
- Focus-Ring: `focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500`

---

## Migrations-Reihenfolge

### Phase 1 — Setup & Grundkonfiguration
- `pnpm add -D tailwindcss @tailwindcss/vite`
- `vite.config.ts` — Tailwind-Plugin einhängen
- `src/styles/main.css` anlegen
- `src/main.ts` — CSS-Import hinzufügen
- `useTheme.ts` — `data-theme` → `class="dark"` (eine Zeile)
- `index.html` — FOUC-Script anpassen
- `App.vue` — gesamten `<style>`-Block entfernen (CSS-Variablen weg)

### Phase 2 — Primitive UI-Komponenten erstellen
Alle 5 neuen Komponenten in `src/components/ui/` schreiben:
`AppBadge` → `AppButton` → `AppCard` → `AppModal` → `AppInput`

### Phase 3 — Feature-Komponenten migrieren (innen nach außen)

**① Blatt-Komponenten (kein Kind-Component-Dependency):**
`StatusBadge` → `ToolTimeline` → `TaskList` → `SubAgentList` → `MachineBadge` → `CrossLinkBanner` → `CostTrend` → `ResourceBar`

**② Container & Views:**
`AgentRow` → `SubAgentRow` → `AgentCard` → `AgentCardGrid` → `AgentTable` → `TaskCard` → `KanbanBoard` → `PipelineBoard` → `StageOutputView` → `AgentChatStream` → `PromptInput`

**③ Modals & komplexe Dialoge:**
`AgentModal` → `TaskModal` → `SpawnDialog` → `BacklogForm` → `SessionList` → `ApiKeySettings`

### Phase 4 — App.vue & Cleanup
- `App.vue` Template auf Tailwind-Klassen umschreiben
- `BaseModal.vue` löschen (durch `AppModal.vue` ersetzt)
- `StatusBadge.vue` löschen (durch `AppBadge.vue` ersetzt)
- Prüfen: kein einziger `<style>`-Block mehr in Feature-Komponenten

---

## Was bleibt unverändert

- Alle TypeScript-Interfaces (`src/types.ts`)
- Alle Composables außer `useTheme.ts` (minimale Änderung)
- Alle Server-seitigen Dateien (`server/`)
- Alle Tests (`e2e/`, `vitest`)
- `src/utils/format.ts`
- Die Vue-Template-Struktur (`v-if`, `v-for`, Events) der bestehenden Komponenten — nur `class`-Attribute und `<style>`-Blöcke werden ersetzt

---

## Erfolgskriterien

1. Kein einziger manueller CSS-`<style>`-Block in Feature-Komponenten
2. Kein `var(--*)` mehr im Frontend-Code
3. Dark/Light-Toggle funktioniert in allen 27 Komponenten
4. `pnpm build` läuft ohne Fehler durch
5. Tailwind's CSS-Output ist signifikant kleiner als der bisherige CSS-Block
