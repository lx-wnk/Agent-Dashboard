# D3 Workflow Visualizations — Implementierungsplan

> **Für agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Ziel:** Neue `/workflows`-Route in der SPA mit vier interaktiven D3.js-Visualisierungen, die cross-session- und cross-task-Daten darstellen. Backend liefert aggregierte Daten über vier neue REST-Endpunkte unter `GET /api/visualizations/{type}`.

**Architektur:** Rein on-demand berechnete Endpunkte (kein materialized-view-Cache in SQLite). Jede Visualisierung ist ein eigenständiges Vue-Komponente, das D3 per `useD3`-Composable einbettet — kein Wrapper-Framework. D3-Packages werden als direkte npm-Dependencies installiert. Der neue `/workflows`-View ist ein zusätzlicher `viewMode`-Wert in `useAgents`, kein separater Vue-Router-Pfad.

**Tech Stack:** Vue 3 + Composition API, D3 v7 (core), d3-sankey, d3-dag, TypeScript, Express 5. Keine neuen Wrapper-Libraries.

**Layering:** Neues `server/routes/visualizationRoutes.ts` importiert nur `db/*`, `server/jsonlParser.ts`, `src/types.ts`. Kein Import von `pipeline/*` außer dem DB-Layer.

---

## Datenverfügbarkeit (Analyse)

| Visualisierung | Datenquelle | Aufwand Extraktion |
|---|---|---|
| Sankey (Tool-Flow) | `jsonlParser.toolCounts` + `extractSessionInfo` (sequence rekonstruieren) | Mittel — Sequenz-Tracking nötig |
| DAG (Pipeline-Stages) | `stage_runs` Tabelle (task_id, stage, started_at, ended_at, status) | Gering — direkte SQL-Abfrage |
| Spawn-Tree (Subagents) | `findSubagents()` in `jsonlParser.ts` | Gering — bereits strukturiert |
| Co-Occurrence (Tools) | `jsonlParser.toolCounts` pro Session | Gering — toolCounts bereits vorhanden |

---

## File Map

### Phase 1 — Backend: Daten-Aggregation (3 PD)

| Aktion | Datei | Beschreibung |
|---|---|---|
| Create | `server/visualizationAggregator.ts` | Reine Compute-Funktionen für alle 4 Viz-Typen (importiert `jsonlParser`, `db/stageRunsRepo`, `db/tasksRepo`) |
| Create | `server/routes/visualizationRoutes.ts` | Express Router: `GET /api/visualizations/sankey`, `.../dag`, `.../spawn-tree`, `.../cooccurrence`; Response-Typen als TypeScript-Interfaces |
| Modify | `server/index.ts` | Mount `visualizationRoutes` unter `/api`; nach bestehenden Routes einhängen |

**Endpunkt-Spezifikation:**

```
GET /api/visualizations/sankey?sessionId={uuid}
  → { nodes: [{id, name}], links: [{source, target, value}] }

GET /api/visualizations/dag
  → { nodes: [{id, stage, taskId, taskTitle, status, costCents, tokensUsed}], edges: [{source, target}] }

GET /api/visualizations/spawn-tree?sessionId={uuid}
  → { id, sessionId, label, children: SpawnNode[] }  (recursive tree)

GET /api/visualizations/cooccurrence
  → { tools: string[], matrix: number[][] }  (symmetrisch, tools[i][j] = count)
```

**Datenextraktion-Details:**

- **Sankey:** `extractSessionInfo` gibt `toolCounts` und `lastTools` zurück, aber keine Sequenz. `visualizationAggregator.ts` liest das JSONL via `parseJsonlLines` (max. 32 KB tail) und rekonstruiert die Tool-Reihenfolge pro Konversations-Turn als Abfolge `assistant-turn-N → tool-A → tool-B`. Nodes = einzigartige Tool-Namen + `"Start"` + `"End"`. Links = Übergänge zwischen aufeinanderfolgenden Tools pro Turn.
- **DAG:** Single SQL-Query auf `stage_runs JOIN tasks`. Jeder `stage_run`-Row wird zu einem Node. Kanten: Verbinde aufeinanderfolgende Stages eines Tasks sortiert nach `started_at`. Nutze `status` (done/failed/running) für Node-Farbe.
- **Spawn-Tree:** `findSubagents()` liefert flache Liste. `findSessionForProject` pro laufendem Agent. Baum-Aufbau: Parent = Agent-PID, Children = subagents aus dem jeweiligen Session-Subagent-Dir.
- **Co-Occurrence:** Alle Sessions via `CLAUDE_PROJECTS_DIR` durchiterieren. Pro Session `toolCounts` laden. Matrix befüllen: für jedes Paar (toolA, toolB) in derselben Session `matrix[i][j] += count_min(toolA, toolB)`.

### Phase 2 — Frontend: Composables + Typen (2 PD)

| Aktion | Datei | Beschreibung |
|---|---|---|
| Create | `src/composables/useVisualizationData.ts` | Fetcht alle 4 Endpunkte; reaktive `ref`s für Daten, Loading, Error-State; optional `sessionId`-Parameter |
| Modify | `src/types.ts` | Neue Interfaces: `SankeyData`, `DagData`, `SpawnTreeNode`, `CooccurrenceData` |

### Phase 3 — Frontend: D3-Komponenten (5 PD)

| Aktion | Datei | Beschreibung |
|---|---|---|
| Create | `src/composables/useD3.ts` | Generischer Composable: `useD3(containerRef, renderFn)` — ruft `renderFn(svg, width, height)` bei Mount + ResizeObserver auf; cleanup on unmount |
| Create | `src/components/viz/SankeyChart.vue` | D3-Sankey mit `d3-sankey`; Nodes als Rechtecke, Links als gefärbte Pfade; Tooltip on hover |
| Create | `src/components/viz/PipelineDag.vue` | DAG mit `d3-dag` (Sugiyama-Layout); Nodes als Stage-Kacheln; Kanten als geschwungene Pfeile; klickbar → öffnet TaskModal |
| Create | `src/components/viz/SpawnTree.vue` | Collapsible D3-Tree (d3.hierarchy + d3.tree); Agent-Nodes klickbar → öffnet AgentModal |
| Create | `src/components/viz/CooccurrenceMatrix.vue` | Heatmap-Grid mit D3-Scales; Zellen eingefärbt nach Häufigkeit; x/y-Achse mit Tool-Namen |
| Create | `src/components/WorkflowsView.vue` | Haupt-View: Tab-Nav (Sankey / DAG / Spawn-Tree / Matrix); rendert je nach aktivem Tab die passende Viz-Komponente; SessionId-Picker für Sankey + Spawn-Tree |

### Phase 4 — Navigation-Integration (0.5 PD)

| Aktion | Datei | Beschreibung |
|---|---|---|
| Modify | `src/App.vue` | `viewMode`-Option `'workflows'` hinzufügen; Header-Button "Workflows" zwischen "Sessions" und Settings; `WorkflowsView` in `<main>` einhängen |
| Modify | `src/composables/useAgents.ts` | `viewMode`-Union-Type um `'workflows'` erweitern |

### Phase 5 — Dependencies + Typisierung (0.5 PD)

| Aktion | Datei | Beschreibung |
|---|---|---|
| Run | `pnpm add d3-sankey d3-dag` | Installiert `d3-sankey` (layout) + `d3-dag` (DAG-Layout) |
| Run | `pnpm add -D @types/d3-sankey` | Typen für d3-sankey (d3-dag hat native TS-Typen) |
| Run | `pnpm add d3` | D3 core (bereits möglicherweise transitiv vorhanden — prüfen) |

> Hinweis: D3 v7 tree, scale, shape, selection, zoom sind Teil von `d3` core. Nur Sankey und DAG-Layout benötigen separate Packages.

### Phase 6 — Tests + Lint (1 PD)

| Aktion | Datei | Beschreibung |
|---|---|---|
| Create | `server/visualizationAggregator.test.ts` | Unit-Tests für alle 4 Aggregator-Funktionen mit Mock-JSONL-Daten und Mock-DB |
| Run | `pnpm lint && pnpm typecheck` | Muss clean durchlaufen |
| Run | `pnpm test` | Alle Unit-Tests grün |

---

## Kritische Implementierungs-Hinweise

### D3 + Vue — DOM-Ownership-Regel

D3 und Vue dürfen nicht gleichzeitig dasselbe DOM-Element verwalten. Die Lösung via `useD3`-Composable:
- Vue rendert nur einen `<svg ref="container" />`-Wrapper (kein Inhalt)
- `useD3` übergibt `container.value` an D3's `select()` — D3 übernimmt vollständigen Besitz des SVG-Inhalts
- Bei `viewMode`-Wechsel: `onUnmounted`-Hook räumt D3-Event-Listener und ResizeObserver auf

### Performance: Kein SQLite-Cache nötig

Die 4 Endpunkte sind on-demand. Begründung:
- `/dag`: Einzelne SQL-Query (`stage_runs JOIN tasks`) — sub-50ms bei realistischer Task-Anzahl (<1000)
- `/cooccurrence`: Iteriert alle Session-Dirs — kann bei >50 Sessions langsam werden. Lösung: 30-Sekunden-In-Memory-Cache in `visualizationAggregator.ts` (einfaches `{ data, timestamp }` Objekt, kein SQLite)
- `/sankey` + `/spawn-tree`: Sessionspezifisch mit `sessionId`-Parameter — nutzen bestehenden `sessionCache` aus `jsonlParser.ts`

### Layering-Konformität

`server/routes/visualizationRoutes.ts` darf importieren:
- `server/db/stageRunsRepo.ts` ✓
- `server/db/tasksRepo.ts` ✓
- `server/visualizationAggregator.ts` ✓
- `src/types.ts` ✓
- `server/jsonlParser.ts` ✓ (nur `parseJsonlLines`, `tailRead`, `CLAUDE_PROJECTS_DIR`)

Nicht erlaubt: `pipeline/orchestrator`, `notifications/`, andere Routes.

### Routing-Ansatz: viewMode statt Vue-Router

Das Projekt verwendet kein Vue Router. Die bestehende Navigation (Dashboard/Kanban) nutzt `viewMode` als `ref<string>`. Die neue Workflows-Ansicht fügt `'workflows'` als weiteren Wert hinzu — konsistent mit dem bestehenden Muster. Kein vue-router installieren.

---

## Abnahmekriterien

- [ ] `GET /api/visualizations/sankey?sessionId=<uuid>` liefert valides `SankeyData`-JSON
- [ ] `GET /api/visualizations/dag` liefert valides `DagData`-JSON
- [ ] `GET /api/visualizations/spawn-tree?sessionId=<uuid>` liefert valides `SpawnTreeNode`-JSON
- [ ] `GET /api/visualizations/cooccurrence` liefert valides `CooccurrenceData`-JSON
- [ ] "Workflows"-Button im Header wechselt in den Workflows-View
- [ ] Alle 4 Viz-Tabs sind klickbar und laden ihre Daten
- [ ] Sankey und Spawn-Tree zeigen SessionId-Picker; bei Auswahl wird Viz aktualisiert
- [ ] DAG-Nodes sind klickbar und öffnen das TaskModal
- [ ] Spawn-Tree-Nodes sind klickbar und öffnen das AgentModal
- [ ] Kein D3-DOM-Leak: nach viewMode-Wechsel sind keine verwaisten SVG-Listener vorhanden
- [ ] `pnpm lint`, `pnpm typecheck`, `pnpm test` laufen ohne Fehler
- [ ] Performance: `/dag` und `/cooccurrence` antworten unter 500 ms bei realistischer Datenlast

---

## Aufwandsschätzung

| Phase | Aufwand |
|---|---|
| Phase 1 — Backend Aggregation | 3,0 PD |
| Phase 2 — Composables + Typen | 2,0 PD |
| Phase 3 — D3-Komponenten (4 Stück) | 5,0 PD |
| Phase 4 — Navigation-Integration | 0,5 PD |
| Phase 5 — Dependencies + Typisierung | 0,5 PD |
| Phase 6 — Tests + Lint | 1,0 PD |
| **Gesamt (inkl. 20% Puffer)** | **14,4 PD** |

---

## Risiken

| Risiko | Wahrscheinlichkeit | Impact | Mitigation |
|---|---|---|---|
| D3-DOM-Konflikte mit Vue-Rerender | Mittel | Hoch | `useD3`-Composable strikt trennen; nur `<svg>`-Root via `ref` übergeben |
| Co-Occurrence bei >100 Sessions langsam | Mittel | Mittel | 30s In-Memory-Cache in Aggregator |
| d3-dag breaking API (v0.x → v1.x) | Niedrig | Mittel | Versionslocking in package.json; Changelogs prüfen |
| Sankey-Daten bei großen Sessions (>10 MB JSONL) | Niedrig | Mittel | Bestehender 32 KB tail-read begrenzt Datenmengen automatisch |
| Kein SessionId-Kontext für Sankey wenn kein Agent läuft | Hoch | Niedrig | Dropdown zeigt alle bekannten Session-IDs aus CLAUDE_PROJECTS_DIR |
