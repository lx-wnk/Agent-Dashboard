# PR-F: Custom System Prompts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow users to define custom system prompts that are prepended to the built-in stage prompts. Prompts are scoped (global or per-task), optionally filtered by stage, and prioritized. Full CRUD API + Vue settings UI.

**Architecture:** New ent schema `SystemPrompt`. New repo interface + ent implementation. API handler at `/api/settings/system-prompts`. Pipeline integration in `stage_prompts.go` — query DB before building each stage's prompt bundle. Vue settings panel section for CRUD.

**Tech Stack:** ent ORM (modernc.org/sqlite), Go chi, Vue 3 + TypeScript

---

## Worktree Setup

```bash
git worktree add ../agent-dashboard-prf feat/custom-system-prompts
cd ../agent-dashboard-prf/server
```

---

## File Map

| Action | File |
|--------|------|
| Create | `server/internal/db/ent/schema/system_prompt.go` |
| Run | `GOWORK=off go run -mod=mod entgo.io/ent/cmd/ent generate ./...` |
| Create | `server/internal/db/repo/system_prompt_repo.go` |
| Modify | `server/internal/pipeline/stage_prompts.go` (add custom prompt injection) |
| Modify | `server/internal/pipeline/types.go` (`StageContext` gets `SystemPromptRepo`) |
| Modify | `server/internal/pipeline/stage_handlers.go` (pass repo into StageContext) |
| Create | `server/internal/api/systemprompts/handler.go` |
| Modify | `server/internal/api/router.go` (mount new routes) |
| Create | `src/components/SystemPromptSettings.vue` |
| Modify | `src/App.vue` or settings panel (add SystemPromptSettings) |

---

### Task 1: Ent schema

**Files:**
- Create: `server/internal/db/ent/schema/system_prompt.go`

- [ ] **Step 1.1: Write schema**

```go
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SystemPrompt holds user-defined prompt overrides injected into pipeline stages.
type SystemPrompt struct{ ent.Schema }

func (SystemPrompt) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		// scope: "global" (all tasks) or "task" (specific task only, not yet wired)
		field.String("scope").Default("global"),
		// stage: nil means all stages; non-nil targets a specific stage name.
		field.String("stage").Optional().Nillable(),
		field.Text("content"),
		// priority: higher number wins when multiple prompts match.
		field.Int("priority").Default(0),
		field.String("created_by").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (SystemPrompt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("scope", "stage"),
		index.Fields("priority"),
	}
}
```

- [ ] **Step 1.2: Run ent codegen**

```bash
cd server
GOWORK=off go run -mod=mod entgo.io/ent/cmd/ent generate \
  --feature sql/versioned-migration \
  ./internal/db/ent/schema/...
```

Wait for codegen to complete. It will generate files in `server/internal/db/ent/`.

- [ ] **Step 1.3: Verify build**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 1.4: Commit schema + generated code**

```bash
git add server/internal/db/ent/
git commit -m "feat: add SystemPrompt ent schema and generated ORM code"
```

---

### Task 2: Repository

**Files:**
- Create: `server/internal/db/repo/system_prompt_repo.go`

- [ ] **Step 2.1: Write the repo interface and implementation**

```go
package repo

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/systemprompt"
)

// SystemPromptRepo defines CRUD for SystemPrompt entities.
type SystemPromptRepo interface {
	Create(ctx context.Context, in CreateSystemPromptInput) (*ent.SystemPrompt, error)
	List(ctx context.Context) ([]*ent.SystemPrompt, error)
	ListForStage(ctx context.Context, stage string) ([]*ent.SystemPrompt, error)
	Update(ctx context.Context, id string, in UpdateSystemPromptInput) (*ent.SystemPrompt, error)
	Delete(ctx context.Context, id string) error
}

type CreateSystemPromptInput struct {
	Scope     string
	Stage     *string
	Content   string
	Priority  int
	CreatedBy *string
}

type UpdateSystemPromptInput struct {
	Content  *string
	Priority *int
	Stage    *string
}

type entSystemPromptRepo struct{ client *ent.Client }

func NewSystemPromptRepo(client *ent.Client) SystemPromptRepo {
	return &entSystemPromptRepo{client: client}
}

func (r *entSystemPromptRepo) Create(ctx context.Context, in CreateSystemPromptInput) (*ent.SystemPrompt, error) {
	id := uuid.New().String()
	q := r.client.SystemPrompt.Create().
		SetID(id).
		SetScope(in.Scope).
		SetContent(in.Content).
		SetPriority(in.Priority).
		SetCreatedAt(time.Now()).
		SetUpdatedAt(time.Now())
	if in.Stage != nil {
		q = q.SetStage(*in.Stage)
	}
	if in.CreatedBy != nil {
		q = q.SetCreatedBy(*in.CreatedBy)
	}
	return q.Save(ctx)
}

func (r *entSystemPromptRepo) List(ctx context.Context) ([]*ent.SystemPrompt, error) {
	return r.client.SystemPrompt.Query().
		Order(ent.Desc(systemprompt.FieldPriority)).
		All(ctx)
}

func (r *entSystemPromptRepo) ListForStage(ctx context.Context, stage string) ([]*ent.SystemPrompt, error) {
	return r.client.SystemPrompt.Query().
		Where(
			systemprompt.ScopeEQ("global"),
			systemprompt.Or(
				systemprompt.StageIsNil(),
				systemprompt.StageEQ(stage),
			),
		).
		Order(ent.Desc(systemprompt.FieldPriority)).
		All(ctx)
}

func (r *entSystemPromptRepo) Update(ctx context.Context, id string, in UpdateSystemPromptInput) (*ent.SystemPrompt, error) {
	q := r.client.SystemPrompt.UpdateOneID(id).SetUpdatedAt(time.Now())
	if in.Content != nil {
		q = q.SetContent(*in.Content)
	}
	if in.Priority != nil {
		q = q.SetPriority(*in.Priority)
	}
	if in.Stage != nil {
		q = q.SetStage(*in.Stage)
	}
	return q.Save(ctx)
}

func (r *entSystemPromptRepo) Delete(ctx context.Context, id string) error {
	return r.client.SystemPrompt.DeleteOneID(id).Exec(ctx)
}
```

- [ ] **Step 2.2: Build check**

```bash
go build ./internal/db/...
```

- [ ] **Step 2.3: Commit**

```bash
git add server/internal/db/repo/system_prompt_repo.go
git commit -m "feat: SystemPromptRepo interface + ent implementation"
```

---

### Task 3: Pipeline integration

**Files:**
- Modify: `server/internal/pipeline/types.go`
- Modify: `server/internal/pipeline/stage_prompts.go`

- [ ] **Step 3.1: Add `SystemPromptRepo` to `StageContext`**

In `server/internal/pipeline/types.go`, add to `StageContext`:
```go
// SystemPromptRepo is used to fetch custom system prompt overrides for this stage.
// May be nil if the feature is not configured.
SystemPromptRepo interface {
    ListForStage(ctx context.Context, stage string) ([]*ent.SystemPrompt, error)
}
```

Use an inline interface to avoid a circular import (the repo package imports ent, pipeline is allowed to import ent already).

- [ ] **Step 3.2: Add `buildCustomSystemPrompt` helper to `stage_prompts.go`**

Append to `server/internal/pipeline/stage_prompts.go`:

```go
// buildCustomSystemPrompt queries the SystemPromptRepo for global prompts
// matching the given stage, combines them (highest priority first), and returns
// the combined text. Returns "" if the repo is nil or no prompts match.
func buildCustomSystemPrompt(sc *StageContext, stage string) string {
	if sc.SystemPromptRepo == nil {
		return ""
	}
	prompts, err := sc.SystemPromptRepo.ListForStage(sc.Ctx, stage)
	if err != nil || len(prompts) == 0 {
		return ""
	}
	var parts []string
	for _, p := range prompts {
		parts = append(parts, p.Content)
	}
	return strings.Join(parts, "\n\n---\n\n")
}
```

Add `"strings"` to the imports if not already present.

- [ ] **Step 3.3: Inject custom prompt into each stage builder**

In each stage prompt builder function (e.g. `ImplementationPrompt`, `SelfReviewPrompt`, etc.) in `stage_prompts.go`, the functions currently accept `(t *ent.Task, ...)` — they do NOT have access to `StageContext`.

The cleanest approach: instead of modifying every function signature, add a top-level wrapper in the stage handler that prepends the custom prompt to the returned `PromptBundle.SystemPrompt`:

In `server/internal/pipeline/stage_handlers.go`, in `createAgentStage`'s inner execute function, after `buildPrompt(sc)` returns a `PromptBundle`:

```go
bundle := buildPrompt(sc)
// Prepend any custom system prompts from DB.
if custom := buildCustomSystemPrompt(sc, stage); custom != "" {
    bundle.SystemPrompt = custom + "\n\n---\n\n" + bundle.SystemPrompt
}
```

This keeps the per-stage builders pure (no DB dependency) and injects at the handler level.

- [ ] **Step 3.4: Build check**

```bash
go build ./internal/pipeline/
```

- [ ] **Step 3.5: Commit**

```bash
git add server/internal/pipeline/
git commit -m "feat: inject custom system prompts into pipeline stage prompt bundles"
```

---

### Task 4: API handler

**Files:**
- Create: `server/internal/api/systemprompts/handler.go`

- [ ] **Step 4.1: Write handler**

```go
package systemprompts

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

type Handler struct {
	repo repo.SystemPromptRepo
}

func NewHandler(r repo.SystemPromptRepo) *Handler {
	return &Handler{repo: r}
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/settings/system-prompts", apierr.ErrorMiddleware(h.list))
	r.Post("/api/settings/system-prompts", apierr.ErrorMiddleware(h.create))
	r.Put("/api/settings/system-prompts/{id}", apierr.ErrorMiddleware(h.update))
	r.Delete("/api/settings/system-prompts/{id}", apierr.ErrorMiddleware(h.delete))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	prompts, err := h.repo.List(r.Context())
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(prompts)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	var in repo.CreateSystemPromptInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return apierr.Bad("invalid body")
	}
	if in.Scope == "" {
		in.Scope = "global"
	}
	if in.Content == "" {
		return apierr.Bad("content is required")
	}
	prompt, err := h.repo.Create(r.Context(), in)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	return json.NewEncoder(w).Encode(prompt)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	var in repo.UpdateSystemPromptInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return apierr.Bad("invalid body")
	}
	prompt, err := h.repo.Update(r.Context(), id, in)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(prompt)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if err := h.repo.Delete(r.Context(), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
```

Note: `apierr.Bad` may not exist — check the `apierr` package. If it only has a generic error helper, use `apierr.New(http.StatusBadRequest, "invalid body")` or the equivalent existing function.

- [ ] **Step 4.2: Mount in router**

In `server/internal/api/router.go`, in the admin-auth block:
```go
if deps.SystemPromptHandler != nil {
    deps.SystemPromptHandler.Mount(r)
}
```

Add `SystemPromptHandler *systemprompts.Handler` to `RouterDeps`.

- [ ] **Step 4.3: Build + test**

```bash
go build ./... && go test -race ./...
```

- [ ] **Step 4.4: Commit**

```bash
git add server/internal/api/systemprompts/ server/internal/api/router.go
git commit -m "feat: system prompts CRUD API at /api/settings/system-prompts"
```

---

### Task 5: Vue settings panel

**Files:**
- Create: `src/components/SystemPromptSettings.vue`

- [ ] **Step 5.1: Write the component**

```vue
<template>
  <div class="system-prompt-settings">
    <div class="section-header">
      <h3>Custom System Prompts</h3>
      <button class="btn btn-primary" @click="showCreate = true">Add Prompt</button>
    </div>

    <div v-if="loading" class="loading">Loading…</div>

    <div v-else-if="prompts.length === 0" class="empty">
      No custom system prompts configured.
    </div>

    <table v-else class="prompt-table">
      <thead>
        <tr>
          <th>Stage</th>
          <th>Priority</th>
          <th>Preview</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="p in prompts" :key="p.id">
          <td>{{ p.stage ?? 'All stages' }}</td>
          <td>{{ p.priority }}</td>
          <td class="preview">{{ p.content.slice(0, 80) }}…</td>
          <td>
            <button class="btn btn-sm" @click="startEdit(p)">Edit</button>
            <button class="btn btn-sm btn-danger" @click="deletePrompt(p.id)">Delete</button>
          </td>
        </tr>
      </tbody>
    </table>

    <!-- Create/Edit dialog -->
    <div v-if="showCreate || editing" class="modal-backdrop" @click.self="closeDialog">
      <div class="modal">
        <h4>{{ editing ? 'Edit Prompt' : 'New System Prompt' }}</h4>
        <label>
          Stage (leave blank for all)
          <input v-model="form.stage" placeholder="e.g. implementation" />
        </label>
        <label>
          Priority (higher = applied first)
          <input v-model.number="form.priority" type="number" />
        </label>
        <label>
          Content
          <textarea v-model="form.content" rows="8" placeholder="System prompt text…" />
        </label>
        <div class="modal-actions">
          <button class="btn btn-primary" @click="save">Save</button>
          <button class="btn" @click="closeDialog">Cancel</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'

interface SystemPrompt {
  id: string
  scope: string
  stage: string | null
  content: string
  priority: number
  created_at: string
}

interface PromptForm {
  stage: string
  priority: number
  content: string
}

const prompts = ref<SystemPrompt[]>([])
const loading = ref(true)
const showCreate = ref(false)
const editing = ref<SystemPrompt | null>(null)
const form = ref<PromptForm>({ stage: '', priority: 0, content: '' })

async function fetchPrompts() {
  loading.value = true
  const res = await fetch('/api/settings/system-prompts')
  prompts.value = res.ok ? await res.json() : []
  loading.value = false
}

function startEdit(p: SystemPrompt) {
  editing.value = p
  form.value = { stage: p.stage ?? '', priority: p.priority, content: p.content }
}

function closeDialog() {
  showCreate.value = false
  editing.value = null
  form.value = { stage: '', priority: 0, content: '' }
}

async function save() {
  const body = {
    scope: 'global',
    stage: form.value.stage || null,
    priority: form.value.priority,
    content: form.value.content,
  }
  const url = editing.value
    ? `/api/settings/system-prompts/${editing.value.id}`
    : '/api/settings/system-prompts'
  const method = editing.value ? 'PUT' : 'POST'
  await fetch(url, { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
  closeDialog()
  fetchPrompts()
}

async function deletePrompt(id: string) {
  if (!confirm('Delete this prompt?')) return
  await fetch(`/api/settings/system-prompts/${id}`, { method: 'DELETE' })
  fetchPrompts()
}

onMounted(fetchPrompts)
</script>

<style scoped>
.system-prompt-settings { padding: 1rem; }
.section-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 1rem; }
.prompt-table { width: 100%; border-collapse: collapse; }
.prompt-table th, .prompt-table td { padding: 0.5rem; border-bottom: 1px solid var(--border, #eee); text-align: left; }
.preview { max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-muted, #888); font-family: monospace; font-size: 0.85em; }
.modal-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.4); display: flex; align-items: center; justify-content: center; z-index: 100; }
.modal { background: var(--bg, #fff); padding: 1.5rem; border-radius: 8px; width: 540px; display: flex; flex-direction: column; gap: 0.75rem; }
.modal label { display: flex; flex-direction: column; gap: 0.25rem; font-size: 0.85em; }
.modal input, .modal textarea { padding: 0.4rem; border: 1px solid var(--border, #ddd); border-radius: 4px; font-size: 0.9em; }
.modal textarea { resize: vertical; font-family: monospace; }
.modal-actions { display: flex; gap: 0.5rem; justify-content: flex-end; }
.btn { padding: 0.4rem 0.9rem; border-radius: 4px; cursor: pointer; border: 1px solid transparent; }
.btn-primary { background: var(--accent, #4f46e5); color: #fff; border-color: var(--accent, #4f46e5); }
.btn-danger { color: #dc2626; border-color: #dc2626; }
.btn-sm { padding: 0.2rem 0.5rem; font-size: 0.8em; }
.empty, .loading { color: var(--text-muted, #888); padding: 1rem 0; }
</style>
```

- [ ] **Step 5.2: Add `SystemPromptSettings` to the settings panel**

Find the file that renders the settings panel (search `src/` for a "Settings" component or section). Add:
```vue
<SystemPromptSettings />
```
with the corresponding import.

- [ ] **Step 5.3: Start dev server and verify**

```bash
pnpm dev
```

Open `http://localhost:5173`, navigate to Settings, confirm "Custom System Prompts" section appears. Test create/edit/delete.

- [ ] **Step 5.4: Commit and push**

```bash
git add src/components/SystemPromptSettings.vue src/
git commit -m "feat: SystemPromptSettings Vue component with CRUD UI"
git push -u origin feat/custom-system-prompts
```

---

### Task 6: Final verification

- [ ] **Step 6.1: Backend tests**

```bash
cd server && go test -race ./...
```

- [ ] **Step 6.2: Type check frontend**

```bash
pnpm typecheck
```

Expected: no errors.
