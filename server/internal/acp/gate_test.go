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
