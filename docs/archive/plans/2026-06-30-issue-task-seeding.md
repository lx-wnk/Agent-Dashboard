# Issue → Task Seeding — TDD Implementation Plan

**Goal:** Let users paste a GitHub or Jira issue reference, fetch it via `POST /api/tracker/fetch`, and pre-fill the New-Task form. Tokens stored encrypted at rest, masked in the API.

**Architecture:** New `server/internal/tracker/` domain package (types, GitHub client, Jira client, Resolve dispatch) + `server/internal/api/tracker/` HTTP handler that wraps `pluginsettings.Service` for encrypted token storage. Frontend: `useTrackerImport` composable + "Import from issue" affordance in `BacklogForm.vue` + `TrackerSettingsPanel.vue`.

**Tech Stack:** Go 1.26 (chi, ent ORM, modernc/sqlite), Vue 3 TypeScript, `go test` with `net/http/httptest`, vitest.

---

## Quick-reference: key seams

| Seam | Location |
|---|---|
| Encrypted settings storage | `pluginsettings.Service` (`server/internal/pluginsettings/service.go`) with `pluginID = "tracker"` — reuses `plugin_setting` ent table; no new table |
| Secret masked sentinel | `pluginsettings.MaskedSentinel = "********"` (line 19 of service.go); `Put` skips persistence when sentinel re-submitted |
| Repo bridge | `pluginSettingRepoAdapter` (`server/cmd/serve/plugin_adapters.go:15`) bridges `repo.PluginSettingRepo` → `pluginsettings.Repo` |
| DI wiring point | `pluginSettingsSvc` built at `server/cmd/serve/di.go:241`; same instance is reused for tracker |
| Router mount pattern | Add field to `RouterDeps`, guard with `if deps.TrackerHandler != nil { deps.TrackerHandler.Mount(r) }` inside the protected `r.Group(...)` block (`server/internal/api/router.go:241`) |
| Origin check | `RequireSameOriginForMutations` middleware already applied to the whole protected group — no per-handler check needed |
| HTTP client pattern | `&http.Client{Timeout: 30 * time.Second}` (matches `pluginlifecycle/engine_http.go:18`) |
| Task create | `tasks.CreateTaskFromInput` + `CreateTaskParams.Metadata map[string]any` for source link |
| Form pre-fill seam | `BacklogForm.vue` has `title`, `slug`, `description` as `ref` vars; expose a `prefill` function |
| Toast error surface | `toast` from `src/composables/useToast.ts` — `toast.error(msg)` |
| Settings panel pattern | `PluginSettingsForm.vue` — masked `type="password"`, skip sentinel on save |

---

## Task 1 — Tracker domain: types + typed errors

### Files
- `server/internal/tracker/types.go` ← new
- `server/internal/tracker/types_test.go` ← new

### Steps

**1a. Write failing test**

```go
// server/internal/tracker/types_test.go
package tracker_test

import (
    "errors"
    "testing"

    "github.com/lx-wnk/agent-dashboard/server/internal/tracker"
)

func TestErrorsAreDistinct(t *testing.T) {
    if errors.Is(tracker.ErrTrackerAuth, tracker.ErrIssueNotFound) {
        t.Fatal("ErrTrackerAuth must not match ErrIssueNotFound")
    }
    if errors.Is(tracker.ErrTrackerAuth, tracker.ErrTrackerUpstream) {
        t.Fatal("ErrTrackerAuth must not match ErrTrackerUpstream")
    }
    if errors.Is(tracker.ErrBadRef, tracker.ErrTrackerAuth) {
        t.Fatal("ErrBadRef must not match ErrTrackerAuth")
    }
}

func TestIssueFields(t *testing.T) {
    iss := tracker.Issue{Tracker: "github", Key: "#1", Title: "T", Body: "B", URL: "https://x", Labels: []string{"bug"}}
    if iss.Tracker != "github" || iss.Key != "#1" {
        t.Fatal("unexpected field values")
    }
}
```

**1b. Run — expect compile failure** (package does not exist):
```
cd server && go test ./internal/tracker/...
```

**1c. Implement `server/internal/tracker/types.go`**

```go
// Package tracker fetches issue metadata from GitHub and Jira.
package tracker

import (
    "context"
    "errors"
)

// Sentinel errors returned by Tracker implementations and Resolve.
var (
    ErrBadRef          = errors.New("tracker: not a recognized issue reference")
    ErrTrackerAuth     = errors.New("tracker: authentication failed")
    ErrIssueNotFound   = errors.New("tracker: issue not found")
    ErrTrackerUpstream = errors.New("tracker: upstream error")
)

// Issue is the normalised issue payload returned by any Tracker.
type Issue struct {
    Tracker string   `json:"tracker"`
    Key     string   `json:"key"`
    Title   string   `json:"title"`
    Body    string   `json:"body"`
    URL     string   `json:"url"`
    Labels  []string `json:"labels"`
}

// Tracker fetches a single issue by ref string.
type Tracker interface {
    FetchIssue(ctx context.Context, ref string) (Issue, error)
}

// Config holds all tracker credentials resolved from the settings store.
type Config struct {
    GitHubToken   string
    GitHubDefRepo string // "owner/repo"; optional — bare #n refs only work when set
    JiraBaseURL   string
    JiraEmail     string
    JiraToken     string
}
```

**1d. Run — expect pass**:
```
cd server && go test ./internal/tracker/...
```

**1e. Commit**:
```
git commit --no-gpg-sign -m "feat: tracker domain types and sentinel errors"
```

---

## Task 2 — GitHub client + ref parsing

### Files
- `server/internal/tracker/github.go` ← new
- `server/internal/tracker/github_test.go` ← new

### Steps

**2a. Write failing tests**

```go
// server/internal/tracker/github_test.go
package tracker_test

import (
    "context"
    "encoding/json"
    "errors"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/lx-wnk/agent-dashboard/server/internal/tracker"
)

func ghServer(t *testing.T, status int, body any) *httptest.Server {
    t.Helper()
    return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(status)
        _ = json.NewEncoder(w).Encode(body)
    }))
}

func TestGitHubFetchIssue_URLRef(t *testing.T) {
    srv := ghServer(t, 200, map[string]any{
        "title":    "Fix login bug",
        "body":     "Description here",
        "html_url": "https://github.com/owner/repo/issues/42",
        "number":   42,
        "labels":   []map[string]any{{"name": "bug"}, {"name": "p1"}},
    })
    defer srv.Close()

    cli := tracker.NewGitHubClientWithBase(srv.URL, "token-abc", "", &http.Client{})
    iss, err := cli.FetchIssue(context.Background(), "https://github.com/owner/repo/issues/42")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if iss.Title != "Fix login bug" {
        t.Errorf("title: got %q", iss.Title)
    }
    if iss.Key != "#42" {
        t.Errorf("key: got %q", iss.Key)
    }
    if len(iss.Labels) != 2 || iss.Labels[0] != "bug" {
        t.Errorf("labels: %v", iss.Labels)
    }
    if iss.Tracker != "github" {
        t.Errorf("tracker: %q", iss.Tracker)
    }
}

func TestGitHubFetchIssue_SlashRef(t *testing.T) {
    srv := ghServer(t, 200, map[string]any{"title": "Slash ref", "body": "", "html_url": "https://github.com/o/r/issues/7", "number": 7, "labels": []any{}})
    defer srv.Close()
    cli := tracker.NewGitHubClientWithBase(srv.URL, "tok", "", &http.Client{})
    iss, err := cli.FetchIssue(context.Background(), "o/r#7")
    if err != nil {
        t.Fatalf("slash ref: %v", err)
    }
    if iss.Key != "#7" {
        t.Errorf("key: %q", iss.Key)
    }
}

func TestGitHubFetchIssue_BareRef_DefaultRepo(t *testing.T) {
    srv := ghServer(t, 200, map[string]any{"title": "Bare", "body": "", "html_url": "https://github.com/d/r/issues/3", "number": 3, "labels": []any{}})
    defer srv.Close()
    cli := tracker.NewGitHubClientWithBase(srv.URL, "tok", "d/r", &http.Client{})
    iss, err := cli.FetchIssue(context.Background(), "#3")
    if err != nil {
        t.Fatalf("bare ref: %v", err)
    }
    if iss.Key != "#3" {
        t.Errorf("key: %q", iss.Key)
    }
}

func TestGitHubFetchIssue_BareRef_NoDefaultRepo(t *testing.T) {
    cli := tracker.NewGitHubClientWithBase("http://unused", "tok", "", &http.Client{})
    _, err := cli.FetchIssue(context.Background(), "#5")
    if err == nil {
        t.Fatal("expected error for bare ref without default repo")
    }
}

func TestGitHubFetchIssue_404(t *testing.T) {
    srv := ghServer(t, 404, map[string]any{"message": "Not Found"})
    defer srv.Close()
    cli := tracker.NewGitHubClientWithBase(srv.URL, "tok", "o/r", &http.Client{})
    _, err := cli.FetchIssue(context.Background(), "o/r#99")
    if !errors.Is(err, tracker.ErrIssueNotFound) {
        t.Errorf("expected ErrIssueNotFound, got %v", err)
    }
}

func TestGitHubFetchIssue_401(t *testing.T) {
    srv := ghServer(t, 401, map[string]any{"message": "Bad credentials"})
    defer srv.Close()
    cli := tracker.NewGitHubClientWithBase(srv.URL, "bad-tok", "o/r", &http.Client{})
    _, err := cli.FetchIssue(context.Background(), "o/r#1")
    if !errors.Is(err, tracker.ErrTrackerAuth) {
        t.Errorf("expected ErrTrackerAuth, got %v", err)
    }
}

func TestGitHubFetchIssue_AuthHeader(t *testing.T) {
    var gotAuth string
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        gotAuth = r.Header.Get("Authorization")
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{"title": "X", "body": "", "html_url": "h", "number": 1, "labels": []any{}})
    }))
    defer srv.Close()
    cli := tracker.NewGitHubClientWithBase(srv.URL, "my-secret-token", "o/r", &http.Client{})
    _, _ = cli.FetchIssue(context.Background(), "o/r#1")
    if gotAuth != "Bearer my-secret-token" {
        t.Errorf("auth header: %q", gotAuth)
    }
}
```

**2b. Run — expect compile failure**.

**2c. Implement `server/internal/tracker/github.go`**

```go
package tracker

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "regexp"
    "strconv"
    "strings"
)

var (
    ghURLRe   = regexp.MustCompile(`(?i)github\.com/([^/]+)/([^/]+)/issues/(\d+)`)
    ghSlashRe = regexp.MustCompile(`^([^/]+)/([^#]+)#(\d+)$`)
    ghBareRe  = regexp.MustCompile(`^#?(\d+)$`)
)

// GitHubClient fetches GitHub issues. Use NewGitHubClient for production;
// NewGitHubClientWithBase lets tests inject an httptest base URL.
type GitHubClient struct {
    baseURL string // "" means "https://api.github.com"
    token   string
    defRepo string // "owner/repo"; may be empty
    client  *http.Client
}

// NewGitHubClient creates a production GitHub client.
func NewGitHubClient(token, defRepo string, client *http.Client) *GitHubClient {
    return &GitHubClient{token: token, defRepo: defRepo, client: client}
}

// NewGitHubClientWithBase creates a client pointing at baseURL (for httptest injection).
func NewGitHubClientWithBase(baseURL, token, defRepo string, client *http.Client) *GitHubClient {
    return &GitHubClient{baseURL: baseURL, token: token, defRepo: defRepo, client: client}
}

func (g *GitHubClient) parseRef(ref string) (owner, repo string, num int, err error) {
    if m := ghURLRe.FindStringSubmatch(ref); m != nil {
        n, _ := strconv.Atoi(m[3])
        return m[1], m[2], n, nil
    }
    if m := ghSlashRe.FindStringSubmatch(ref); m != nil {
        n, _ := strconv.Atoi(m[3])
        return m[1], m[2], n, nil
    }
    stripped := strings.TrimPrefix(ref, "#")
    if m := ghBareRe.FindStringSubmatch(stripped); m != nil {
        n, _ := strconv.Atoi(m[1])
        if g.defRepo == "" {
            return "", "", 0, fmt.Errorf("%w: bare ref #%d requires a configured default repo", ErrBadRef, n)
        }
        parts := strings.SplitN(g.defRepo, "/", 2)
        if len(parts) != 2 {
            return "", "", 0, fmt.Errorf("%w: invalid default repo %q", ErrBadRef, g.defRepo)
        }
        return parts[0], parts[1], n, nil
    }
    return "", "", 0, fmt.Errorf("%w: not a recognized GitHub issue ref: %q", ErrBadRef, ref)
}

func (g *GitHubClient) apiBase() string {
    if g.baseURL != "" {
        return g.baseURL
    }
    return "https://api.github.com"
}

// FetchIssue fetches a GitHub issue by ref. Supports full URL, owner/repo#n, and bare #n.
func (g *GitHubClient) FetchIssue(ctx context.Context, ref string) (Issue, error) {
    owner, repo, num, err := g.parseRef(ref)
    if err != nil {
        return Issue{}, err
    }
    u := fmt.Sprintf("%s/repos/%s/%s/issues/%d", g.apiBase(), owner, repo, num)
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
    if err != nil {
        return Issue{}, fmt.Errorf("%w: build request: %s", ErrTrackerUpstream, err)
    }
    req.Header.Set("Authorization", "Bearer "+g.token)
    req.Header.Set("Accept", "application/vnd.github+json")

    resp, err := g.client.Do(req)
    if err != nil {
        return Issue{}, fmt.Errorf("%w: %s", ErrTrackerUpstream, err)
    }
    defer resp.Body.Close()

    switch resp.StatusCode {
    case http.StatusUnauthorized, http.StatusForbidden:
        return Issue{}, fmt.Errorf("%w: HTTP %d", ErrTrackerAuth, resp.StatusCode)
    case http.StatusNotFound:
        return Issue{}, ErrIssueNotFound
    }
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        return Issue{}, fmt.Errorf("%w: HTTP %d", ErrTrackerUpstream, resp.StatusCode)
    }

    var payload struct {
        Title   string `json:"title"`
        Body    string `json:"body"`
        HTMLURL string `json:"html_url"`
        Number  int    `json:"number"`
        Labels  []struct {
            Name string `json:"name"`
        } `json:"labels"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
        return Issue{}, fmt.Errorf("%w: decode: %s", ErrTrackerUpstream, err)
    }
    labels := make([]string, len(payload.Labels))
    for i, l := range payload.Labels {
        labels[i] = l.Name
    }
    return Issue{
        Tracker: "github",
        Key:     fmt.Sprintf("#%d", payload.Number),
        Title:   payload.Title,
        Body:    payload.Body,
        URL:     payload.HTMLURL,
        Labels:  labels,
    }, nil
}
```

**2d. Run — expect pass**:
```
cd server && go test ./internal/tracker/...
```

**2e. Commit**:
```
git commit --no-gpg-sign -m "feat: GitHub issue client with URL, slash, and bare ref parsing"
```

---

## Task 3 — Jira client + ADF flattener

### Files
- `server/internal/tracker/jira.go` ← new
- `server/internal/tracker/jira_test.go` ← new

### Steps

**3a. Write failing tests**

```go
// server/internal/tracker/jira_test.go
package tracker_test

import (
    "context"
    "encoding/json"
    "errors"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/lx-wnk/agent-dashboard/server/internal/tracker"
)

// adfDoc is a minimal ADF (Atlassian Document Format) document with two paragraphs.
var adfDoc = map[string]any{
    "type": "doc",
    "content": []any{
        map[string]any{
            "type": "paragraph",
            "content": []any{
                map[string]any{"type": "text", "text": "First paragraph."},
            },
        },
        map[string]any{
            "type": "paragraph",
            "content": []any{
                map[string]any{"type": "text", "text": "Second paragraph."},
            },
        },
    },
}

func jiraServer(t *testing.T, statusCode int, body any) *httptest.Server {
    t.Helper()
    return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(statusCode)
        _ = json.NewEncoder(w).Encode(body)
    }))
}

func TestJiraFetchIssue_KeyRef(t *testing.T) {
    srv := jiraServer(t, 200, map[string]any{
        "fields": map[string]any{
            "summary":     "Login timeout",
            "description": adfDoc,
            "labels":      []string{"backend"},
        },
    })
    defer srv.Close()

    cli := tracker.NewJiraClient(srv.URL, "user@example.com", "jira-tok", &http.Client{})
    iss, err := cli.FetchIssue(context.Background(), "PROJ-42")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if iss.Title != "Login timeout" {
        t.Errorf("title: %q", iss.Title)
    }
    if iss.Key != "PROJ-42" {
        t.Errorf("key: %q", iss.Key)
    }
    if iss.Tracker != "jira" {
        t.Errorf("tracker: %q", iss.Tracker)
    }
    // ADF body should contain both paragraphs
    if iss.Body == "" {
        t.Error("body must not be empty after ADF flatten")
    }
    if len(iss.Labels) != 1 || iss.Labels[0] != "backend" {
        t.Errorf("labels: %v", iss.Labels)
    }
    if iss.URL == "" {
        t.Error("URL must not be empty")
    }
}

func TestJiraFetchIssue_BrowseURLRef(t *testing.T) {
    srv := jiraServer(t, 200, map[string]any{
        "fields": map[string]any{"summary": "Browse URL ref", "description": nil, "labels": []string{}},
    })
    defer srv.Close()
    cli := tracker.NewJiraClient(srv.URL, "u", "t", &http.Client{})
    iss, err := cli.FetchIssue(context.Background(), srv.URL+"/browse/AB-7")
    if err != nil {
        t.Fatalf("browse url ref: %v", err)
    }
    if iss.Key != "AB-7" {
        t.Errorf("key: %q", iss.Key)
    }
}

func TestJiraFetchIssue_BasicAuthHeader(t *testing.T) {
    var gotAuth string
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        gotAuth = r.Header.Get("Authorization")
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{"fields": map[string]any{"summary": "X", "description": nil, "labels": []string{}}})
    }))
    defer srv.Close()
    cli := tracker.NewJiraClient(srv.URL, "alice@example.com", "secret", &http.Client{})
    _, _ = cli.FetchIssue(context.Background(), "AB-1")
    // Basic base64("alice@example.com:secret")
    import64 := "YWxpY2VAZXhhbXBsZS5jb206c2VjcmV0"
    if gotAuth != "Basic "+import64 {
        t.Errorf("auth header: %q (want Basic %s)", gotAuth, import64)
    }
}

func TestJiraFetchIssue_404(t *testing.T) {
    srv := jiraServer(t, 404, map[string]any{"errorMessages": []string{"Issue not found"}})
    defer srv.Close()
    cli := tracker.NewJiraClient(srv.URL, "u", "t", &http.Client{})
    _, err := cli.FetchIssue(context.Background(), "PROJ-99")
    if !errors.Is(err, tracker.ErrIssueNotFound) {
        t.Errorf("expected ErrIssueNotFound, got %v", err)
    }
}

func TestJiraFetchIssue_401(t *testing.T) {
    srv := jiraServer(t, 401, map[string]any{"errorMessages": []string{"Unauthorized"}})
    defer srv.Close()
    cli := tracker.NewJiraClient(srv.URL, "u", "badtok", &http.Client{})
    _, err := cli.FetchIssue(context.Background(), "PROJ-1")
    if !errors.Is(err, tracker.ErrTrackerAuth) {
        t.Errorf("expected ErrTrackerAuth, got %v", err)
    }
}

func TestJiraFetchIssue_NilADF(t *testing.T) {
    srv := jiraServer(t, 200, map[string]any{
        "fields": map[string]any{"summary": "No body", "description": nil, "labels": []string{}},
    })
    defer srv.Close()
    cli := tracker.NewJiraClient(srv.URL, "u", "t", &http.Client{})
    iss, err := cli.FetchIssue(context.Background(), "X-1")
    if err != nil {
        t.Fatalf("nil ADF: %v", err)
    }
    if iss.Body != "" {
        t.Errorf("body should be empty for nil ADF, got %q", iss.Body)
    }
}
```

**3b. Run — expect compile failure**.

**3c. Implement `server/internal/tracker/jira.go`**

```go
package tracker

import (
    "context"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "net/http"
    "regexp"
    "strings"
)

var (
    jiraKeyRe = regexp.MustCompile(`^([A-Z][A-Z0-9]+-\d+)$`)
    jiraBrowse = regexp.MustCompile(`/browse/([A-Z][A-Z0-9]+-\d+)`)
)

// JiraClient fetches Jira issues using REST API v3 with Basic auth.
type JiraClient struct {
    baseURL string
    email   string
    token   string
    client  *http.Client
}

// NewJiraClient creates a Jira client.
func NewJiraClient(baseURL, email, token string, client *http.Client) *JiraClient {
    return &JiraClient{
        baseURL: strings.TrimRight(baseURL, "/"),
        email:   email,
        token:   token,
        client:  client,
    }
}

func (j *JiraClient) parseRef(ref string) (string, error) {
    if m := jiraKeyRe.FindStringSubmatch(ref); m != nil {
        return m[1], nil
    }
    if m := jiraBrowse.FindStringSubmatch(ref); m != nil {
        return m[1], nil
    }
    return "", fmt.Errorf("%w: not a recognized Jira issue ref: %q", ErrBadRef, ref)
}

// flattenADF extracts plain text from an Atlassian Document Format JSON node.
// On any parse error it returns an empty string (non-fatal per spec).
func flattenADF(raw json.RawMessage) string {
    if len(raw) == 0 || string(raw) == "null" {
        return ""
    }
    var node map[string]json.RawMessage
    if err := json.Unmarshal(raw, &node); err != nil {
        return ""
    }
    var sb strings.Builder
    walkADF(&sb, node, false)
    return strings.TrimSpace(sb.String())
}

func walkADF(sb *strings.Builder, node map[string]json.RawMessage, addNewline bool) {
    // Leaf: text node
    if raw, ok := node["text"]; ok {
        var s string
        if json.Unmarshal(raw, &s) == nil {
            if addNewline && sb.Len() > 0 {
                sb.WriteString("\n\n")
            }
            sb.WriteString(s)
        }
        return
    }
    // Container: recurse into content array
    contentRaw, ok := node["content"]
    if !ok {
        return
    }
    var children []map[string]json.RawMessage
    if json.Unmarshal(contentRaw, &children) != nil {
        return
    }
    // Determine whether this node is a block-level element (paragraph, etc.)
    var nodeType string
    if tr, ok := node["type"]; ok {
        _ = json.Unmarshal(tr, &nodeType)
    }
    isParagraph := nodeType == "paragraph" || nodeType == "heading"
    for _, child := range children {
        walkADF(sb, child, isParagraph)
    }
}

// FetchIssue fetches a Jira issue by KEY-123 reference or browse URL.
func (j *JiraClient) FetchIssue(ctx context.Context, ref string) (Issue, error) {
    key, err := j.parseRef(ref)
    if err != nil {
        return Issue{}, err
    }
    u := fmt.Sprintf("%s/rest/api/3/issue/%s?fields=summary,description,labels", j.baseURL, key)
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
    if err != nil {
        return Issue{}, fmt.Errorf("%w: build request: %s", ErrTrackerUpstream, err)
    }
    creds := base64.StdEncoding.EncodeToString([]byte(j.email + ":" + j.token))
    req.Header.Set("Authorization", "Basic "+creds)
    req.Header.Set("Accept", "application/json")

    resp, err := j.client.Do(req)
    if err != nil {
        return Issue{}, fmt.Errorf("%w: %s", ErrTrackerUpstream, err)
    }
    defer resp.Body.Close()

    switch resp.StatusCode {
    case http.StatusUnauthorized, http.StatusForbidden:
        return Issue{}, fmt.Errorf("%w: HTTP %d", ErrTrackerAuth, resp.StatusCode)
    case http.StatusNotFound:
        return Issue{}, ErrIssueNotFound
    }
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        return Issue{}, fmt.Errorf("%w: HTTP %d", ErrTrackerUpstream, resp.StatusCode)
    }

    var payload struct {
        Fields struct {
            Summary     string          `json:"summary"`
            Description json.RawMessage `json:"description"`
            Labels      []string        `json:"labels"`
        } `json:"fields"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
        return Issue{}, fmt.Errorf("%w: decode: %s", ErrTrackerUpstream, err)
    }
    body := flattenADF(payload.Fields.Description)
    labels := payload.Fields.Labels
    if labels == nil {
        labels = []string{}
    }
    return Issue{
        Tracker: "jira",
        Key:     key,
        Title:   payload.Fields.Summary,
        Body:    body,
        URL:     fmt.Sprintf("%s/browse/%s", j.baseURL, key),
        Labels:  labels,
    }, nil
}
```

**3d. Run — expect pass**:
```
cd server && go test ./internal/tracker/...
```

**3e. Commit**:
```
git commit --no-gpg-sign -m "feat: Jira issue client with ADF body flattening"
```

---

## Task 4 — Resolve dispatch

### Files
- `server/internal/tracker/resolve.go` ← new
- `server/internal/tracker/resolve_test.go` ← new

### Steps

**4a. Write failing tests**

```go
// server/internal/tracker/resolve_test.go
package tracker_test

import (
    "errors"
    "net/http"
    "testing"

    "github.com/lx-wnk/agent-dashboard/server/internal/tracker"
)

func TestResolve_GitHubURL(t *testing.T) {
    cfg := tracker.Config{GitHubToken: "tok"}
    tr, err := tracker.Resolve("https://github.com/owner/repo/issues/1", cfg, &http.Client{})
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if tr == nil {
        t.Fatal("expected non-nil tracker")
    }
}

func TestResolve_GitHubSlash(t *testing.T) {
    cfg := tracker.Config{GitHubToken: "tok"}
    tr, err := tracker.Resolve("owner/repo#5", cfg, &http.Client{})
    if err != nil || tr == nil {
        t.Fatalf("slash ref: err=%v tr=%v", err, tr)
    }
}

func TestResolve_JiraKey(t *testing.T) {
    cfg := tracker.Config{JiraBaseURL: "https://jira.example.com", JiraToken: "tok", JiraEmail: "u@x.com"}
    tr, err := tracker.Resolve("PROJ-42", cfg, &http.Client{})
    if err != nil || tr == nil {
        t.Fatalf("jira key: err=%v tr=%v", err, tr)
    }
}

func TestResolve_JiraURL(t *testing.T) {
    cfg := tracker.Config{JiraBaseURL: "https://j.example.com", JiraToken: "t", JiraEmail: "e"}
    tr, err := tracker.Resolve("https://j.example.com/browse/AB-7", cfg, &http.Client{})
    if err != nil || tr == nil {
        t.Fatalf("jira browse url: err=%v tr=%v", err, tr)
    }
}

func TestResolve_MissingGitHubToken(t *testing.T) {
    cfg := tracker.Config{} // no token
    _, err := tracker.Resolve("owner/repo#1", cfg, &http.Client{})
    if err == nil {
        t.Fatal("expected error for missing GitHub token")
    }
}

func TestResolve_MissingJiraConfig(t *testing.T) {
    cfg := tracker.Config{} // no jira config
    _, err := tracker.Resolve("PROJ-1", cfg, &http.Client{})
    if err == nil {
        t.Fatal("expected error for missing Jira config")
    }
}

func TestResolve_UnrecognizedRef(t *testing.T) {
    cfg := tracker.Config{GitHubToken: "tok"}
    _, err := tracker.Resolve("not-any-tracker-ref", cfg, &http.Client{})
    if !errors.Is(err, tracker.ErrBadRef) {
        t.Errorf("expected ErrBadRef, got %v", err)
    }
}
```

**4b. Run — expect compile failure**.

**4c. Implement `server/internal/tracker/resolve.go`**

```go
package tracker

import (
    "errors"
    "net/http"
    "regexp"
    "strings"
    "time"
)

var (
    // resolveGHURL matches both https://github.com/... and bare owner/repo#n
    resolveGHRef  = regexp.MustCompile(`(?i)github\.com/|^[^/]+/[^#]+#\d+$|^#?\d+$`)
    resolveJiraKey = regexp.MustCompile(`^[A-Z][A-Z0-9]+-\d+$`)
    resolveJiraBrowse = regexp.MustCompile(`/browse/[A-Z][A-Z0-9]+-\d+`)
)

// Resolve selects the right Tracker by inspecting the ref shape and returns
// a configured client. Returns ErrBadRef for unrecognized ref shapes.
// client may be nil — a 30-second default is used in that case.
func Resolve(ref string, cfg Config, client *http.Client) (Tracker, error) {
    if client == nil {
        client = &http.Client{Timeout: 30 * time.Second}
    }
    // Jira: KEY-123 pattern or a browse URL
    if resolveJiraKey.MatchString(ref) || resolveJiraBrowse.MatchString(ref) {
        if cfg.JiraBaseURL == "" || cfg.JiraToken == "" {
            return nil, errors.New("configure the Jira token and base URL in Settings")
        }
        return NewJiraClient(cfg.JiraBaseURL, cfg.JiraEmail, cfg.JiraToken, client), nil
    }
    // GitHub: full URL, owner/repo#n, bare #n
    if resolveGHRef.MatchString(ref) {
        if cfg.GitHubToken == "" {
            return nil, errors.New("configure the GitHub token in Settings")
        }
        return NewGitHubClient(cfg.GitHubToken, cfg.GitHubDefRepo, client), nil
    }
    return nil, ErrBadRef
}
```

**4d. Run — expect pass**:
```
cd server && go test ./internal/tracker/...
```

**4e. Commit**:
```
git commit --no-gpg-sign -m "feat: tracker Resolve dispatch by ref shape"
```

---

## Task 5 — Tracker settings + HTTP handler

Reuse `pluginsettings.Service` with `pluginID = "tracker"` and a fixed `[]plugin.SettingField` schema. Two secret keys (`tracker.github.token`, `tracker.jira.token`) are stored encrypted in the existing `plugin_setting` table. No new ent table. The existing `pluginSettingRepoAdapter` in `plugin_adapters.go` bridges `repo.PluginSettingRepo` → `pluginsettings.Repo`.

### Files
- `server/internal/api/tracker/handler.go` ← new
- `server/internal/api/tracker/handler_test.go` ← new

### Steps

**5a. Write failing tests**

```go
// server/internal/api/tracker/handler_test.go
package tracker_test

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "net/http"
    "net/http/httptest"
    "sync"
    "testing"

    "github.com/go-chi/chi/v5"
    trackerapi "github.com/lx-wnk/agent-dashboard/server/internal/api/tracker"
    "github.com/lx-wnk/agent-dashboard/server/internal/plugin"
    "github.com/lx-wnk/agent-dashboard/server/internal/pluginsettings"
    "github.com/lx-wnk/agent-dashboard/server/internal/secretbox"
    "github.com/lx-wnk/agent-dashboard/server/internal/tracker"
)

// memRepo is an in-memory pluginsettings.Repo for tests.
type memRepo struct {
    mu   sync.Mutex
    rows map[string]pluginsettings.Stored // key -> row
}

func newMemRepo() *memRepo { return &memRepo{rows: make(map[string]pluginsettings.Stored)} }

func (m *memRepo) ListByPlugin(_ context.Context, _ string) ([]pluginsettings.Stored, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    out := make([]pluginsettings.Stored, 0, len(m.rows))
    for _, r := range m.rows {
        out = append(out, r)
    }
    return out, nil
}

func (m *memRepo) Upsert(_ context.Context, _ string, s pluginsettings.Stored) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.rows[s.Key] = s
    return nil
}

func (m *memRepo) DeleteByPlugin(_ context.Context, _ string) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    clear(m.rows)
    return nil
}

func newTestBox(t *testing.T) *secretbox.Box {
    t.Helper()
    key := make([]byte, 32)
    box, err := secretbox.New(key)
    if err != nil {
        t.Fatalf("secretbox.New: %v", err)
    }
    return box
}

func newTestHandler(t *testing.T) (*trackerapi.Handler, *memRepo) {
    t.Helper()
    repo := newMemRepo()
    svc := pluginsettings.New(repo, newTestBox(t))
    h := trackerapi.NewHandler(svc, &http.Client{}, tracker.Resolve)
    return h, repo
}

func doRequest(t *testing.T, h *trackerapi.Handler, method, path string, body any) *httptest.ResponseRecorder {
    t.Helper()
    r := chi.NewRouter()
    h.Mount(r)
    var buf bytes.Buffer
    if body != nil {
        _ = json.NewEncoder(&buf).Encode(body)
    }
    req := httptest.NewRequest(method, path, &buf)
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)
    return w
}

func TestGetSettings_EmptyInitial(t *testing.T) {
    h, _ := newTestHandler(t)
    w := doRequest(t, h, http.MethodGet, "/api/tracker/settings", nil)
    if w.Code != http.StatusOK {
        t.Fatalf("status: %d", w.Code)
    }
    var resp struct {
        Schema []plugin.SettingField     `json:"schema"`
        Values map[string]string         `json:"values"`
    }
    if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
        t.Fatalf("decode: %v", err)
    }
    if len(resp.Schema) == 0 {
        t.Error("schema must be non-empty")
    }
    // All values empty on first load
    for _, v := range resp.Values {
        if v != "" {
            t.Errorf("unexpected non-empty initial value: %q", v)
        }
    }
}

func TestPutSettings_SecretRoundTrip(t *testing.T) {
    h, _ := newTestHandler(t)
    // PUT a secret value
    w := doRequest(t, h, http.MethodPut, "/api/tracker/settings", map[string]any{
        "values": map[string]string{"tracker.github.token": "ghp_test1234"},
    })
    if w.Code != http.StatusNoContent {
        t.Fatalf("PUT status: %d body: %s", w.Code, w.Body.String())
    }
    // GET should return masked sentinel for the secret
    w2 := doRequest(t, h, http.MethodGet, "/api/tracker/settings", nil)
    var resp struct {
        Values map[string]string `json:"values"`
    }
    _ = json.NewDecoder(w2.Body).Decode(&resp)
    if resp.Values["tracker.github.token"] != pluginsettings.MaskedSentinel {
        t.Errorf("secret not masked: %q", resp.Values["tracker.github.token"])
    }
}

func TestPutSettings_SentinelPreservesExistingSecret(t *testing.T) {
    h, _ := newTestHandler(t)
    // Store an initial secret
    _ = doRequest(t, h, http.MethodPut, "/api/tracker/settings", map[string]any{
        "values": map[string]string{"tracker.github.token": "initial-secret"},
    })
    // PUT the sentinel — should not change the stored value
    _ = doRequest(t, h, http.MethodPut, "/api/tracker/settings", map[string]any{
        "values": map[string]string{"tracker.github.token": pluginsettings.MaskedSentinel},
    })
    // The token should still decrypt to the initial value
    // (verified indirectly: GET still returns masked sentinel, not empty)
    w := doRequest(t, h, http.MethodGet, "/api/tracker/settings", nil)
    var resp struct{ Values map[string]string `json:"values"` }
    _ = json.NewDecoder(w.Body).Decode(&resp)
    if resp.Values["tracker.github.token"] != pluginsettings.MaskedSentinel {
        t.Errorf("expected masked sentinel, got %q", resp.Values["tracker.github.token"])
    }
}

func TestFetch_EmptyRef(t *testing.T) {
    h, _ := newTestHandler(t)
    w := doRequest(t, h, http.MethodPost, "/api/tracker/fetch", map[string]string{"ref": ""})
    if w.Code != http.StatusBadRequest {
        t.Errorf("empty ref: got %d", w.Code)
    }
}

func TestFetch_BadRef(t *testing.T) {
    h, _ := newTestHandler(t)
    w := doRequest(t, h, http.MethodPost, "/api/tracker/fetch", map[string]string{"ref": "not-any-ref"})
    if w.Code != http.StatusBadRequest {
        t.Errorf("bad ref: got %d", w.Code)
    }
}

func TestFetch_MissingGitHubToken_Returns400(t *testing.T) {
    h, _ := newTestHandler(t)
    // GitHub ref with no token configured
    w := doRequest(t, h, http.MethodPost, "/api/tracker/fetch", map[string]string{"ref": "owner/repo#1"})
    if w.Code != http.StatusBadRequest {
        t.Errorf("missing token: got %d", w.Code)
    }
}

func TestFetch_JiraSuccess(t *testing.T) {
    // Spin up a fake Jira server
    jiraSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{
            "fields": map[string]any{
                "summary":     "From Jira",
                "description": nil,
                "labels":      []string{},
            },
        })
    }))
    defer jiraSrv.Close()

    h, _ := newTestHandler(t)
    // Configure Jira settings
    _ = doRequest(t, h, http.MethodPut, "/api/tracker/settings", map[string]any{
        "values": map[string]string{
            "tracker.jira.baseUrl": jiraSrv.URL,
            "tracker.jira.email":   "u@example.com",
            "tracker.jira.token":   "jira-tok",
        },
    })
    w := doRequest(t, h, http.MethodPost, "/api/tracker/fetch", map[string]string{"ref": "PROJ-7"})
    if w.Code != http.StatusOK {
        t.Fatalf("jira fetch: %d — %s", w.Code, w.Body.String())
    }
    var iss tracker.Issue
    if err := json.NewDecoder(w.Body).Decode(&iss); err != nil {
        t.Fatalf("decode: %v", err)
    }
    if iss.Title != "From Jira" {
        t.Errorf("title: %q", iss.Title)
    }
    if iss.Tracker != "jira" {
        t.Errorf("tracker: %q", iss.Tracker)
    }
}

func TestFetch_IssueNotFound_Returns404(t *testing.T) {
    jiraSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
        w.WriteHeader(404)
        _ = json.NewEncoder(w).Encode(map[string]any{"errorMessages": []string{"Not Found"}})
    }))
    defer jiraSrv.Close()

    h, _ := newTestHandler(t)
    _ = doRequest(t, h, http.MethodPut, "/api/tracker/settings", map[string]any{
        "values": map[string]string{
            "tracker.jira.baseUrl": jiraSrv.URL,
            "tracker.jira.email":   "u@x.com",
            "tracker.jira.token":   "tok",
        },
    })
    w := doRequest(t, h, http.MethodPost, "/api/tracker/fetch", map[string]string{"ref": "PROJ-99"})
    if w.Code != http.StatusNotFound {
        t.Errorf("not found: got %d", w.Code)
    }
}
```

**5b. Run — expect compile failure** (package doesn't exist):
```
cd server && go test ./internal/api/tracker/...
```

**5c. Implement `server/internal/api/tracker/handler.go`**

```go
// Package tracker provides the /api/tracker/* HTTP handler for issue fetching
// and encrypted token settings management.
package tracker

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "net/http"

    "github.com/go-chi/chi/v5"

    "github.com/lx-wnk/agent-dashboard/server/internal/apierr"
    "github.com/lx-wnk/agent-dashboard/server/internal/plugin"
    "github.com/lx-wnk/agent-dashboard/server/internal/pluginsettings"
    "github.com/lx-wnk/agent-dashboard/server/internal/tracker"
)

const settingsPluginID = "tracker"

// trackerSchema is the canonical settings schema for tracker credentials.
// Secret fields are encrypted at rest; non-secret fields are plaintext.
// The two *.token entries use the secretbox path via pluginsettings.Service.
var trackerSchema = []plugin.SettingField{
    {Key: "tracker.github.token", Type: "string", Label: "GitHub personal access token", Secret: true},
    {Key: "tracker.github.defaultRepo", Type: "string", Label: "GitHub default repo (owner/repo)", Secret: false},
    {Key: "tracker.jira.baseUrl", Type: "url", Label: "Jira base URL (https://yourorg.atlassian.net)", Secret: false},
    {Key: "tracker.jira.email", Type: "string", Label: "Jira account email", Secret: false},
    {Key: "tracker.jira.token", Type: "string", Label: "Jira API token", Secret: true},
}

// ResolverFn is the tracker.Resolve signature; injectable for tests.
type ResolverFn func(ref string, cfg tracker.Config, client *http.Client) (tracker.Tracker, error)

// Handler serves /api/tracker/* endpoints.
type Handler struct {
    settings *pluginsettings.Service
    httpCli  *http.Client
    resolver ResolverFn
}

// NewHandler builds a Handler. resolver defaults to tracker.Resolve if nil.
func NewHandler(settings *pluginsettings.Service, httpCli *http.Client, resolver ResolverFn) *Handler {
    if resolver == nil {
        resolver = tracker.Resolve
    }
    if httpCli == nil {
        httpCli = &http.Client{Timeout: 30 * time.Second}
    }
    return &Handler{settings: settings, httpCli: httpCli, resolver: resolver}
}

// Mount registers the tracker routes. Callers must apply JWT + same-origin middleware.
func (h *Handler) Mount(r chi.Router) {
    r.Get("/api/tracker/settings", apierr.ErrorMiddleware(h.getSettings))
    r.Put("/api/tracker/settings", apierr.ErrorMiddleware(h.putSettings))
    r.Post("/api/tracker/fetch", apierr.ErrorMiddleware(h.fetch))
}

type settingView struct {
    Key    string   `json:"key"`
    Label  string   `json:"label"`
    Type   string   `json:"type"`
    Secret bool     `json:"secret"`
    Enum   []string `json:"enum,omitempty"`
}

func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) error {
    values, err := h.settings.Get(r.Context(), settingsPluginID, trackerSchema)
    if err != nil {
        return fmt.Errorf("tracker.settings.get: %w", err)
    }
    schema := make([]settingView, len(trackerSchema))
    for i, f := range trackerSchema {
        schema[i] = settingView{Key: f.Key, Label: f.Label, Type: f.Type, Secret: f.Secret}
    }
    w.Header().Set("Content-Type", "application/json")
    return json.NewEncoder(w).Encode(map[string]any{"schema": schema, "values": values})
}

func (h *Handler) putSettings(w http.ResponseWriter, r *http.Request) error {
    var body struct {
        Values map[string]string `json:"values"`
    }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        return fmt.Errorf("%w: invalid JSON", apierr.ErrBadRequest)
    }
    if err := h.settings.Put(r.Context(), settingsPluginID, trackerSchema, body.Values); err != nil {
        if errors.Is(err, pluginsettings.ErrUnknownKey) || errors.Is(err, pluginsettings.ErrInvalidValue) {
            return fmt.Errorf("%w: %s", apierr.ErrBadRequest, err)
        }
        return fmt.Errorf("tracker.settings.put: %w", err)
    }
    w.WriteHeader(http.StatusNoContent)
    return nil
}

func (h *Handler) fetch(w http.ResponseWriter, r *http.Request) error {
    var body struct {
        Ref string `json:"ref"`
    }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Ref == "" {
        return fmt.Errorf("%w: ref is required", apierr.ErrBadRequest)
    }
    cfg, err := h.loadConfig(r.Context())
    if err != nil {
        return fmt.Errorf("tracker.fetch.loadConfig: %w", err)
    }
    t, err := h.resolver(body.Ref, cfg, h.httpCli)
    if err != nil {
        if errors.Is(err, tracker.ErrBadRef) {
            return apierr.NewAppError(http.StatusBadRequest, err.Error())
        }
        return apierr.NewAppError(http.StatusBadRequest, err.Error()) // missing config
    }
    iss, err := t.FetchIssue(r.Context(), body.Ref)
    if err != nil {
        switch {
        case errors.Is(err, tracker.ErrTrackerAuth):
            return apierr.NewAppError(http.StatusUnauthorized, "tracker rejected the token")
        case errors.Is(err, tracker.ErrIssueNotFound):
            return apierr.NewAppError(http.StatusNotFound, "issue not found")
        default:
            return apierr.NewAppError(http.StatusBadGateway, "upstream error fetching issue")
        }
    }
    w.Header().Set("Content-Type", "application/json")
    return json.NewEncoder(w).Encode(iss)
}

func (h *Handler) loadConfig(ctx context.Context) (tracker.Config, error) {
    vals, err := h.settings.Decrypted(ctx, settingsPluginID, trackerSchema)
    if err != nil {
        return tracker.Config{}, err
    }
    return tracker.Config{
        GitHubToken:   vals["tracker.github.token"],
        GitHubDefRepo: vals["tracker.github.defaultRepo"],
        JiraBaseURL:   vals["tracker.jira.baseUrl"],
        JiraEmail:     vals["tracker.jira.email"],
        JiraToken:     vals["tracker.jira.token"],
    }, nil
}
```

Add missing import in handler.go: `"time"`.

**5d. Run — expect pass**:
```
cd server && go test ./internal/api/tracker/...
```

**5e. Commit**:
```
git commit --no-gpg-sign -m "feat: tracker settings + fetch HTTP handler with encrypted token storage"
```

---

## Task 6 — Router mount + DI wiring

### Files
- `server/internal/api/router.go` — add `TrackerHandler` field + mount
- `server/cmd/serve/di.go` — construct `trackerapi.Handler` using existing `pluginSettingsSvc`

### Steps

**6a. Edit `router.go`** — add to `RouterDeps` struct and protected group:

In `RouterDeps` (after `EvalHandler` around line 156):
```go
TrackerHandler *trackerapi.Handler
```

Add import:
```go
trackerapi "github.com/lx-wnk/agent-dashboard/server/internal/api/tracker"
```

In the protected `r.Group(func(r chi.Router) {` block, after `EvalHandler` mount:
```go
if deps.TrackerHandler != nil {
    deps.TrackerHandler.Mount(r)
}
```

**6b. Edit `di.go`** — wire the tracker handler after `pluginSettingsSvc` is constructed (around line 312, after `pluginLifecycleHandler` block):

Add import:
```go
trackerapi "github.com/lx-wnk/agent-dashboard/server/internal/api/tracker"
```

Add construction:
```go
var trackerHandler *trackerapi.Handler
if pluginSettingsSvc != nil {
    trackerHandler = trackerapi.NewHandler(pluginSettingsSvc, &http.Client{Timeout: 30 * time.Second}, nil)
}
```

Pass to the router deps struct:
```go
TrackerHandler: trackerHandler,
```

**6c. Build check**:
```
cd server && go build ./...
```

**6d. Commit**:
```
git commit --no-gpg-sign -m "feat: mount tracker handler in router and wire DI"
```

---

## Task 7 — Frontend: useTrackerImport + BacklogForm pre-fill + TrackerSettingsPanel

### Files
- `src/composables/useTrackerImport.ts` ← new
- `src/composables/useTrackerImport.test.ts` ← new
- `src/components/TrackerSettingsPanel.vue` ← new
- `src/components/BacklogForm.vue` — add import affordance

### Steps

**7a. Write failing test for `useTrackerImport`**

```ts
// src/composables/useTrackerImport.test.ts
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useTrackerImport } from './useTrackerImport'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

const mockIssue = {
  tracker: 'github',
  key: '#1',
  title: 'Fix the bug',
  body: 'Details here',
  url: 'https://github.com/owner/repo/issues/1',
  labels: ['bug'],
}

describe('useTrackerImport', () => {
  it('fetchIssue posts ref with Origin header and returns issue', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => mockIssue }))
    const { fetchIssue } = useTrackerImport()
    const iss = await fetchIssue('https://github.com/owner/repo/issues/1')
    expect(iss).toEqual(mockIssue)
    expect(fetch).toHaveBeenCalledWith('/api/tracker/fetch', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ ref: 'https://github.com/owner/repo/issues/1' }),
    }))
    const call = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][1] as RequestInit
    expect((call.headers as Record<string, string>)['Origin']).toBe(window.location.origin)
  })

  it('throws with error message on HTTP error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      json: async () => ({ error: 'issue not found' }),
    }))
    const { fetchIssue } = useTrackerImport()
    await expect(fetchIssue('#999')).rejects.toThrow('issue not found')
  })

  it('throws with fallback message when no JSON body', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      json: async () => { throw new Error('parse error') },
    }))
    const { fetchIssue } = useTrackerImport()
    await expect(fetchIssue('X')).rejects.toThrow('HTTP')
  })
})
```

**7b. Run — expect compile failure**:
```
pnpm test --run src/composables/useTrackerImport.test.ts
```

**7c. Implement `src/composables/useTrackerImport.ts`**

```ts
export interface TrackerIssue {
  tracker: string
  key: string
  title: string
  body: string
  url: string
  labels: string[]
}

export function useTrackerImport() {
  async function fetchIssue(ref: string): Promise<TrackerIssue> {
    const res = await fetch('/api/tracker/fetch', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Origin': window.location.origin,
      },
      body: JSON.stringify({ ref }),
    })
    if (!res.ok) {
      let detail = `HTTP ${res.status}`
      try {
        const b = await res.json()
        if (b?.error)
          detail = b.error
      }
      catch { /* no body */ }
      throw new Error(detail)
    }
    return res.json()
  }

  return { fetchIssue }
}
```

**7d. Run — expect pass**:
```
pnpm test --run src/composables/useTrackerImport.test.ts
```

**7e. Implement `TrackerSettingsPanel.vue`**

The panel mirrors `PluginSettingsForm.vue` patterns: masked `type="password"` for secrets, sentinel preserved on save. It calls `GET/PUT /api/tracker/settings`.

```vue
<!-- src/components/TrackerSettingsPanel.vue -->
<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { toast } from '../composables/useToast'
import { errorMessage } from '../utils/errorMessage'

const SECRET_SENTINEL = '********'

interface FieldDef {
  key: string
  label: string
  type: string
  secret: boolean
}

const schema = ref<FieldDef[]>([])
const model = reactive<Record<string, string>>({})
const initial = reactive<Record<string, string>>({})
const saving = ref(false)
const loading = ref(true)

onMounted(async () => {
  try {
    const res = await fetch('/api/tracker/settings', { credentials: 'same-origin' })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    const data = await res.json()
    schema.value = data.schema
    for (const f of data.schema) {
      model[f.key] = data.values[f.key] ?? ''
      initial[f.key] = data.values[f.key] ?? ''
    }
  }
  catch (e) {
    toast.error(errorMessage(e, 'Failed to load tracker settings'))
  }
  finally {
    loading.value = false
  }
})

async function save() {
  saving.value = true
  try {
    const changed: Record<string, string> = {}
    for (const f of schema.value) {
      if (f.secret && model[f.key] === SECRET_SENTINEL)
        continue
      if (model[f.key] !== initial[f.key])
        changed[f.key] = model[f.key]
    }
    if (Object.keys(changed).length === 0) {
      toast.info('No changes to save.')
      return
    }
    const res = await fetch('/api/tracker/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', 'Origin': window.location.origin },
      body: JSON.stringify({ values: changed }),
    })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    for (const k of Object.keys(changed))
      initial[k] = model[k]
    toast.success('Tracker settings saved.')
  }
  catch (e) {
    toast.error(errorMessage(e, 'Failed to save tracker settings'))
  }
  finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="space-y-4">
    <p v-if="loading" class="text-fg-subtle text-sm">
      Loading…
    </p>
    <template v-else>
      <div v-for="f in schema" :key="f.key" class="flex flex-col gap-1">
        <label :for="`tracker-${f.key}`" class="text-sm font-medium text-fg">{{ f.label }}</label>
        <input
          :id="`tracker-${f.key}`"
          v-model="model[f.key]"
          :type="f.secret ? 'password' : (f.type === 'url' ? 'url' : 'text')"
          :data-field="f.key"
          class="w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2 focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
          autocomplete="off"
        >
      </div>
      <div class="flex justify-end">
        <button
          type="button"
          :disabled="saving"
          class="px-4 py-2 rounded bg-accent text-white text-sm disabled:opacity-50"
          data-action="save"
          @click="save"
        >
          {{ saving ? 'Saving…' : 'Save settings' }}
        </button>
      </div>
    </template>
  </div>
</template>
```

**7f. Update `BacklogForm.vue`** — add "Import from issue" affordance

Add after the imports block (existing imports):
```ts
import { useTrackerImport } from '../composables/useTrackerImport'
import type { TrackerIssue } from '../composables/useTrackerImport'
```

Add reactive state after existing refs:
```ts
const importRef = ref('')
const isImporting = ref(false)
```

Add import handler function after `onSlugInput`:
```ts
async function importFromIssue(): Promise<void> {
  const ref = importRef.value.trim()
  if (!ref || isImporting.value)
    return
  const { fetchIssue } = useTrackerImport()
  isImporting.value = true
  try {
    const iss: TrackerIssue = await fetchIssue(ref)
    title.value = iss.title
    slug.value = slugify(iss.title)
    const sourceLink = `\n\nSource: ${iss.url}`
    description.value = iss.body ? iss.body + sourceLink : iss.url
  }
  catch (err: unknown) {
    toast.error(errorMessage(err, 'Failed to fetch issue'))
  }
  finally {
    isImporting.value = false
  }
}
```

Add in `<template>` before the Title field section (after the project picker):
```html
<div class="flex flex-col gap-1">
  <AppFieldLabel>Import from issue</AppFieldLabel>
  <div class="flex gap-2 items-stretch">
    <AppInput
      v-model="importRef"
      placeholder="github.com/owner/repo/issues/1 · KEY-123 · owner/repo#1"
      class="flex-1"
      data-testid="import-ref-input"
    />
    <AppButton
      type="button"
      variant="secondary"
      :disabled="!importRef.trim() || isImporting"
      :aria-busy="isImporting"
      data-testid="import-ref-fetch"
      @click="importFromIssue"
    >
      {{ isImporting ? 'Fetching…' : 'Fetch' }}
    </AppButton>
  </div>
</div>
```

**7g. Run frontend tests**:
```
pnpm test --run src/composables/useTrackerImport.test.ts
```

**7h. Commit**:
```
git commit --no-gpg-sign -m "feat: useTrackerImport composable + Import from issue affordance in BacklogForm + TrackerSettingsPanel"
```

---

## Task 8 — Docs + CHANGELOG

### Files
- `CHANGELOG.md` — add entry under `[Unreleased]`
- `README.md` — add brief mention in Features section

### CHANGELOG entry

```markdown
### Added
- Issue → Task seeding: paste a GitHub or Jira issue reference in the New-Task form and click **Fetch** to pre-fill title, description, and source link. GitHub personal-access tokens and Jira API tokens are stored encrypted at rest and masked in the settings UI (`Settings > Tracker`).
- `POST /api/tracker/fetch {ref}` — resolves a tracker issue by ref and returns a normalised Issue payload (title, body, URL, labels). Typed error mapping: 400 bad ref / missing config, 401 auth rejected, 404 not found, 502 upstream.
- `GET /PUT /api/tracker/settings` — encrypted tracker credential management (GitHub token, Jira base URL / email / token) reusing the existing AES-GCM secretbox settings mechanism.
```

**8a. Commit**:
```
git commit --no-gpg-sign -m "docs: add tracker issue seeding to CHANGELOG and README"
```

---

## Final verify

```bash
# Go: build + scoped tests (no ./... to avoid ent regen)
cd server
go build ./...
go test ./internal/tracker/... ./internal/api/tracker/...

# Frontend: full suite
cd ..
pnpm test
pnpm typecheck
pnpm lint
```

If `go test ./...` must be run (e.g. for CI check), restore ent afterwards:
```bash
cd server && go test ./... && git checkout -- internal/db/ent/
```

---

## Ent change summary

**No ent schema change required.** Tracker tokens are stored in the existing `plugin_setting` table via `pluginsettings.Service` with `pluginID = "tracker"`. The `pluginSettingRepoAdapter` (`server/cmd/serve/plugin_adapters.go:15`) bridges the ent repo to the `pluginsettings.Repo` interface. No `go generate ./...` needed.

---

## Spec / reality notes

1. **`pluginsettings.validateValue` for `url` type** requires a valid `http(s)` URL with host. `tracker.jira.baseUrl` is declared `Type: "url"` — this is correct behaviour (users will enter a real Jira URL). GitHub token fields are `Type: "string"` (no URL validation).

2. **`BacklogForm.vue` slug auto-update**: `slugify(iss.title)` reuses the existing `slugify` from `src/utils/validation.ts`. The existing `onTitleInput` logic is NOT called (to avoid double-slugify). The import handler sets both `title.value` and `slug.value` directly.

3. **`useTrackerImport` called inside handler**: The composable has no reactive state, so calling it inside `importFromIssue` (not at setup level) is safe. If the linter flags this, extract `const { fetchIssue } = useTrackerImport()` to component setup level.

4. **Router Origin check**: `RequireSameOriginForMutations` middleware at `router.go:249` applies to all routes in the protected group including `POST /api/tracker/fetch` and `PUT /api/tracker/settings`. No per-handler check needed; the frontend sends `Origin: window.location.origin` in all mutation requests.
