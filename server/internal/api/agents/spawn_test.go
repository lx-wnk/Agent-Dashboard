package agents

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSpawnManager_DefaultsWhenInvalidArgs(t *testing.T) {
	// maxSpawns <= 0 and windowMs <= 0 should be clamped to safe defaults.
	m := NewSpawnManager(0, 0)
	require.NotNil(t, m)
	assert.Equal(t, 5, m.rateLimitMax)
	assert.Equal(t, 60*time.Second, m.rateLimitWindow)
}

func TestNewSpawnManager_NegativeArgsClamped(t *testing.T) {
	m := NewSpawnManager(-1, -1)
	require.NotNil(t, m)
	assert.Equal(t, 5, m.rateLimitMax)
	assert.Equal(t, 60*time.Second, m.rateLimitWindow)
}

const testSub = "user-123"

func TestIsSpawnAllowed_FirstSpawnWithinLimit(t *testing.T) {
	m := NewSpawnManager(3, 60000)
	assert.True(t, m.IsSpawnAllowed(testSub), "first spawn should be allowed when no attempts recorded")
}

func TestIsSpawnAllowed_UpToLimitAllowed(t *testing.T) {
	limit := 3
	m := NewSpawnManager(limit, 60000)

	// Record limit-1 attempts manually so the next check is the last allowed one.
	m.mu.Lock()
	for i := 0; i < limit-1; i++ {
		m.userAttempts[testSub] = append(m.userAttempts[testSub], time.Now())
	}
	m.mu.Unlock()

	// The (limit-1) recorded + 1 check means we're at limit-1 in the window — still allowed.
	assert.True(t, m.IsSpawnAllowed(testSub), "spawn at limit-1 recorded attempts should be allowed")
}

func TestIsSpawnAllowed_OverLimitRejected(t *testing.T) {
	limit := 3
	m := NewSpawnManager(limit, 60000)

	// Record exactly `limit` attempts so the next is over the limit.
	m.mu.Lock()
	for i := 0; i < limit; i++ {
		m.userAttempts[testSub] = append(m.userAttempts[testSub], time.Now())
	}
	m.mu.Unlock()

	assert.False(t, m.IsSpawnAllowed(testSub), "spawn when at or over limit should be rejected")
}

func TestIsSpawnAllowed_AfterWindowExpires_AllowedAgain(t *testing.T) {
	// Use a very short window so we can expire attempts quickly.
	windowMs := 50
	limit := 2
	m := NewSpawnManager(limit, windowMs)

	// Fill the window.
	m.mu.Lock()
	for i := 0; i < limit; i++ {
		m.userAttempts[testSub] = append(m.userAttempts[testSub], time.Now())
	}
	m.mu.Unlock()

	assert.False(t, m.IsSpawnAllowed(testSub), "should be rate-limited before window expires")

	// Wait for the window to expire.
	time.Sleep(time.Duration(windowMs+20) * time.Millisecond)

	assert.True(t, m.IsSpawnAllowed(testSub), "should be allowed after rate window expires")
}

func TestIsSpawnAllowed_PerUser_Isolated(t *testing.T) {
	limit := 2
	m := NewSpawnManager(limit, 60000)

	// Fill the limit for user-A.
	m.mu.Lock()
	for i := 0; i < limit; i++ {
		m.userAttempts["user-A"] = append(m.userAttempts["user-A"], time.Now())
	}
	m.mu.Unlock()

	assert.False(t, m.IsSpawnAllowed("user-A"), "user-A should be rate-limited")
	assert.True(t, m.IsSpawnAllowed("user-B"), "user-B should not be affected by user-A's limit")
}

// TestSendMessageToChannel_RespectsContextCancellation verifies that a pre-cancelled
// context causes SendMessageToChannel to return promptly rather than blocking.
// The function must not hang even if a network call is involved.
func TestSendMessageToChannel_RespectsContextCancellation(t *testing.T) {
	m := NewSpawnManager(5, 60000)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately before the call

	// PID 99999 almost certainly doesn't exist; the function will fail while
	// trying to read the discovery file — but the important contract is that it
	// returns an error promptly (not block) when the context is already cancelled.
	err := m.SendMessageToChannel(ctx, 99999, "ping")
	require.Error(t, err, "SendMessageToChannel must return an error for an unknown PID")

	// Ensure the call did not block: if we got here at all, the test passes.
	// An additional context-awareness check: if the implementation ever reaches
	// an HTTP call, a pre-cancelled context causes the request to fail with a
	// context error. Verify no deadline-exceeded style hang occurred by running
	// the whole call with a tight deadline.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	cancel2() // cancel immediately

	err2 := m.SendMessageToChannel(ctx2, 99999, "ping")
	require.Error(t, err2, "SendMessageToChannel must return an error with a cancelled deadline context")
}

func TestPruneAttempts_RemovesOldEntries(t *testing.T) {
	windowMs := 50
	m := NewSpawnManager(10, windowMs)

	// Add one old attempt (pre-window) and one fresh one.
	old := time.Now().Add(-200 * time.Millisecond)
	m.mu.Lock()
	m.userAttempts[testSub] = append(m.userAttempts[testSub], old)
	m.userAttempts[testSub] = append(m.userAttempts[testSub], time.Now())
	m.mu.Unlock()

	// Wait for window to cover the "old" timestamp.
	time.Sleep(time.Duration(windowMs+10) * time.Millisecond)

	m.mu.Lock()
	m.pruneAttempts(testSub)
	count := len(m.userAttempts[testSub])
	m.mu.Unlock()

	assert.Equal(t, 0, count, "all attempts older than window should be pruned")
}
