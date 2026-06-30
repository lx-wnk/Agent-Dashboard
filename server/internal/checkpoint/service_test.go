package checkpoint_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/checkpoint"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// fakeCheckpointRepo is an in-memory repo.CheckpointRepo backed by a slice.
type fakeCheckpointRepo struct {
	mu   sync.Mutex
	rows []*ent.Checkpoint
	next int
}

func (f *fakeCheckpointRepo) Create(_ context.Context, in repo.CreateCheckpointInput) (*ent.Checkpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	cp := &ent.Checkpoint{
		ID:           "cp-" + itoa(f.next),
		TaskID:       in.TaskID,
		Seq:          in.Seq,
		CommitSha:    in.CommitSHA,
		TreeSha:      in.TreeSHA,
		FilesChanged: in.FilesChanged,
		PreRevert:    in.PreRevert,
	}
	f.rows = append(f.rows, cp)
	return cp, nil
}

func (f *fakeCheckpointRepo) GetByID(_ context.Context, id string) (*ent.Checkpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, nil
}

func (f *fakeCheckpointRepo) GetLatestByTask(_ context.Context, taskID string) (*ent.Checkpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var latest *ent.Checkpoint
	for _, r := range f.rows {
		if r.TaskID == taskID && (latest == nil || r.Seq > latest.Seq) {
			latest = r
		}
	}
	return latest, nil
}

func (f *fakeCheckpointRepo) ListByTask(_ context.Context, taskID string) ([]*ent.Checkpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*ent.Checkpoint
	for i := len(f.rows) - 1; i >= 0; i-- {
		if f.rows[i].TaskID == taskID {
			out = append(out, f.rows[i])
		}
	}
	return out, nil
}

func (f *fakeCheckpointRepo) CountByTask(_ context.Context, taskID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.rows {
		if r.TaskID == taskID {
			n++
		}
	}
	return n, nil
}

func (f *fakeCheckpointRepo) PruneOldest(_ context.Context, _ string, _ int) error { return nil }

func (f *fakeCheckpointRepo) DeleteByTask(_ context.Context, _ string) error { return nil }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestService_TakeSnapshot_CreatesRow(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "x.go", "package x")
	fr := &fakeCheckpointRepo{}
	svc := checkpoint.NewService(checkpoint.ServiceOptions{Repo: fr, MaxPerTask: 50})
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
	fr := &fakeCheckpointRepo{}
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
	fr := &fakeCheckpointRepo{}
	var killed, parked string
	svc := checkpoint.NewService(checkpoint.ServiceOptions{
		Repo:       fr,
		MaxPerTask: 50,
		KillFn:     func(_ context.Context, taskID string) error { killed = taskID; return nil },
		ParkFn:     func(_ context.Context, taskID, _ string) error { parked = taskID; return nil },
	})

	if err := svc.TakeSnapshot(context.Background(), "task-rv", dir); err != nil {
		t.Fatal(err)
	}
	if len(fr.rows) == 0 {
		t.Fatal("no checkpoint taken")
	}
	cpID := fr.rows[0].ID

	// Damage the worktree.
	writeFile(t, dir, "a.go", "CORRUPTED")
	writeFile(t, dir, "b.go", "package b")

	if err := svc.Revert(context.Background(), "task-rv", cpID, dir); err != nil {
		t.Fatal(err)
	}
	if killed != "task-rv" {
		t.Fatal("KillFn not called")
	}
	if parked != "task-rv" {
		t.Fatal("ParkFn not called")
	}
	if len(fr.rows) != 2 {
		t.Fatalf("expected 2 rows (original + pre-revert), got %d", len(fr.rows))
	}
	if !fr.rows[1].PreRevert {
		t.Fatal("second row must be pre_revert=true")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "a.go"))
	if string(got) != "package a" {
		t.Fatalf("a.go not restored: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.go")); err == nil {
		t.Fatal("b.go added after snapshot must be removed by revert")
	}
}
