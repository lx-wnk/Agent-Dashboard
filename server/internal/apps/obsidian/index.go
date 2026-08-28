package obsidian

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/memory"
)

// summaryMaxRunes bounds the pointer summary derived from a note's first
// line, so a note whose first paragraph runs to several kilobytes does not
// turn "summary" into a second copy of most of the body.
const summaryMaxRunes = 200

// IndexNotes indexes the notes under client's configured VaultRoot into the
// memory space identified by spaceID, as memory_entry rows of kind
// "pointer" (source_kind "application", source_ref the note's vault-root-
// relative path). The pointer holds a summary and the path, never the note
// body: the vault stays the content, memory is only the index.
//
// Writing requires memory.write against the space, exactly like an agent
// write — the application holds no privileged path around the capability
// gate. capabilities, grants and grantUsage are the three repos
// memory.Authorize needs to enforce that; they are parameters here (rather
// than the plan's original mem-only signature) because nothing else in
// scope could supply them to the gate — a signature that cannot call the
// authorization it is required to perform is not a smaller signature, it is
// a broken one.
//
// A previous run's pointers are reconciled on every call: a note that
// disappeared from the vault since it was indexed is discovered by directly
// re-reading its path, and on that failed access the existing entry is
// expired (repo.MemoryRepo.ExpireEntry) rather than deleted, so the
// contradiction between "memory says this exists" and "the vault does not
// have it" stays visible instead of quietly vanishing.
func IndexNotes(
	ctx context.Context,
	client *Client,
	mem repo.MemoryRepo,
	capabilities repo.CapabilityRepo,
	grants repo.GrantRepo,
	grantUsage repo.GrantUsageRepo,
	spaceID string,
) (int, error) {
	space, err := mem.GetSpaceByID(ctx, spaceID)
	if err != nil {
		return 0, fmt.Errorf("obsidian.IndexNotes: resolve space: %w", err)
	}
	scope := repo.Scope{Kind: repo.ScopeKind(space.ScopeKind), Ref: space.ScopeRef}.Normalize()

	if err := memory.Authorize(ctx, capabilities, grants, grantUsage, repo.CapabilityMemoryWrite, space.Slug, scope); err != nil {
		return 0, fmt.Errorf("obsidian.IndexNotes: %w", err)
	}

	now := time.Now()
	existing, err := mem.ListValid(ctx, space.ID, now)
	if err != nil {
		return 0, fmt.Errorf("obsidian.IndexNotes: list existing pointers: %w", err)
	}
	priorPointers := make(map[string]string, len(existing)) // note path -> entry id
	for _, e := range existing {
		if e.Kind == "pointer" && e.SourceKind == "application" && e.SourceRef != nil {
			priorPointers[*e.SourceRef] = e.ID
		}
	}

	// Client.Search searches the whole vault, not just VaultRoot (see its
	// doc comment) — this is the boundary question the client's own task
	// left open. An empty query is relied on to mean "every note"; that
	// assumption is exercised here against a fake server and gets its real
	// proof in the live-vault walkthrough the settings task runs. Results
	// are then confined to VaultRoot by pathUnderRoot below: the configured
	// root is the only boundary this application models, so it is the
	// boundary enforced here — a memory grant's scope (global/project/
	// application) carries no vault-path semantics to narrow this further.
	results, err := client.Search(ctx, "")
	if err != nil {
		return 0, fmt.Errorf("obsidian.IndexNotes: search: %w", err)
	}

	seen := make(map[string]bool, len(results))
	indexed := 0
	for _, r := range results {
		notePath, ok := pathUnderRoot(client.vaultRoot, r.Path)
		if !ok {
			continue
		}
		seen[notePath] = true
		if _, already := priorPointers[notePath]; already {
			continue
		}

		body, err := client.Read(ctx, notePath)
		if err != nil {
			// A note Search just reported but Read cannot fetch is a
			// transient vault condition, not invalid input to refuse —
			// unlike the cases below, there is no prior entry to protect
			// here, so skipping it leaves nothing unrecorded.
			continue
		}

		summary := firstLine(body, summaryMaxRunes)
		cleanSummary, cleanContent, err := memory.SanitizeForStore(summary, notePath)
		if err != nil {
			// A note whose derived content is empty after sanitizing must
			// not become a silent successful index.
			return indexed, fmt.Errorf("obsidian.IndexNotes: sanitize %s: %w", notePath, err)
		}

		sourceRef := notePath
		if _, err := mem.CreateEntry(ctx, repo.CreateEntryInput{
			SpaceID:    space.ID,
			Summary:    cleanSummary,
			Content:    cleanContent,
			Kind:       "pointer",
			SourceKind: "application",
			SourceRef:  &sourceRef,
			Confidence: 1,
		}); err != nil {
			return indexed, fmt.Errorf("obsidian.IndexNotes: create entry for %s: %w", notePath, err)
		}
		indexed++
	}

	for notePath, entryID := range priorPointers {
		if seen[notePath] {
			continue
		}
		if _, err := client.Read(ctx, notePath); err != nil {
			if expireErr := mem.ExpireEntry(ctx, entryID, now); expireErr != nil {
				return indexed, fmt.Errorf("obsidian.IndexNotes: expire stale pointer for %s: %w", notePath, expireErr)
			}
		}
	}

	return indexed, nil
}

// pathUnderRoot reports whether fullPath — a vault-relative path as returned
// by Client.Search, which searches the whole vault — falls under root, and
// if so returns the path relative to root that Read/Write/Delete expect.
// This is the confinement Search itself does not apply.
func pathUnderRoot(root, fullPath string) (string, bool) {
	rootClean := path.Clean("/" + root)
	fullClean := path.Clean("/" + fullPath)
	prefix := rootClean + "/"
	if !strings.HasPrefix(fullClean, prefix) {
		return "", false
	}
	return strings.TrimPrefix(fullClean, prefix), true
}

// firstLine derives a short summary from a note body: its first non-blank
// line, capped to maxRunes. SanitizeForStore does the actual safety
// stripping; this only keeps a huge first paragraph from becoming the whole
// summary.
func firstLine(body string, maxRunes int) string {
	trimmed := strings.TrimSpace(body)
	if idx := strings.IndexAny(trimmed, "\r\n"); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	runes := []rune(trimmed)
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	return string(runes)
}
