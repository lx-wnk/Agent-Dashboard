# F051: Remove /api/plugins Alias Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the legacy `/api/plugins` route alias, keeping only the canonical `/api/settings/plugins`.

**Architecture:** Single file change. The alias was kept for backwards compatibility after the route was renamed. All frontend callers already use `/api/settings/plugins`. The TODO comment in the handler is the source of truth.

**Tech Stack:** Go (chi router)

---

## File Map

- Modify: `server/internal/api/plugins/handler.go` — remove alias route and update comments

---

### Task 1: Remove alias and update comment

**Files:**
- Modify: `server/internal/api/plugins/handler.go`

- [ ] **Step 1: Verify no callers use /api/plugins in frontend**

```bash
grep -r "/api/plugins" src/ --include="*.ts" --include="*.vue" -n
```

Expected: no output (all callers use `/api/settings/plugins`).

- [ ] **Step 2: Remove alias line and update Mount comment**

In `server/internal/api/plugins/handler.go`, replace the `Mount` function:

```go
// Mount registers plugin routes on r.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/settings/plugins", h.list)
}
```

Remove the old doc comment about the alias and the `// TODO(F051)` line entirely.

- [ ] **Step 3: Build to verify no compile errors**

```bash
cd server && go build ./...
```

Expected: exits 0 with no output.

- [ ] **Step 4: Run tests**

```bash
task test
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add server/internal/api/plugins/handler.go
git commit -m "fix: remove /api/plugins alias — all callers migrated to /api/settings/plugins (F051)"
```
