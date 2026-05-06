# Concept: D3 Workflow Visualizations

## Summary

Eine neue Workflows-Ansicht in der SPA stellt vier interaktive D3.js-Visualisierungen bereit, die cross-session- und cross-task-Daten des Dashboards grafisch aufbereiten. Das Backend liefert aggregierte Rohdaten über vier neue REST-Endpunkte; die Visualisierungen laufen vollständig im Browser via D3 v7 und Vue 3 Composition API.

## Requirements

### Functional

- [ ] Neuer "Workflows"-View in der SPA (viewMode: 'workflows')
- [ ] Header-Button "Workflows" zwischen "Sessions" und Settings-Button
- [ ] Tab-Navigation innerhalb des Views: Sankey / DAG / Spawn-Tree / Matrix
- [ ] **Sankey-Diagramm:** Visualisiert den Tool-Execution-Flow einer Session — Nodes = Tool-Namen, Links = Übergänge zwischen aufeinanderfolgenden Tools pro Konversations-Turn
- [ ] **Pipeline-DAG:** Visualisiert den Stage-Verlauf aller Pipeline-Tasks — Nodes = stage_runs, Kanten = Stage-Übergänge; Nodes klickbar (öffnet TaskModal)
- [ ] **Spawn-Tree:** Collapsible Baumstruktur eines Parent-Agents mit seinen Subagents; Nodes klickbar (öffnet AgentModal)
- [ ] **Co-Occurrence-Matrix:** Heatmap, welche Tool-Paare häufig in denselben Sessions auftreten
- [ ] SessionId-Picker für Sankey und Spawn-Tree (Auswahl aus bekannten Sessions)
- [ ] Hover-Tooltips auf allen Diagrammen
- [ ] Dark-Mode-kompatibel (nutzt bestehende Slate/CSS-Variablen)

### Non-Functional

- Performance: Alle vier Endpunkte antworten unter 500 ms; Co-Occurrence mit 30s In-Memory-Cache
- Sicherheit: Keine neuen Auth-Bypass-Wege; Endpunkte unterliegen `requireApiToken` wie bestehende Routes
- Wartbarkeit: D3-Logik vollständig in Composables isoliert, kein DOM-Ownership-Konflikt mit Vue
- Skalierbarkeit: Sankey und Spawn-Tree sind sessionspezifisch (nutzen bestehenden 32 KB tail-read); DAG und Matrix sind global (reagieren auf DB-Wachstum)
- Kompatibilität: macOS und Linux (keine plattformspezifische Logik nötig)

## Solution Approaches

### Option A: Direktes D3 via useD3-Composable (empfohlen)

**Beschreibung:** D3 v7 core + `d3-sankey` + `d3-dag` werden direkt als npm-Dependencies installiert. Ein generischer `useD3(containerRef, renderFn)`-Composable kapselt Mount/Resize/Cleanup-Logik. Vue rendert nur einen `<svg>`-Wrapper; D3 übernimmt vollständigen DOM-Besitz des SVG-Inhalts.

**Pros:**
- Kein Wrapper-Overhead; volles D3-API verfügbar
- Pattern ist etablierter Vue-3-Standard (2025-Konsens)
- Volle TypeScript-Kontrolle über Datentransformationen
- Kein zusätzliches Framework-Risiko

**Cons:**
- Mehr Boilerplate pro Komponente als bei High-Level-Libraries
- D3-DOM-Ownership muss konsequent eingehalten werden (Risiko bei Refactoring)

**Effort:** 12,0 PD (ohne Puffer)

### Option B: Vue-Wrapper-Library (z. B. vue-chartjs oder Apache ECharts)

**Beschreibung:** Ersetzt D3 durch eine Vue-native Chart-Library. ECharts unterstützt Sankey nativ; für DAG und Spawn-Tree müssten Custom-Renderer gebaut werden.

**Pros:**
- Einfachere API für Standarddiagramme (Sankey, Heatmap)
- Kein DOM-Ownership-Problem

**Cons:**
- DAG und Spawn-Tree werden von keiner Standard-Library nativ unterstützt
- Zusätzliche Abhängigkeit (ECharts ~1 MB minified)
- Inkonsistenter Ansatz: Teil D3, Teil ECharts
- Schlechtere TypeScript-Integration für komplexe Layouts

**Effort:** 14,0 PD (höher wegen gemischter Ansätze)

### Option C: Observable Plot statt D3

**Beschreibung:** Observable Plot (von D3-Autoren, 2022) bietet deklarativere API. Sankey und Matrix sind mit Plot machbar; DAG-Layout fehlt nativ.

**Pros:**
- Weniger Boilerplate für einfache Diagramme
- Vue-kompatibel via Composable

**Cons:**
- DAG-Layout nicht verfügbar — `d3-dag` wäre trotzdem nötig
- Weniger Kontrolle für Custom-Interaktionen (Tooltips, Clickhandler)
- Kleinere Community, weniger Dokumentation als D3

**Effort:** 11,0 PD, aber technische Lücke beim DAG

## Recommendation

**Option A — Direktes D3 via useD3-Composable.**

D3 v7 ist der Industriestandard für custom Data-Visualizations; alle vier Viz-Typen (Sankey, DAG, Tree, Matrix) sind nativ unterstützt. Der `useD3`-Composable-Pattern ist der etablierte Vue-3-Ansatz (2025), eliminiert DOM-Konflikte, und hält die Codebase konsistent ohne Framework-Mischung. Der Aufwand ist geringer als Option B und technisch vollständiger als Option C.

## User Stories

| ID | Story | Acceptance Criteria | Effort |
|---|---|---|---|
| US-001 | Als Entwickler möchte ich den Tool-Flow meiner Agent-Session als Sankey-Diagramm sehen, damit ich Bottlenecks in der Tool-Nutzung erkenne | - Sankey-Diagramm lädt für ausgewählte Session<br>- Nodes = Tool-Namen, Links proportional zur Häufigkeit<br>- Hover zeigt Tool-Namen + Anzahl | 3,5 PD |
| US-002 | Als Nutzer möchte ich den Stage-Verlauf aller Pipeline-Tasks als DAG sehen, damit ich den gesamten Workflow-Fortschritt auf einen Blick erkenne | - DAG zeigt alle stage_runs als Nodes<br>- Nodes eingefärbt nach Status (done/failed/running)<br>- Klick auf Node öffnet TaskModal | 3,0 PD |
| US-003 | Als Entwickler möchte ich den Subagent-Spawn-Tree eines Parent-Agents einsehen, damit ich die Delegation-Hierarchie verstehe | - Tree zeigt Parent-Agent + alle Subagents<br>- Kollaps/Expand-Funktion<br>- Klick auf Node öffnet AgentModal | 2,5 PD |
| US-004 | Als Analyst möchte ich eine Tool-Co-Occurrence-Matrix sehen, damit ich erkennen kann, welche Tools typischerweise zusammen genutzt werden | - Matrix zeigt alle bekannten Tools<br>- Zellen eingefärbt nach Häufigkeit<br>- Hover zeigt Tool-Paar + Count | 2,0 PD |
| US-005 | Als Nutzer möchte ich zwischen den vier Visualisierungen per Tab navigieren, damit ich schnell zwischen den Ansichten wechseln kann | - Tab-Navigation mit 4 Tabs<br>- Aktiver Tab visuell hervorgehoben<br>- Daten laden lazy beim ersten Tab-Klick | 0,5 PD |

## Technical Details

### Affected Components

**Backend:**
- `server/visualizationAggregator.ts` (neu)
- `server/routes/visualizationRoutes.ts` (neu)
- `server/index.ts` (Modify: Route mounten)

**Frontend:**
- `src/composables/useD3.ts` (neu)
- `src/composables/useVisualizationData.ts` (neu)
- `src/components/viz/SankeyChart.vue` (neu)
- `src/components/viz/PipelineDag.vue` (neu)
- `src/components/viz/SpawnTree.vue` (neu)
- `src/components/viz/CooccurrenceMatrix.vue` (neu)
- `src/components/WorkflowsView.vue` (neu)
- `src/App.vue` (Modify: viewMode + Button)
- `src/composables/useAgents.ts` (Modify: viewMode-Union)
- `src/types.ts` (Modify: neue Interfaces)

### Database Changes

Keine Schema-Änderungen. Leseabfragen auf bestehenden Tabellen:
- `stage_runs` JOIN `tasks` für DAG-Daten
- Keine neuen Tabellen oder Indizes

In-Memory-Cache (30s TTL) für Co-Occurrence in `visualizationAggregator.ts` — kein SQLite.

### API Changes

Neue Endpunkte (alle GET, `requireApiToken`):

```
GET /api/visualizations/sankey?sessionId={uuid}
GET /api/visualizations/dag
GET /api/visualizations/spawn-tree?sessionId={uuid}
GET /api/visualizations/cooccurrence
```

Response-Typen in `src/types.ts`:
```typescript
interface SankeyData {
  nodes: Array<{ id: string; name: string }>
  links: Array<{ source: string; target: string; value: number }>
}

interface DagData {
  nodes: Array<{ id: string; stage: string; taskId: string; taskTitle: string; status: string; costCents: number; tokensUsed: number }>
  edges: Array<{ source: string; target: string }>
}

interface SpawnTreeNode {
  id: string
  sessionId: string
  label: string
  children: SpawnTreeNode[]
}

interface CooccurrenceData {
  tools: string[]
  matrix: number[][]
}
```

### Dependencies

Neue npm-Dependencies:
- `d3` — D3 v7 core (scale, shape, selection, zoom, hierarchy, tree)
- `d3-sankey` — Sankey-Layout-Algorithmus
- `d3-dag` — DAG-Layout (Sugiyama)
- `@types/d3-sankey` — TypeScript-Typen (devDependency)

d3-dag v1.x hat native TypeScript-Typen, kein separates `@types/d3-dag` nötig.

## Effort Estimate

| Phase | Aufwand | Beschreibung |
|---|---|---|
| Phase 1 — Backend Aggregation | 3,0 PD | visualizationAggregator.ts + visualizationRoutes.ts + server/index.ts |
| Phase 2 — Composables + Typen | 2,0 PD | useD3.ts, useVisualizationData.ts, src/types.ts |
| Phase 3 — D3-Komponenten | 5,0 PD | 4 Chart-Komponenten + WorkflowsView.vue |
| Phase 4 — Navigation | 0,5 PD | App.vue + useAgents.ts |
| Phase 5 — Dependencies | 0,5 PD | pnpm add + Typen-Check |
| Phase 6 — Tests + Lint | 1,0 PD | Unit-Tests Aggregator, lint, typecheck |
| Zwischensumme | 12,0 PD | |
| Puffer (+20%) | 2,4 PD | |
| **Total** | **14,4 PD** | |

## Risks

| Risiko | Wahrscheinlichkeit | Impact | Mitigation |
|---|---|---|---|
| D3-DOM-Konflikte mit Vue-Rerender | Mittel | Hoch | useD3-Composable strikt als einziger DOM-Owner; nur leerer `<svg ref>` aus Vue heraus |
| Co-Occurrence langsam bei >100 Sessions | Mittel | Mittel | 30s In-Memory-Cache im Aggregator |
| d3-dag breaking API zwischen Versionen | Niedrig | Mittel | Exakte Version in package.json pinnen |
| Sankey-Sequenz-Extraktion liefert leere Daten bei sehr kurzen Sessions | Hoch | Niedrig | Fallback: zeige "Keine Daten für diese Session" statt Fehler |
| Kein laufender Agent → kein SessionId für Sankey/Spawn-Tree | Hoch | Niedrig | Picker listet alle bekannten Session-IDs aus CLAUDE_PROJECTS_DIR |
