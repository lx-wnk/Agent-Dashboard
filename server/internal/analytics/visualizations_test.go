package analytics

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildSankey_LinearFixture(t *testing.T) {
	sessID := "10000000-0000-0000-0000-000000000001"
	root := setupFakeClaudeDir(t, map[string]string{sessID: "single-session-linear.jsonl"})

	got, err := BuildSankey(context.Background(), ScanOpts{}, []string{root})
	if err != nil {
		t.Fatalf("BuildSankey: %v", err)
	}
	if got.Meta.SessionCount != 1 {
		t.Errorf("SessionCount = %d, want 1", got.Meta.SessionCount)
	}
	if got.Meta.CallCount != 3 {
		t.Errorf("CallCount = %d, want 3", got.Meta.CallCount)
	}
	// 3 unique tool names → 3 nodes.
	if len(got.Nodes) != 3 {
		t.Errorf("len(Nodes) = %d, want 3", len(got.Nodes))
	}
	// 2 consecutive transitions: Read→Edit, Edit→Bash.
	if len(got.Links) != 2 {
		t.Fatalf("len(Links) = %d, want 2", len(got.Links))
	}
	want := map[string]int{"Read|Edit": 1, "Edit|Bash": 1}
	for _, l := range got.Links {
		key := l.Source + "|" + l.Target
		if want[key] != l.Value {
			t.Errorf("link %s value = %d, want %d", key, l.Value, want[key])
		}
	}
}

func TestBuildSankey_EmptyWindow(t *testing.T) {
	root := setupFakeClaudeDir(t, map[string]string{})
	got, err := BuildSankey(context.Background(), ScanOpts{}, []string{root})
	if err != nil {
		t.Fatalf("BuildSankey: %v", err)
	}
	if len(got.Nodes) != 0 || len(got.Links) != 0 {
		t.Errorf("expected empty, got %+v", got)
	}
}

func TestBuildDAG_RequiresOneSession(t *testing.T) {
	_, err := BuildDAG(context.Background(), ScanOpts{}, nil)
	if err == nil {
		t.Fatal("expected error for missing session")
	}
}

func TestBuildDAG_LinearFixture(t *testing.T) {
	sessID := "10000000-0000-0000-0000-000000000002"
	root := setupFakeClaudeDir(t, map[string]string{sessID: "single-session-linear.jsonl"})

	got, err := BuildDAG(context.Background(), ScanOpts{Sessions: []string{sessID}}, []string{root})
	if err != nil {
		t.Fatalf("BuildDAG: %v", err)
	}
	// 3 tool nodes + 3 user nodes (one per tool_result line). The user line
	// before any tool calls has only text and does not introduce a node
	// here because BuildDAG only emits user nodes with tool_results OR a
	// generic "user" node when none — the first user line "please help"
	// has neither tool_result nor any blocks our parser keeps, so it lands
	// as a generic user node.
	wantToolCount := 3
	wantResultCount := 3
	toolNodes := 0
	resultLinks := 0
	for _, n := range got.Nodes {
		if n.Type == "tool" {
			toolNodes++
		}
	}
	for _, l := range got.Links {
		if l.Kind == "result" {
			resultLinks++
		}
	}
	if toolNodes != wantToolCount {
		t.Errorf("tool nodes = %d, want %d", toolNodes, wantToolCount)
	}
	if resultLinks != wantResultCount {
		t.Errorf("result links = %d, want %d", resultLinks, wantResultCount)
	}
}

func TestBuildCoOccurrence_TwoSessions(t *testing.T) {
	rootDir := t.TempDir()
	projectDir := filepath.Join(rootDir, "projects", "-tmp-fixture")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for sessID, fixture := range map[string]string{
		"20000000-0000-0000-0000-000000000001": "two-sessions-branching.jsonl",
		"20000000-0000-0000-0000-000000000002": "two-sessions-branching-2.jsonl",
	} {
		data, err := os.ReadFile(filepath.Join("testdata", fixture))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(projectDir, sessID+".jsonl"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := BuildCoOccurrence(context.Background(), ScanOpts{}, []string{rootDir})
	if err != nil {
		t.Fatalf("BuildCoOccurrence: %v", err)
	}
	if got.Meta.SessionCount != 2 {
		t.Errorf("SessionCount = %d, want 2", got.Meta.SessionCount)
	}
	// Tools across both sessions: Read, Edit, Bash, Grep.
	if len(got.Tools) != 4 {
		t.Fatalf("len(Tools) = %d, want 4 (got %v)", len(got.Tools), got.Tools)
	}
	// Matrix must be symmetric.
	for i := range got.Tools {
		for j := range got.Tools {
			if got.Matrix[i][j] != got.Matrix[j][i] {
				t.Errorf("matrix not symmetric at [%d][%d]=%d vs [%d][%d]=%d",
					i, j, got.Matrix[i][j], j, i, got.Matrix[j][i])
			}
		}
	}
	// Read+Bash appear together in both sessions.
	idx := make(map[string]int, len(got.Tools))
	for i, name := range got.Tools {
		idx[name] = i
	}
	if got.Matrix[idx["Read"]][idx["Bash"]] != 2 {
		t.Errorf("Read+Bash co-occurrence = %d, want 2", got.Matrix[idx["Read"]][idx["Bash"]])
	}
	// Diagonal: Read appears in 2 sessions, Edit in 1, Bash in 2, Grep in 1.
	wantDiag := map[string]int{"Read": 2, "Edit": 1, "Bash": 2, "Grep": 1}
	for name, want := range wantDiag {
		if got.Matrix[idx[name]][idx[name]] != want {
			t.Errorf("diagonal[%s] = %d, want %d", name, got.Matrix[idx[name]][idx[name]], want)
		}
	}
}

func TestBuildSpawnTree_ParentChildFromSubagentDir(t *testing.T) {
	rootDir := t.TempDir()
	projectDir := filepath.Join(rootDir, "projects", "-tmp-fixture")
	parent := "30000000-0000-0000-0000-000000000001"
	child1 := "30000000-0000-0000-0000-0000000000c1"
	child2 := "30000000-0000-0000-0000-0000000000c2"

	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	parentData, err := os.ReadFile(filepath.Join("testdata", "single-session-linear.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{parent, child1, child2} {
		if err := os.WriteFile(filepath.Join(projectDir, id+".jsonl"), parentData, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Subagent dir convention: {projectDir}/{parentID}/subagents/{childID}.jsonl
	subDir := filepath.Join(projectDir, parent, "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{child1, child2} {
		if err := os.WriteFile(filepath.Join(subDir, id+".jsonl"), parentData, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := BuildSpawnTree(context.Background(), ScanOpts{}, []string{rootDir})
	if err != nil {
		t.Fatalf("BuildSpawnTree: %v", err)
	}
	if len(got.Roots) != 1 || got.Roots[0] != parent {
		t.Errorf("Roots = %v, want [%s]", got.Roots, parent)
	}
	if len(got.Links) != 2 {
		t.Errorf("Links = %d, want 2", len(got.Links))
	}
	depthByID := map[string]int{}
	for _, n := range got.Nodes {
		depthByID[n.ID] = n.Depth
	}
	if depthByID[parent] != 0 {
		t.Errorf("parent depth = %d, want 0", depthByID[parent])
	}
	if depthByID[child1] != 1 || depthByID[child2] != 1 {
		t.Errorf("child depths = %d/%d, want 1/1", depthByID[child1], depthByID[child2])
	}
}
