package agents

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplyStore_StoreAndRetrieve(t *testing.T) {
	s := NewReplyStore()
	ts := time.Now().UTC().Format(time.RFC3339)

	s.Add(1234, "hello world", ts)

	replies := s.Since(1234, "")
	require.Len(t, replies, 1)
	assert.Equal(t, "hello world", replies[0].Message)
	assert.Equal(t, ts, replies[0].Timestamp)
}

func TestReplyStore_SinceEmptyReturnsAll(t *testing.T) {
	s := NewReplyStore()
	now := time.Now().UTC()

	for i := 0; i < 5; i++ {
		ts := now.Add(time.Duration(i) * time.Second).Format(time.RFC3339)
		s.Add(42, fmt.Sprintf("msg-%d", i), ts)
	}

	replies := s.Since(42, "")
	assert.Len(t, replies, 5)
}

func TestReplyStore_SinceFiltersOlderEntries(t *testing.T) {
	s := NewReplyStore()
	base := time.Now().UTC().Truncate(time.Second)

	// Add entries at t+0, t+1, t+2.
	for i := 0; i < 3; i++ {
		ts := base.Add(time.Duration(i) * time.Second).Format(time.RFC3339)
		s.Add(99, fmt.Sprintf("msg-%d", i), ts)
	}

	// Request replies since t+0 — should return only t+1 and t+2.
	sinceStr := base.Format(time.RFC3339)
	replies := s.Since(99, sinceStr)
	assert.Len(t, replies, 2)
	assert.Equal(t, "msg-1", replies[0].Message)
	assert.Equal(t, "msg-2", replies[1].Message)
}

func TestReplyStore_RingBufferWrapsOldestEvicted(t *testing.T) {
	s := NewReplyStore()
	pid := 7

	// Add maxRepliesPerPID + 5 entries; the first 5 should be evicted.
	total := maxRepliesPerPID + 5
	for i := 0; i < total; i++ {
		ts := time.Now().UTC().Format(time.RFC3339)
		s.Add(pid, fmt.Sprintf("msg-%d", i), ts)
	}

	replies := s.Since(pid, "")
	require.Len(t, replies, maxRepliesPerPID, "ring buffer should cap at maxRepliesPerPID")

	// The oldest evicted messages are msg-0 through msg-4; the first kept is msg-5.
	assert.Equal(t, "msg-5", replies[0].Message)
	assert.Equal(t, fmt.Sprintf("msg-%d", total-1), replies[len(replies)-1].Message)
}

func TestReplyStore_RingBufferExactCapacity(t *testing.T) {
	s := NewReplyStore()
	pid := 8

	for i := 0; i < maxRepliesPerPID; i++ {
		s.Add(pid, fmt.Sprintf("msg-%d", i), time.Now().UTC().Format(time.RFC3339))
	}

	replies := s.Since(pid, "")
	assert.Len(t, replies, maxRepliesPerPID, "ring buffer should hold exactly maxRepliesPerPID entries")
}

func TestReplyStore_UnknownPidReturnsEmpty(t *testing.T) {
	s := NewReplyStore()
	replies := s.Since(9999, "")
	assert.NotNil(t, replies)
	assert.Empty(t, replies)
}

func TestReplyStore_SeparateStorePerPid(t *testing.T) {
	s := NewReplyStore()
	ts := time.Now().UTC().Format(time.RFC3339)

	s.Add(1, "pid1-msg", ts)
	s.Add(2, "pid2-msg", ts)

	replies1 := s.Since(1, "")
	replies2 := s.Since(2, "")

	require.Len(t, replies1, 1)
	require.Len(t, replies2, 1)
	assert.Equal(t, "pid1-msg", replies1[0].Message)
	assert.Equal(t, "pid2-msg", replies2[0].Message)
}

func TestReplyStore_ConcurrentWritesSafe(t *testing.T) {
	s := NewReplyStore()
	pid := 55

	var wg sync.WaitGroup
	goroutines := 50
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			ts := time.Now().UTC().Format(time.RFC3339)
			s.Add(pid, fmt.Sprintf("concurrent-msg-%d", i), ts)
		}()
	}
	wg.Wait()

	replies := s.Since(pid, "")
	// Expect at most maxRepliesPerPID entries; all goroutines wrote successfully.
	assert.LessOrEqual(t, len(replies), maxRepliesPerPID)
	assert.Greater(t, len(replies), 0)
}
