package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/memoryentry"
)

// CreateSpaceInput is the named input for CreateSpace.
type CreateSpaceInput struct {
	Slug  string
	Name  string
	Scope Scope
}

// CreateEntryInput is the named input for CreateEntry.
type CreateEntryInput struct {
	SpaceID    string
	Summary    string
	Content    string
	Kind       string
	SourceKind string
	SourceRef  *string
	Confidence float64
	// ValidFrom overrides the schema default of now when set.
	ValidFrom *time.Time
	UserID    *string
}

// RecordInjectionInput is the named input for RecordInjection.
type RecordInjectionInput struct {
	StageRunID     string
	EntryIDs       []string
	CharBudget     int
	CharsUsed      int
	CandidateCount int
}

// MemoryRepo persists memory spaces (as resource rows), the entries held
// inside them, and the record of what got injected into a spawn.
type MemoryRepo interface {
	// CreateSpace creates or refreshes the resource row that is a memory
	// space's identity. There is no memory_space table: a space IS a
	// resource row of kind ResourceKindMemorySpace.
	CreateSpace(ctx context.Context, in CreateSpaceInput) (*ent.Resource, error)
	GetSpace(ctx context.Context, scope Scope, slug string) (*ent.Resource, error)
	ListSpaces(ctx context.Context, scope Scope) ([]*ent.Resource, error)
	// DeleteSpace refuses while any entry still references the space —
	// see the implementation comment for why.
	DeleteSpace(ctx context.Context, id string) error
	CreateEntry(ctx context.Context, in CreateEntryInput) (*ent.MemoryEntry, error)
	GetEntry(ctx context.Context, id string) (*ent.MemoryEntry, error)
	// SupersedeEntry marks oldID as replaced by newID. It writes a pointer on
	// the old row rather than mutating or deleting it, so the chain of what
	// replaced what survives as an audit trail.
	SupersedeEntry(ctx context.Context, oldID, newID string) error
	// ExpireEntry marks id as no longer valid as of at, without deleting it —
	// same reasoning as SupersedeEntry.
	ExpireEntry(ctx context.Context, id string, at time.Time) error
	// ListValid returns the entries of spaceID that are neither expired as of
	// now nor superseded.
	ListValid(ctx context.Context, spaceID string, now time.Time) ([]*ent.MemoryEntry, error)
	RecordInjection(ctx context.Context, in RecordInjectionInput) (*ent.MemoryInjection, error)
}

type entMemoryRepo struct {
	client    *ent.Client
	resources ResourceRepo
}

// NewMemoryRepo returns a MemoryRepo backed by the ent client. Space
// operations are delegated to a ResourceRepo built on the same client, rather
// than reimplementing resource identity handling here.
func NewMemoryRepo(client *ent.Client) MemoryRepo {
	return &entMemoryRepo{client: client, resources: NewResourceRepo(client)}
}

func (r *entMemoryRepo) CreateSpace(ctx context.Context, in CreateSpaceInput) (*ent.Resource, error) {
	row, err := r.resources.Upsert(ctx, UpsertResourceInput{
		Kind:  ResourceKindMemorySpace,
		Slug:  in.Slug,
		Name:  in.Name,
		Scope: in.Scope,
	})
	if err != nil {
		return nil, fmt.Errorf("memory.CreateSpace: %w", err)
	}
	return row, nil
}

func (r *entMemoryRepo) GetSpace(ctx context.Context, scope Scope, slug string) (*ent.Resource, error) {
	row, err := r.resources.Get(ctx, ResourceKindMemorySpace, scope, slug)
	if err != nil {
		return nil, fmt.Errorf("memory.GetSpace: %w", err)
	}
	return row, nil
}

func (r *entMemoryRepo) ListSpaces(ctx context.Context, scope Scope) ([]*ent.Resource, error) {
	rows, err := r.resources.ListForScope(ctx, ResourceKindMemorySpace, scope)
	if err != nil {
		return nil, fmt.Errorf("memory.ListSpaces: %w", err)
	}
	return rows, nil
}

// DeleteSpace deletes a memory space's resource row, refusing while any entry
// still references it. space_id is a loose reference with no ent edge or FK,
// so deleting the row out from under existing entries would not fail — it
// would leave them pointing at an id that no longer resolves: not deleted,
// just silently unreachable, in a store whose whole point is durable history.
// Refusing is reversible (delete or move the entries first, then retry);
// cascading would decide that for the caller, which "delete this space"
// should not imply on its own.
//
// The reference count and the delete run inside one write transaction so a
// concurrent CreateEntry cannot slip a new row in between the check and the
// delete and produce the exact orphan this guards against.
func (r *entMemoryRepo) DeleteSpace(ctx context.Context, id string) error {
	err := WithTx(ctx, r.client, func(tx *ent.Tx) error {
		row, err := tx.Resource.Get(ctx, id)
		if err != nil {
			return err
		}
		if row.Kind != ResourceKindMemorySpace {
			return fmt.Errorf("%s: not a memory space", id)
		}
		if row.Origin == ResourceOriginBuiltin {
			return ErrResourceBuiltIn
		}
		count, err := tx.MemoryEntry.Query().Where(memoryentry.SpaceIDEQ(id)).Count(ctx)
		if err != nil {
			return err
		}
		if count > 0 {
			return ErrResourceReferenced
		}
		return tx.Resource.DeleteOneID(id).Exec(ctx)
	})
	if err != nil {
		return fmt.Errorf("memory.DeleteSpace %s: %w", id, err)
	}
	return nil
}

// mustBeSpace fails closed on a space_id that is not a live memory_space
// resource row. space_id is a loose string reference (no ent edge, no FK), so
// nothing at the database level stops an unrecognised or wrong-kind id from
// being written; this is the one place that refuses it.
func (r *entMemoryRepo) mustBeSpace(ctx context.Context, spaceID string) error {
	row, err := r.client.Resource.Get(ctx, spaceID)
	if err != nil {
		return fmt.Errorf("space %s: %w", spaceID, err)
	}
	if row.Kind != ResourceKindMemorySpace {
		return fmt.Errorf("space %s: not a memory space", spaceID)
	}
	return nil
}

func (r *entMemoryRepo) CreateEntry(ctx context.Context, in CreateEntryInput) (*ent.MemoryEntry, error) {
	if in.SpaceID == "" {
		return nil, fmt.Errorf("memory.CreateEntry: space_id is required")
	}
	if in.Summary == "" {
		return nil, fmt.Errorf("memory.CreateEntry: summary is required")
	}
	if in.Kind == "" {
		return nil, fmt.Errorf("memory.CreateEntry: kind is required")
	}
	if in.SourceKind == "" {
		return nil, fmt.Errorf("memory.CreateEntry: source_kind is required")
	}
	if err := r.mustBeSpace(ctx, in.SpaceID); err != nil {
		return nil, fmt.Errorf("memory.CreateEntry: %w", err)
	}

	create := r.client.MemoryEntry.Create().
		SetID(uuid.New().String()).
		SetSpaceID(in.SpaceID).
		SetSummary(in.Summary).
		SetContent(in.Content).
		SetKind(in.Kind).
		SetSourceKind(in.SourceKind).
		SetNillableSourceRef(in.SourceRef).
		SetConfidence(in.Confidence).
		SetNillableUserID(in.UserID)
	if in.ValidFrom != nil {
		create = create.SetValidFrom(*in.ValidFrom)
	}
	row, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("memory.CreateEntry: %w", err)
	}
	return row, nil
}

func (r *entMemoryRepo) GetEntry(ctx context.Context, id string) (*ent.MemoryEntry, error) {
	row, err := r.client.MemoryEntry.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("memory.GetEntry: %w", err)
	}
	return row, nil
}

func (r *entMemoryRepo) SupersedeEntry(ctx context.Context, oldID, newID string) error {
	if oldID == "" || newID == "" {
		return fmt.Errorf("memory.SupersedeEntry: old and new entry ids are required")
	}
	if oldID == newID {
		return fmt.Errorf("memory.SupersedeEntry: an entry cannot supersede itself")
	}
	err := WithTx(ctx, r.client, func(tx *ent.Tx) error {
		// Verify the replacement exists before writing a pointer at it, in the
		// same transaction as the write: a dangling superseded_by must never
		// be observable, not even under a concurrent delete of newID.
		if _, err := tx.MemoryEntry.Get(ctx, newID); err != nil {
			return fmt.Errorf("replacement entry: %w", err)
		}
		return tx.MemoryEntry.UpdateOneID(oldID).SetSupersededBy(newID).Exec(ctx)
	})
	if err != nil {
		return fmt.Errorf("memory.SupersedeEntry: %w", err)
	}
	return nil
}

func (r *entMemoryRepo) ExpireEntry(ctx context.Context, id string, at time.Time) error {
	if err := r.client.MemoryEntry.UpdateOneID(id).SetValidUntil(at).Exec(ctx); err != nil {
		return fmt.Errorf("memory.ExpireEntry: %w", err)
	}
	return nil
}

func (r *entMemoryRepo) ListValid(ctx context.Context, spaceID string, now time.Time) ([]*ent.MemoryEntry, error) {
	rows, err := r.client.MemoryEntry.Query().
		Where(
			memoryentry.SpaceIDEQ(spaceID),
			memoryentry.SupersededByIsNil(),
			memoryentry.Or(
				memoryentry.ValidUntilIsNil(),
				memoryentry.ValidUntilGT(now),
			),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("memory.ListValid: %w", err)
	}
	return rows, nil
}

func (r *entMemoryRepo) RecordInjection(ctx context.Context, in RecordInjectionInput) (*ent.MemoryInjection, error) {
	if in.StageRunID == "" {
		return nil, fmt.Errorf("memory.RecordInjection: stage_run_id is required")
	}
	entryIDs := in.EntryIDs
	if entryIDs == nil {
		entryIDs = []string{}
	}
	row, err := r.client.MemoryInjection.Create().
		SetID(uuid.New().String()).
		SetStageRunID(in.StageRunID).
		SetEntryIds(entryIDs).
		SetCharBudget(in.CharBudget).
		SetCharsUsed(in.CharsUsed).
		SetCandidateCount(in.CandidateCount).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("memory.RecordInjection: %w", err)
	}
	return row, nil
}
