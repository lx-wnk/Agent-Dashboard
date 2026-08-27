package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/grant"
)

// Grant modes. Free strings rather than a Go enum, matching the capability
// class and resource kind conventions.
const (
	GrantModeAllow = "allow"
	GrantModeDeny  = "deny"
	GrantModeAsk   = "ask"
)

// Grant context kinds a grant can bind to, ranked by specificity per the
// resolution rule (agent_session is most specific, global least).
const (
	GrantContextAgentSession = "agent_session"
	GrantContextTask         = "task"
	GrantContextRoutine      = "routine"
	GrantContextApplication  = "application"
	GrantContextProject      = "project"
	GrantContextGlobal       = "global"
)

// GrantContext is where a grant applies: a context kind plus the instance it
// names. Ref is "" for GrantContextGlobal — the sentinel, not NULL, matching
// repo.Scope's convention.
type GrantContext struct {
	Kind string
	Ref  string
}

// GrantContextFor builds a GrantContext from a kind and ref.
func GrantContextFor(kind, ref string) GrantContext {
	return GrantContext{Kind: kind, Ref: ref}
}

// CreateGrantInput is the named input for Create. Named rather than
// positional because the call has more than four parameters, which is where
// this codebase's convention switches.
type CreateGrantInput struct {
	CapabilityName     string
	Context            GrantContext
	Pattern            string
	Mode               string
	LimitCount         int
	LimitWindowSeconds int
	ExpiresAt          *time.Time
	GrantedBy          string
	Reason             string
}

// GrantRepo persists grants: a capability bound to a context, carrying an
// expiry, a rate limit, and a mode a narrower context can use to overrule a
// broader allow.
type GrantRepo interface {
	Create(ctx context.Context, in CreateGrantInput) (*ent.Grant, error)
	ListForCapability(ctx context.Context, capabilityName string) ([]*ent.Grant, error)
	// Revoke tombstones a grant: revoked_at and revoked_by are set, the row
	// stays — history is never lost to a DELETE. revokedBy is required for
	// the same reason granted_by is: a revocation is a security decision,
	// and "who revoked this" must stay answerable.
	Revoke(ctx context.Context, id, revokedBy string) error
}

type entGrantRepo struct {
	client *ent.Client
}

// NewGrantRepo returns a GrantRepo backed by the ent client.
func NewGrantRepo(client *ent.Client) GrantRepo {
	return &entGrantRepo{client: client}
}

func (r *entGrantRepo) Create(ctx context.Context, in CreateGrantInput) (*ent.Grant, error) {
	if in.GrantedBy == "" {
		return nil, fmt.Errorf("grant.Create: granted_by is required")
	}
	if _, err := capability.ParsePattern(in.Pattern); err != nil {
		return nil, fmt.Errorf("grant.Create: invalid pattern: %w", err)
	}
	row, err := r.client.Grant.Create().
		SetID(uuid.New().String()).
		SetCapabilityName(in.CapabilityName).
		SetContextKind(in.Context.Kind).
		SetContextRef(in.Context.Ref).
		SetPattern(in.Pattern).
		SetMode(in.Mode).
		SetLimitCount(in.LimitCount).
		SetLimitWindowSeconds(in.LimitWindowSeconds).
		SetNillableExpiresAt(in.ExpiresAt).
		SetGrantedBy(in.GrantedBy).
		SetReason(in.Reason).
		SetNodeID(defaultNodeID).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("grant.Create: %w", err)
	}
	return row, nil
}

func (r *entGrantRepo) ListForCapability(ctx context.Context, capabilityName string) ([]*ent.Grant, error) {
	rows, err := r.client.Grant.Query().
		Where(grant.CapabilityNameEQ(capabilityName)).
		Order(ent.Asc(grant.FieldGrantedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("grant.ListForCapability: %w", err)
	}
	return rows, nil
}

func (r *entGrantRepo) Revoke(ctx context.Context, id, revokedBy string) error {
	if revokedBy == "" {
		return fmt.Errorf("grant.Revoke: revoked_by is required")
	}
	if err := r.client.Grant.UpdateOneID(id).
		SetRevokedAt(time.Now()).
		SetRevokedBy(revokedBy).
		Exec(ctx); err != nil {
		return fmt.Errorf("grant.Revoke: %w", err)
	}
	return nil
}
