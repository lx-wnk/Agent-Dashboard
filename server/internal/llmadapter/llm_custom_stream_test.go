package llmadapter

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFakeSpawnerBinary(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake spawner script is POSIX sh")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-spawner.sh")
	script := `#!/bin/sh
in=$(cat)
case "$in" in
  *'"Stream":true'*) printf 'chunk-a\nchunk-b\n' ;;
  *) printf '{"PID":0,"SessionID":"fake","SessionFile":"/tmp/fake.jsonl"}' ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCustomSpawner_SetsStreamFalseOnSpawn(t *testing.T) {
	c := &CustomCommandSpawner{Command: writeFakeSpawnerBinary(t)}
	res, err := c.Spawn(context.Background(), LLMSpawnArgs{StageRunID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.SessionID != "fake" {
		t.Fatalf("Spawn must send Stream=false → result branch, got %+v", res)
	}
}

func TestCustomSpawner_SetsStreamTrueOnSpawnStream(t *testing.T) {
	c := &CustomCommandSpawner{Command: writeFakeSpawnerBinary(t)}
	ch, err := c.SpawnStream(context.Background(), LLMSpawnArgs{StageRunID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for line := range ch {
		got = append(got, line)
	}
	if len(got) != 2 || got[0] != "chunk-a" || got[1] != "chunk-b" {
		t.Fatalf("SpawnStream must send Stream=true → token-lines, got %v", got)
	}
}
