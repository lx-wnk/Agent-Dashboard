package acp

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RequestStatus is the lifecycle of a filed permission request.
type RequestStatus int

const (
	StatusPending RequestStatus = iota
	StatusGranted
	StatusDenied
)

const (
	// acpGatePollInterval is how often Decide re-reads one filed permission
	// request: 500ms, paced for a human clicking approve. Unrelated to the
	// pipeline's defaultPollInterval (2s, pipeline/orchestrator.go), which
	// paces stage-run sweeps.
	acpGatePollInterval = 500 * time.Millisecond
	// acpGatePollTimeout is how long Decide blocks the ACP call waiting for
	// that request before denying: 30 minutes. Unrelated to the pipeline's
	// defaultAwaitingUserTimeout (14400s, pipeline/orchestrator.go), which
	// bounds a whole awaiting_user stage run rather than one blocked call.
	acpGatePollTimeout = 30 * time.Minute
	withdrawTimeout    = 5 * time.Second
)

// PollingGate answers an ACP permission request from the dashboard's
// asynchronous approval flow: it files the request, then blocks until someone
// resolves it. Every failure path returns DecisionDeny.
type PollingGate struct {
	File     func(ctx context.Context, req PermissionRequest) (string, error)
	Status   func(ctx context.Context, id string) (RequestStatus, error)
	Interval time.Duration
	Timeout  time.Duration

	// Withdraw releases a filed request when Decide gives up on it before it
	// was resolved (timeout or caller cancellation), so it doesn't stay
	// pending forever. Optional.
	Withdraw func(ctx context.Context, id string) error
}

// Decide satisfies Client.OnPermission.
func (g *PollingGate) Decide(ctx context.Context, req PermissionRequest) (PermissionDecision, error) {
	if g.File == nil || g.Status == nil {
		return DecisionDeny, errors.New("acp: gate is not wired, denying")
	}

	interval := g.Interval
	if interval <= 0 {
		interval = acpGatePollInterval
	}
	timeout := g.Timeout
	if timeout <= 0 {
		timeout = acpGatePollTimeout
	}

	id, err := g.File(ctx, req)
	if err != nil {
		return DecisionDeny, fmt.Errorf("acp: filing permission request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastErr error
	for {
		switch st, sErr := g.Status(ctx, id); {
		case sErr != nil:
			// A transient read must not decide anything; keep waiting.
			lastErr = sErr
		case st == StatusGranted:
			return DecisionAllow, nil
		case st == StatusDenied:
			return DecisionDeny, nil
		default:
			lastErr = nil
		}

		select {
		case <-ctx.Done():
			g.withdraw(ctx, id)
			if lastErr != nil {
				return DecisionDeny, fmt.Errorf("acp: permission request %s unresolved: %w", id, lastErr)
			}
			return DecisionDeny, fmt.Errorf("acp: permission request %s unresolved: %w", id, ctx.Err())
		case <-ticker.C:
		}
	}
}

// withdraw releases a filed request that Decide is about to give up on. ctx
// is already cancelled at this point, so it is used only for its values, via
// context.WithoutCancel, bounded by withdrawTimeout to keep a misbehaving
// Withdraw from stalling the caller. Errors are swallowed: a cleanup failure
// must not change the deny decision or the unresolved-request error above.
func (g *PollingGate) withdraw(ctx context.Context, id string) {
	if g.Withdraw == nil {
		return
	}
	wCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), withdrawTimeout)
	defer cancel()
	_ = g.Withdraw(wCtx, id)
}
