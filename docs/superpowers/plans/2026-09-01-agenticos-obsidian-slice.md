# AgenticOS Obsidian Slice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Obsidian application reachable in production — configurable, constructible, callable by an agent, and triggerable — so that the AgenticOS MVP exit criterion can be demonstrated end to end.

**Architecture:** The encryption primitive (`secretbox`, AES-256-GCM) and its master-key bootstrap already exist and are already wired at boot; only the plugin settings service consumes them. This plan extends the flat settings registry with a `Secret` flag so an encrypted value is an ordinary registry definition, then declares the four Obsidian settings against it, constructs `obsidian.Client` in DI when configured, and exposes the vault to agents through MCP tools that call the capability gate themselves.

**Tech Stack:** Go 1.26 (chi, ent ORM, modernc/sqlite, cobra), Vue 3 + TypeScript SPA (Vite, pnpm, Vitest)

**Spec:** `docs/superpowers/specs/2026-08-27-agenticos-obsidian-slice-design.md`, framed by `docs/superpowers/specs/2026-08-27-agenticos-overview-design.md` (decision D8: the first application is Obsidian *including write and delete*, because a gate designed against a harmless case repeats the mistake it was meant to prevent).

## Global Constraints

- Server MUST bind to `127.0.0.1`. Never `0.0.0.0`.
- Never run `go test ./...` or `task test` — both regenerate `server/internal/db/ent/`. Use package-scoped test paths. Task 1 regenerates ent deliberately; that is the only task where a changed `server/internal/db/ent/` belongs in the commit.
- `gofmt -l <pkg>` is mandatory before every commit. CI runs `golangci-lint fmt --diff`, which fails on struct-literal alignment that `go build`, `go vet` and `go test` all pass. Removing or adding a field in a literal is exactly when this bites.
- ent regeneration must use the project's own path (`go generate ./internal/db/ent/`, which carries `--feature sql/upsert`). Regenerating without it strips every `OnConflict*` method and breaks `db/repo`. Verify after regen: `grep -rl "OnConflict" server/internal/db/ent/ | head` must print files.
- ent auto-migrate is non-destructive and the project deliberately does not enable `WithDropColumn`. Added columns must be additive-safe with defaults.
- All code, comments, commit messages, PR titles and bodies in English. Conventional Commits.
- The Obsidian API key is a vault-wide credential. It must never be written to the database in plaintext, never returned by any read endpoint, and never logged. Task 1 and Task 2 exist so that Task 4 has somewhere safe to put it — do not reorder them.

---

## Decisions already taken

**D-A1 — the encrypted value lives in `app_setting`, not in a second store.** `plugin_setting` already carries the exact column set needed (`secret bool`, `nonce string` alongside `value`), and `plugin.NewSettingsService` is generic over its scope key, so reusing it with a pseudo-plugin id would have worked and been cheaper. It was rejected: the Obsidian key would then sit outside the settings registry that `layer2-project-core.md` declares the single source of truth, and `/api/settings`, `dashboard settings` and the Settings UI would not see it — one value, two admin surfaces. Extending `app_setting` makes every future connector credential (mail, calendar) a one-line `Secret: true` definition.

**D-A2 — ciphertext and nonce are separate columns, not a combined marker blob in `value`.** Encoding `enc:v1:<nonce>:<ct>` into the existing column would avoid the migration entirely, but a genuine plaintext value beginning with the marker would be misread. `plugin_setting` already chose separate columns for this reason; matching it keeps one convention rather than two.

**D-A3 — the vault gate is called inside each MCP tool, not inside `obsidian.Client`.** `Client.Read/Write/Search/Delete` take no capability repos and enforce nothing; today the only gated caller is `IndexNotes`, which is why the gate is currently exercised by exactly one code path and `obsidian.write`/`obsidian.delete` are exercised by none. Putting the check in the tool follows the established shape in `server/internal/mcp/tools/memory.go`. A caller reaching `Client` directly still bypasses the gate — that is a known property of the design, stated here so it is not rediscovered as a surprise.

---

## Files

**Task 1 — encrypted column**
- Modify: `server/internal/db/ent/schema/app_setting.go`
- Modify: `server/internal/db/repo/app_setting_repo.go`
- Regenerate: `server/internal/db/ent/` (deliberate, belongs in this commit)
- Test: `server/internal/db/repo/app_setting_repo_test.go`, `server/internal/db/client_test.go`

**Task 2 — secret-aware settings service**
- Modify: `server/internal/settings/registry.go`, `server/internal/settings/service.go`
- Modify: `server/serverapp/di.go` (the `settingsRepoAdapter`, ~lines 87-107, and the `settings.Service` construction, ~lines 204-217)
- Test: `server/internal/settings/service_test.go`

**Task 3 — masking reaches HTTP and CLI**
- Modify: `server/internal/api/settings/handler.go`, `server/internal/cli/cmd_settings.go`, `server/internal/cli/dbstore.go`
- Test: `server/internal/api/settings/handler_test.go`

**Task 4 — Obsidian settings and client construction**
- Modify: `server/internal/settings/registry.go` (four definitions), `server/serverapp/di.go`
- Create: `server/serverapp/di_obsidian.go`
- Test: `server/serverapp/di_obsidian_test.go`

**Task 5 — memory space and index trigger**
- Modify: `server/serverapp/di_obsidian.go`, `server/internal/api/settings/handler.go` or a new route file (decide in-task)
- Test: alongside the chosen surface

**Task 6 — MCP vault tools**
- Create: `server/internal/mcp/tools/obsidian.go`, `server/internal/mcp/tools/obsidian_test.go`
- Modify: `server/internal/mcp/auth.go`, `server/serverapp/di_mcp.go`

**Task 7 — Settings UI panel**
- Create: `src/features/settings/components/ObsidianSettings.vue`, `src/features/settings/components/__tests__/ObsidianSettings.test.ts`
- Modify: `src/features/settings/components/ApiKeySettings.vue`

---

### Task 1: Encrypted values in `app_setting`

**Files:**
- Modify: `server/internal/db/ent/schema/app_setting.go`
- Modify: `server/internal/db/repo/app_setting_repo.go`
- Regenerate: `server/internal/db/ent/`
- Test: `server/internal/db/repo/app_setting_repo_test.go`, `server/internal/db/client_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `repo.AppSettingRepo` gains `UpsertSecret(ctx context.Context, key, ciphertext, nonce string) (*ent.AppSetting, error)` and `GetSecret(ctx context.Context, key string) (ciphertext, nonce string, found bool, err error)`. The existing `Get`, `List` and `Upsert` signatures are unchanged, so no existing caller breaks.

- [ ] **Step 1: Write the failing repo test**

Append to `server/internal/db/repo/app_setting_repo_test.go`:

```go
func TestAppSettingRepo_SecretRoundTrip(t *testing.T) {
	client := openTestDB(t)
	r := repo.NewAppSettingRepo(client)
	ctx := t.Context()

	_, err := r.UpsertSecret(ctx, "obsidian.apiKey", "Y2lwaGVy", "bm9uY2U=")
	require.NoError(t, err)

	ct, nonce, ok, err := r.GetSecret(ctx, "obsidian.apiKey")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "Y2lwaGVy", ct)
	assert.Equal(t, "bm9uY2U=", nonce)

	// A plaintext row reports no nonce, so a reader can tell the two apart
	// without consulting the registry.
	_, err = r.Upsert(ctx, "git.allowPush", "true")
	require.NoError(t, err)
	_, nonce2, ok2, err := r.GetSecret(ctx, "git.allowPush")
	require.NoError(t, err)
	assert.True(t, ok2)
	assert.Empty(t, nonce2)
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd server && go test -count=1 ./internal/db/repo/ -run SecretRoundTrip`
Expected: FAIL, compile error `r.UpsertSecret undefined (type repo.AppSettingRepo has no field or method UpsertSecret)`.

- [ ] **Step 3: Add the schema columns**

In `server/internal/db/ent/schema/app_setting.go`, the field list becomes:

```go
func (AppSetting) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("key").Unique(),
		field.String("value"),
		// secret marks value as AES-256-GCM ciphertext produced by
		// internal/secretbox; nonce is that ciphertext's GCM nonce. Both are
		// base64. Kept in their own columns rather than encoded into value,
		// so a plaintext value that happens to look like a marker cannot be
		// misread as ciphertext — the same reason plugin_setting splits them.
		field.Bool("secret").Default(false),
		field.String("nonce").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
```

Both columns carry defaults, so ent's non-destructive auto-migrate adds them to an existing database without touching existing rows.

- [ ] **Step 4: Regenerate ent through the project's own path**

```bash
cd server && go generate ./internal/db/ent/
grep -rl "OnConflict" internal/db/ent/ | head -3   # MUST print files
go mod tidy                                        # entc dirties go.sum; put it back
git diff --stat go.sum                             # MUST be empty
```

If `OnConflict` is gone you regenerated without `--feature sql/upsert`. Restore and retry.

- [ ] **Step 5: Extend the repo**

In `server/internal/db/repo/app_setting_repo.go`, add to the interface and implement:

```go
// UpsertSecret stores an encrypted value. ciphertext and nonce are both
// base64, exactly as internal/secretbox.Box.Encrypt returns them.
func (r *entAppSettingRepo) UpsertSecret(ctx context.Context, key, ciphertext, nonce string) (*ent.AppSetting, error) {
	err := r.client.AppSetting.Create().
		SetID(uuid.New().String()).
		SetKey(key).
		SetValue(ciphertext).
		SetSecret(true).
		SetNonce(nonce).
		OnConflictColumns(appsetting.FieldKey).
		UpdateNewValues().
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("appsetting.UpsertSecret: %w", err)
	}
	return r.byKey(ctx, key)
}

// GetSecret returns the stored ciphertext and nonce. A row written by Upsert
// rather than UpsertSecret comes back with an empty nonce, which is how a
// caller distinguishes the two without consulting the settings registry.
func (r *entAppSettingRepo) GetSecret(ctx context.Context, key string) (string, string, bool, error) {
	row, err := r.client.AppSetting.Query().Where(appsetting.KeyEQ(key)).Only(ctx)
	if ent.IsNotFound(err) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("appsetting.GetSecret: %w", err)
	}
	return row.Value, row.Nonce, true, nil
}
```

Extract the existing reload-by-key logic in `Upsert` into the `byKey` helper both methods use, rather than duplicating the query.

- [ ] **Step 6: Run the repo test green**

Run: `cd server && go test -count=1 ./internal/db/repo/ -run SecretRoundTrip -v`
Expected: PASS.

- [ ] **Step 7: Prove an existing database survives the added columns**

Append to `server/internal/db/client_test.go`, following `TestOpen_LegacyPreApprovedColumnSurvives` in the same file — a file database, never `:memory:`, because an in-memory fixture pins one connection and cannot show a migration fault:

```go
func TestOpen_AppSettingGainsSecretColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = raw.Exec("CREATE TABLE `app_settings` (`id` text NOT NULL, `key` text NOT NULL UNIQUE, `value` text NOT NULL, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, PRIMARY KEY (`id`))")
	require.NoError(t, err)
	_, err = raw.Exec("INSERT INTO app_settings (id, key, value, created_at, updated_at) VALUES ('1','git.allowPush','true',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)")
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	bundle, err := db.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	r := repo.NewAppSettingRepo(bundle.Client)
	v, ok, err := r.Get(t.Context(), "git.allowPush")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "true", v)

	_, err = r.UpsertSecret(t.Context(), "obsidian.apiKey", "Y2lwaGVy", "bm9uY2U=")
	require.NoError(t, err)
}
```

Confirm the assertion has teeth: temporarily give the added columns no default, re-run, and check it fails on the pre-existing row. Restore afterwards.

- [ ] **Step 8: Gate and commit**

```bash
cd server && go build ./... && go vet ./... && gofmt -l ./internal/db/
go test -count=1 ./internal/db/... 
cd .. && git add server/internal/db/ && git commit --no-gpg-sign -m "feat(db): store encrypted values in app_setting"
```

---

### Task 2: A secret-aware settings registry and service

**Files:**
- Modify: `server/internal/secretbox/secretbox.go` (add `MaskedSentinel`)
- Modify: `server/internal/plugin/settings_service.go` (alias its existing constant to the new one)
- Modify: `server/internal/settings/registry.go`, `server/internal/settings/service.go`
- Modify: `server/serverapp/di.go`
- Test: `server/internal/settings/service_test.go`

**Interfaces:**
- Consumes: `repo.AppSettingRepo.UpsertSecret` / `GetSecret` from Task 1.
- Produces: `settings.Definition` gains `Secret bool`. `settings.Service` gains `Secret(ctx context.Context, key string) (string, error)` returning the decrypted value, and `Service.Set` transparently encrypts when the definition is secret. `settings.Repo` gains `SetSecret(ctx context.Context, key, ciphertext, nonce string) error` and `GetSecret(ctx context.Context, key string) (ciphertext, nonce string, ok bool, err error)`. `secretbox.MaskedSentinel` is the single definition of the mask string.

- [ ] **Step 1: Write the failing service test**

Append to `server/internal/settings/service_test.go`, matching that file's existing in-package `fakeRepo` style:

```go
func TestService_SecretRoundTripAndMasking(t *testing.T) {
	box, err := secretbox.New(make([]byte, 32))
	require.NoError(t, err)
	repo := newFakeRepo()
	svc := New(repo, box)

	require.NoError(t, svc.Load(t.Context()))

	require.NoError(t, svc.Set(t.Context(), "obsidian.apiKey", "sk-live-123"))

	// Stored ciphertext must not be the plaintext.
	assert.NotEqual(t, "sk-live-123", repo.values["obsidian.apiKey"])
	assert.NotEmpty(t, repo.nonces["obsidian.apiKey"])

	// Reading it back decrypts.
	got, err := svc.Secret(t.Context(), "obsidian.apiKey")
	require.NoError(t, err)
	assert.Equal(t, "sk-live-123", got)

	// Every non-decrypting read path masks.
	assert.Equal(t, secretbox.MaskedSentinel, svc.String("obsidian.apiKey"))
	assert.Equal(t, secretbox.MaskedSentinel, svc.Effective()["obsidian.apiKey"])

	// Re-submitting the mask leaves the stored value untouched, so a UI that
	// round-trips what it was shown cannot overwrite the real secret.
	before := repo.values["obsidian.apiKey"]
	require.NoError(t, svc.Set(t.Context(), "obsidian.apiKey", secretbox.MaskedSentinel))
	assert.Equal(t, before, repo.values["obsidian.apiKey"])
}
```

`fakeRepo` in that file is `{m map[string]string}` with `Get`/`Set`/`ListAll` (`service_test.go:13-18`). Rename its map to `values`, add `nonces map[string]string`, and add `SetSecret`/`GetSecret` so it satisfies the widened `Repo`. The constructor is `New(repo)` (`service.go`), not `NewService` — this task adds the box as its second parameter, so every existing `New(&fakeRepo{...})` call site in the package must gain it.

- [ ] **Step 2: Run it and watch it fail**

Run: `cd server && go test -count=1 ./internal/settings/ -run SecretRoundTripAndMasking`
Expected: FAIL, compile error `unknown field Secret in struct literal of type Definition`.

- [ ] **Step 3: Give the mask one definition**

In `server/internal/secretbox/secretbox.go`:

```go
// MaskedSentinel is what a secret value reads as on any surface that is not
// explicitly decrypting it. Submitting it back means "leave unchanged", so it
// must be a value no real secret would be.
const MaskedSentinel = "********"
```

In `server/internal/plugin/settings_service.go`, replace the local literal so there is one definition rather than two that happen to agree:

```go
const MaskedSentinel = secretbox.MaskedSentinel
```

- [ ] **Step 4: Add `Secret` to the registry definition, and the first secret key**

`definitions` in `server/internal/settings/registry.go:106` is a package-level map built once by a func literal, with only `Lookup` and `All` exported — there is no way to register a definition from a test. The test in Step 1 therefore has to use a real registered key, so this task adds `obsidian.apiKey` (the key that motivates the whole feature); Task 4 adds the remaining three Obsidian settings.

```go
{Key: "obsidian.apiKey", Type: TypeString, Secret: true, Apply: ApplyRestart, Category: "obsidian"},
```


In `server/internal/settings/registry.go`, add the field to `Definition` with the comment that states the consequence:

```go
type Definition struct {
	Key      string
	Type     Type
	Default  string
	Apply    Apply
	Category string
	Enum     []string
	// Secret routes the value through secretbox on write and masks it on
	// every read that is not Service.Secret. A secret definition must not
	// carry a Default: a default would be returned in clear by the
	// registry-fallback path before anything was ever stored.
	Secret   bool
	validate func(raw string) error
}
```

Add to `Definition.Validate` a guard rejecting a registered secret definition that carries a non-empty `Default`, so the rule is enforced rather than merely documented.

- [ ] **Step 5: Teach the service to encrypt, decrypt and mask**

In `server/internal/settings/service.go`, widen `Repo`, add the `box` field, and implement:

```go
type Repo interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string) error
	SetSecret(ctx context.Context, key, ciphertext, nonce string) error
	GetSecret(ctx context.Context, key string) (string, string, bool, error)
	ListAll(ctx context.Context) (map[string]string, error)
}

// Secret returns the decrypted value of a secret setting. It is the only read
// path that does not mask; every other accessor returns secretbox.MaskedSentinel
// so a secret cannot leak through a surface that was written for plain values.
func (s *Service) Secret(ctx context.Context, key string) (string, error) {
	def, ok := Lookup(key)
	if !ok {
		return "", fmt.Errorf("settings: unknown setting %q", key)
	}
	if !def.Secret {
		return "", fmt.Errorf("settings: %q is not a secret setting", key)
	}
	ct, nonce, found, err := s.repo.GetSecret(ctx, key)
	if err != nil {
		return "", fmt.Errorf("settings.Secret: %w", err)
	}
	if !found || nonce == "" {
		return "", nil
	}
	return s.box.Decrypt(ct, nonce)
}
```

`Set` gains, before its existing validate-and-persist path:

```go
if def.Secret {
	if value == secretbox.MaskedSentinel {
		return nil // the caller is echoing back what it was shown
	}
	ct, nonce, err := s.box.Encrypt(value)
	if err != nil {
		return fmt.Errorf("settings.Set: encrypt %q: %w", key, err)
	}
	if err := s.repo.SetSecret(ctx, key, ct, nonce); err != nil {
		return fmt.Errorf("settings.Set: %w", err)
	}
	s.snapshot[key] = secretbox.MaskedSentinel
	return nil
}
```

The snapshot deliberately holds the mask, so `String`, `Effective` and every typed accessor return it without needing a per-call-site rule.

`Load` must likewise store the mask rather than the ciphertext for any key whose definition is secret — otherwise `Effective()` would publish base64 ciphertext, which is not a leak of the plaintext but is still a value no consumer should see.

- [ ] **Step 6: Wire the box into the service in DI**

In `server/serverapp/di.go`, the `settingsRepoAdapter` (around lines 87-107) gains `SetSecret`/`GetSecret` delegating to `inner.UpsertSecret`/`inner.GetSecret`. The `settings.Service` construction (around lines 204-217) must receive a `*secretbox.Box`.

The master key is resolved today at line ~328, *after* the settings service is built, and only inside the `entClient != nil` branch. Move that resolution above the settings-service construction and pass the same `*secretbox.Box` to both consumers — build it once, not twice. A second `secretbox.New(masterKey)` call would also work and is stateless, but two boxes from one key invites the assumption that they could diverge.

If the box cannot be built, the settings service must still be constructed; a secret read then returns an error rather than panicking. `plugin.Service` has no nil-box guard today and relies on never being constructed without one — do not copy that; add an explicit guard in `Service.Secret` and `Service.Set`.

- [ ] **Step 7: Run the service test green**

Run: `cd server && go test -count=1 ./internal/settings/ -v`
Expected: PASS, including the pre-existing tests.

- [ ] **Step 8: Gate and commit**

```bash
cd server && go build ./... && go vet ./... && gofmt -l ./internal/settings/ ./internal/secretbox/ ./internal/plugin/ ./serverapp/
go test -count=1 ./internal/settings/... ./internal/plugin/... ./serverapp/...
cd .. && git add -A && git commit --no-gpg-sign -m "feat(settings): support encrypted values in the settings registry"
```

---

### Task 3: Masking reaches the HTTP surface and the CLI

**Files:**
- Modify: `server/internal/api/settings/handler.go`
- Modify: `server/internal/cli/cmd_settings.go`, `server/internal/cli/dbstore.go`
- Test: `server/internal/api/settings/handler_test.go`

**Interfaces:**
- Consumes: `settings.Service.Set`/`Secret` and `secretbox.MaskedSentinel` from Task 2.
- Produces: no new exported Go API. `GET /api/settings` returns `secretbox.MaskedSentinel` as the value of every secret key; `PATCH /api/settings/{key}` accepts the sentinel as a no-op.

- [ ] **Step 1: Write the failing handler test**

`GET /api/settings` today echoes `eff[d.Key]` for every registered key with no masking, because nothing was secret until now. Append to `server/internal/api/settings/handler_test.go`, in that file's existing `memRepo`-fake style:

```go
func TestList_MasksSecretValues(t *testing.T) {
	h, svc := newRouter(t)
	require.NoError(t, svc.Set(t.Context(), "obsidian.apiKey", "sk-live-123"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var got []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	for _, row := range got {
		if row["key"] == "obsidian.apiKey" {
			assert.Equal(t, secretbox.MaskedSentinel, row["value"])
			assert.NotContains(t, rec.Body.String(), "sk-live-123")
			return
		}
	}
	t.Fatal("obsidian.apiKey not present in the settings list")
}
```

The `assert.NotContains` on the whole body is the load-bearing assertion: it fails even if the plaintext leaks through some field other than the one being inspected.

- [ ] **Step 2: Run it and watch it fail**

Run: `cd server && go test -count=1 ./internal/api/settings/ -run MasksSecretValues`
Expected: FAIL — the body contains `sk-live-123`.

- [ ] **Step 3: Make list masking explicit at the handler**

`Service.Effective()` already returns the mask after Task 2, so this test may pass without a handler change. Verify that; if it does, keep the test (it pins the behaviour against a future regression in the service) and add no handler code. If it does not, mask in the handler where `settingView.Value` is filled. Do not add a second masking rule if the first already holds — a duplicated rule that can drift is worse than none.

- [ ] **Step 4: Give the CLI a box**

`server/internal/cli/dbstore.go` opens SQLite directly and constructs no `secretbox.Box`, so `dashboard settings set` on a secret key would write plaintext straight into the `value` column, bypassing Task 2 entirely. This is the lockout-recovery path, so it must not silently do the wrong thing.

In `openDBStore`, resolve the master key the same way the server does — `secretbox.LoadOrGenerateMasterKey(os.Getenv("DASHBOARD_SECRET_KEY"))` — and build the box. Route `SetValidated` for a secret definition through the encrypting path. If the key cannot be resolved, refuse the write for a secret key with a named error rather than falling back to plaintext.

- [ ] **Step 5: Add the CLI test**

Append to `server/internal/cli/cmd_settings_test.go` a test that sets a secret key through the CLI store and asserts the row's `value` column is not the plaintext, mirroring the plugin service test's `assert.NotEqual(t, "KEY123", ...)` shape.

- [ ] **Step 6: Run green**

Run: `cd server && go test -count=1 ./internal/api/settings/... ./internal/cli/... -v`
Expected: PASS.

- [ ] **Step 7: Gate and commit**

```bash
cd server && go build ./... && go vet ./... && gofmt -l ./internal/api/settings/ ./internal/cli/
cd .. && git add -A && git commit --no-gpg-sign -m "feat(settings): mask secret values on the HTTP and CLI surfaces"
```

---

### Task 4: Obsidian settings and client construction

**Files:**
- Modify: `server/internal/settings/registry.go`
- Create: `server/serverapp/di_obsidian.go`, `server/serverapp/di_obsidian_test.go`
- Modify: `server/serverapp/di.go`

**Interfaces:**
- Consumes: `settings.Service.Secret`/`String` from Task 2.
- Produces: `func buildObsidianClient(ctx context.Context, settingsSvc *settings.Service) (*obsidian.Client, error)` in package `serverapp`, returning `(nil, nil)` when Obsidian is not configured.

- [ ] **Step 1: Add the four definitions**

`obsidian.Config` is `{BaseURL, APIKey, VaultRoot, TLSMode string}` (`server/internal/apps/obsidian/client.go:49-54`), and `NewClient` fails closed on an empty `BaseURL`, an empty `VaultRoot`, a non-`https` scheme, or a `TLSMode` outside the three constants. Append to the `definitions` list in `server/internal/settings/registry.go`. `obsidian.apiKey` was already added in Task 2, because the registry has no dynamic registration and that task's test needed a real secret key:

```go
{Key: "obsidian.baseURL", Type: TypeString, Default: "", Apply: ApplyRestart, Category: "obsidian"},
{Key: "obsidian.vaultRoot", Type: TypeString, Default: "", Apply: ApplyRestart, Category: "obsidian"},
{Key: "obsidian.tlsMode", Type: TypeEnum, Enum: []string{"verify", "pinned", "insecure-loopback"}, Default: "verify", Apply: ApplyRestart, Category: "obsidian"},
```

`ApplyRestart` because the client pins the resolved host, IP and port at construction — a live value change would not reach an already-built client, and claiming `ApplyLive` would be a lie the UI repeats to the user. The enum values must equal `obsidian.TLSVerify`, `obsidian.TLSPinned` and `obsidian.TLSInsecureLoopback`; add a test asserting that equality rather than trusting the copies to stay in step, since the Vue client cannot import the Go constants and this list is already a hand-kept parity copy.

- [ ] **Step 2: Write the failing construction test**

Create `server/serverapp/di_obsidian_test.go`. It needs a settings service backed by a real DB, since `Service.Secret` reads through the repo; write that helper first in the same file:

```go
// newSettingsServiceForTest returns a settings service over a fresh in-memory
// database with a deterministic box, so a secret written in a test is readable
// back in the same test.
func newSettingsServiceForTest(t *testing.T) *settings.Service {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	box, err := secretbox.New(make([]byte, 32))
	require.NoError(t, err)
	svc := settings.New(settingsRepoAdapter{inner: repo.NewAppSettingRepo(bundle.Client)}, box)
	require.NoError(t, svc.Load(t.Context()))
	return svc
}

func TestBuildObsidianClient_UnconfiguredIsNotAnError(t *testing.T) {
	svc := newSettingsServiceForTest(t) // nothing set
	client, err := buildObsidianClient(t.Context(), svc)
	require.NoError(t, err)
	assert.Nil(t, client, "an unconfigured vault must disable the feature, not fail the boot")
}

func TestBuildObsidianClient_PartialConfigIsAnError(t *testing.T) {
	svc := newSettingsServiceForTest(t)
	require.NoError(t, svc.Set(t.Context(), "obsidian.baseURL", "https://127.0.0.1:27124"))
	// vaultRoot deliberately left empty
	_, err := buildObsidianClient(t.Context(), svc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vaultRoot")
}
```

The distinction these two tests pin is the whole point of the task: absent configuration is a feature that is off, half-present configuration is a mistake that must be reported. Silently treating the second as the first is how a user ends up with a vault integration that appears configured and never runs.

- [ ] **Step 3: Run and watch it fail**

Run: `cd server && go test -count=1 ./serverapp/ -run BuildObsidianClient`
Expected: FAIL, `undefined: buildObsidianClient`.

- [ ] **Step 4: Implement**

Create `server/serverapp/di_obsidian.go`:

```go
// buildObsidianClient returns nil, nil when Obsidian is unconfigured: the
// application is optional, and an absent vault must leave the rest of the
// server running. A partially configured vault is an error rather than a
// silent no-op — obsidian.NewClient already fails closed on each missing
// piece, and reporting that at boot is the only way the operator learns the
// integration is not running.
func buildObsidianClient(ctx context.Context, settingsSvc *settings.Service) (*obsidian.Client, error) {
	baseURL := settingsSvc.String("obsidian.baseURL")
	vaultRoot := settingsSvc.String("obsidian.vaultRoot")
	if baseURL == "" && vaultRoot == "" {
		return nil, nil
	}
	apiKey, err := settingsSvc.Secret(ctx, "obsidian.apiKey")
	if err != nil {
		return nil, fmt.Errorf("obsidian: read api key: %w", err)
	}
	client, err := obsidian.NewClient(obsidian.Config{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		VaultRoot: vaultRoot,
		TLSMode:   settingsSvc.String("obsidian.tlsMode"),
	})
	if err != nil {
		return nil, fmt.Errorf("obsidian: %w", err)
	}
	return client, nil
}
```

Never log `apiKey`, and never include it in a wrapped error — `NewClient`'s own errors name the field that failed, not its value.

- [ ] **Step 5: Call it from DI**

In `server/serverapp/di.go`, next to the existing `obsidian.Register` call (~line 290), construct the client and carry it on the dependency struct. A construction error must fail the boot with a clear message: a vault the operator configured and that silently does not run is worse than a refused start.

While there, correct two comments that describe code which does not exist: the block at ~line 280 calls `obsidian.IndexNotes` "the one production caller" of the catalogue, and the comment at ~line 544 says the vault indexer "builds its own memory.Gate". Neither is true before Task 5 lands. Update them to say what the code does now, in the same commit that makes the first half true.

- [ ] **Step 6: Run green, gate, commit**

```bash
cd server && go test -count=1 ./serverapp/ -run BuildObsidianClient -v
go build ./... && go vet ./... && gofmt -l ./serverapp/ ./internal/settings/
cd .. && git add -A && git commit --no-gpg-sign -m "feat(obsidian): construct the vault client from settings"
```

---

### Task 5: A memory space and a trigger for `IndexNotes`

**Files:**
- Modify: `server/serverapp/di_obsidian.go`
- Create: a route file under `server/internal/api/` for the manual trigger (decide the package in-task; `api/settings` is wrong — this is an action, not a setting)
- Test: alongside the chosen surface

**Interfaces:**
- Consumes: `buildObsidianClient` from Task 4.
- Produces: `func ensureObsidianSpace(ctx context.Context, resources repo.ResourceRepo) (*ent.Resource, error)` and an HTTP route that runs one indexing pass.

- [ ] **Step 1: Establish the space**

`IndexNotes` takes a `spaceID` (`server/internal/apps/obsidian/index.go:49-55`) and no Obsidian memory space is created anywhere — memory spaces are never auto-created, so the trigger would fail on a fresh install. A memory space is a `resource` row with `Kind: repo.ResourceKindMemorySpace`; create one with a fixed slug (`obsidian`) at global scope, idempotently, next to `obsidian.Register`.

Write the test first: call the function twice against an in-memory DB and assert one row and a stable id.

- [ ] **Step 2: Build the gate with no asker, deliberately**

`IndexNotes` needs a `memory.Gate`. Build it exactly as the pipeline's memory push does (`server/serverapp/di_pipeline.go:159-162`) — `memory.Gate{Capabilities: capabilityRepo, Grants: grantRepo, GrantUsage: grantUsageRepo}` with the `Asker` field omitted:

```go
// No Asker: an index run is unattended background work, so an ask decision
// must deny rather than hold the run open waiting for a human who is not
// watching. "Must never ask" is a property of how the Gate is built, not a
// rule every call site has to remember.
gate := memory.Gate{Capabilities: capabilityRepo, Grants: grantRepo, GrantUsage: grantUsageRepo}
```

- [ ] **Step 3: Add the manual trigger**

A scheduled job and a manual button are both defensible. Start with the manual one: `POST /api/obsidian/index`, session-authenticated, returning the count `IndexNotes` reports. It makes the slice demonstrable in one click, and a scheduler entry can wrap the same function later without redesign. Say in the handler's doc comment that the run is unattended with respect to capabilities even though a human pressed the button, because the gate has no asker.

Write the handler test first, asserting that a missing `obsidian.search` grant produces a 403 and not a 500 — the denial is the expected path on a fresh install, not an error.

- [ ] **Step 4: Run green, gate, commit**

```bash
cd server && go build ./... && go vet ./... && gofmt -l ./serverapp/ ./internal/api/
go test -count=1 ./serverapp/... ./internal/api/... 
cd .. && git add -A && git commit --no-gpg-sign -m "feat(obsidian): index the vault into memory on demand"
```

---

### Task 6: MCP vault tools

**Files:**
- Create: `server/internal/mcp/tools/obsidian.go`, `server/internal/mcp/tools/obsidian_test.go`
- Modify: `server/internal/mcp/auth.go`, `server/serverapp/di_mcp.go`

**Interfaces:**
- Consumes: the `*obsidian.Client` from Task 4.
- Produces: MCP tools `obsidian_read`, `obsidian_search`, `obsidian_write`, `obsidian_delete`; scopes `obsidian:read` and `obsidian:write`.

This is the task that makes decision D8 real: until an agent can write and delete, the gate has only ever been exercised against harmless operations.

- [ ] **Step 1: Write the failing scope-wiring test**

`mcp.ToolRegistry.Register` panics for a tool with no `ToolScopeMap` entry, so the wiring guard comes first. Mirror `TestMemoryToolsHaveScopeEntries` in `server/internal/mcp/tools/memory_test.go`:

```go
func TestObsidianToolsHaveScopeEntries(t *testing.T) {
	for _, name := range []string{"obsidian_read", "obsidian_search", "obsidian_write", "obsidian_delete"} {
		_, ok := mcp.ToolScopeMap[name]
		assert.True(t, ok, "tool %q has no ToolScopeMap entry; Register would panic", name)
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `cd server && go test -count=1 ./internal/mcp/tools/ -run ObsidianToolsHaveScopeEntries`
Expected: FAIL on all four names.

- [ ] **Step 3: Add the scopes**

In `server/internal/mcp/auth.go`, add to `ToolScopeMap`:

```go
"obsidian_read":   "obsidian:read",
"obsidian_search": "obsidian:read",
"obsidian_write":  "obsidian:write",
"obsidian_delete": "obsidian:write",
```

and to `scopeImplies`:

```go
"obsidian:read":  {},
"obsidian:write": {"obsidian:read"},
```

Add both to `validKeyScopes` so they can be granted to an API key, and add them to `keys:manage`'s expansion. Mirror `TestMemoryScopesAreGrantableToKeys` for the new pair.

- [ ] **Step 4: Write the failing gate test**

The gate check belongs in the tool, not in `Client` — `Client.Write`/`Delete` take no capability repos and enforce nothing. Following `TestMemoryWriteDeniedBeforeSpaceLookup`, assert that a delete without a grant is refused **before** the client is called, using a client stub that fails the test if invoked:

Write the helper first, mirroring `newMemoryDepsForTest` (`server/internal/mcp/tools/memory_test.go:47`), which returns four values — deps, the grant repo, the capability repo and a context. Like its model, it must NOT seed the capability catalogue, so a test can exercise the "capability never catalogued, therefore denied" path explicitly:

```go
// newObsidianDepsForTest wires ObsidianDeps against an in-memory database with
// a vault stub that records whether it was reached. Capabilities are
// deliberately left unseeded; a test that wants them calls SeedCapabilities.
func newObsidianDepsForTest(t *testing.T) (ObsidianDeps, repo.GrantRepo, repo.CapabilityRepo, context.Context) {
	t.Helper()
	// ... open :memory: db, build capability/grant/grant-usage repos,
	// build memory.Gate, and set Client to a stub recording vaultCalled
}

func TestObsidianDeleteDeniedBeforeVaultCall(t *testing.T) {
	deps, _, capRepo, ctx := newObsidianDepsForTest(t)
	repo.SeedCapabilities(ctx, capRepo)
	registry := mcp.ToolRegistry{}
	RegisterObsidianTools(registry, deps)

	_, err := registry["obsidian_delete"].Handler(ctx, map[string]any{"path": "notes/a.md"})
	require.Error(t, err)
	assert.False(t, deps.vaultCalled, "the vault must not be touched before the gate allows it")
}
```

- [ ] **Step 5: Implement the tools**

Follow `server/internal/mcp/tools/memory.go` exactly: a `ObsidianDeps` struct, a `RegisterObsidianTools(registry mcp.ToolRegistry, d ObsidianDeps)` entry point, one `registry.Register(&mcp.ToolDef{...})` per tool with `Name`, `Description`, `InputSchema`, `Handler`; read args with `mcp.StringArg` / `mcp.OptionalString`; return `mcp.Fail("obsidian_write: " + err.Error())` and `mcp.OK(payload)`.

Each handler calls `d.Gate.Authorize(ctx, obsidian.CapabilityWrite, notePath, scope)` before touching the client, passing the note path as the capability value so a grant can be narrowed to a subtree by pattern. `obsidian_search` passes `""`, the documented wildcard, because a search fans out rather than naming one target.

When `d.Client` is nil — Obsidian unconfigured — return a named error saying the vault is not configured. Do not register the tools at all in that case if the registry allows conditional registration; an agent discovering a tool that always fails is worse than not discovering it.

- [ ] **Step 6: Register in DI**

In `server/serverapp/di_mcp.go`, alongside `RegisterMemoryTools`, add `RegisterObsidianTools` with a `memory.Gate` carrying the real asker (an agent is waiting on the tool response, so an ask can legitimately hold — this is the opposite of Task 5's unattended run, and the difference must be deliberate).

- [ ] **Step 7: Run green, gate, commit**

```bash
cd server && go build ./... && go vet ./... && gofmt -l ./internal/mcp/ ./serverapp/
go test -count=1 ./internal/mcp/... ./serverapp/...
cd .. && git add -A && git commit --no-gpg-sign -m "feat(mcp): expose the Obsidian vault as gated MCP tools"
```

---

### Task 7: Obsidian settings panel

**Files:**
- Create: `src/features/settings/components/ObsidianSettings.vue`, `src/features/settings/components/__tests__/ObsidianSettings.test.ts`
- Modify: `src/features/settings/components/ApiKeySettings.vue`

**Interfaces:**
- Consumes: `GET /api/settings` and `PATCH /api/settings/{key}` (unchanged), plus `POST /api/obsidian/index` from Task 5.
- Produces: a settings section with id `obsidian`.

- [ ] **Step 1: Write the failing component test**

Follow `src/features/settings/components/GrantSettings.test.ts`: Vitest, `vi.mock` of the composable, seeded refs, `mount(..., { attachTo: document.body })`, `data-testid` selectors, `flushPromises()`. No manual `unmount()` — `src/test/setup.ts:20` registers `enableAutoUnmount(afterEach)` globally, added after a forgotten unmount let a live poller feed shared module-level mocks across files.

The test that matters most asserts the mask contract: given `obsidian.apiKey` returning `********`, saving without touching that field must PATCH the sentinel (which the server treats as "leave unchanged"), and the field must never render a real key.

- [ ] **Step 2: Build the panel**

Reuse `useSettings.ts` — these are ordinary registry settings, so no new composable is needed. Bind the four keys, use a password-type input for the key, and surface `apply: 'restart'` in the save toast, since all four are `ApplyRestart` and a user who changes them and sees no effect will otherwise assume it failed.

Add an **Index now** button posting to `POST /api/obsidian/index` and reporting the returned count, plus the denial case as a readable message rather than a raw 403.

- [ ] **Step 3: Register the section**

Three edits in `src/features/settings/components/ApiKeySettings.vue`: add `'obsidian'` to the `Section` union (~line 44), add `{ id: 'obsidian', icon: '🗒', label: 'Obsidian' }` to `SECTIONS` (~lines 47-64), and add the `<section v-else-if="activeSection === 'obsidian'"><ObsidianSettings /></section>` branch. Static import — the panel is small.

- [ ] **Step 4: Run green, gate, commit**

```bash
pnpm lint && pnpm typecheck && pnpm test
git add -A && git commit --no-gpg-sign -m "feat(ui): configure the Obsidian vault from Settings"
```

---

## Definition of done for the slice

The exit criterion this plan serves is demonstrable when, on a fresh install:

1. A vault is configured in Settings and the API key is unreadable in the database (`sqlite3 <db> "select value from app_settings where key='obsidian.apiKey'"` returns base64, not the key).
2. `agent-dashboard grants add obsidian.search --pattern '*' --mode allow` (and `obsidian.read`, `memory.write`) makes **Index now** report a non-zero count, and omitting them makes it report a denial rather than an error.
3. An agent holding an `obsidian:write` scope can write a note through `obsidian_write`, and the same agent without an `obsidian.write` grant is refused before the vault is touched.
4. A grant carrying a limit is exhausted and the refusal is visible.

Steps 3 and 4 are the ones decision D8 was written for. Until they run, the capability gate has never been exercised against an irreversible operation.
