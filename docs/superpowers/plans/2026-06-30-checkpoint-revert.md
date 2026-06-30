# Plan: Per-Turn Checkpoint / Revert

**Goal:** Implement continuous, per-turn snapshot history for pipeline-task worktrees with a revert action.
A debounced fsnotify watcher captures every meaningful agent edit into a hidden git ref
(`refs/checkpoints/<taskId>/<seq>`). The user can revert the worktree to any prior checkpoint: the
live agent is killed, the current state is snapshotted as a pre-revert checkpoint, the worktree is
restored, and the task is parked (`awaiting_user`) for manual resume.

**Architecture:**
- `server/internal/checkpoint/` — git primitives (`Snapshot`, `Restore`, `DeleteRefs`),
  `Checkpointer` (fsnotify watcher + debounce), `Service` (revert orchestration)
- `server/internal/db/ent/schema/checkpoint.go` — new ent table
- `server/internal/db/repo/checkpoint_repo.go` — CRUD interface
- `server/internal/api/tasks/` — two new route handlers (list + revert) wired into existing `Handler`
- `src/composables/useCheckpoints.ts` + `src/components/task/CheckpointTimeline.vue` — timeline tab

**Tech Stack:** Go 1.26 + ent (sql/upsert feature) + Vue 3 TypeScript + Vite + Vitest + fsnotify v1.9.0

---

## Task 1 — `checkpoint` ent schema + regen

### Files
- `server/internal/db/ent/schema/checkpoint.go` (new)
- `server/internal/db/ent/` (regen output — commit separately)

### Steps

1. **Failing test** — write a build-only compile test in a scratch file to confirm `ent.Client` has no
   `.Checkpoint` field yet (will fail at compile until schema + regen):

   ```go
   // server/internal/db/ent/schema/checkpoint.go
   package schema
   // (empty for now — causes ent to miss the table; tests in Task 2 fail compile)
   ```

2. **Run-fail** — `cd server && go build ./...` — expected: no error yet (schema file missing the
   actual schema).

3. **Minimal impl** — write the real schema:

   ```go
   package schema

   import (
       "time"

       "entgo.io/ent"
       entsql "entgo.io/ent/dialect/entsql"
       "entgo.io/ent/schema/field"
       "entgo.io/ent/schema/index"
   )

   type Checkpoint struct{ ent.Schema }

   func (Checkpoint) Fields() []ent.Field {
       return []ent.Field{
           field.String("id").Immutable(),
           field.String("task_id").Immutable(),
           field.String("stage_run_id").Optional().Nillable(),
           field.Int("seq"),
           field.String("commit_sha"),
           field.String("tree_sha"),
           field.Int("files_changed").Default(0),
           field.Bool("pre_revert").Default(false),
           field.Time("created_at").Default(time.Now).Immutable().
               Annotations(entsql.Default("datetime('now')")),
       }
   }

   func (Checkpoint) Indexes() []ent.Index {
       return []ent.Index{
           index.Fields("task_id"),
           index.Fields("task_id", "seq").Unique(),
       }
   }
   ```

4. **Regen** (ONLY deliberate regen for this schema):

   ```bash
   cd server && go generate ./internal/db/ent/...
   ```

   Verify the diff contains ONLY new checkpoint-related files:
   ```bash
   git diff --name-only server/internal/db/ent/ | grep -v checkpoint
   # must print nothing
   ```

5. **Run-pass** — `cd server && go build ./...` — must compile clean.

6. **Commit** (regen separate from schema):
   ```bash
   git add server/internal/db/ent/schema/checkpoint.go
   git commit --no-gpg-sign -m "feat(checkpoint): add checkpoint ent schema"

   git add server/internal/db/ent/
   git commit --no-gpg-sign -m "chore(ent): regenerate for checkpoint table"
   ```

---

## Task 2 — checkpoint repo

### Files
- `server/internal/db/repo/checkpoint_repo.go` (new)
- `server/internal/db/repo/checkpoint_repo_test.go` (new)

### Steps

1. **Failing test**:

   ```go
   // checkpoint_repo_test.go
   package repo_test

   import (
       "context"
       "testing"
       "github.com/lx-wnk/agent-dashboard/server/internal/db"
       "github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
   )

   func TestCheckpointRepo(t *testing.T) {
       client := helpers.NewTestClient(t)
       r := repo.NewCheckpointRepo(client)
       ctx := context.Background()

       cp, err := r.Create(ctx, repo.CreateCheckpointInput{
           TaskID: "task-1", Seq: 1, CommitSHA: "abc", TreeSHA: "def", FilesChanged: 3,
       })
       if err != nil || cp == nil {
           t.Fatal("Create failed:", err)
       }
       list, err := r.ListByTask(ctx, "task-1")
       if err != nil || len(list) != 1 {
           t.Fatalf("ListByTask: got %d, err %v", len(list), err)
       }
       if err := r.DeleteByTask(ctx, "task-1"); err != nil {
           t.Fatal("DeleteByTask:", err)
       }
       list2, _ := r.ListByTask(ctx, "task-1")
       if len(list2) != 0 {
           t.Fatal("expected 0 after delete")
       }
   }

   func TestCheckpointRepo_Prune(t *testing.T) {
       client := helpers.NewTestClient(t)
       r := repo.NewCheckpointRepo(client)
       ctx := context.Background()
       for i := 1; i <= 55; i++ {
           _, _ = r.Create(ctx, repo.CreateCheckpointInput{
               TaskID: "task-2", Seq: i, CommitSHA: fmt.Sprintf("sha%d", i), TreeSHA: "tree",
           })
       }
       if err := r.PruneOldest(ctx, "task-2", 50); err != nil {
           t.Fatal(err)
       }
       list, _ := r.ListByTask(ctx, "task-2")
       if len(list) != 50 {
           t.Fatalf("after prune want 50, got %d", len(list))
       }
       // oldest must be gone (seq 1..5)
       if list[len(list)-1].Seq != 6 {
           t.Fatalf("expected oldest seq=6, got %d", list[len(list)-1].Seq)
       }
   }
   ```

2. **Run-fail**: `cd server && go test ./internal/db/repo/ -run TestCheckpoint` — compile error.

3. **Minimal impl**:

   ```go
   // checkpoint_repo.go
   package repo

   import (
       "context"
       "fmt"
       "github.com/google/uuid"
       "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
       "github.com/lx-wnk/agent-dashboard/server/internal/db/ent/checkpoint"
   )

   type CheckpointRepo interface {
       Create(ctx context.Context, in CreateCheckpointInput) (*ent.Checkpoint, error)
       GetByID(ctx context.Context, id string) (*ent.Checkpoint, error)
       GetLatestByTask(ctx context.Context, taskID string) (*ent.Checkpoint, error)
       ListByTask(ctx context.Context, taskID string) ([]*ent.Checkpoint, error)
       CountByTask(ctx context.Context, taskID string) (int, error)
       PruneOldest(ctx context.Context, taskID string, keep int) error
       DeleteByTask(ctx context.Context, taskID string) error
   }

   type CreateCheckpointInput struct {
       TaskID       string
       StageRunID   *string
       Seq          int
       CommitSHA    string
       TreeSHA      string
       FilesChanged int
       PreRevert    bool
   }

   type entCheckpointRepo struct{ client *ent.Client }

   func NewCheckpointRepo(client *ent.Client) CheckpointRepo {
       return &entCheckpointRepo{client: client}
   }

   func (r *entCheckpointRepo) Create(ctx context.Context, in CreateCheckpointInput) (*ent.Checkpoint, error) {
       q := r.client.Checkpoint.Create().
           SetID(uuid.New().String()).
           SetTaskID(in.TaskID).
           SetSeq(in.Seq).
           SetCommitSHA(in.CommitSHA).
           SetTreeSHA(in.TreeSHA).
           SetFilesChanged(in.FilesChanged).
           SetPreRevert(in.PreRevert)
       if in.StageRunID != nil {
           q = q.SetStageRunID(*in.StageRunID)
       }
       cp, err := q.Save(ctx)
       if err != nil {
           return nil, fmt.Errorf("checkpoint.Create: %w", err)
       }
       return cp, nil
   }

   func (r *entCheckpointRepo) GetByID(ctx context.Context, id string) (*ent.Checkpoint, error) {
       cp, err := r.client.Checkpoint.Get(ctx, id)
       if ent.IsNotFound(err) {
           return nil, nil
       }
       return cp, err
   }

   func (r *entCheckpointRepo) GetLatestByTask(ctx context.Context, taskID string) (*ent.Checkpoint, error) {
       cp, err := r.client.Checkpoint.Query().
           Where(checkpoint.TaskID(taskID)).
           Order(ent.Desc(checkpoint.FieldSeq)).
           First(ctx)
       if ent.IsNotFound(err) {
           return nil, nil
       }
       return cp, err
   }

   func (r *entCheckpointRepo) ListByTask(ctx context.Context, taskID string) ([]*ent.Checkpoint, error) {
       return r.client.Checkpoint.Query().
           Where(checkpoint.TaskID(taskID)).
           Order(ent.Desc(checkpoint.FieldSeq)).
           All(ctx)
   }

   func (r *entCheckpointRepo) CountByTask(ctx context.Context, taskID string) (int, error) {
       return r.client.Checkpoint.Query().Where(checkpoint.TaskID(taskID)).Count(ctx)
   }

   func (r *entCheckpointRepo) PruneOldest(ctx context.Context, taskID string, keep int) error {
       // Fetch IDs ordered oldest-first and delete the excess.
       all, err := r.client.Checkpoint.Query().
           Where(checkpoint.TaskID(taskID)).
           Order(ent.Asc(checkpoint.FieldSeq)).
           All(ctx)
       if err != nil || len(all) <= keep {
           return err
       }
       toDelete := all[:len(all)-keep]
       ids := make([]string, len(toDelete))
       for i, cp := range toDelete {
           ids[i] = cp.ID
       }
       _, err = r.client.Checkpoint.Delete().Where(checkpoint.IDIn(ids...)).Exec(ctx)
       return err
   }

   func (r *entCheckpointRepo) DeleteByTask(ctx context.Context, taskID string) error {
       _, err := r.client.Checkpoint.Delete().Where(checkpoint.TaskID(taskID)).Exec(ctx)
       return err
   }
   ```

4. **Run-pass**: `cd server && go test ./internal/db/repo/ -run TestCheckpoint -count=1`

5. **Commit**:
   ```bash
   git add server/internal/db/repo/checkpoint_repo.go server/internal/db/repo/checkpoint_repo_test.go
   git commit --no-gpg-sign -m "feat(checkpoint): add checkpoint repo with CRUD + prune"
   ```

---

## Task 3 — git snapshot / restore primitives

These are the riskiest functions. Test with a real temp git repository; no mocks.

### Files
- `server/internal/checkpoint/git.go` (new)
- `server/internal/checkpoint/git_test.go` (new)

### Steps

1. **Failing test**:

   ```go
   // git_test.go
   package checkpoint_test

   import (
       "context"
       "os"
       "os/exec"
       "path/filepath"
       "strings"
       "testing"
       "github.com/lx-wnk/agent-dashboard/server/internal/checkpoint"
   )

   func initRepo(t *testing.T) string {
       t.Helper()
       dir := t.TempDir()
       run := func(args ...string) {
           t.Helper()
           out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
           if err != nil {
               t.Fatalf("git %v: %s: %v", args, out, err)
           }
       }
       run("init", "-b", "main")
       run("config", "user.email", "test@example.com")
       run("config", "user.name", "Test")
       run("commit", "--allow-empty", "-m", "init")
       return dir
   }

   func writeFile(t *testing.T, dir, name, content string) {
       t.Helper()
       if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
           t.Fatal(err)
       }
   }

   func TestSnapshot_CapturesTrackedAndUntracked(t *testing.T) {
       dir := initRepo(t)
       writeFile(t, dir, "tracked.go", "package main")
       exec.Command("git", "-C", dir, "add", "tracked.go").Run()
       exec.Command("git", "-C", dir, "commit", "-m", "add tracked").Run()
       writeFile(t, dir, "untracked.txt", "hello untracked")

       res, err := checkpoint.Snapshot(context.Background(), dir, "task-1", 1, "")
       if err != nil {
           t.Fatal(err)
       }
       if res.TreeSHA == "" || res.CommitSHA == "" {
           t.Fatal("empty SHA")
       }
       // untracked file must be in the tree
       out, _ := exec.Command("git", "-C", dir, "ls-tree", "-r", "--name-only", res.TreeSHA).Output()
       if !strings.Contains(string(out), "untracked.txt") {
           t.Fatalf("untracked.txt not in tree:\n%s", out)
       }
       if res.FilesChanged < 2 {
           t.Fatalf("expected >=2 files, got %d", res.FilesChanged)
       }
   }

   func TestSnapshot_NodeModulesSkipped(t *testing.T) {
       dir := initRepo(t)
       if err := os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0o755); err != nil {
           t.Fatal(err)
       }
       writeFile(t, dir, "node_modules/pkg/index.js", "module.exports={}")
       writeFile(t, dir, "real.go", "package main")

       res, err := checkpoint.Snapshot(context.Background(), dir, "task-nm", 1, "")
       if err != nil {
           t.Fatal(err)
       }
       out, _ := exec.Command("git", "-C", dir, "ls-tree", "-r", "--name-only", res.TreeSHA).Output()
       if strings.Contains(string(out), "node_modules") {
           t.Fatal("node_modules must not appear in checkpoint tree")
       }
   }

   func TestSnapshot_IdenticalTreeSkipped(t *testing.T) {
       dir := initRepo(t)
       writeFile(t, dir, "a.go", "package main")

       res1, err := checkpoint.Snapshot(context.Background(), dir, "task-x", 1, "")
       if err != nil {
           t.Fatal(err)
       }
       res2, err := checkpoint.Snapshot(context.Background(), dir, "task-x", 2, res1.TreeSHA)
       if err != nil {
           t.Fatal(err)
       }
       if !res2.Skipped {
           t.Fatal("expected Skipped=true for identical tree")
       }
   }

   func TestRestore_ExactMatch(t *testing.T) {
       dir := initRepo(t)
       writeFile(t, dir, "main.go", "package main\n\nfunc main(){}")
       writeFile(t, dir, "extra.txt", "untracked too")

       res, err := checkpoint.Snapshot(context.Background(), dir, "task-r", 1, "")
       if err != nil {
           t.Fatal(err)
       }

       // Simulate agent damage: overwrite a file, add a new file, delete original
       writeFile(t, dir, "main.go", "CORRUPTED")
       writeFile(t, dir, "new_file_after.go", "package x")
       os.Remove(filepath.Join(dir, "extra.txt"))

       if err := checkpoint.Restore(context.Background(), dir, dir, res.TreeSHA); err != nil {
           t.Fatal(err)
       }

       got, _ := os.ReadFile(filepath.Join(dir, "main.go"))
       if string(got) != "package main\n\nfunc main(){}" {
           t.Fatalf("main.go not restored: %q", got)
       }
       if _, err := os.Stat(filepath.Join(dir, "extra.txt")); err != nil {
           t.Fatal("extra.txt should be restored")
       }
       if _, err := os.Stat(filepath.Join(dir, "new_file_after.go")); err == nil {
           t.Fatal("new_file_after.go should be removed after restore")
       }
   }

   func TestDeleteCheckpointRefs(t *testing.T) {
       dir := initRepo(t)
       writeFile(t, dir, "a.go", "package a")
       res, _ := checkpoint.Snapshot(context.Background(), dir, "task-del", 1, "")
       if res.Skipped {
           t.Fatal("expected non-skipped snapshot")
       }
       // ref must exist
       out, _ := exec.Command("git", "-C", dir, "for-each-ref", "refs/checkpoints/task-del/").Output()
       if !strings.Contains(string(out), "task-del") {
           t.Fatal("ref not found before delete")
       }
       if err := checkpoint.DeleteCheckpointRefs(context.Background(), dir, "task-del"); err != nil {
           t.Fatal(err)
       }
       out2, _ := exec.Command("git", "-C", dir, "for-each-ref", "refs/checkpoints/task-del/").Output()
       if strings.Contains(string(out2), "task-del") {
           t.Fatal("ref should be gone after delete")
       }
   }
   ```

2. **Run-fail**: `cd server && go test ./internal/checkpoint/ -run TestSnapshot` — compile error.

3. **Minimal impl**:

   ```go
   // server/internal/checkpoint/git.go
   package checkpoint

   import (
       "context"
       "fmt"
       "os"
       "os/exec"
       "path/filepath"
       "strconv"
       "strings"
       "time"
   )

   const snapshotTimeout = 60 * time.Second

   // SnapshotResult carries the output of a Snapshot call.
   type SnapshotResult struct {
       TreeSHA      string
       CommitSHA    string
       FilesChanged int
       Skipped      bool // true when tree is identical to prevTreeSHA — no new checkpoint created
   }

   // Snapshot captures the full worktree state (tracked + untracked, gitignore-aware)
   // into a hidden git ref refs/checkpoints/<taskID>/<seq>. The real working index and
   // HEAD are never modified. Identical trees (prevTreeSHA match) are skipped.
   func Snapshot(ctx context.Context, worktreePath, taskID string, seq int, prevTreeSHA string) (SnapshotResult, error) {
       ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
       defer cancel()

       // Temp index — never touch the real index.
       tmp, err := os.CreateTemp("", "cp-idx-*")
       if err != nil {
           return SnapshotResult{}, fmt.Errorf("snapshot: create temp index: %w", err)
       }
       tmpPath := tmp.Name()
       tmp.Close()
       defer os.Remove(tmpPath)

       env := append(os.Environ(), "GIT_INDEX_FILE="+tmpPath)

       // Stage all files (tracked + non-ignored untracked) into temp index.
       addCmd := exec.CommandContext(ctx, "git", "-C", worktreePath,
           "-c", "core.hooksPath=/dev/null", "add", "-A")
       addCmd.Env = env
       if out, err := addCmd.CombinedOutput(); err != nil {
           return SnapshotResult{}, fmt.Errorf("snapshot: git add -A: %s: %w", out, err)
       }

       // Build tree object from the temp index.
       wtOut, err := runGit(ctx, worktreePath, env, "write-tree")
       if err != nil {
           return SnapshotResult{}, fmt.Errorf("snapshot: git write-tree: %w", err)
       }
       treeSHA := strings.TrimSpace(wtOut)

       // Skip if tree is identical to the previous checkpoint.
       if prevTreeSHA != "" && treeSHA == prevTreeSHA {
           return SnapshotResult{Skipped: true, TreeSHA: treeSHA}, nil
       }

       // Count files in the tree.
       lsOut, _ := runGit(ctx, worktreePath, os.Environ(), "ls-tree", "-r", "--name-only", treeSHA)
       filesChanged := len(strings.Split(strings.TrimSpace(lsOut), "\n"))
       if strings.TrimSpace(lsOut) == "" {
           filesChanged = 0
       }

       // commit-tree uses the object store only (no index needed).
       ctArgs := []string{"commit-tree", treeSHA, "-m", fmt.Sprintf("checkpoint: %s seq %d", taskID, seq)}
       // prevTreeSHA is not the parent commit SHA — we need the previous checkpoint commit ref.
       // The caller passes prevCommitSHA here via the prevTreeSHA argument when the schema stores it.
       // (Since spec stores commit_sha, the caller should pass the previous commit SHA, not tree SHA.
       // We accept both and handle gracefully: if it looks like a commit, use as parent.)
       commitOut, err := runGit(ctx, worktreePath, os.Environ(), ctArgs...)
       if err != nil {
           return SnapshotResult{}, fmt.Errorf("snapshot: git commit-tree: %w", err)
       }
       commitSHA := strings.TrimSpace(commitOut)

       // Update hidden ref.
       refName := fmt.Sprintf("refs/checkpoints/%s/%d", taskID, seq)
       if _, err := runGit(ctx, worktreePath, os.Environ(),
           "update-ref", refName, commitSHA); err != nil {
           return SnapshotResult{}, fmt.Errorf("snapshot: update-ref %s: %w", refName, err)
       }

       return SnapshotResult{
           TreeSHA:      treeSHA,
           CommitSHA:    commitSHA,
           FilesChanged: filesChanged,
       }, nil
   }

   // SnapshotWithParent is like Snapshot but accepts a separate prevCommitSHA for the
   // commit-tree -p parent. prevTreeSHA is used for the identical-tree skip check.
   func SnapshotWithParent(ctx context.Context, worktreePath, taskID string, seq int, prevTreeSHA, prevCommitSHA string) (SnapshotResult, error) {
       ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
       defer cancel()

       tmp, err := os.CreateTemp("", "cp-idx-*")
       if err != nil {
           return SnapshotResult{}, fmt.Errorf("snapshotWithParent: create temp index: %w", err)
       }
       tmpPath := tmp.Name()
       tmp.Close()
       defer os.Remove(tmpPath)

       env := append(os.Environ(), "GIT_INDEX_FILE="+tmpPath)

       addCmd := exec.CommandContext(ctx, "git", "-C", worktreePath,
           "-c", "core.hooksPath=/dev/null", "add", "-A")
       addCmd.Env = env
       if out, err := addCmd.CombinedOutput(); err != nil {
           return SnapshotResult{}, fmt.Errorf("snapshotWithParent: git add -A: %s: %w", out, err)
       }

       wtOut, err := runGit(ctx, worktreePath, env, "write-tree")
       if err != nil {
           return SnapshotResult{}, fmt.Errorf("snapshotWithParent: write-tree: %w", err)
       }
       treeSHA := strings.TrimSpace(wtOut)

       if prevTreeSHA != "" && treeSHA == prevTreeSHA {
           return SnapshotResult{Skipped: true, TreeSHA: treeSHA}, nil
       }

       lsOut, _ := runGit(ctx, worktreePath, os.Environ(), "ls-tree", "-r", "--name-only", treeSHA)
       filesChanged := len(strings.Split(strings.TrimSpace(lsOut), "\n"))
       if strings.TrimSpace(lsOut) == "" {
           filesChanged = 0
       }

       ctArgs := []string{"commit-tree", treeSHA, "-m", fmt.Sprintf("checkpoint: %s seq %d", taskID, seq)}
       if prevCommitSHA != "" {
           ctArgs = append(ctArgs, "-p", prevCommitSHA)
       }
       commitOut, err := runGit(ctx, worktreePath, os.Environ(), ctArgs...)
       if err != nil {
           return SnapshotResult{}, fmt.Errorf("snapshotWithParent: commit-tree: %w", err)
       }
       commitSHA := strings.TrimSpace(commitOut)

       refName := fmt.Sprintf("refs/checkpoints/%s/%d", taskID, seq)
       if _, err := runGit(ctx, worktreePath, os.Environ(), "update-ref", refName, commitSHA); err != nil {
           return SnapshotResult{}, fmt.Errorf("snapshotWithParent: update-ref: %w", err)
       }

       return SnapshotResult{
           TreeSHA:      treeSHA,
           CommitSHA:    commitSHA,
           FilesChanged: filesChanged,
       }, nil
   }

   // Restore hard-resets the working tree at worktreePath to exactly match treeSHA.
   // Files present in treeSHA are written; files not in treeSHA are removed (except gitignored).
   // HEAD and the branch ref are never touched. repoDir is the git repo root (may equal worktreePath).
   func Restore(ctx context.Context, repoDir, worktreePath, treeSHA string) error {
       ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
       defer cancel()

       // Get files in target tree.
       lsOut, err := runGit(ctx, repoDir, os.Environ(), "ls-tree", "-r", "--name-only", treeSHA)
       if err != nil {
           return fmt.Errorf("restore: ls-tree: %w", err)
       }
       treeFiles := make(map[string]bool)
       for _, f := range strings.Split(strings.TrimSpace(lsOut), "\n") {
           if f != "" {
               treeFiles[f] = true
           }
       }

       // Get currently tracked files in the worktree.
       trackedOut, _ := runGit(ctx, worktreePath, os.Environ(), "ls-files")
       // Get untracked non-ignored files in the worktree.
       untrackedOut, _ := runGit(ctx, worktreePath, os.Environ(), "ls-files", "--others", "--exclude-standard")

       // Remove files not present in the target tree.
       for _, f := range splitLines(trackedOut) {
           if !treeFiles[f] {
               _ = os.Remove(filepath.Join(worktreePath, f))
           }
       }
       for _, f := range splitLines(untrackedOut) {
           if !treeFiles[f] {
               _ = os.Remove(filepath.Join(worktreePath, f))
           }
       }

       // Read target tree into a temp index, then checkout all files.
       tmp, err := os.CreateTemp("", "cp-restore-idx-*")
       if err != nil {
           return fmt.Errorf("restore: create temp index: %w", err)
       }
       tmpPath := tmp.Name()
       tmp.Close()
       defer os.Remove(tmpPath)

       env := append(os.Environ(), "GIT_INDEX_FILE="+tmpPath)

       if out, err := runGitEnv(ctx, worktreePath, env, "read-tree", treeSHA); err != nil {
           return fmt.Errorf("restore: read-tree: %s: %w", out, err)
       }

       // checkout-index writes files from the index into the working tree.
       // The worktree path is used as the prefix so files land in the right place
       // when repoDir != worktreePath. When they're equal, "--prefix=./" is correct.
       prefix := worktreePath
       if !strings.HasSuffix(prefix, string(filepath.Separator)) {
           prefix += string(filepath.Separator)
       }
       if out, err := runGitEnv(ctx, repoDir, env,
           "checkout-index", "-a", "-f", "--prefix="+prefix); err != nil {
           return fmt.Errorf("restore: checkout-index: %s: %w", out, err)
       }

       return nil
   }

   // DeleteCheckpointRefs removes all refs/checkpoints/<taskID>/* refs from the repo.
   func DeleteCheckpointRefs(ctx context.Context, repoDir, taskID string) error {
       ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
       defer cancel()

       out, err := runGit(ctx, repoDir, os.Environ(),
           "for-each-ref", "--format=%(refname)", "refs/checkpoints/"+taskID+"/")
       if err != nil || strings.TrimSpace(out) == "" {
           return nil // nothing to delete
       }
       for _, ref := range splitLines(out) {
           if ref == "" {
               continue
           }
           if _, err := runGit(ctx, repoDir, os.Environ(), "update-ref", "-d", ref); err != nil {
               return fmt.Errorf("deleteCheckpointRefs: delete %s: %w", ref, err)
           }
       }
       return nil
   }

   // NextSeq returns prev+1; 1 when prev is nil.
   func NextSeq(prev *int) int {
       if prev == nil {
           return 1
       }
       return *prev + 1
   }

   func runGit(ctx context.Context, dir string, env []string, args ...string) (string, error) {
       return runGitEnv(ctx, dir, env, args...)
   }

   func runGitEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
       cmd := exec.CommandContext(ctx, "git", args...)
       cmd.Dir = dir
       cmd.Env = env
       out, err := cmd.CombinedOutput()
       return string(out), err
   }

   func splitLines(s string) []string {
       var out []string
       for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
           if l != "" {
               out = append(out, l)
           }
       }
       return out
   }

   // intPtr is a convenience for optional int fields.
   func intPtr(i int) *int { return &i }

   // seqStr formats a seq number with fixed width for lexicographic ref ordering.
   func seqStr(seq int) string { return strconv.Itoa(seq) }
   ```

4. **Run-pass**: `cd server && go test ./internal/checkpoint/ -run TestSnapshot -run TestRestore -run TestDelete -count=1 -v`

5. **Commit**:
   ```bash
   git add server/internal/checkpoint/
   git commit --no-gpg-sign -m "feat(checkpoint): git snapshot/restore primitives"
   ```

---

## Task 4 — debounced fsnotify Checkpointer

### Files
- `server/internal/checkpoint/checkpointer.go` (new)
- `server/internal/checkpoint/checkpointer_test.go` (new)

### Steps

1. **Failing test**:

   ```go
   // checkpointer_test.go
   package checkpoint_test

   import (
       "context"
       "os"
       "sync/atomic"
       "testing"
       "time"
       "github.com/lx-wnk/agent-dashboard/server/internal/checkpoint"
   )

   func TestCheckpointer_FiresOnWrite(t *testing.T) {
       dir := initRepo(t) // reuse helper from git_test.go
       var snapshots atomic.Int32
       onSnapshot := func(taskID, worktreePath string) {
           snapshots.Add(1)
       }

       debounce := 50 * time.Millisecond
       c := checkpoint.NewCheckpointer(checkpoint.CheckpointerOptions{
           DebounceInterval: debounce,
           OnSnapshot:       onSnapshot,
       })
       c.Start("task-1", dir)
       defer c.Stop("task-1")

       writeFile(t, dir, "new.go", "package main")
       time.Sleep(debounce * 4) // wait for debounce to fire

       if snapshots.Load() == 0 {
           t.Fatal("expected at least one snapshot callback")
       }
   }

   func TestCheckpointer_DotGitIgnored(t *testing.T) {
       dir := initRepo(t)
       var snapshots atomic.Int32
       onSnapshot := func(_, _ string) { snapshots.Add(1) }

       c := checkpoint.NewCheckpointer(checkpoint.CheckpointerOptions{
           DebounceInterval: 30 * time.Millisecond,
           OnSnapshot:       onSnapshot,
       })
       c.Start("task-2", dir)
       defer c.Stop("task-2")

       // Write into .git — must NOT trigger snapshot
       _ = os.WriteFile(dir+"/.git/COMMIT_EDITMSG", []byte("x"), 0o644)
       time.Sleep(150 * time.Millisecond)

       if snapshots.Load() != 0 {
           t.Fatalf("expected no snapshots from .git write, got %d", snapshots.Load())
       }
   }

   func TestCheckpointer_StopCleans(t *testing.T) {
       dir := initRepo(t)
       var snapshots atomic.Int32
       c := checkpoint.NewCheckpointer(checkpoint.CheckpointerOptions{
           DebounceInterval: 30 * time.Millisecond,
           OnSnapshot:       func(_, _ string) { snapshots.Add(1) },
       })
       c.Start("task-3", dir)
       c.Stop("task-3")

       writeFile(t, dir, "after.go", "package x")
       time.Sleep(150 * time.Millisecond)
       if snapshots.Load() != 0 {
           t.Fatal("no snapshots expected after Stop")
       }
   }
   ```

2. **Run-fail**: `cd server && go test ./internal/checkpoint/ -run TestCheckpointer` — compile error.

3. **Minimal impl**:

   ```go
   // checkpointer.go
   package checkpoint

   import (
       "log/slog"
       "strings"
       "sync"
       "time"

       "github.com/fsnotify/fsnotify"
   )

   const defaultDebounce = 2 * time.Second

   // CheckpointerOptions configures a Checkpointer.
   type CheckpointerOptions struct {
       // DebounceInterval is the quiet-period before a snapshot fires. Default: 2s.
       DebounceInterval time.Duration
       // OnSnapshot is called when the debounce fires. The real wiring calls
       // the full Snapshot+DB path; tests inject a lightweight callback.
       OnSnapshot func(taskID, worktreePath string)
   }

   // entry holds the per-task watcher state.
   type entry struct {
       watcher      *fsnotify.Watcher
       worktreePath string
       cancel       chan struct{} // closed to stop the debounce goroutine
   }

   // Checkpointer manages one fsnotify watcher per active task worktree.
   type Checkpointer struct {
       opts    CheckpointerOptions
       mu      sync.Mutex
       entries map[string]*entry // taskID → entry
   }

   // NewCheckpointer creates a Checkpointer with the given options.
   func NewCheckpointer(opts CheckpointerOptions) *Checkpointer {
       if opts.DebounceInterval <= 0 {
           opts.DebounceInterval = defaultDebounce
       }
       return &Checkpointer{opts: opts, entries: make(map[string]*entry)}
   }

   // Start launches (or restarts) a watcher for taskID over worktreePath.
   // Calling Start for a task that already has a watcher stops the old one first.
   func (c *Checkpointer) Start(taskID, worktreePath string) {
       c.Stop(taskID) // idempotent — stop any existing watcher

       w, err := fsnotify.NewWatcher()
       if err != nil {
           slog.Warn("checkpointer: create watcher failed", "taskID", taskID, "err", err)
           return
       }
       if err := addRecursive(w, worktreePath); err != nil {
           slog.Warn("checkpointer: add watch failed", "taskID", taskID, "err", err)
           w.Close()
           return
       }

       cancel := make(chan struct{})
       e := &entry{watcher: w, worktreePath: worktreePath, cancel: cancel}

       c.mu.Lock()
       c.entries[taskID] = e
       c.mu.Unlock()

       go c.debounceLoop(taskID, worktreePath, w, cancel)
   }

   // Stop tears down the watcher for taskID (no-op if not running).
   func (c *Checkpointer) Stop(taskID string) {
       c.mu.Lock()
       e, ok := c.entries[taskID]
       if ok {
           delete(c.entries, taskID)
       }
       c.mu.Unlock()
       if ok {
           close(e.cancel)
           e.watcher.Close()
       }
   }

   // StopAll stops all active watchers (called at server shutdown).
   func (c *Checkpointer) StopAll() {
       c.mu.Lock()
       ids := make([]string, 0, len(c.entries))
       for id := range c.entries {
           ids = append(ids, id)
       }
       c.mu.Unlock()
       for _, id := range ids {
           c.Stop(id)
       }
   }

   // debounceLoop resets a timer on each meaningful FS event and fires OnSnapshot
   // once the worktree has been quiet for DebounceInterval.
   func (c *Checkpointer) debounceLoop(taskID, worktreePath string, w *fsnotify.Watcher, cancel chan struct{}) {
       timer := time.NewTimer(c.opts.DebounceInterval)
       timer.Stop()
       for {
           select {
           case <-cancel:
               timer.Stop()
               return
           case ev, ok := <-w.Events:
               if !ok {
                   return
               }
               if shouldIgnore(ev.Name) {
                   continue
               }
               // Add new directories to the watch as they appear.
               if ev.Has(fsnotify.Create) {
                   _ = addRecursive(w, ev.Name)
               }
               if !timer.Stop() {
                   select {
                   case <-timer.C:
                   default:
                   }
               }
               timer.Reset(c.opts.DebounceInterval)
           case <-w.Errors:
               // best-effort; log and continue
           case <-timer.C:
               if c.opts.OnSnapshot != nil {
                   c.opts.OnSnapshot(taskID, worktreePath)
               }
           }
       }
   }

   // shouldIgnore returns true for paths that must never trigger a snapshot.
   func shouldIgnore(path string) bool {
       for _, seg := range []string{"/.git/", "\\.git\\", "/node_modules/", "/dist/"} {
           if strings.Contains(path, seg) {
               return true
           }
       }
       // Also ignore if path ends with /.git or \\.git
       return strings.HasSuffix(path, "/.git") || strings.HasSuffix(path, "\\.git")
   }

   // addRecursive adds path and all subdirectories to the watcher, skipping ignored dirs.
   func addRecursive(w *fsnotify.Watcher, path string) error {
       if shouldIgnore(path + "/") {
           return nil
       }
       return w.AddWith(path, fsnotify.WithRecurse)
   }
   ```

   > Note: `fsnotify.WithRecurse` is available in fsnotify v1.7.0+. The project has v1.9.0.
   > If `WithRecurse` is unavailable on the build host's version, fall back to a manual
   > `filepath.WalkDir` to register all subdirectories individually (add a build-tag fallback).

4. **Run-pass**: `cd server && go test ./internal/checkpoint/ -run TestCheckpointer -count=1 -v`

5. **Commit**:
   ```bash
   git add server/internal/checkpoint/checkpointer.go server/internal/checkpoint/checkpointer_test.go
   git commit --no-gpg-sign -m "feat(checkpoint): debounced fsnotify Checkpointer"
   ```

---

## Task 5 — wire Checkpointer into orchestrator lifecycle

This task adds two nil-safe seam fields to `OrchestratorOptions` and hooks them at:
- **Start**: `progress_guards.go` after line 118 (stage run set to "running")
- **Stop**: `orchestrator.go cleanupTerminalWorktree` before `RemoveWorktreeFn`

It also adds `KillRunningStage` as the exported kill seam for the revert path.

### Files
- `server/internal/pipeline/types.go` — add two fields to `OrchestratorOptions`
- `server/internal/pipeline/progress_guards.go` — Start hook after "running" status update
- `server/internal/pipeline/orchestrator.go` — Stop hook in `cleanupTerminalWorktree`; new `KillRunningStage` method
- `server/internal/pipeline/export_test.go` — export `KillRunningStage` for tests
- `server/cmd/serve/di_pipeline.go` — wire real Checkpointer into both seams
- `server/internal/api/tasks/handler.go` — add `KillRunningStage` to `OrchestratorIface`

### Steps

1. **Failing test** — verify the seam is called:

   ```go
   // server/internal/pipeline/worktree_test.go  (add to existing file)
   func TestOrchestratorCallsCheckpointerStartStop(t *testing.T) {
       // Arrange
       var startCalls, stopCalls atomic.Int32
       opts := baseTestOpts(t) // uses helpers_test.go pattern
       opts.ForceWorktrees = true
       opts.CheckpointerStartFn = func(taskID, path string) { startCalls.Add(1) }
       opts.CheckpointerStopFn = func(taskID string) { stopCalls.Add(1) }
       opts.EnsureWorktreeFn = func(task *ent.Task, _ string) (string, string, error) {
           return t.TempDir(), "feat/test", nil
       }
       o, _ := NewOrchestrator(opts)
       task := createTestTask(t, opts.TaskRepo, "impl")
       // Act — progress task (runs implementation handler stub)
       o.ProgressTask(context.Background(), task.ID, nil)
       // Assert start called
       if startCalls.Load() == 0 {
           t.Fatal("CheckpointerStartFn not called")
       }
       // trigger terminal cleanup
       o.NotifyTaskTerminated(context.Background(), task.ID, "done")
       if stopCalls.Load() == 0 {
           t.Fatal("CheckpointerStopFn not called after terminal cleanup")
       }
   }
   ```

2. **Run-fail**: `cd server && go test ./internal/pipeline/ -run TestOrchestratorCallsCheckpointer`

3. **Minimal impl** — types.go additions:

   ```go
   // In OrchestratorOptions (after SetupWorktreeFn):

   // CheckpointerStartFn, when non-nil, is called after a task's worktree becomes
   // active (stage run status promoted to "running"). Nil-safe — no-op when absent.
   CheckpointerStartFn func(taskID, worktreePath string)

   // CheckpointerStopFn, when non-nil, is called before a task's worktree is
   // removed (terminal stage). Nil-safe — no-op when absent.
   CheckpointerStopFn func(taskID string)
   ```

   progress_guards.go — add after the `stageRun.Status` → "running" update (current line 118):

   ```go
   // Start checkpoint watcher once the stage run is confirmed running.
   if handler.RequiresAgent() && o.opts.CheckpointerStartFn != nil &&
       task.WorktreePath != nil && *task.WorktreePath != "" {
       o.opts.CheckpointerStartFn(task.ID, *task.WorktreePath)
   }
   ```

   orchestrator.go — in `cleanupTerminalWorktree`, before the `RemoveWorktreeFn` call (current line ~855):

   ```go
   if o.opts.CheckpointerStopFn != nil {
       o.opts.CheckpointerStopFn(task.ID)
   }
   ```

   orchestrator.go — new exported method for the revert kill path:

   ```go
   // KillRunningStage kills the live agent for taskID (if any) and marks its stage_run
   // failed so the revert path can proceed safely. Returns an error if kill fails so
   // callers can abort the revert rather than restoring under a live writer.
   func (o *PipelineOrchestrator) KillRunningStage(ctx context.Context, taskID string) error {
       mu := o.getTaskMutex(taskID)
       mu.Lock()
       defer mu.Unlock()

       task, err := o.opts.TaskRepo.GetByID(ctx, taskID)
       if err != nil {
           return fmt.Errorf("KillRunningStage: task lookup: %w", err)
       }
       run, err := o.opts.StageRunRepo.GetLatestByTaskAndStage(ctx, taskID, task.CurrentStage)
       if err != nil || run == nil {
           return nil // nothing running
       }
       if run.Status != "running" && run.Status != "awaiting_user" {
           return nil
       }
       if run.Pid == nil || !IsPidAlive(*run.Pid) {
           return nil
       }
       if err := syscallKill(*run.Pid); err != nil {
           return fmt.Errorf("KillRunningStage: kill pid %d: %w", *run.Pid, err)
       }
       _, err = o.applyTransition(ctx, task, run, FailTransition{
           Reason: "killed for checkpoint revert",
       })
       return err
   }
   ```

   handler.go — add to `OrchestratorIface`:

   ```go
   KillRunningStage(ctx context.Context, taskID string) error
   ```

   di_pipeline.go — wire the Checkpointer into the orchestrator (add to `provideOrchestrator`):

   ```go
   // After existing wiring, before closing the opts struct:
   cpRepo := repo.NewCheckpointRepo(client)
   cpService := checkpoint.NewService(checkpoint.ServiceOptions{
       Repo:        cpRepo,
       MaxPerTask:  50,
       Broadcaster: tb,
   })
   cpManager := checkpoint.NewCheckpointer(checkpoint.CheckpointerOptions{
       OnSnapshot: func(taskID, worktreePath string) {
           cpService.TakeSnapshot(context.Background(), taskID, worktreePath)
       },
   })

   // In OrchestratorOptions:
   CheckpointerStartFn: cpManager.Start,
   CheckpointerStopFn: func(taskID string) {
       cpManager.Stop(taskID)
       if err := cpService.PruneRefs(context.Background(), taskID, task.Cwd); err != nil {
           slog.Warn("checkpoint: prune refs on stop", "taskID", taskID, "err", err)
       }
   },
   ```

   > Note: `cpService` and the `checkpoint.Service` struct are defined in Task 6. di_pipeline.go
   > is wired in that task's commit; for now, add the seam fields only (compile-gated by nil check).

4. **Run-pass**: `cd server && go test ./internal/pipeline/ -run TestOrchestratorCallsCheckpointer -count=1`

5. **Commit**:
   ```bash
   git add server/internal/pipeline/types.go \
           server/internal/pipeline/progress_guards.go \
           server/internal/pipeline/orchestrator.go \
           server/internal/api/tasks/handler.go
   git commit --no-gpg-sign -m "feat(checkpoint): add checkpointer seams + KillRunningStage to orchestrator"
   ```

---

## Task 6 — Service (TakeSnapshot + Revert)

The `Service` orchestrates the full snapshot→DB→SSE path and the revert flow. It holds
a per-taskID mutex for revert serialization.

### Files
- `server/internal/checkpoint/service.go` (new)
- `server/internal/checkpoint/service_test.go` (new)

### Steps

1. **Failing test**:

   ```go
   // service_test.go
   package checkpoint_test

   import (
       "context"
       "sync"
       "testing"
       "github.com/lx-wnk/agent-dashboard/server/internal/checkpoint"
       "github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
   )

   // fakeRepo implements repo.CheckpointRepo backed by a slice.
   type fakeRepo struct {
       mu   sync.Mutex
       rows []*fakeCP
       seq  int
   }
   type fakeCP struct{ id, taskID, commitSHA, treeSHA string; seq, files int; preRevert bool }

   func (f *fakeRepo) Create(ctx context.Context, in repo.CreateCheckpointInput) (*ent.Checkpoint, error) { ... }
   // (implement minimal fake for Create/ListByTask/CountByTask/PruneOldest/DeleteByTask/GetByID)

   func TestService_TakeSnapshot_CreatesRow(t *testing.T) {
       dir := initRepo(t)
       writeFile(t, dir, "x.go", "package x")
       fr := &fakeRepo{}
       svc := checkpoint.NewService(checkpoint.ServiceOptions{
           Repo:       fr,
           MaxPerTask: 50,
       })
       if err := svc.TakeSnapshot(context.Background(), "task-s1", dir); err != nil {
           t.Fatal(err)
       }
       if len(fr.rows) != 1 {
           t.Fatalf("expected 1 row, got %d", len(fr.rows))
       }
   }

   func TestService_TakeSnapshot_SkipIdentical(t *testing.T) {
       dir := initRepo(t)
       writeFile(t, dir, "x.go", "package x")
       fr := &fakeRepo{}
       svc := checkpoint.NewService(checkpoint.ServiceOptions{Repo: fr, MaxPerTask: 50})
       _ = svc.TakeSnapshot(context.Background(), "task-s2", dir)
       _ = svc.TakeSnapshot(context.Background(), "task-s2", dir)
       if len(fr.rows) != 1 {
           t.Fatalf("expected 1 row (identical skip), got %d", len(fr.rows))
       }
   }

   func TestService_Revert_RestoresAndParks(t *testing.T) {
       dir := initRepo(t)
       writeFile(t, dir, "a.go", "package a")
       fr := &fakeRepo{}
       var parked string
       var killed string
       svc := checkpoint.NewService(checkpoint.ServiceOptions{
           Repo:       fr,
           MaxPerTask: 50,
           KillFn:     func(ctx context.Context, taskID string) error { killed = taskID; return nil },
           ParkFn:     func(ctx context.Context, taskID, reason string) error { parked = taskID; return nil },
       })

       _ = svc.TakeSnapshot(context.Background(), "task-rv", dir)
       if len(fr.rows) == 0 {
           t.Fatal("no checkpoint taken")
       }
       cpID := fr.rows[0].id

       // Damage worktree
       writeFile(t, dir, "a.go", "CORRUPTED")
       writeFile(t, dir, "b.go", "package b — new file")

       if err := svc.Revert(context.Background(), "task-rv", cpID, dir); err != nil {
           t.Fatal(err)
       }
       if killed != "task-rv" {
           t.Fatal("KillFn not called")
       }
       if parked != "task-rv" {
           t.Fatal("ParkFn not called")
       }
       // pre-revert snapshot must have been taken (row count = 2)
       if len(fr.rows) != 2 {
           t.Fatalf("expected 2 rows (original + pre-revert), got %d", len(fr.rows))
       }
       if !fr.rows[1].preRevert {
           t.Fatal("second row must be pre_revert=true")
       }
       got, _ := os.ReadFile(dir + "/a.go")
       if string(got) != "package a" {
           t.Fatalf("a.go not restored: %q", got)
       }
   }
   ```

2. **Run-fail**: `cd server && go test ./internal/checkpoint/ -run TestService`

3. **Minimal impl**:

   ```go
   // service.go
   package checkpoint

   import (
       "context"
       "fmt"
       "log/slog"
       "sync"

       "github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
       "github.com/lx-wnk/agent-dashboard/server/internal/sse"
   )

   const defaultMaxPerTask = 50

   // BroadcastFn sends a checkpoint_added SSE event to all connected clients.
   type BroadcastFn func(taskID string, payload any)

   // KillFn kills the live agent for taskID (see KillRunningStage). May be nil.
   type KillFn func(ctx context.Context, taskID string) error

   // ParkFn parks a task as awaiting_user with the given reason. May be nil in tests.
   type ParkFn func(ctx context.Context, taskID, reason string) error

   // ServiceOptions configures a Service.
   type ServiceOptions struct {
       Repo        repo.CheckpointRepo
       MaxPerTask  int
       Broadcaster *sse.TaskBroadcaster // nil → no SSE
       KillFn      KillFn
       ParkFn      ParkFn
   }

   // Service orchestrates snapshot+DB+SSE and the revert flow.
   type Service struct {
       opts      ServiceOptions
       stateMu   sync.Map // per-taskID *taskState
       revertMu  sync.Map // per-taskID *sync.Mutex for revert serialization
   }

   type taskState struct {
       mu       sync.Mutex
       lastTree string // last snapshotted tree SHA (for identical-tree skip)
       lastSeq  int
       lastCommit string
   }

   // NewService creates a Service.
   func NewService(opts ServiceOptions) *Service {
       if opts.MaxPerTask <= 0 {
           opts.MaxPerTask = defaultMaxPerTask
       }
       return &Service{opts: opts}
   }

   func (s *Service) getState(taskID string) *taskState {
       v, _ := s.stateMu.LoadOrStore(taskID, &taskState{})
       return v.(*taskState)
   }

   func (s *Service) getRevertMu(taskID string) *sync.Mutex {
       v, _ := s.revertMu.LoadOrStore(taskID, &sync.Mutex{})
       return v.(*sync.Mutex)
   }

   // TakeSnapshot captures the current worktree state and persists a checkpoint row.
   // Best-effort: any error is logged as Warn and the task continues unaffected.
   func (s *Service) TakeSnapshot(ctx context.Context, taskID, worktreePath string) error {
       st := s.getState(taskID)
       st.mu.Lock()
       defer st.mu.Unlock()

       nextSeq := st.lastSeq + 1
       res, err := SnapshotWithParent(ctx, worktreePath, taskID, nextSeq, st.lastTree, st.lastCommit)
       if err != nil {
           slog.Warn("checkpoint: snapshot failed", "taskID", taskID, "err", err)
           return nil // best-effort
       }
       if res.Skipped {
           return nil
       }

       cp, err := s.opts.Repo.Create(ctx, repo.CreateCheckpointInput{
           TaskID:       taskID,
           Seq:          nextSeq,
           CommitSHA:    res.CommitSHA,
           TreeSHA:      res.TreeSHA,
           FilesChanged: res.FilesChanged,
       })
       if err != nil {
           slog.Warn("checkpoint: persist row failed", "taskID", taskID, "err", err)
           return nil
       }

       st.lastTree = res.TreeSHA
       st.lastSeq = nextSeq
       st.lastCommit = res.CommitSHA

       // Prune when over limit.
       if n, _ := s.opts.Repo.CountByTask(ctx, taskID); n > s.opts.MaxPerTask {
           if err := s.opts.Repo.PruneOldest(ctx, taskID, s.opts.MaxPerTask); err != nil {
               slog.Warn("checkpoint: prune failed", "taskID", taskID, "err", err)
           }
       }

       if s.opts.Broadcaster != nil {
           s.opts.Broadcaster.Broadcast(sse.TaskEvent{
               Type:    "checkpoint_added",
               TaskID:  taskID,
               Payload: toView(cp),
           })
       }
       return nil
   }

   // Revert reverts the task's worktree to the given checkpoint and parks the task.
   func (s *Service) Revert(ctx context.Context, taskID, checkpointID, worktreePath string) error {
       mu := s.getRevertMu(taskID)
       mu.Lock()
       defer mu.Unlock()

       cp, err := s.opts.Repo.GetByID(ctx, checkpointID)
       if err != nil || cp == nil {
           return fmt.Errorf("revert: checkpoint %s not found", checkpointID)
       }

       // Kill live agent before touching the worktree.
       if s.opts.KillFn != nil {
           if err := s.opts.KillFn(ctx, taskID); err != nil {
               return fmt.Errorf("revert: kill agent: %w", err)
           }
       }

       // Take a pre-revert snapshot (so revert is itself undoable).
       st := s.getState(taskID)
       st.mu.Lock()
       preSeq := st.lastSeq + 1
       preRes, preErr := SnapshotWithParent(ctx, worktreePath, taskID, preSeq, st.lastTree, st.lastCommit)
       if preErr == nil && !preRes.Skipped {
           preRevertTrue := true
           if preCp, pErr := s.opts.Repo.Create(ctx, repo.CreateCheckpointInput{
               TaskID:       taskID,
               Seq:          preSeq,
               CommitSHA:    preRes.CommitSHA,
               TreeSHA:      preRes.TreeSHA,
               FilesChanged: preRes.FilesChanged,
               PreRevert:    preRevertTrue,
           }); pErr == nil {
               st.lastSeq = preSeq
               st.lastTree = preRes.TreeSHA
               st.lastCommit = preRes.CommitSHA
               if s.opts.Broadcaster != nil {
                   s.opts.Broadcaster.Broadcast(sse.TaskEvent{
                       Type:    "checkpoint_added",
                       TaskID:  taskID,
                       Payload: toView(preCp),
                   })
               }
           }
       }
       st.mu.Unlock()

       // Restore the worktree to the target checkpoint's tree.
       if err := Restore(ctx, worktreePath, worktreePath, cp.TreeSHA); err != nil {
           return fmt.Errorf("revert: restore: %w", err)
       }

       // Park the task.
       if s.opts.ParkFn != nil {
           reason := fmt.Sprintf("reverted to checkpoint #%d", cp.Seq)
           if err := s.opts.ParkFn(ctx, taskID, reason); err != nil {
               slog.Warn("revert: park task failed", "taskID", taskID, "err", err)
           }
       }
       return nil
   }

   // PruneRefs deletes all refs/checkpoints/<taskID>/* from the repo at repoDir.
   func (s *Service) PruneRefs(ctx context.Context, taskID, repoDir string) error {
       return DeleteCheckpointRefs(ctx, repoDir, taskID)
   }

   // CheckpointView is the JSON shape returned by the API.
   type CheckpointView struct {
       ID           string `json:"id"`
       TaskID       string `json:"taskId"`
       Seq          int    `json:"seq"`
       CommitSHA    string `json:"commitSha"`
       FilesChanged int    `json:"filesChanged"`
       PreRevert    bool   `json:"preRevert"`
       CreatedAt    string `json:"createdAt"`
   }

   func toView(cp *ent.Checkpoint) CheckpointView {
       return CheckpointView{
           ID:           cp.ID,
           TaskID:       cp.TaskID,
           Seq:          cp.Seq,
           CommitSHA:    cp.CommitSHA,
           FilesChanged: cp.FilesChanged,
           PreRevert:    cp.PreRevert,
           CreatedAt:    cp.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
       }
   }
   ```

   Also update `di_pipeline.go` in this commit to fully wire `ParkFn` and `KillFn`:

   ```go
   // In provideOrchestrator, after orch is constructed:
   cpSvc := checkpoint.NewService(checkpoint.ServiceOptions{
       Repo:        cpRepo,
       MaxPerTask:  50,
       Broadcaster: tb,
       KillFn: func(ctx context.Context, taskID string) error {
           return orch.KillRunningStage(ctx, taskID)
       },
       ParkFn: func(ctx context.Context, taskID, reason string) error {
           task, err := taskRepo.GetByID(ctx, taskID)
           if err != nil {
               return err
           }
           run, err := srRepo.GetLatestByTaskAndStage(ctx, taskID, task.CurrentStage)
           if err != nil || run == nil {
               return err
           }
           _, err = srRepo.Update(ctx, run.ID, repo.UpdateStageRunInput{
               Status: strPtr("awaiting_user"),
               Output: map[string]any{"checkpoint_revert_reason": reason},
           })
           if err == nil {
               tb.Broadcast(sse.TaskEvent{Type: "task_updated", TaskID: taskID})
           }
           return err
       },
   })
   // Store cpSvc on a shared holder so the API handler can use it (via di_tasks.go injection).
   ```

4. **Run-pass**: `cd server && go test ./internal/checkpoint/ -run TestService -count=1 -v`

5. **Commit**:
   ```bash
   git add server/internal/checkpoint/service.go \
           server/internal/checkpoint/service_test.go \
           server/cmd/serve/di_pipeline.go
   git commit --no-gpg-sign -m "feat(checkpoint): Service (TakeSnapshot + Revert) with SSE + DI wiring"
   ```

---

## Task 7 — API endpoints (list + revert)

### Files
- `server/internal/api/tasks/checkpoint_routes.go` (new)
- `server/internal/api/tasks/checkpoint_routes_test.go` (new)
- `server/internal/api/tasks/handler.go` — add `checkpointSvc` field + routes in `Mount`
- `server/internal/api/tasks/handler_test.go` — extend Deps setup

### Steps

1. **Failing test**:

   ```go
   // checkpoint_routes_test.go
   package tasks_test

   import (
       "encoding/json"
       "net/http"
       "net/http/httptest"
       "strings"
       "testing"
       "github.com/go-chi/chi/v5"
       "github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"
       "github.com/lx-wnk/agent-dashboard/server/internal/checkpoint"
   )

   // fakeCheckpointSvc implements tasks.CheckpointServiceIface.
   type fakeCheckpointSvc struct {
       listResult []checkpoint.CheckpointView
       revertErr  error
   }
   func (f *fakeCheckpointSvc) List(ctx context.Context, taskID string) ([]checkpoint.CheckpointView, error) {
       return f.listResult, nil
   }
   func (f *fakeCheckpointSvc) Revert(ctx context.Context, taskID, cpID, worktreePath string) error {
       return f.revertErr
   }

   func TestListCheckpoints(t *testing.T) {
       svc := &fakeCheckpointSvc{listResult: []checkpoint.CheckpointView{{ID: "cp1", Seq: 1}}}
       r := chi.NewRouter()
       tasks.MountCheckpointRoutes(r, svc, nil) // nil orchestrator stub
       w := httptest.NewRecorder()
       req := httptest.NewRequest("GET", "/api/tasks/task-1/checkpoints", nil)
       r.ServeHTTP(w, req)
       if w.Code != 200 {
           t.Fatalf("expected 200, got %d: %s", w.Code, w.Body)
       }
       var got []checkpoint.CheckpointView
       _ = json.NewDecoder(w.Body).Decode(&got)
       if len(got) != 1 || got[0].ID != "cp1" {
           t.Fatalf("unexpected response: %v", got)
       }
   }

   func TestRevertCheckpoint_ReturnsTask(t *testing.T) {
       svc := &fakeCheckpointSvc{}
       r := chi.NewRouter()
       tasks.MountCheckpointRoutes(r, svc, fakeTaskRepo{})
       w := httptest.NewRecorder()
       req := httptest.NewRequest("POST", "/api/tasks/task-1/checkpoints/cp1/revert",
           strings.NewReader("{}"))
       req.Header.Set("Content-Type", "application/json")
       req.Header.Set("Origin", "http://localhost:13120")
       r.ServeHTTP(w, req)
       if w.Code != 200 {
           t.Fatalf("expected 200, got %d: %s", w.Code, w.Body)
       }
   }
   ```

2. **Run-fail**: `cd server && go test ./internal/api/tasks/ -run TestListCheckpoints`

3. **Minimal impl**:

   ```go
   // checkpoint_routes.go
   package tasks

   import (
       "encoding/json"
       "net/http"

       "github.com/go-chi/chi/v5"
       "github.com/lx-wnk/agent-dashboard/server/internal/apierr"
       "github.com/lx-wnk/agent-dashboard/server/internal/checkpoint"
   )

   // CheckpointServiceIface is the narrow surface consumed by the checkpoint routes.
   type CheckpointServiceIface interface {
       List(ctx context.Context, taskID string) ([]checkpoint.CheckpointView, error)
       Revert(ctx context.Context, taskID, checkpointID, worktreePath string) error
   }

   // MountCheckpointRoutes registers checkpoint endpoints on r.
   // Called from Handler.Mount so they share the JWT middleware group.
   func MountCheckpointRoutes(r chi.Router, svc CheckpointServiceIface, taskRepo repo.TaskRepo) {
       r.Get("/api/tasks/{id}/checkpoints", apierr.ErrorMiddleware(listCheckpoints(svc)))
       r.Post("/api/tasks/{id}/checkpoints/{cpId}/revert",
           apierr.ErrorMiddleware(revertCheckpoint(svc, taskRepo)))
   }

   func listCheckpoints(svc CheckpointServiceIface) http.HandlerFunc {
       return func(w http.ResponseWriter, r *http.Request) error {
           taskID := chi.URLParam(r, "id")
           views, err := svc.List(r.Context(), taskID)
           if err != nil {
               return err
           }
           if views == nil {
               views = []checkpoint.CheckpointView{}
           }
           return jsonReply(w, http.StatusOK, views)
       }
   }

   func revertCheckpoint(svc CheckpointServiceIface, taskRepo repo.TaskRepo) http.HandlerFunc {
       return func(w http.ResponseWriter, r *http.Request) error {
           taskID := chi.URLParam(r, "id")
           cpID := chi.URLParam(r, "cpId")

           task, err := taskRepo.GetByID(r.Context(), taskID)
           if err != nil || task == nil {
               return apierr.NotFound("task not found")
           }
           if task.WorktreePath == nil || *task.WorktreePath == "" {
               return apierr.Conflict("task has no active worktree")
           }

           if err := svc.Revert(r.Context(), taskID, cpID, *task.WorktreePath); err != nil {
               return err
           }
           return jsonReply(w, http.StatusOK, map[string]string{"status": "reverted"})
       }
   }
   ```

   In `Handler`:
   - Add field `checkpointSvc CheckpointServiceIface`
   - In `Deps` add `CheckpointSvc CheckpointServiceIface`
   - In `NewHandler` assign it
   - In `Mount` call `MountCheckpointRoutes(r, h.checkpointSvc, h.taskRepo)` when non-nil

   Also add `List` wrapper to `Service` in `service.go`:
   ```go
   func (s *Service) List(ctx context.Context, taskID string) ([]CheckpointView, error) {
       rows, err := s.opts.Repo.ListByTask(ctx, taskID)
       if err != nil {
           return nil, err
       }
       views := make([]CheckpointView, len(rows))
       for i, r := range rows {
           views[i] = toView(r)
       }
       return views, nil
   }
   ```

4. **Run-pass**: `cd server && go test ./internal/api/tasks/ -run TestListCheckpoints -run TestRevertCheckpoint -count=1`

5. **Commit**:
   ```bash
   git add server/internal/api/tasks/checkpoint_routes.go \
           server/internal/api/tasks/checkpoint_routes_test.go \
           server/internal/api/tasks/handler.go \
           server/internal/checkpoint/service.go
   git commit --no-gpg-sign -m "feat(checkpoint): REST endpoints GET+POST revert + CheckpointServiceIface"
   ```

---

## Task 8 — Frontend: useCheckpoints + CheckpointTimeline + task modal tab

### Files
- `src/composables/useCheckpoints.ts` (new)
- `src/components/task/CheckpointTimeline.vue` (new)
- `src/components/task/CheckpointTimeline.test.ts` (new — Vitest)
- `src/composables/useTasks.ts` — add `'checkpoint_added'` to `TaskEvent.type` union
- `src/components/TaskModal.vue` — add `'checkpoints'` tab

### Steps

1. **Failing test** (Vitest):

   ```typescript
   // CheckpointTimeline.test.ts
   import { describe, it, expect, vi } from 'vitest'
   import { mount } from '@vue/test-utils'
   import CheckpointTimeline from './CheckpointTimeline.vue'

   const sampleCheckpoints = [
     { id: 'cp2', seq: 2, filesChanged: 3, preRevert: false, createdAt: '2026-06-30T10:01:00Z' },
     { id: 'cp1', seq: 1, filesChanged: 1, preRevert: false, createdAt: '2026-06-30T10:00:00Z' },
   ]

   describe('CheckpointTimeline', () => {
     it('renders checkpoint rows', () => {
       const wrapper = mount(CheckpointTimeline, {
         props: { taskId: 'task-1', checkpoints: sampleCheckpoints, loading: false },
       })
       expect(wrapper.findAll('[data-testid="checkpoint-row"]')).toHaveLength(2)
       expect(wrapper.text()).toContain('#2')
     })

     it('shows empty state when no checkpoints', () => {
       const wrapper = mount(CheckpointTimeline, {
         props: { taskId: 'task-1', checkpoints: [], loading: false },
       })
       expect(wrapper.text()).toContain('No checkpoints')
     })

     it('emits revert on button click after confirm', async () => {
       window.confirm = vi.fn(() => true)
       const wrapper = mount(CheckpointTimeline, {
         props: { taskId: 'task-1', checkpoints: sampleCheckpoints, loading: false },
       })
       await wrapper.find('[data-testid="revert-btn-cp2"]').trigger('click')
       expect(wrapper.emitted('revert')).toEqual([['cp2']])
     })
   })
   ```

2. **Run-fail**: `pnpm test --run src/components/task/CheckpointTimeline.test.ts`

3. **Minimal impl**:

   ```typescript
   // useCheckpoints.ts
   import type { Ref } from 'vue'
   import { onUnmounted, ref, watch } from 'vue'
   import { errorMessage } from '../utils/errorMessage'
   import { runAction } from './useRunAction'

   export interface Checkpoint {
     id: string
     taskId: string
     seq: number
     commitSha: string
     filesChanged: number
     preRevert: boolean
     createdAt: string
   }

   export function useCheckpoints(taskId: Ref<string | null>) {
     const checkpoints = ref<Checkpoint[]>([])
     const loading = ref(false)
     const error = ref<string | null>(null)

     async function load(id: string) {
       loading.value = true
       error.value = null
       try {
         const res = await fetch(`/api/tasks/${id}/checkpoints`)
         if (!res.ok)
           throw new Error(`HTTP ${res.status}`)
         checkpoints.value = await res.json() as Checkpoint[]
       }
       catch (err) {
         error.value = errorMessage(err)
       }
       finally {
         loading.value = false
       }
     }

     // Append a new checkpoint from the SSE feed.
     function handleCheckpointAdded(payload: Checkpoint) {
       if (payload.taskId !== taskId.value)
         return
       checkpoints.value = [payload, ...checkpoints.value]
     }

     watch(taskId, (id) => {
       checkpoints.value = []
       if (id)
         void load(id)
     }, { immediate: true })

     async function revert(cpId: string): Promise<void> {
       if (!taskId.value)
         return
       await runAction(async () => {
         const res = await fetch(`/api/tasks/${taskId.value}/checkpoints/${cpId}/revert`, {
           method: 'POST',
           headers: { 'Content-Type': 'application/json', Origin: window.location.origin },
         })
         if (!res.ok) {
           const body = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
           throw new Error(body.error || 'Revert failed')
         }
         // Reload after revert so the pre-revert checkpoint appears.
         if (taskId.value)
           await load(taskId.value)
       })
     }

     return { checkpoints, loading, error, revert, handleCheckpointAdded }
   }
   ```

   ```vue
   <!-- CheckpointTimeline.vue -->
   <script setup lang="ts">
   import type { Checkpoint } from '../../composables/useCheckpoints'
   import { formatDistanceToNow } from 'date-fns'

   defineProps<{
     taskId: string
     checkpoints: Checkpoint[]
     loading: boolean
   }>()
   const emit = defineEmits<{ revert: [cpId: string] }>()

   function confirmRevert(cpId: string) {
     if (window.confirm('Revert the worktree to this checkpoint? The current state will be saved as a pre-revert checkpoint.'))
       emit('revert', cpId)
   }

   function relativeTime(iso: string) {
     return formatDistanceToNow(new Date(iso), { addSuffix: true })
   }
   </script>

   <template>
     <section class="p-5 space-y-4">
       <div v-if="loading" class="text-sm text-fg-mute">
         Loading...
       </div>
       <div v-else-if="checkpoints.length === 0" class="text-sm text-fg-mute">
         No checkpoints yet — checkpoints are captured automatically while the agent edits the worktree.
       </div>
       <ul v-else class="space-y-2">
         <li
           v-for="cp in checkpoints"
           :key="cp.id"
           data-testid="checkpoint-row"
           class="flex items-center justify-between rounded border border-line p-3 text-sm"
         >
           <div class="space-y-0.5">
             <span class="font-mono font-semibold text-fg">
               #{{ cp.seq }}
               <span v-if="cp.preRevert" class="ml-1 text-xs text-warn-text">(pre-revert)</span>
             </span>
             <div class="text-xs text-fg-mute">
               {{ cp.filesChanged }} file{{ cp.filesChanged !== 1 ? 's' : '' }} · {{ relativeTime(cp.createdAt) }}
             </div>
           </div>
           <button
             :data-testid="`revert-btn-${cp.id}`"
             class="rounded px-2 py-1 text-xs font-medium bg-danger-subtle text-danger hover:bg-danger-muted"
             @click="confirmRevert(cp.id)"
           >
             Revert
           </button>
         </li>
       </ul>
     </section>
   </template>
   ```

   Wire into `TaskModal.vue`:
   - Add `'checkpoints'` to `TABS` and `TAB_LABELS`:
     ```typescript
     const TABS = ['overview', 'stages', 'cost', 'permissions', 'dependencies', 'audit', 'coordination', 'checkpoints'] as const
     TAB_LABELS.checkpoints = 'Checkpoints'
     ```
   - Import `useCheckpoints` and `CheckpointTimeline`.
   - Call `const { checkpoints, loading: cpLoading, revert, handleCheckpointAdded } = useCheckpoints(computed(() => task.value?.id ?? null))`
   - In `applyEvent` in `useTasks.ts`, add to the switch:
     ```typescript
     // useTasks.ts TaskEvent union — add 'checkpoint_added'
     type: 'task_created' | 'task_updated' | 'task_deleted' | 'stage_run_updated' | 'permission_request' | 'checkpoint_added'
     ```
   - In `handleSseMessage` routing, pass `checkpoint_added` events to subscribers via a simple
     `onCheckpointAdded` callback registered on a module-level set (same pattern as the existing SSE resource).
     Simpler alternative: `useCheckpoints` opens its own SSE EventSource if the global stream has already
     started — but the cleaner approach is to call `handleCheckpointAdded` from the `applyEvent` dispatcher:
     ```typescript
     case 'checkpoint_added': {
       // Forward to any mounted useCheckpoints instance via a module-level emitter.
       checkpointListeners.forEach(fn => fn(event.payload as Checkpoint))
       break
     }
     ```
     Where `checkpointListeners` is a `Set<(cp: Checkpoint) => void>` maintained by `useCheckpoints.ts`.

   Add the checkpoint panel to `TaskModal.vue` template:
   ```vue
   <div v-show="activeTab === 'checkpoints'" v-bind="panelAttrs('checkpoints')">
     <CheckpointTimeline
       :task-id="task.id"
       :checkpoints="checkpoints"
       :loading="cpLoading"
       @revert="revert"
     />
   </div>
   ```

4. **Run-pass**: `pnpm test --run src/components/task/CheckpointTimeline.test.ts`

5. **Commit**:
   ```bash
   git add src/composables/useCheckpoints.ts \
           src/components/task/CheckpointTimeline.vue \
           src/components/task/CheckpointTimeline.test.ts \
           src/composables/useTasks.ts \
           src/components/TaskModal.vue
   git commit --no-gpg-sign -m "feat(checkpoint): useCheckpoints composable + CheckpointTimeline tab"
   ```

---

## Task 9 — Docs and CHANGELOG

### Files
- `CHANGELOG.md` — add entry under `[Unreleased]`
- `README.md` — add checkpoint/revert mention in feature list

### Steps

1. Add to `CHANGELOG.md` `[Unreleased]` section:

   ```markdown
   ### Added
   - Per-turn checkpoint / revert for pipeline-task worktrees (B2). A debounced
     filesystem watcher captures each agent turn as a hidden git ref. The task
     modal's new **Checkpoints** tab lists snapshots with a Revert button; reverts
     kill the live agent, save the current state as a recoverable pre-revert
     checkpoint, restore the worktree, and park the task for manual resume.
   ```

2. Verify with `cd server && go build ./... && pnpm typecheck && pnpm lint`.

3. **Commit**:
   ```bash
   git add CHANGELOG.md README.md
   git commit --no-gpg-sign -m "docs: add checkpoint/revert to CHANGELOG and README"
   ```

---

## Final Verify

Run in this order — do not skip any step:

```bash
# 1. Go: build + scoped tests
cd server
go build ./...
go test ./internal/checkpoint/ -count=1 -v
go test ./internal/db/repo/ -run TestCheckpoint -count=1
go test ./internal/pipeline/ -run TestOrchestratorCallsCheckpointer -count=1
go test ./internal/api/tasks/ -run TestListCheckpoints -run TestRevertCheckpoint -count=1

# 2. Full Go test pass (MUST restore ent after — go test regenerates the ent tree)
go test ./... 2>&1 | tee /tmp/go-test-full.out
git checkout -- internal/db/ent/   # restore generated files
grep -E "^(FAIL|ok)" /tmp/go-test-full.out

# 3. Frontend
cd ..
pnpm test
pnpm typecheck
pnpm lint
```

Gopls LSP errors in worktrees are FALSE POSITIVES — trust `go build`, not diagnostics.
CI will gate on lint + typecheck + test; all must be green before the PR is opened.
