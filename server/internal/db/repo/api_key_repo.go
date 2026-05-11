package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/apikey"
)

// ApiKeyRepo manages API key persistence.
type ApiKeyRepo interface {
	Create(ctx context.Context, name, hash string, scopes []string) (*ent.ApiKey, error)
	GetByHash(ctx context.Context, hash string) (*ent.ApiKey, error)
	GetByID(ctx context.Context, id string) (*ent.ApiKey, error)
	List(ctx context.Context) ([]*ent.ApiKey, error)
	Delete(ctx context.Context, id string) error
	TouchLastUsed(ctx context.Context, id string) error
}

type entApiKeyRepo struct {
	client *ent.Client
}

// NewApiKeyRepo returns an ApiKeyRepo backed by the given ent client.
func NewApiKeyRepo(client *ent.Client) ApiKeyRepo {
	return &entApiKeyRepo{client: client}
}

func (r *entApiKeyRepo) Create(ctx context.Context, name, hash string, scopes []string) (*ent.ApiKey, error) {
	if scopes == nil {
		scopes = []string{}
	}
	k, err := r.client.ApiKey.Create().
		SetID(uuid.New().String()).
		SetName(name).
		SetKeyHash(hash).
		SetScopes(scopes).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("apikey.Create: %w", err)
	}
	return k, nil
}

func (r *entApiKeyRepo) GetByID(ctx context.Context, id string) (*ent.ApiKey, error) {
	k, err := r.client.ApiKey.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("apikey.GetByID: %w", err)
	}
	return k, nil
}

func (r *entApiKeyRepo) GetByHash(ctx context.Context, hash string) (*ent.ApiKey, error) {
	k, err := r.client.ApiKey.Query().
		Where(apikey.KeyHash(hash), apikey.Active(true)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("apikey.GetByHash: %w", err)
	}
	return k, nil
}

func (r *entApiKeyRepo) List(ctx context.Context) ([]*ent.ApiKey, error) {
	keys, err := r.client.ApiKey.Query().
		Where(apikey.Active(true)).
		Order(apikey.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("apikey.List: %w", err)
	}
	return keys, nil
}

func (r *entApiKeyRepo) Delete(ctx context.Context, id string) error {
	// Soft-delete: set active = false so hash remains in DB for audit.
	if err := r.client.ApiKey.UpdateOneID(id).SetActive(false).Exec(ctx); err != nil {
		return fmt.Errorf("apikey.Delete: %w", err)
	}
	return nil
}

func (r *entApiKeyRepo) TouchLastUsed(ctx context.Context, id string) error {
	if err := r.client.ApiKey.UpdateOneID(id).SetLastUsedAt(time.Now()).Exec(ctx); err != nil {
		return fmt.Errorf("apikey.TouchLastUsed: %w", err)
	}
	return nil
}
