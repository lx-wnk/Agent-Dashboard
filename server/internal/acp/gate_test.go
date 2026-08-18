package acp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func gateFor(t *testing.T, status func(context.Context, string) (RequestStatus, error)) *PollingGate {
	t.Helper()
	return &PollingGate{
		File:     func(context.Context, PermissionRequest) (string, error) { return "req-1", nil },
		Status:   status,
		Interval: time.Millisecond,
		Timeout:  50 * time.Millisecond,
	}
}

func TestPollingGateAllowsOnceGranted(t *testing.T) {
	var calls atomic.Int32
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) {
		if calls.Add(1) < 3 {
			return StatusPending, nil
		}
		return StatusGranted, nil
	})

	d, err := g.Decide(context.Background(), PermissionRequest{SessionID: "s", ToolCallID: "t"})
	require.NoError(t, err)
	require.Equal(t, DecisionAllow, d)
}

func TestPollingGateDeniesWhenDenied(t *testing.T) {
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) { return StatusDenied, nil })

	d, err := g.Decide(context.Background(), PermissionRequest{})
	require.NoError(t, err)
	require.Equal(t, DecisionDeny, d)
}

func TestPollingGateDeniesOnTimeout(t *testing.T) {
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) { return StatusPending, nil })

	d, err := g.Decide(context.Background(), PermissionRequest{})
	require.Error(t, err, "a timeout is an error, and the decision is still deny")
	require.Equal(t, DecisionDeny, d)
}

func TestPollingGateDeniesOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) { return StatusPending, nil })

	d, err := g.Decide(ctx, PermissionRequest{})
	require.Error(t, err)
	require.Equal(t, DecisionDeny, d)
}

func TestPollingGateDeniesWhenFilingFails(t *testing.T) {
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) { return StatusGranted, nil })
	g.File = func(context.Context, PermissionRequest) (string, error) { return "", errors.New("db down") }

	d, err := g.Decide(context.Background(), PermissionRequest{})
	require.Error(t, err)
	require.Equal(t, DecisionDeny, d)
}

func TestPollingGateDeniesWhenStatusKeepsErroring(t *testing.T) {
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) { return StatusPending, errors.New("read failed") })

	d, err := g.Decide(context.Background(), PermissionRequest{})
	require.Error(t, err)
	require.Equal(t, DecisionDeny, d)
}

func TestPollingGateSurvivesATransientStatusError(t *testing.T) {
	var calls atomic.Int32
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) {
		if calls.Add(1) == 1 {
			return StatusPending, errors.New("transient")
		}
		return StatusGranted, nil
	})

	d, err := g.Decide(context.Background(), PermissionRequest{})
	require.NoError(t, err)
	require.Equal(t, DecisionAllow, d)
}

func TestPollingGateTimeoutReportsTimeoutNotStaleTransientError(t *testing.T) {
	var calls atomic.Int32
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) {
		if calls.Add(1) == 1 {
			return StatusPending, errors.New("transient")
		}
		return StatusPending, nil
	})

	d, err := g.Decide(context.Background(), PermissionRequest{})
	require.Error(t, err)
	require.Equal(t, DecisionDeny, d)
	require.NotContains(t, err.Error(), "transient")
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestPollingGateWithoutFileDenies(t *testing.T) {
	g := &PollingGate{}
	d, err := g.Decide(context.Background(), PermissionRequest{})
	require.Error(t, err)
	require.Equal(t, DecisionDeny, d)
}

func TestPollingGateWithdrawsOnTimeout(t *testing.T) {
	var calls atomic.Int32
	var gotID atomic.Value
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) { return StatusPending, nil })
	g.Withdraw = func(ctx context.Context, id string) error {
		calls.Add(1)
		gotID.Store(id)
		return nil
	}

	d, err := g.Decide(context.Background(), PermissionRequest{})
	require.Error(t, err)
	require.Equal(t, DecisionDeny, d)
	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, "req-1", gotID.Load())
}

func TestPollingGateWithdrawsOnCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	var gotID atomic.Value
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) { return StatusPending, nil })
	g.Withdraw = func(ctx context.Context, id string) error {
		calls.Add(1)
		gotID.Store(id)
		return nil
	}

	d, err := g.Decide(ctx, PermissionRequest{})
	require.Error(t, err)
	require.Equal(t, DecisionDeny, d)
	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, "req-1", gotID.Load())
}

func TestPollingGateDoesNotWithdrawWhenGranted(t *testing.T) {
	var calls atomic.Int32
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) { return StatusGranted, nil })
	g.Withdraw = func(context.Context, string) error { calls.Add(1); return nil }

	d, err := g.Decide(context.Background(), PermissionRequest{})
	require.NoError(t, err)
	require.Equal(t, DecisionAllow, d)
	require.Equal(t, int32(0), calls.Load())
}

func TestPollingGateDoesNotWithdrawWhenDenied(t *testing.T) {
	var calls atomic.Int32
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) { return StatusDenied, nil })
	g.Withdraw = func(context.Context, string) error { calls.Add(1); return nil }

	d, err := g.Decide(context.Background(), PermissionRequest{})
	require.NoError(t, err)
	require.Equal(t, DecisionDeny, d)
	require.Equal(t, int32(0), calls.Load())
}

func TestPollingGateDoesNotWithdrawWhenFilingFails(t *testing.T) {
	var calls atomic.Int32
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) { return StatusGranted, nil })
	g.File = func(context.Context, PermissionRequest) (string, error) { return "", errors.New("db down") }
	g.Withdraw = func(context.Context, string) error { calls.Add(1); return nil }

	d, err := g.Decide(context.Background(), PermissionRequest{})
	require.Error(t, err)
	require.Equal(t, DecisionDeny, d)
	require.Equal(t, int32(0), calls.Load())
}

func TestPollingGateDoesNotWithdrawWhenUnwired(t *testing.T) {
	var calls atomic.Int32
	g := &PollingGate{Withdraw: func(context.Context, string) error { calls.Add(1); return nil }}

	d, err := g.Decide(context.Background(), PermissionRequest{})
	require.Error(t, err)
	require.Equal(t, DecisionDeny, d)
	require.Equal(t, int32(0), calls.Load())
}

func TestPollingGateWithdrawFailureDoesNotChangeOutcome(t *testing.T) {
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) { return StatusPending, nil })
	g.Withdraw = func(context.Context, string) error { return errors.New("withdraw failed") }

	d, err := g.Decide(context.Background(), PermissionRequest{})
	require.Error(t, err)
	require.Equal(t, DecisionDeny, d)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NotContains(t, err.Error(), "withdraw failed")
}

func TestPollingGateWithdrawContextIsNotAlreadyCancelled(t *testing.T) {
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) { return StatusPending, nil })
	g.Withdraw = func(ctx context.Context, id string) error {
		require.NoError(t, ctx.Err(), "withdraw must not receive the already-cancelled ctx")
		return nil
	}

	_, _ = g.Decide(context.Background(), PermissionRequest{})
}

func TestPollingGateNonPositiveIntervalFallsBackToDefault(t *testing.T) {
	// time.NewTicker panics on a non-positive duration. Client.ask contains that
	// panic into a transient deny (client.go:145-152), so the guard is what makes
	// the default interval actually apply rather than what prevents a crash.
	for _, interval := range []time.Duration{0, -time.Second} {
		g := &PollingGate{
			File:     func(context.Context, PermissionRequest) (string, error) { return "req-1", nil },
			Status:   func(context.Context, string) (RequestStatus, error) { return StatusGranted, nil },
			Interval: interval,
			Timeout:  50 * time.Millisecond,
		}

		d, err := g.Decide(context.Background(), PermissionRequest{})
		require.NoError(t, err, "interval %s", interval)
		require.Equal(t, DecisionAllow, d, "interval %s", interval)
	}
}

func TestPollingGateNonPositiveTimeoutFallsBackToDefault(t *testing.T) {
	// context.WithTimeout(ctx, 0) is already expired, so an unguarded non-positive
	// Timeout denies on the first poll instead of waiting for an answer. The caller
	// context is bounded so a regression that stops observing StatusGranted fails
	// here rather than running into the default timeout of 30 minutes.
	for _, timeout := range []time.Duration{0, -time.Second} {
		var calls atomic.Int32
		g := &PollingGate{
			File: func(context.Context, PermissionRequest) (string, error) { return "req-1", nil },
			Status: func(context.Context, string) (RequestStatus, error) {
				if calls.Add(1) < 2 {
					return StatusPending, nil
				}
				return StatusGranted, nil
			},
			Interval: time.Millisecond,
			Timeout:  timeout,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		d, err := g.Decide(ctx, PermissionRequest{})
		cancel()

		require.NoError(t, err, "timeout %s", timeout)
		require.Equal(t, DecisionAllow, d, "timeout %s", timeout)
		require.Equal(t, int32(2), calls.Load(), "gate must poll exactly until granted, timeout %s", timeout)
	}
}
