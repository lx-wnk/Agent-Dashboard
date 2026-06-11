package llmadapter

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCustomCommandSpawner_SpawnStream_LineByLine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("custom command streaming relies on POSIX scripts")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'chunk1\\nchunk2\\nchunk3\\n'\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	c := &CustomCommandSpawner{Command: script}
	ch, err := c.SpawnStream(context.Background(), LLMSpawnArgs{UserPrompt: "x"})
	if err != nil {
		t.Fatalf("SpawnStream: %v", err)
	}
	var got []string
	for s := range ch {
		got = append(got, s)
	}
	want := []string{"chunk1", "chunk2", "chunk3"}
	if len(got) != len(want) {
		t.Fatalf("chunks: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chunk[%d]: got %q want %q", i, got[i], want[i])
		}
	}
}
