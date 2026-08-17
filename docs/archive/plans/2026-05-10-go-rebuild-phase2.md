# Go Rebuild Phase 2 — Database + GitHub OAuth + API Keys

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add SQLite persistence via ent ORM, GitHub OAuth login, and API key management — completing the auth layer so protected routes work with real users and MCP clients.

**Architecture:** ent schema-first ORM generates a type-safe SQLite client from `server/internal/db/ent/schema/`. Database opens at startup with auto-migrate (`client.Schema.Create`). GitHub OAuth exchanges a GitHub authorization code for a signed JWT cookie. API keys are SHA-256 hashed before storage — the raw token is shown once at creation, only the hash persists.

**Tech Stack:** Go 1.23+, entgo.io/ent v0.14.6, modernc.org/sqlite (CGO-free), github.com/google/uuid, net/http (OAuth)

---

> **Module path:** `github.com/lx-wnk/agent-dashboard/server`
> **Working directory for all `go` commands:** `server/` unless stated otherwise
> **Config struct** (`server/internal/config/config.go`) already has: `Host`, `Port`, `JWTSecret`, `DBPath`, `GitHubClientID`, `GitHubClientSecret`

---

## File Map

```
server/
  go.mod                                           ← add modernc.org/sqlite, github.com/google/uuid
  internal/
    db/
      ent/
        schema/
          api_key.go                               ← ApiKey entity
          user.go                                  ← User entity (GitHub OAuth users)
          task.go                                  ← Task stub (Phase 3 expands this)
          pipeline_config.go                       ← PipelineConfig stub
        generate.go                                ← //go:generate directive
        (generated files committed alongside)
      client.go                                    ← Open(path) *ent.Client + auto-migrate
      repo/
        api_key_repo.go                            ← ApiKeyRepo interface + ent impl
        api_key_repo_test.go
        user_repo.go                               ← UserRepo interface + ent impl
        user_repo_test.go
    auth/
      github.go                                    ← GitHubClient: ExchangeCode, GetUser
      github_test.go
    api/
      auth/
        handler.go                                 ← /api/auth/github, /callback, /logout, /me
        handler_test.go
      apikeys/
        handler.go                                 ← GET/POST/DELETE /api/settings/api-keys
        handler_test.go
    config/
      config.go                                    ← add CallbackURL() method
    router.go                                      ← add auth + apikeys routes, db client
  cmd/serve/
    wire.go                                        ← add db.Client, GitHubClient providers
    wire_gen.go                                    ← updated
```

---

## Task 1: Add Dependencies

**Files:**
- Modify: `server/go.mod` (via `go get`)

- [ ] **Step 1: Add modernc.org/sqlite and uuid**

```bash
cd server
go get modernc.org/sqlite@latest
go get github.com/google/uuid@latest
go mod tidy
```

Expected: no errors. `go.mod` now includes `modernc.org/sqlite` and `github.com/google/uuid`.

- [ ] **Step 2: Verify ent is present**

```bash
grep "entgo.io/ent" go.mod
```

Expected: `entgo.io/ent v0.14.6` (already added in Phase 1).

- [ ] **Step 3: Build to confirm no import issues**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
git -C .. add server/go.mod server/go.sum
git -C .. commit -m "feat(server): add modernc.org/sqlite and uuid dependencies"
```

---

## Task 2: ent Entity Schemas

**Files:**
- Create: `server/internal/db/ent/schema/api_key.go`
- Create: `server/internal/db/ent/schema/user.go`
- Create: `server/internal/db/ent/schema/task.go`
- Create: `server/internal/db/ent/schema/pipeline_config.go`

- [ ] **Step 1: Create schema directory**

```bash
mkdir -p internal/db/ent/schema
```

- [ ] **Step 2: Create api_key.go**

```go
// server/internal/db/ent/schema/api_key.go
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ApiKey holds the schema definition for the ApiKey entity.
type ApiKey struct{ ent.Schema }

// Fields of the ApiKey.
func (ApiKey) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id"),
		field.String("name").Unique(),
		field.String("key_hash").Unique().Sensitive(), // SHA-256 hex; never store raw token
		field.JSON("scopes", []string{}).Default([]string{}),
		field.Bool("active").Default(true),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("last_used_at").Optional().Nillable(),
	}
}

// Indexes of the ApiKey.
func (ApiKey) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("key_hash"),
		index.Fields("active"),
	}
}
```

- [ ] **Step 3: Create user.go**

```go
// server/internal/db/ent/schema/user.go
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// User holds the schema definition for the User entity.
type User struct{ ent.Schema }

// Fields of the User.
func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("id"), // GitHub numeric user ID (stable across renames)
		field.String("github_login"),
		field.String("display_name").Optional(),
		field.String("avatar_url").Optional(),
		field.Bool("is_admin").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("last_login_at").Optional().Nillable(),
	}
}
```

- [ ] **Step 4: Create task.go (Phase 3 stub)**

```go
// server/internal/db/ent/schema/task.go
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Task holds the schema definition for the Task entity.
// Phase 2 stub — Phase 3 adds full fields, edges, and indexes.
type Task struct{ ent.Schema }

// Fields of the Task.
func (Task) Fields() []ent.Field {
	return []ent.Field{
		field.String("id"),
		field.String("slug").Unique(),
		field.String("title"),
		field.String("description").Optional(),
		field.String("cwd"),
		field.String("current_stage").Default("concept"),
		field.String("priority").Default("medium"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
```

- [ ] **Step 5: Create pipeline_config.go (Phase 3 stub)**

```go
// server/internal/db/ent/schema/pipeline_config.go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// PipelineConfig holds the schema definition for the PipelineConfig entity.
// Stores key-value pipeline settings (e.g. maxParallelOrchestrators).
type PipelineConfig struct{ ent.Schema }

// Fields of the PipelineConfig.
func (PipelineConfig) Fields() []ent.Field {
	return []ent.Field{
		field.String("key"),
		field.String("value"),
	}
}
```

- [ ] **Step 6: Commit schemas (before generate)**

```bash
git -C .. add server/internal/db/ent/schema/
git -C .. commit -m "feat(server/db): add ent entity schemas for ApiKey, User, Task stub, PipelineConfig stub"
```

---

## Task 3: ent Code Generation

**Files:**
- Create: `server/internal/db/ent/generate.go`
- Generated: `server/internal/db/ent/*.go` (committed)

- [ ] **Step 1: Create generate.go**

```go
// server/internal/db/ent/generate.go
package ent

//go:generate go run -mod=mod entgo.io/ent/cmd/ent generate --target . ./schema
```

- [ ] **Step 2: Run code generation**

```bash
cd server && go generate ./internal/db/ent/...
```

Expected: several files created in `server/internal/db/ent/` — `client.go`, `ent.go`, `api_key.go`, `api_key_create.go`, `api_key_query.go`, `user.go`, etc. No errors.

If you get `entgo.io/ent/cmd/ent: module not found`, run:
```bash
go get entgo.io/ent/cmd/ent@v0.14.6
```
Then re-run the generate command.

- [ ] **Step 3: Verify build**

```bash
cd server && go build ./internal/db/ent/...
```

Expected: clean.

- [ ] **Step 4: Commit generated files**

```bash
git -C .. add server/internal/db/ent/
git -C .. commit -m "feat(server/db): generate ent ORM client from schema"
```

---

## Task 4: Database Client

**Files:**
- Create: `server/internal/db/client.go`

- [ ] **Step 1: Write the test first**

```go
// server/internal/db/client_test.go
package db_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
)

func TestOpen_InMemory(t *testing.T) {
	client, err := db.Open(":memory:")
	require.NoError(t, err)
	require.NotNil(t, client)
	_ = client.Close()
}

func TestOpen_AutoMigrate(t *testing.T) {
	// Verify schema is created: api_keys table must exist after Open.
	client, err := db.Open(":memory:")
	require.NoError(t, err)
	defer client.Close()

	// Creating a record proves the table exists.
	_, err = client.ApiKey.Create().
		SetID("test-id").
		SetName("test").
		SetKeyHash("abc123").
		SetScopes([]string{"tasks:read"}).
		Save(t.Context())
	require.NoError(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server && go test ./internal/db/... -run TestOpen -v
```

Expected: FAIL — `db` package doesn't exist yet.

- [ ] **Step 3: Create client.go**

```go
// server/internal/db/client.go
package db

import (
	"context"
	"fmt"

	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite" // register sqlite3 driver

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

// Open returns an ent.Client connected to the SQLite database at path.
// Creates the database file if absent. Runs auto-migrate to apply schema.
// Use ":memory:" as path for in-memory databases (testing).
func Open(path string) (*ent.Client, error) {
	dsn := "file:" + path + "?_fk=1&_journal_mode=WAL"
	if path == ":memory:" {
		dsn = "file::memory:?mode=memory&_fk=1"
	}
	drv, err := entsql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open %q: %w", path, err)
	}
	client := ent.NewClient(ent.Driver(drv))
	if err := client.Schema.Create(context.Background()); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("db: auto-migrate: %w", err)
	}
	return client, nil
}
```

- [ ] **Step 4: Run tests**

```bash
cd server && go test ./internal/db/... -run TestOpen -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C .. add server/internal/db/client.go server/internal/db/client_test.go
git -C .. commit -m "feat(server/db): database client with auto-migrate"
```

---

## Task 5: ApiKey Repository

**Files:**
- Create: `server/internal/db/repo/api_key_repo.go`
- Create: `server/internal/db/repo/api_key_repo_test.go`

- [ ] **Step 1: Write the tests first**

```go
// server/internal/db/repo/api_key_repo_test.go
package repo_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func openTestDB(t *testing.T) *db.Client { // returns *ent.Client
	t.Helper()
	client, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestApiKeyRepo_CreateAndGetByHash(t *testing.T) {
	client := openTestDB(t)
	r := repo.NewApiKeyRepo(client)

	key, err := r.Create(t.Context(), "my-key", "deadbeef", []string{"tasks:read"})
	require.NoError(t, err)
	require.Equal(t, "my-key", key.Name)
	require.Equal(t, "deadbeef", key.KeyHash)

	got, err := r.GetByHash(t.Context(), "deadbeef")
	require.NoError(t, err)
	require.Equal(t, key.ID, got.ID)
}

func TestApiKeyRepo_List(t *testing.T) {
	client := openTestDB(t)
	r := repo.NewApiKeyRepo(client)

	_, err := r.Create(t.Context(), "k1", "hash1", []string{"tasks:read"})
	require.NoError(t, err)
	_, err = r.Create(t.Context(), "k2", "hash2", []string{"tasks:write"})
	require.NoError(t, err)

	keys, err := r.List(t.Context())
	require.NoError(t, err)
	require.Len(t, keys, 2)
}

func TestApiKeyRepo_Delete(t *testing.T) {
	client := openTestDB(t)
	r := repo.NewApiKeyRepo(client)

	key, err := r.Create(t.Context(), "to-delete", "hashX", nil)
	require.NoError(t, err)

	require.NoError(t, r.Delete(t.Context(), key.ID))

	keys, err := r.List(t.Context())
	require.NoError(t, err)
	require.Empty(t, keys)
}

func TestApiKeyRepo_TouchLastUsed(t *testing.T) {
	client := openTestDB(t)
	r := repo.NewApiKeyRepo(client)

	key, err := r.Create(t.Context(), "track", "hashT", nil)
	require.NoError(t, err)
	require.Nil(t, key.LastUsedAt)

	require.NoError(t, r.TouchLastUsed(t.Context(), key.ID))

	got, err := r.GetByHash(t.Context(), "hashT")
	require.NoError(t, err)
	require.NotNil(t, got.LastUsedAt)
	require.WithinDuration(t, time.Now(), *got.LastUsedAt, 2*time.Second)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd server && go test ./internal/db/repo/... -v
```

Expected: FAIL — `repo` package doesn't exist yet.

- [ ] **Step 3: Create api_key_repo.go**

```go
// server/internal/db/repo/api_key_repo.go
package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/apikey"
)

// ApiKeyRepo manages API key persistence.
type ApiKeyRepo interface {
	Create(ctx context.Context, name, hash string, scopes []string) (*ent.ApiKey, error)
	GetByHash(ctx context.Context, hash string) (*ent.ApiKey, error)
	List(ctx context.Context) ([]*ent.ApiKey, error)
	Delete(ctx context.Context, id string) error
	TouchLastUsed(ctx context.Context, id string) error
}

type entApiKeyRepo struct {
	client *ent.Client
}

// NewApiKeyRepo returns an ApiKeyRepo backed by the given ent client.
func NewApiKeyRepo(client *ent.Client) ApiKeyRepo {
	return &entApiKeyRepo{client: client}
}

func (r *entApiKeyRepo) Create(ctx context.Context, name, hash string, scopes []string) (*ent.ApiKey, error) {
	if scopes == nil {
		scopes = []string{}
	}
	k, err := r.client.ApiKey.Create().
		SetID(uuid.New().String()).
		SetName(name).
		SetKeyHash(hash).
		SetScopes(scopes).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("apikey.Create: %w", err)
	}
	return k, nil
}

func (r *entApiKeyRepo) GetByHash(ctx context.Context, hash string) (*ent.ApiKey, error) {
	k, err := r.client.ApiKey.Query().
		Where(apikey.KeyHash(hash), apikey.Active(true)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("apikey.GetByHash: %w", err)
	}
	return k, nil
}

func (r *entApiKeyRepo) List(ctx context.Context) ([]*ent.ApiKey, error) {
	keys, err := r.client.ApiKey.Query().
		Where(apikey.Active(true)).
		Order(ent.Asc(apikey.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("apikey.List: %w", err)
	}
	return keys, nil
}

func (r *entApiKeyRepo) Delete(ctx context.Context, id string) error {
	// Soft-delete: set active = false so hash remains in DB for audit.
	err := r.client.ApiKey.UpdateOneID(id).
		SetActive(false).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("apikey.Delete %s: %w", id, err)
	}
	return nil
}

func (r *entApiKeyRepo) TouchLastUsed(ctx context.Context, id string) error {
	now := time.Now()
	err := r.client.ApiKey.UpdateOneID(id).
		SetLastUsedAt(now).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("apikey.TouchLastUsed %s: %w", id, err)
	}
	return nil
}
```

Note: the exact import path for the predicate package depends on ent code gen output. If the entity is named `ApiKey`, ent generates a `apikey` predicate package at `github.com/lx-wnk/agent-dashboard/server/internal/db/ent/apikey`. Adjust the import if it differs (check `server/internal/db/ent/` after step 3 of Task 3).

- [ ] **Step 4: Run tests**

```bash
cd server && go test ./internal/db/repo/... -v -run TestApiKeyRepo
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git -C .. add server/internal/db/repo/
git -C .. commit -m "feat(server/db): ApiKey repository with CRUD and hash lookup"
```

---

## Task 6: User Repository

**Files:**
- Create: `server/internal/db/repo/user_repo.go`
- Create: `server/internal/db/repo/user_repo_test.go`

- [ ] **Step 1: Write tests first**

```go
// server/internal/db/repo/user_repo_test.go
package repo_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func TestUserRepo_UpsertAndGet(t *testing.T) {
	client := openTestDB(t) // defined in api_key_repo_test.go
	r := repo.NewUserRepo(client)

	user, err := r.Upsert(t.Context(), repo.GitHubUserInfo{
		ID:          "123456",
		Login:       "octocat",
		DisplayName: "The Octocat",
		AvatarURL:   "https://example.com/avatar.png",
	})
	require.NoError(t, err)
	require.Equal(t, "123456", user.ID)
	require.Equal(t, "octocat", user.GithubLogin)

	got, err := r.GetByID(t.Context(), "123456")
	require.NoError(t, err)
	require.Equal(t, user.ID, got.ID)
}

func TestUserRepo_Upsert_UpdatesLogin(t *testing.T) {
	client := openTestDB(t)
	r := repo.NewUserRepo(client)

	_, err := r.Upsert(t.Context(), repo.GitHubUserInfo{ID: "7", Login: "oldname"})
	require.NoError(t, err)

	// GitHub login changed — upsert should update it.
	updated, err := r.Upsert(t.Context(), repo.GitHubUserInfo{ID: "7", Login: "newname"})
	require.NoError(t, err)
	require.Equal(t, "newname", updated.GithubLogin)
}

func TestUserRepo_GetByID_NotFound(t *testing.T) {
	client := openTestDB(t)
	r := repo.NewUserRepo(client)

	_, err := r.GetByID(t.Context(), "nonexistent")
	require.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd server && go test ./internal/db/repo/... -run TestUserRepo -v
```

Expected: FAIL — `UserRepo` not defined.

- [ ] **Step 3: Create user_repo.go**

```go
// server/internal/db/repo/user_repo.go
package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	entuser "github.com/lx-wnk/agent-dashboard/server/internal/db/ent/user"
)

// GitHubUserInfo holds the fields returned by the GitHub user API.
type GitHubUserInfo struct {
	ID          string
	Login       string
	DisplayName string
	AvatarURL   string
}

// UserRepo manages user persistence.
type UserRepo interface {
	Upsert(ctx context.Context, info GitHubUserInfo) (*ent.User, error)
	GetByID(ctx context.Context, id string) (*ent.User, error)
}

type entUserRepo struct {
	client *ent.Client
}

// NewUserRepo returns a UserRepo backed by the given ent client.
func NewUserRepo(client *ent.Client) UserRepo {
	return &entUserRepo{client: client}
}

// Upsert creates or updates a user by GitHub ID.
// GitHub ID is used as the primary key (stable across username renames).
func (r *entUserRepo) Upsert(ctx context.Context, info GitHubUserInfo) (*ent.User, error) {
	now := time.Now()
	u, err := r.client.User.Create().
		SetID(info.ID).
		SetGithubLogin(info.Login).
		SetDisplayName(info.DisplayName).
		SetAvatarURL(info.AvatarURL).
		SetLastLoginAt(now).
		OnConflictColumns(entuser.FieldID).
		UpdateNewValues().
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("user.Upsert %s: %w", info.ID, err)
	}
	return u, nil
}

// GetByID returns a user by their GitHub ID.
func (r *entUserRepo) GetByID(ctx context.Context, id string) (*ent.User, error) {
	u, err := r.client.User.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("user.GetByID %s: %w", id, err)
	}
	return u, nil
}
```

Note: `OnConflictColumns` requires ent v0.12+. With ent v0.14.6 this is available. If the ent-generated `user` package is at a different import path, adjust accordingly.

- [ ] **Step 4: Run all repo tests**

```bash
cd server && go test ./internal/db/repo/... -v
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git -C .. add server/internal/db/repo/user_repo.go server/internal/db/repo/user_repo_test.go
git -C .. commit -m "feat(server/db): User repository with GitHub upsert"
```

---

## Task 7: GitHub OAuth Client

**Files:**
- Create: `server/internal/auth/github.go`
- Create: `server/internal/auth/github_test.go`

- [ ] **Step 1: Write tests first**

```go
// server/internal/auth/github_test.go
package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
)

func TestGitHubClient_GetUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         12345,
			"login":      "octocat",
			"name":       "The Octocat",
			"avatar_url": "https://example.com/avatar.png",
		})
	}))
	defer srv.Close()

	client := auth.NewGitHubClient("id", "secret", auth.WithUserAPIURL(srv.URL))
	user, err := client.GetUser(t.Context(), "test-token")
	require.NoError(t, err)
	require.Equal(t, "12345", user.ID) // numeric GitHub ID converted to string
	require.Equal(t, "octocat", user.Login)
}

func TestGitHubClient_BuildAuthURL(t *testing.T) {
	client := auth.NewGitHubClient("my-client-id", "secret")
	url := client.BuildAuthURL("my-state", "http://callback")
	require.Contains(t, url, "client_id=my-client-id")
	require.Contains(t, url, "state=my-state")
	require.Contains(t, url, "redirect_uri=")
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd server && go test ./internal/auth/... -run TestGitHub -v
```

Expected: FAIL — `GitHubClient` not defined.

- [ ] **Step 3: Create github.go**

```go
// server/internal/auth/github.go
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultGitHubTokenURL = "https://github.com/login/oauth/access_token"
	defaultGitHubUserURL  = "https://api.github.com/user"
	defaultGitHubAuthURL  = "https://github.com/login/oauth/authorize"
)

// GitHubUserProfile holds the fields we care about from the GitHub user API.
type GitHubUserProfile struct {
	ID          string // numeric GitHub user ID, as string
	Login       string
	DisplayName string
	AvatarURL   string
}

// GitHubClient exchanges OAuth codes and fetches GitHub user profiles.
type GitHubClient struct {
	clientID     string
	clientSecret string
	tokenURL     string
	userURL      string
	authURL      string
	httpClient   *http.Client
}

// githubOption is a functional option for GitHubClient.
type githubOption func(*GitHubClient)

// WithUserAPIURL overrides the GitHub user API URL (for testing).
func WithUserAPIURL(u string) githubOption {
	return func(c *GitHubClient) { c.userURL = u }
}

// WithTokenURL overrides the GitHub token exchange URL (for testing).
func WithTokenURL(u string) githubOption {
	return func(c *GitHubClient) { c.tokenURL = u }
}

// NewGitHubClient creates a GitHubClient for the given OAuth app credentials.
func NewGitHubClient(clientID, clientSecret string, opts ...githubOption) *GitHubClient {
	c := &GitHubClient{
		clientID:     clientID,
		clientSecret: clientSecret,
		tokenURL:     defaultGitHubTokenURL,
		userURL:      defaultGitHubUserURL,
		authURL:      defaultGitHubAuthURL,
		httpClient:   &http.Client{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// BuildAuthURL returns the GitHub authorization URL for the OAuth flow.
func (c *GitHubClient) BuildAuthURL(state, redirectURI string) string {
	v := url.Values{}
	v.Set("client_id", c.clientID)
	v.Set("state", state)
	v.Set("redirect_uri", redirectURI)
	v.Set("scope", "read:user")
	return c.authURL + "?" + v.Encode()
}

// ExchangeCode exchanges an OAuth authorization code for an access token.
func (c *GitHubClient) ExchangeCode(ctx context.Context, code, redirectURI string) (string, error) {
	v := url.Values{}
	v.Set("client_id", c.clientID)
	v.Set("client_secret", c.clientSecret)
	v.Set("code", code)
	v.Set("redirect_uri", redirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(v.Encode()))
	if err != nil {
		return "", fmt.Errorf("github.ExchangeCode: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github.ExchangeCode: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("github.ExchangeCode: decode: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("github.ExchangeCode: %s", result.Error)
	}
	return result.AccessToken, nil
}

// GetUser fetches the GitHub user profile for the given access token.
func (c *GitHubClient) GetUser(ctx context.Context, accessToken string) (*GitHubUserProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.userURL, nil)
	if err != nil {
		return nil, fmt.Errorf("github.GetUser: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github.GetUser: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("github.GetUser: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github.GetUser: HTTP %d: %s", resp.StatusCode, body)
	}

	var raw struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("github.GetUser: decode: %w", err)
	}
	return &GitHubUserProfile{
		ID:          strconv.FormatInt(raw.ID, 10),
		Login:       raw.Login,
		DisplayName: raw.Name,
		AvatarURL:   raw.AvatarURL,
	}, nil
}
```

- [ ] **Step 4: Run tests**

```bash
cd server && go test ./internal/auth/... -v
```

Expected: all pass (including existing JWT tests).

- [ ] **Step 5: Commit**

```bash
git -C .. add server/internal/auth/github.go server/internal/auth/github_test.go
git -C .. commit -m "feat(server/auth): GitHub OAuth client with code exchange and user fetch"
```

---

## Task 8: Auth Route Handler

**Files:**
- Create: `server/internal/api/auth/handler.go`
- Create: `server/internal/api/auth/handler_test.go`
- Modify: `server/internal/config/config.go` — add `CallbackURL()` method

- [ ] **Step 1: Add CallbackURL() to config**

In `server/internal/config/config.go`, add after the existing `Addr()` method:

```go
// CallbackURL returns the GitHub OAuth redirect URI derived from Host and Port.
func (c Config) CallbackURL() string {
	return fmt.Sprintf("http://%s/api/auth/callback", c.Addr())
}
```

Verify build:
```bash
cd server && go build ./internal/config/...
```

- [ ] **Step 2: Write handler tests**

```go
// server/internal/api/auth/handler_test.go
package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	apiauth "github.com/lx-wnk/agent-dashboard/server/internal/api/auth"
)

func TestHandler_GitHubRedirect_NoClientID(t *testing.T) {
	h := apiauth.NewHandler(apiauth.Deps{
		JWTSecret:   "test-secret",
		CallbackURL: "http://localhost/api/auth/callback",
		// GitHubClient nil: misconfigured server
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/auth/github", nil)
	err := h.GitHubRedirect(w, r)
	require.Error(t, err) // must return error when GitHub not configured
}

func TestHandler_Logout(t *testing.T) {
	h := apiauth.NewHandler(apiauth.Deps{JWTSecret: "test-secret"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	err := h.Logout(w, r)
	require.NoError(t, err)
	// Cookie should be cleared
	cookies := w.Result().Cookies()
	var found bool
	for _, c := range cookies {
		if c.Name == "auth_token" {
			found = true
			require.LessOrEqual(t, c.MaxAge, 0)
		}
	}
	require.True(t, found, "auth_token cookie should be cleared")
}

func TestHandler_Me_Unauthenticated(t *testing.T) {
	h := apiauth.NewHandler(apiauth.Deps{JWTSecret: "test-secret"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	err := h.Me(w, r)
	require.Error(t, err) // no JWT in context → error
}
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
cd server && go test ./internal/api/auth/... -v
```

Expected: FAIL — package not found.

- [ ] **Step 4: Create handler.go**

```go
// server/internal/api/auth/handler.go
package auth

import (
	"encoding/json"
	"fmt"
	"net/http"

	serverauth "github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// Deps holds all dependencies for the auth handler.
type Deps struct {
	JWTSecret    string
	CallbackURL  string
	GitHubClient *serverauth.GitHubClient
	UserRepo     repo.UserRepo
}

// Handler handles GitHub OAuth routes.
type Handler struct {
	deps Deps
}

// NewHandler creates an auth Handler.
func NewHandler(deps Deps) *Handler {
	return &Handler{deps: deps}
}

// GitHubRedirect redirects the browser to the GitHub authorization URL.
// GET /api/auth/github
func (h *Handler) GitHubRedirect(w http.ResponseWriter, r *http.Request) error {
	if h.deps.GitHubClient == nil {
		return fmt.Errorf("%w: GitHub OAuth not configured (set DASHBOARD_GITHUB_CLIENT_ID)", errNotConfigured)
	}
	// Use a short-lived JWT as the CSRF state token.
	state, err := serverauth.SignJWT(h.deps.JWTSecret, "oauth-state")
	if err != nil {
		return fmt.Errorf("auth: build state: %w", err)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		MaxAge:   300, // 5 minutes
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})
	redirectURL := h.deps.GitHubClient.BuildAuthURL(state, h.deps.CallbackURL)
	http.Redirect(w, r, redirectURL, http.StatusFound)
	return nil
}

// Callback handles the GitHub OAuth callback.
// GET /api/auth/callback?code=XXX&state=YYY
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) error {
	if h.deps.GitHubClient == nil {
		return fmt.Errorf("%w: GitHub OAuth not configured", errNotConfigured)
	}

	// Verify CSRF state.
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil {
		return fmt.Errorf("%w: missing state cookie", serverauth.ErrTokenInvalid)
	}
	if _, err := serverauth.VerifyJWT(h.deps.JWTSecret, stateCookie.Value); err != nil {
		return fmt.Errorf("%w: invalid state", serverauth.ErrTokenInvalid)
	}
	if r.URL.Query().Get("state") != stateCookie.Value {
		return fmt.Errorf("%w: state mismatch", serverauth.ErrTokenInvalid)
	}

	// Exchange code for access token.
	code := r.URL.Query().Get("code")
	accessToken, err := h.deps.GitHubClient.ExchangeCode(r.Context(), code, h.deps.CallbackURL)
	if err != nil {
		return fmt.Errorf("auth: exchange code: %w", err)
	}

	// Fetch GitHub user profile.
	profile, err := h.deps.GitHubClient.GetUser(r.Context(), accessToken)
	if err != nil {
		return fmt.Errorf("auth: get user: %w", err)
	}

	// Upsert user in DB.
	user, err := h.deps.UserRepo.Upsert(r.Context(), repo.GitHubUserInfo{
		ID:          profile.ID,
		Login:       profile.Login,
		DisplayName: profile.DisplayName,
		AvatarURL:   profile.AvatarURL,
	})
	if err != nil {
		return fmt.Errorf("auth: upsert user: %w", err)
	}

	// Issue JWT with GitHub user ID as subject.
	token, err := serverauth.SignJWT(h.deps.JWTSecret, user.ID)
	if err != nil {
		return fmt.Errorf("auth: sign jwt: %w", err)
	}

	// Clear state cookie, set auth cookie.
	http.SetCookie(w, &http.Cookie{Name: "oauth_state", MaxAge: -1, Path: "/"})
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		MaxAge:   86400 * 7, // 7 days
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})
	http.Redirect(w, r, "/", http.StatusFound)
	return nil
}

// Logout clears the auth cookie.
// POST /api/auth/logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) error {
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		MaxAge:   -1,
		HttpOnly: true,
		Path:     "/",
	})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Me returns the currently authenticated user.
// GET /api/auth/me
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) error {
	payload, ok := serverauth.PayloadFromContext(r.Context())
	if !ok {
		return serverauth.ErrTokenInvalid
	}
	if h.deps.UserRepo == nil {
		return fmt.Errorf("auth: user repo not configured")
	}
	user, err := h.deps.UserRepo.GetByID(r.Context(), payload.Sub)
	if err != nil {
		return fmt.Errorf("%w: user not found", serverauth.ErrTokenInvalid)
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(map[string]any{
		"id":           user.ID,
		"github_login": user.GithubLogin,
		"display_name": user.DisplayName,
		"avatar_url":   user.AvatarURL,
		"is_admin":     user.IsAdmin,
	})
}

var errNotConfigured = fmt.Errorf("not configured")
```

- [ ] **Step 5: Run tests**

```bash
cd server && go test ./internal/api/auth/... -v
```

Expected: all pass.

- [ ] **Step 6: Run all tests to check no regressions**

```bash
cd server && go test ./...
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git -C .. add server/internal/api/auth/ server/internal/config/config.go
git -C .. commit -m "feat(server/api): auth handler — GitHub OAuth redirect, callback, logout, me"
```

---

## Task 9: API Key Route Handler

**Files:**
- Create: `server/internal/api/apikeys/handler.go`
- Create: `server/internal/api/apikeys/handler_test.go`

- [ ] **Step 1: Write handler tests**

```go
// server/internal/api/apikeys/handler_test.go
package apikeys_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/apikeys"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func setupHandler(t *testing.T) (*apikeys.Handler, *chi.Mux) {
	t.Helper()
	client, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	r := repo.NewApiKeyRepo(client)
	h := apikeys.NewHandler(r)

	mux := chi.NewRouter()
	mux.Get("/api/settings/api-keys", apikeys.Wrap(h.List))
	mux.Post("/api/settings/api-keys", apikeys.Wrap(h.Create))
	mux.Delete("/api/settings/api-keys/{id}", apikeys.Wrap(h.Delete))
	return h, mux
}

func TestApiKeyHandler_CreateAndList(t *testing.T) {
	_, mux := setupHandler(t)

	body, _ := json.Marshal(map[string]any{"name": "my-key", "scopes": []string{"tasks:read"}})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/settings/api-keys", bytes.NewReader(body)))
	require.Equal(t, http.StatusCreated, w.Code)

	var created map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&created))
	require.Contains(t, created, "token") // raw token shown once
	require.Contains(t, created, "id")

	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/settings/api-keys", nil))
	require.Equal(t, http.StatusOK, w2.Code)

	var list []map[string]any
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&list))
	require.Len(t, list, 1)
	require.NotContains(t, list[0], "key_hash") // hash must never be returned
	require.NotContains(t, list[0], "token")    // raw token only shown at creation
}

func TestApiKeyHandler_Delete(t *testing.T) {
	_, mux := setupHandler(t)

	body, _ := json.Marshal(map[string]any{"name": "to-delete", "scopes": []string{}})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/settings/api-keys", bytes.NewReader(body)))
	require.Equal(t, http.StatusCreated, w.Code)

	var created map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&created))
	id := created["id"].(string)

	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, httptest.NewRequest(http.MethodDelete, "/api/settings/api-keys/"+id, nil))
	require.Equal(t, http.StatusNoContent, w2.Code)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd server && go test ./internal/api/apikeys/... -v
```

Expected: FAIL — package not found.

- [ ] **Step 3: Create handler.go**

```go
// server/internal/api/apikeys/handler.go
package apikeys

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	apiErr "github.com/lx-wnk/agent-dashboard/server/internal/api"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// Handler handles API key management routes.
type Handler struct {
	repo repo.ApiKeyRepo
}

// NewHandler creates an API key Handler.
func NewHandler(r repo.ApiKeyRepo) *Handler {
	return &Handler{repo: r}
}

// Wrap converts a handler-returns-error function to chi-compatible HandlerFunc.
// Reuses the project-standard pattern from api/errors.go.
func Wrap(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return apiErr.ErrorMiddleware(apiErr.HandlerFunc(fn))
}

// List returns all active API keys (never includes hash or raw token).
// GET /api/settings/api-keys
func (h *Handler) List(w http.ResponseWriter, r *http.Request) error {
	keys, err := h.repo.List(r.Context())
	if err != nil {
		return fmt.Errorf("apikeys.List: %w", err)
	}
	type keyView struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		Scopes    []string `json:"scopes"`
		Active    bool     `json:"active"`
		CreatedAt string   `json:"created_at"`
	}
	out := make([]keyView, len(keys))
	for i, k := range keys {
		out[i] = keyView{
			ID:        k.ID,
			Name:      k.Name,
			Scopes:    k.Scopes,
			Active:    k.Active,
			CreatedAt: k.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(out)
}

// Create creates a new API key. Returns the raw token once — it cannot be retrieved again.
// POST /api/settings/api-keys  body: {"name":"...","scopes":["tasks:read",...]}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return fmt.Errorf("%w: invalid JSON", apiErr.ErrBadRequest)
	}
	if body.Name == "" {
		return fmt.Errorf("%w: name is required", apiErr.ErrBadRequest)
	}

	// Generate random 32-byte token, base64url-encode as the raw token.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("apikeys.Create: generate token: %w", err)
	}
	token := "mcp_" + base64.RawURLEncoding.EncodeToString(raw)

	// Store only the SHA-256 hash.
	sum := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(sum[:])

	key, err := h.repo.Create(r.Context(), body.Name, hash, body.Scopes)
	if err != nil {
		return fmt.Errorf("apikeys.Create: %w", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	return json.NewEncoder(w).Encode(map[string]any{
		"id":         key.ID,
		"name":       key.Name,
		"scopes":     key.Scopes,
		"token":      token, // shown once only
		"created_at": key.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// Delete soft-deletes an API key by ID.
// DELETE /api/settings/api-keys/{id}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if id == "" {
		return fmt.Errorf("%w: id is required", apiErr.ErrBadRequest)
	}
	if err := h.repo.Delete(r.Context(), id); err != nil {
		return fmt.Errorf("apikeys.Delete: %w", err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
```

- [ ] **Step 4: Run tests**

```bash
cd server && go test ./internal/api/apikeys/... -v
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git -C .. add server/internal/api/apikeys/
git -C .. commit -m "feat(server/api): API key CRUD handler — create returns raw token once"
```

---

## Task 10: Router + Wire DI Integration

**Files:**
- Modify: `server/internal/api/router.go`
- Modify: `server/cmd/serve/wire.go`
- Modify: `server/cmd/serve/wire_gen.go`

- [ ] **Step 1: Update RouterDeps to carry db client and GitHub client**

In `server/internal/api/router.go`, update the deps structs:

```go
// RouterDeps holds all dependencies injected into the router.
type RouterDeps struct {
	Config           RouterConfig
	AgentBroadcaster *sse.Broadcaster
	DBClient         *ent.Client      // nil if DB not configured
	GitHubClient     *authpkg.GitHubClient // nil if OAuth not configured
	UserRepo         repo.UserRepo    // nil if DB not configured
	ApiKeyRepo       repo.ApiKeyRepo  // nil if DB not configured
}
```

Add the new imports to router.go:
```go
import (
    // existing imports ...
    apiauth "github.com/lx-wnk/agent-dashboard/server/internal/api/auth"
    "github.com/lx-wnk/agent-dashboard/server/internal/api/apikeys"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
    authpkg "github.com/lx-wnk/agent-dashboard/server/internal/auth"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)
```

Add auth and API key routes inside `NewRouter`, after the existing `/api/agents` routes and before the SPA catch-all:

```go
// Auth routes (public — no JWT required for the OAuth dance)
authHandler := apiauth.NewHandler(apiauth.Deps{
    JWTSecret:    deps.Config.JWTSecret,
    CallbackURL:  deps.Config.CallbackURL,
    GitHubClient: deps.GitHubClient,
    UserRepo:     deps.UserRepo,
})
r.Get("/api/auth/github", ErrorMiddleware(authHandler.GitHubRedirect))
r.Get("/api/auth/callback", ErrorMiddleware(authHandler.Callback))
r.Post("/api/auth/logout", ErrorMiddleware(authHandler.Logout))

// Protected routes requiring JWT
r.Group(func(r chi.Router) {
    r.Use(auth.RequireAuth(deps.Config.JWTSecret))
    // ... existing agent routes ...

    // Auth: current user
    r.Get("/api/auth/me", ErrorMiddleware(authHandler.Me))

    // API keys (admin only in Phase 3; Phase 2 any authenticated user)
    if deps.ApiKeyRepo != nil {
        apiKeyHandler := apikeys.NewHandler(deps.ApiKeyRepo)
        r.Get("/api/settings/api-keys", apikeys.Wrap(apiKeyHandler.List))
        r.Post("/api/settings/api-keys", apikeys.Wrap(apiKeyHandler.Create))
        r.Delete("/api/settings/api-keys/{id}", apikeys.Wrap(apiKeyHandler.Delete))
    }
})
```

Note: `RouterConfig` needs a `CallbackURL` field OR you add a `CallbackURL` field to `RouterDeps.Config`. Since `Config.CallbackURL()` is a method, either pass it as a string in RouterConfig or call it directly from RouterDeps.Config. Add it to `RouterConfig`:

```go
type RouterConfig struct {
    JWTSecret   string
    Embedded    http.FileSystem // Vue SPA embed (set in wire_gen)
    CallbackURL string          // GitHub OAuth callback URL
}
```

- [ ] **Step 2: Update wire_gen.go to wire DB + GitHub client**

In `server/cmd/serve/wire_gen.go`, update `initializeServer` and provider functions:

```go
// Code generated by Wire. DO NOT EDIT.

package main

import (
	"net/http"

	"github.com/lx-wnk/agent-dashboard/server/internal/api"
	authpkg "github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/config"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

func initializeServer(cfg config.Config) (*api.Server, *sse.Broadcaster, error) {
	entClient, err := provideDB(cfg)
	if err != nil {
		return nil, nil, err
	}
	broadcaster := sse.NewBroadcaster()
	routerConfig := provideRouterConfig(cfg)
	routerDeps := provideRouterDeps(cfg, routerConfig, broadcaster, entClient)
	router := api.NewRouter(routerDeps)
	server := provideServer(cfg, router)
	return server, broadcaster, nil
}

func provideDB(cfg config.Config) (*ent.Client, error) {
	return db.Open(cfg.DBPath)
}

func provideGitHubClient(cfg config.Config) *authpkg.GitHubClient {
	if cfg.GitHubClientID == "" {
		return nil
	}
	return authpkg.NewGitHubClient(cfg.GitHubClientID, cfg.GitHubClientSecret)
}

func provideRouterConfig(cfg config.Config) api.RouterConfig {
	return api.RouterConfig{
		JWTSecret:   cfg.JWTSecret,
		CallbackURL: cfg.CallbackURL(),
	}
}

func provideRouterDeps(cfg config.Config, rc api.RouterConfig, b *sse.Broadcaster, client *ent.Client) api.RouterDeps {
	var userRepo repo.UserRepo
	var apiKeyRepo repo.ApiKeyRepo
	if client != nil {
		userRepo = repo.NewUserRepo(client)
		apiKeyRepo = repo.NewApiKeyRepo(client)
	}
	return api.RouterDeps{
		Config:           rc,
		AgentBroadcaster: b,
		DBClient:         client,
		GitHubClient:     provideGitHubClient(cfg),
		UserRepo:         userRepo,
		ApiKeyRepo:       apiKeyRepo,
	}
}

func provideServer(cfg config.Config, handler http.Handler) *api.Server {
	return api.NewServer(cfg.Addr(), handler, cfg.ShutdownTimeout())
}
```

- [ ] **Step 3: Build**

```bash
cd server && go build ./...
```

Fix any import errors (package paths, field names). Common issues:
- `ent.Client` import path: `github.com/lx-wnk/agent-dashboard/server/internal/db/ent`
- `RouterDeps.Config.CallbackURL` field — if you added it to `RouterConfig`, also pass it in `provideRouterConfig`
- auth handler import conflicts: use aliases (`apiauth`, `authpkg`) to avoid collision with `server/internal/auth`

- [ ] **Step 4: Run all tests**

```bash
cd server && go test ./...
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git -C .. add server/internal/api/router.go server/cmd/serve/wire.go server/cmd/serve/wire_gen.go
git -C .. commit -m "feat(server): wire database, GitHub OAuth, and API key routes into router"
```

---

## Task 11: Test + Lint Pass

- [ ] **Step 1: Run all tests with race detector**

```bash
cd /path/to/repo && task test
```

Expected: all pass. If failures, fix before continuing.

- [ ] **Step 2: Run linter**

```bash
task lint
```

Common fixes:
- Missing doc comments on exported types in `db/repo/`, `api/auth/`, `api/apikeys/`
- `errcheck`: wrap `json.NewEncoder(w).Encode(v)` as `_ = json.NewEncoder(w).Encode(v)` or handle error
- If `gosec` flags `crypto/rand.Read` return value: `if _, err := rand.Read(raw); err != nil { ... }` (already handled)
- If `revive` flags `errNotConfigured` as unexported: move it to `api/errors.go` as an exported sentinel or change to inline `errors.New`

- [ ] **Step 3: Run vuln scan**

```bash
task vuln
```

Expected: clean.

- [ ] **Step 4: Commit any lint fixes**

Only commit if there were fixes:
```bash
git -C .. add -A
git -C .. commit -m "fix: resolve golangci-lint findings in Phase 2 code"
```

---

## Task 12: Integration Smoke Test

- [ ] **Step 1: Start server (requires DASHBOARD_JWT_SECRET)**

```bash
cd server
DASHBOARD_JWT_SECRET=test-secret go run ./cmd/serve/... serve &
SERVER_PID=$!
sleep 1
```

- [ ] **Step 2: Health check still works**

```bash
curl -s http://127.0.0.1:13120/api/system/health | python3 -m json.tool
```

Expected: `{"status":"ok",...}`

- [ ] **Step 3: GitHub redirect (no client ID configured → error)**

```bash
curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:13120/api/auth/github
```

Expected: `500` (GitHub OAuth not configured — intentional, no client ID set).

If DASHBOARD_GITHUB_CLIENT_ID is set:
Expected: `302` redirect to `https://github.com/login/oauth/authorize?...`

- [ ] **Step 4: API key create (requires auth — expect 401)**

```bash
curl -s -o /dev/null -w "%{http_code}" \
  -X POST http://127.0.0.1:13120/api/settings/api-keys \
  -H "Content-Type: application/json" \
  -d '{"name":"test","scopes":["tasks:read"]}'
```

Expected: `401` (no JWT).

- [ ] **Step 5: Stop server**

```bash
kill $SERVER_PID
wait $SERVER_PID 2>/dev/null || true
```

- [ ] **Step 6: Final commit**

```bash
git -C .. add -A
# Only if there are uncommitted changes:
git -C .. commit -m "feat: Phase 2 complete — database (ent), GitHub OAuth, API key management"
```

---

## Self-Review Checklist

### Spec Coverage

| Spec requirement | Task |
|---|---|
| ent ORM setup (schema-first, code gen) | Tasks 2–3 |
| SQLite via modernc.org/sqlite (CGO-free) | Task 1, 4 |
| Auto-migrate at startup (`client.Schema.Create`) | Task 4 |
| ApiKey entity (id, name, hash, scopes, active) | Task 2 |
| User entity (GitHub ID, login, display name, admin) | Task 2 |
| ApiKey repo: create, get by hash, list, soft-delete, touch last used | Task 5 |
| User repo: upsert by GitHub ID, get by ID | Task 6 |
| GitHub OAuth: exchange code, fetch user profile | Task 7 |
| Auth routes: `/api/auth/github`, `/callback`, `/logout`, `/me` | Task 8 |
| API key routes: GET/POST/DELETE `/api/settings/api-keys` | Task 9 |
| Raw token shown once at create; SHA-256 hash persisted | Task 9 |
| Wire DI updated to include DB client, GitHub client, repos | Task 10 |
| DB opened at startup; startup aborts on migration failure | Task 10 |

### Phase 3 Deferred

- Full pipeline orchestrator (Task, StageRun, permissions tables)
- Task CRUD + `/api/tasks/stream` SSE
- MCP server (`/api/mcp` StreamableHTTP)
- Permission management routes
- Hooks routes, presets, webpush, history, memory, search routes
- Channel bridge (Go version of `channel/`)
- Versioned Atlas migration files (currently using auto-migrate)
- Admin-only gate on API key endpoints
