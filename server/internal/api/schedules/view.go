package schedules

import (
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

const isoFmt = time.RFC3339

// scheduleBody is the create/update request shape (camelCase wire contract).
type scheduleBody struct {
	Name                string  `json:"name"`
	Enabled             *bool   `json:"enabled"`
	NLText              string  `json:"nlText"`
	CronExpr            string  `json:"cronExpr"`
	Timezone            string  `json:"timezone"`
	Catchup             string  `json:"catchup"`
	SlugPrefix          string  `json:"slugPrefix"`
	Title               string  `json:"title"`
	Description         *string `json:"description"`
	Cwd                 string  `json:"cwd"`
	SourceBranch        *string `json:"sourceBranch"`
	TargetBranch        *string `json:"targetBranch"`
	Priority            string  `json:"priority"`
	MaxIterations       int     `json:"maxIterations"`
	TokenBudget         *int    `json:"tokenBudget"`
	CostBudgetCents     *int    `json:"costBudgetCents"`
	StageTimeoutSeconds int     `json:"stageTimeoutSeconds"`
	SilverBullet        bool    `json:"silverBullet"`
	ProjectID           string  `json:"projectId"`
	SpawnerID           string  `json:"spawnerId"`
	PermissionTemplate  *string `json:"permissionTemplate"`
}

// scheduleView is the JSON shape returned for a schedule.
type scheduleView struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Enabled            bool    `json:"enabled"`
	NLText             *string `json:"nlText,omitempty"`
	CronExpr           string  `json:"cronExpr"`
	Human              string  `json:"human"`
	Timezone           string  `json:"timezone"`
	Catchup            string  `json:"catchup"`
	SlugPrefix         string  `json:"slugPrefix"`
	Title              string  `json:"title"`
	Description        *string `json:"description,omitempty"`
	Cwd                string  `json:"cwd"`
	Priority           string  `json:"priority"`
	SilverBullet       bool    `json:"silverBullet"`
	MaxIterations      int     `json:"maxIterations"`
	ProjectID          *string `json:"projectId,omitempty"`
	SpawnerID          *string `json:"spawnerId,omitempty"`
	PermissionTemplate *string `json:"permissionTemplate,omitempty"`
	NextRunAt          *string `json:"nextRunAt,omitempty"`
	LastRunAt          *string `json:"lastRunAt,omitempty"`
	LastTaskID         *string `json:"lastTaskId,omitempty"`
	CreatedAt          string  `json:"createdAt"`
	UpdatedAt          string  `json:"updatedAt"`
}

func toView(s *ent.TaskSchedule) scheduleView {
	v := scheduleView{
		ID:                 s.ID,
		Name:               s.Name,
		Enabled:            s.Enabled,
		NLText:             s.NlText,
		CronExpr:           s.CronExpr,
		Human:              describeCron(s.CronExpr),
		Timezone:           s.Timezone,
		Catchup:            s.Catchup,
		SlugPrefix:         s.SlugPrefix,
		Title:              s.Title,
		Description:        s.Description,
		Cwd:                s.Cwd,
		Priority:           s.Priority,
		SilverBullet:       s.SilverBullet,
		MaxIterations:      s.MaxIterations,
		ProjectID:          s.ProjectID,
		SpawnerID:          s.SpawnerID,
		PermissionTemplate: s.PermissionTemplate,
		LastTaskID:         s.LastTaskID,
		CreatedAt:          s.CreatedAt.UTC().Format(isoFmt),
		UpdatedAt:          s.UpdatedAt.UTC().Format(isoFmt),
	}
	if s.NextRunAt != nil {
		t := s.NextRunAt.UTC().Format(isoFmt)
		v.NextRunAt = &t
	}
	if s.LastRunAt != nil {
		t := s.LastRunAt.UTC().Format(isoFmt)
		v.LastRunAt = &t
	}
	return v
}
