package mcpapps

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

//go:embed presets/*.json
var presetFiles embed.FS

var (
	ErrRoutineRequired   = errors.New("mcpapps: a routine is required to allow tools")
	ErrPresetUnconfirmed = errors.New("mcpapps: preset is not confirmed by a human yet")
)

type Preset struct {
	Server          string   `json:"server"`
	Version         string   `json:"version"`
	Confirmed       bool     `json:"confirmed"`
	AllowForRoutine []string `json:"allowForRoutine"`
	DenyGlobal      []string `json:"denyGlobal"`
}

type PresetResult struct {
	Created  []string `json:"created"`
	Existing []string `json:"existing"`
	Skipped  []string `json:"skipped"`
}

func LoadPreset(name string) (Preset, error) {
	data, err := presetFiles.ReadFile("presets/" + name + ".json")
	if err != nil {
		return Preset{}, fmt.Errorf("mcpapps: unknown preset %q", name)
	}
	var p Preset
	if err := json.Unmarshal(data, &p); err != nil {
		return Preset{}, fmt.Errorf("mcpapps: preset %q: %w", name, err)
	}
	return p, nil
}

func ApplyPreset(ctx context.Context, grants repo.GrantRepo, app *ent.MCPApplication, p Preset, routineID, grantedBy string) (PresetResult, error) {
	if routineID == "" && len(p.AllowForRoutine) > 0 {
		return PresetResult{}, ErrRoutineRequired
	}
	if !p.Confirmed {
		return PresetResult{}, ErrPresetUnconfirmed
	}
	known := make(map[string]bool, len(app.Catalogue))
	for _, t := range app.Catalogue {
		known[t.Name] = true
	}
	res := PresetResult{Created: []string{}, Existing: []string{}, Skipped: []string{}}
	apply := func(tool, mode string, grantCtx repo.GrantContext) error {
		if !known[tool] {
			res.Skipped = append(res.Skipped, tool)
			return nil
		}
		name := CapabilityName(app.ServerName, tool)
		rows, err := grants.ListForCapability(ctx, name)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.RevokedAt == nil && row.ExpiresAt == nil && row.Pattern == "" && row.LimitCount == 0 &&
				row.Mode == mode && row.ContextKind == grantCtx.Kind && row.ContextRef == grantCtx.Ref {
				res.Existing = append(res.Existing, name)
				return nil
			}
		}
		if _, err := grants.Create(ctx, repo.CreateGrantInput{
			CapabilityName: name,
			Context:        grantCtx,
			Mode:           mode,
			GrantedBy:      grantedBy,
			Reason:         "preset " + p.Server,
		}); err != nil {
			return err
		}
		res.Created = append(res.Created, name)
		return nil
	}
	for _, tool := range p.AllowForRoutine {
		if err := apply(tool, "allow", repo.GrantContext{Kind: repo.GrantContextRoutine, Ref: routineID}); err != nil {
			return PresetResult{}, fmt.Errorf("mcpapps.ApplyPreset: %w", err)
		}
	}
	for _, tool := range p.DenyGlobal {
		if err := apply(tool, "deny", repo.GrantContext{Kind: repo.GrantContextGlobal}); err != nil {
			return PresetResult{}, fmt.Errorf("mcpapps.ApplyPreset: %w", err)
		}
	}
	return res, nil
}
