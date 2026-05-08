# Development Commands

```bash
pnpm dev           # Starts Express (port 13120) + Vite SPA middleware with hot reload
pnpm build         # Production build via Vite
pnpm lint          # ESLint check
pnpm test          # Vitest unit tests (single run)
pnpm test:e2e      # Playwright E2E tests
pnpm typecheck     # vue-tsc type checking
```

**Package manager:** pnpm (workspace setup — `pnpm install` in root installs both root and `channel/` dependencies).
