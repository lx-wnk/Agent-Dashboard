package channel

import (
	"testing"
	"time"
)

func TestPtyHub_ReplayThenLive(t *testing.T) {
	h := newPtyHub(1024)
	h.Write([]byte("HISTORY")) // before any subscriber
	replay, ch, unsub := h.Subscribe()
	defer unsub()
	if string(replay) != "HISTORY" {
		t.Fatalf("replay = %q, want HISTORY", replay)
	}
	h.Write([]byte("LIVE"))
	select {
	case b := <-ch:
		if string(b) != "LIVE" {
			t.Fatalf("live = %q, want LIVE", b)
		}
	case <-time.After(time.Second):
		t.Fatal("no live frame")
	}
}
