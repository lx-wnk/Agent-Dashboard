package channel

import (
	"bytes"
	"testing"
)

func TestScrollback_KeepsLastNBytes(t *testing.T) {
	rb := newScrollback(8)
	rb.Write([]byte("abcdefghij")) // 10 bytes into an 8-byte buffer
	if got := rb.Snapshot(); !bytes.Equal(got, []byte("cdefghij")) {
		t.Fatalf("snapshot = %q, want %q", got, "cdefghij")
	}
}

func TestScrollback_ConcurrentWriteSnapshot(t *testing.T) {
	rb := newScrollback(1024)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			rb.Write([]byte("x"))
		}
		close(done)
	}()
	for i := 0; i < 1000; i++ {
		_ = rb.Snapshot()
	}
	<-done
}
