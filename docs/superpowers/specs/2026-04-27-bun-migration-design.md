# Bun Migration Design

**Date:** 2026-04-27  
**Branch:** feat/teamAndMultiRemote  
**Scope:** Runtime-Migration von Node.js + tsx → Bun; SQLite-Adapter-Migration; gezielte Refactorings während der Migration

---

## Ziel

Die Anwendung auf Bun umstellen, um:
- `tsx`-Transpilierung zur Laufzeit zu eliminieren (native TypeScript in Bun)
- `better-sqlite3` (nativer Addon, plattformabhängiger Build) durch `bun:sqlite` (built-in) zu ersetzen
- Einen Single-Binary-Deployment-Pfad zu ermöglichen (`bun build --compile`)
- Offensichtliche strukturelle Probleme (insbesondere `server/index.ts`) im Zuge der Migration zu beheben

Kein App-Verhalten ändert sich. Reine Infrastruktur- und Strukturmigration.

---

## Was sich ändert — was bleibt

### Bleibt komplett unverändert

- Gesamter Frontend-Code (`src/`, Vue 3, Vite, Tailwind, alle Components/Composables)
- Alle Business-Logic (Pipeline-Orchestrator, Auth, MCP, Notifications, Pipeline-Stages)
- Alle `node:*` API-Aufrufe — Bun hat volle Node.js-API-Kompatibilität
- Express 5, MCP SDK, alle anderen npm-Packages (mit Kompatibilitätstest, s.u.)
- TypeScript-Typen, ESLint-Config, alle Vitest-Unit-Tests, alle Playwright-E2E-Tests
- `pnpm` als Package-Manager (Bun ist pnpm-kompatibel)

### Ändert sich

| Was | Alt | Neu |
|---|---|---|
| Runtime | Node.js | Bun |
| TypeScript-Execution | `tsx` (esbuild at runtime) | Bun nativ |
| SQLite | `better-sqlite3` (native addon) | `bun:sqlite` (built-in) |
| Dev-Script | `tsx watch server/index.ts` | `bun --watch server/index.ts` |
| Prod-Start | `node --import tsx/esm server/index.ts` | `bun server/index.ts` |
| Single Binary | Nicht vorhanden | `bun build --compile` |
| `tsx` Dependency | devDependency | Entfernt |

---

## SQLite-Migration

### Strategie: Direktmigration (kein Adapter-Shim)

`bun:sqlite` hat eine API, die `better-sqlite3` sehr ähnlich ist. Ein Compat-Shim wäre dauerhafter Tech-Debt — stattdessen werden alle `server/db/`-Dateien direkt migriert.

### API-Delta

```ts
// VORHER (better-sqlite3)
import Database from 'better-sqlite3'
import type { Database as DatabaseType } from 'better-sqlite3'
const db = new Database(path)
db.pragma('journal_mode = WAL')

// NACHHER (bun:sqlite)
import { Database } from 'bun:sqlite'
const db = new Database(path)
db.run('PRAGMA journal_mode = WAL')
```

**Betroffene Dateien:** `server/db/client.ts` (Hauptänderung), alle Repo-Dateien (nur Type-Imports).

### WAL + Pragma

`better-sqlite3` hat eine `.pragma()` convenience-Methode. `bun:sqlite` verwendet stattdessen direktes SQL via `.run()`. Betrifft nur `server/db/client.ts`.

### Schema-Loading in Single-Binary-Kontext

Aktuell liest `runMigrations` die `schema.sql` via `readFileSync` + `fileURLToPath(import.meta.url)`.  
In einem Bun-kompilierten Binary ist `import.meta.url` weiterhin verfügbar und korrekt — kein Änderungsbedarf. Alternativ: `schema.sql` direkt in `client.ts` als Template-Literal einbetten (eliminiert File-Read zur Laufzeit, sauberer für Single-Binary).

---

## Refactorings während der Migration

### R1 — `server/index.ts` aufteilen (700 Zeilen, 24 inline Route-Handler)

`server/index.ts` ist Composition Root + Auth-Routes + Agent-Routes + System-Routes + Vite-Middleware in einer Datei. Das ist das klarste strukturelle Problem im Codebase.

**Neue Struktur:**

```
server/
  index.ts              ← Pure Composition Root (wiring, server.listen) — Ziel: <150 Zeilen
  routes/
    authRoutes.ts       ← NEU: /auth/login, /auth/callback, /auth/logout, /api/me
    agentRoutes.ts      ← NEU: /api/agents/*, /api/sessions, /api/channel-reply
    systemRoutes.ts     ← NEU: /api/config, /api/system
    taskRoutes.ts       ← Bleibt (876 Zeilen, separates Thema)
    apiKeyRoutes.ts     ← Bleibt
    remoteRoutes.ts     ← Bleibt
```

Jeder neue Router-File exportiert eine `create*Router(deps)` Factory — gleiche Injection-Pattern wie bestehende Router.

### R2 — `server/db/client.ts` Migrations-Cleanup

`runMigrations` ist eine wachsende monolithische Funktion mit inline-SQL-Strings. Jede Migration wird in eine benannte Funktion extrahiert:

```ts
function migrate_v1_base_schema(db: Database) { ... }
function migrate_v2_multi_user(db: Database) { ... }
// etc.
```

Macht die Migrations-History lesbar und testbar.

### R3 — `tsx` vollständig entfernen

`tsx` aus `devDependencies` entfernen. Alle Referenzen in `package.json`, CI-Scripts und Dokumentation ersetzen.

---

## Build & Deployment Pipeline

### Development

```bash
bun --watch server/index.ts   # replaces: tsx watch server/index.ts
```

Vite-Dev-Middleware wird als normales npm-Modul geladen — funktioniert unter Bun unverändert.

### Production — Variante A: Bun-Runtime

```bash
pnpm build        # vite build (Frontend)
bun server/index.ts
```

Anforderung: Bun auf dem Server installiert (`curl -fsSL https://bun.sh/install | bash`). Kein `node_modules` Rebuild für native Addons mehr.

### Production — Variante B: Single Binary (empfohlen für Team-Server)

```bash
pnpm build                                              # Frontend bauen
bun build --compile server/index.ts --outfile=dashboard  # Backend kompilieren
```

Ergebnis:
- `dashboard` — Single Binary (~30 MB, Bun-Runtime embedded)
- `dist/` — Pre-built Vue SPA

Deployment: Binary + `dist/`-Ordner kopieren, starten. Null Runtime-Dependencies auf dem Server.

**Frontend-Embedding** (optionale Verbesserung, nicht im initialen Scope): `dist/`-Assets via `Bun.embeddedFiles` direkt ins Binary einbetten. Macht das Deployment zu einem einzigen File. Aufwand: ~1 Tag extra, nach erfolgreicher Basisintegration.

---

## Risikobereiche & Kompatibilitätstest

| Package | Risiko | Test-Kriterium |
|---|---|---|
| `@modelcontextprotocol/sdk` | Mittel | MCP-Server startet, Tool-Calls funktionieren |
| `express` v5 | Niedrig | Alle API-Endpoints antworten korrekt |
| `nodemailer` | Niedrig | E-Mail-Adapter initialisiert ohne Fehler |
| `cookie-parser` | Niedrig | Auth-Cookie gesetzt und gelesen |
| `better-sqlite3` | **Entfällt** | — |
| `vue` / `vite` | Nicht betroffen | Frontend-Build unverändert |

Test-Strategie: Existing Vitest Unit-Tests + Playwright E2E-Tests laufen nach Migration durch — das ist der primäre Korrektheitsbeweis.

---

## Migrationspfad (Reihenfolge)

1. **Scripts & Tooling** — `package.json` Scripts updaten, `tsx` entfernen, Bun-Version via `.bun-version`-File pinnen (analog zu `.nvmrc`)
2. **SQLite-Migration** — `server/db/client.ts` + alle Repo-Dateien auf `bun:sqlite` umstellen (R2 gleichzeitig)
3. **Smoke-Test** — `bun server/index.ts` startet, DB-Layer funktioniert, alle Unit-Tests grün
4. **`server/index.ts` Refactoring** — R1: AuthRoutes, AgentRoutes, SystemRoutes extrahieren
5. **Dep-Kompatibilitätstest** — MCP SDK, Express 5, alle kritischen Packages verifizieren
6. **E2E-Test** — Playwright-Suite komplett grün
7. **Single Binary Build** — `bun build --compile` verifizieren, Deployment-Anleitung aktualisieren

---

## Out of Scope

- Frontend-Änderungen jeglicher Art
- Neue Features
- `server/routes/taskRoutes.ts` (876 Zeilen) — eigenes Refactoring-Ticket
- Frontend-Embedding ins Binary
- CI/CD-Pipeline-Anpassungen (folgt nach erfolgreichem lokalen Build)
