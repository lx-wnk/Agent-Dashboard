package channel

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// lockedBuf is a concurrency-safe sink so the test itself cannot be the source
// of a race.
type lockedBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A multi-part injection is indivisible: another writer active during the gap
// between the text and its submitting CR must not land between them, or its
// bytes get submitted as part of the injected prompt.
func TestPtyWriter_MultiPartWriteIsNotInterleaved(t *testing.T) {
	sink := &lockedBuf{}
	w := newPtyWriter(sink)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := w.WriteParts(60*time.Millisecond, []byte("prompt"), []byte("\r")); err != nil {
			t.Errorf("WriteParts: %v", err)
		}
	}()

	// Land squarely inside the gap.
	time.Sleep(20 * time.Millisecond)
	if _, err := w.Write([]byte("XYZ")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	wg.Wait()

	got := sink.String()
	if !strings.Contains(got, "prompt\r") {
		t.Fatalf("injection was split by the concurrent write: %q", got)
	}
	if got != "prompt\rXYZ" {
		t.Fatalf("got %q, want the concurrent write serialized after the injection", got)
	}
}

// Write must keep satisfying io.Writer for the WebSocket pump and the stdin copy.
func TestPtyWriter_SatisfiesIoWriter(t *testing.T) {
	sink := &lockedBuf{}
	var w io.Writer = newPtyWriter(sink)
	n, err := io.WriteString(w, "hello")
	if err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if n != 5 {
		t.Fatalf("n = %d, want 5", n)
	}
	if sink.String() != "hello" {
		t.Fatalf("sink = %q", sink.String())
	}
}
