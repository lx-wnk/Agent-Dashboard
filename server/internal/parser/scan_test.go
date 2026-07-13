package parser

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanJSONLLines_EmptyInput(t *testing.T) {
	var called int
	err := ScanJSONLLines(strings.NewReader(""), func(_ []byte) error {
		called++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called != 0 {
		t.Fatalf("fn called %d times, want 0", called)
	}
}

func TestScanJSONLLines_SingleLine(t *testing.T) {
	var got []string
	err := ScanJSONLLines(strings.NewReader(`{"a":1}`), func(line []byte) error {
		got = append(got, string(line))
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != `{"a":1}` {
		t.Fatalf("got %v", got)
	}
}

func TestScanJSONLLines_SkipsBlanks(t *testing.T) {
	var got []string
	err := ScanJSONLLines(strings.NewReader("a\n\n  \nb\n"), func(line []byte) error {
		got = append(got, string(line))
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v", got)
	}
}

func TestScanJSONLLines_ErrStopScan(t *testing.T) {
	input := "a\nb\nc\nd\n"
	var calls int
	err := ScanJSONLLines(strings.NewReader(input), func(_ []byte) error {
		calls++
		if calls == 2 {
			return ErrStopScan
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("fn called %d times, want 2", calls)
	}
}

func TestScanJSONLLines_PropagatesError(t *testing.T) {
	sentinel := errors.New("my error")
	err := ScanJSONLLines(strings.NewReader("a\nb\n"), func(_ []byte) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel error, got %v", err)
	}
}

func TestScanJSONLLines_LargeLine(t *testing.T) {
	// Build a line just over 256 KB but under 4 MB.
	const size = 300 * 1024
	big := strings.Repeat("x", size)
	var receivedLen int
	err := ScanJSONLLines(strings.NewReader(big+"\n"), func(line []byte) error {
		receivedLen = len(line)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedLen != size {
		t.Fatalf("received line length %d, want %d", receivedLen, size)
	}
}

func TestScanJSONLLines_OverLongLineSkippedNotFailed(t *testing.T) {
	over := strings.Repeat("y", 5*1024*1024) // over the 4 MB per-line cap
	input := "first\n" + over + "\n" + "last\n"

	var got []string
	err := ScanJSONLLines(strings.NewReader(input), func(line []byte) error {
		got = append(got, string(line))
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "first" || got[1] != "last" {
		t.Fatalf("got %v, want [first last] (over-long line skipped, not failed)", got)
	}
}

func TestOpenJSONLReader_MaxBytesZero_SmallFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")
	content := []byte("{\"a\":1}\n{\"b\":2}\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	rc, err := OpenJSONLReader(path, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer rc.Close() //nolint:errcheck

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content mismatch: got %q, want %q", got, content)
	}
}

func TestOpenJSONLReader_MaxBytesZero_LargeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.jsonl")
	// 100 KB of content
	content := []byte(strings.Repeat("a", 100*1024))
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	rc, err := OpenJSONLReader(path, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer rc.Close() //nolint:errcheck

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if len(got) != len(content) {
		t.Fatalf("length mismatch: got %d, want %d", len(got), len(content))
	}
}

func TestOpenJSONLReader_MaxBytesLargerThanFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.jsonl")
	// N/2 bytes where maxBytes=N -> no truncation
	content := []byte("hello world\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	rc, err := OpenJSONLReader(path, int64(len(content)*2))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer rc.Close() //nolint:errcheck

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content mismatch: got %q, want %q", got, content)
	}
}

func TestOpenJSONLReader_TailsWhenFileLarger(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.jsonl")
	// Build 2N bytes: first half "X", second half (tail) "Y".
	const halfN = 64
	content := []byte(strings.Repeat("X", halfN) + strings.Repeat("Y", halfN))
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	rc, err := OpenJSONLReader(path, halfN)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer rc.Close() //nolint:errcheck

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	want := strings.Repeat("Y", halfN)
	if string(got) != want {
		t.Fatalf("tail content mismatch: got %q, want %q", got, want)
	}
}

func TestOpenJSONLReader_MissingFile(t *testing.T) {
	rc, err := OpenJSONLReader("/no/such/file.jsonl", 0)
	if err == nil {
		rc.Close() //nolint:errcheck
		t.Fatal("expected error for missing file, got nil")
	}
}
