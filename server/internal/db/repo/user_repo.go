package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	entuser "github.com/lx-wnk/agent-dashboard/server/internal/db/ent/user"
)

// GitHubUserInfo holds fields from the GitHub user API.
type GitHubUserInfo struct {
	ID          string
	Login       string
	DisplayName string
	AvatarURL   string
}

// UserRepo manages user persistence.
type UserRepo interface {
	Upsert(ctx context.Context, info GitHubUserInfo) (*ent.User, error)
	GetByID(ctx context.Context, id string) (*ent.User, error)
}

type entUserRepo struct {
	client *ent.Client
}

// NewUserRepo returns a UserRepo backed by the given ent client.
func NewUserRepo(client *ent.Client) UserRepo {
	return &entUserRepo{client: client}
}

// Upsert creates or updates a user by GitHub ID.
// GitHub ID is stable across username renames, making it the correct PK.
func (r *entUserRepo) Upsert(ctx context.Context, info GitHubUserInfo) (*ent.User, error) {
	now := time.Now()

	existing, err := r.client.User.Get(ctx, info.ID)
	if err != nil && !ent.IsNotFound(err) {
		return nil, fmt.Errorf("user.Upsert lookup %s: %w", info.ID, err)
	}

	if existing != nil {
		// Update the existing record with latest login data.
		update := r.client.User.UpdateOneID(info.ID).
			SetGithubLogin(info.Login).
			SetLastLoginAt(now)
		if info.DisplayName != "" {
			update = update.SetDisplayName(info.DisplayName)
		}
		if info.AvatarURL != "" {
			update = update.SetAvatarURL(info.AvatarURL)
		}
		u, err := update.Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("user.Upsert update %s: %w", info.ID, err)
		}
		return u, nil
	}

	// Insert new user.
	create := r.client.User.Create().
		SetID(info.ID).
		SetGithubLogin(info.Login).
		SetLastLoginAt(now)
	if info.DisplayName != "" {
		create = create.SetDisplayName(info.DisplayName)
	}
	if info.AvatarURL != "" {
		create = create.SetAvatarURL(info.AvatarURL)
	}
	u, err := create.Save(ctx)
	if err != nil {
		// Handle concurrent insert race: retry as update.
		if ent.IsConstraintError(err) {
			return r.retryAsUpdate(ctx, info, now)
		}
		return nil, fmt.Errorf("user.Upsert create %s: %w", info.ID, err)
	}
	return u, nil
}

// retryAsUpdate handles the concurrent-insert race by falling back to an update.
func (r *entUserRepo) retryAsUpdate(ctx context.Context, info GitHubUserInfo, now time.Time) (*ent.User, error) {
	update := r.client.User.UpdateOneID(info.ID).
		SetGithubLogin(info.Login).
		SetLastLoginAt(now)
	if info.DisplayName != "" {
		update = update.SetDisplayName(info.DisplayName)
	}
	if info.AvatarURL != "" {
		update = update.SetAvatarURL(info.AvatarURL)
	}
	u, err := update.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("user.Upsert retry-update %s: %w", info.ID, err)
	}
	return u, nil
}

// GetByID returns a user by their GitHub ID.
func (r *entUserRepo) GetByID(ctx context.Context, id string) (*ent.User, error) {
	u, err := r.client.User.Query().
		Where(entuser.IDEQ(id)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("user.GetByID %s: %w", id, err)
	}
	return u, nil
}

