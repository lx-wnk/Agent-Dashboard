package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/lx-wnk/kontor/server/internal/db/ent"
	"github.com/lx-wnk/kontor/server/internal/worktree"
)

// decideFinalizationTransition decides what happens once the finalization
// stage completes. Tasks with no worktree (no git checkout to push or open a
// PR from) pass through unchanged. Otherwise it pushes the branch (when
// allowed), refuses to finish with unpushed work, and opens (or reuses) a
// draft PR before handing the task to DoneTransition — so a task never
// reaches done with orphaned, unpushed work.
func (o *PipelineOrchestrator) decideFinalizationTransition(ctx context.Context, task *ent.Task, output map[string]any) StageTransition {
	if task.WorktreePath == nil || *task.WorktreePath == "" {
		return DoneTransition{Output: output}
	}

	pushAllowed := IsGitPushAllowed(task, o.opts.AllowGitPush)
	if o.opts.PushFn != nil && pushAllowed {
		if err := o.opts.PushFn(ctx, task); err != nil {
			slog.Warn("finalization: git push failed", "taskID", task.ID, "err", err)
			return FailTransition{Reason: fmt.Sprintf("finalization: git push failed: %s", err), Output: output}
		}
	}

	if o.opts.HasUnpushedWorkFn != nil && o.opts.HasUnpushedWorkFn(ctx, task) {
		note := "push ran but work remains unpushed"
		if !pushAllowed {
			note = "push is disabled for this task"
		}
		slog.Warn("finalization: worktree has unpushed work", "taskID", task.ID, "note", note)
		return FailTransition{
			Reason: fmt.Sprintf("finalization: worktree has unpushed work (%s)", note),
			Output: output,
		}
	}

	if o.opts.CreateDraftPRFn == nil {
		return DoneTransition{Output: output}
	}

	selfRun, _ := o.stageRuns.GetLatestByTaskAndStage(ctx, task.ID, "self_review")
	title, base, prBody := buildFinalizationPRArgs(task, output, selfRun)
	branch := worktree.CreateBranch(task.SourceBranch, task.Slug)

	prNumber, prURL, err := o.opts.CreateDraftPRFn(ctx, *task.WorktreePath, branch, base, title, prBody)
	if err != nil {
		slog.Warn("finalization: create draft PR failed", "taskID", task.ID, "err", err)
		return FailTransition{Reason: fmt.Sprintf("finalization: create draft PR failed: %s", err), Output: output}
	}

	return DoneTransition{
		Output: output,
		MetadataPatch: map[string]any{
			"pr_number": prNumber,
			"pr_url":    prURL,
		},
	}
}

// ghTimeout bounds every individual `gh` invocation.
const ghTimeout = 30 * time.Second

// ProductionPushFn is the production wiring for OrchestratorOptions.PushFn.
// It pushes the task's worktree branch to origin, setting the upstream if
// none is configured yet.
func ProductionPushFn(ctx context.Context, task *ent.Task) error {
	if task.WorktreePath == nil || *task.WorktreePath == "" {
		return fmt.Errorf("push: task has no worktree")
	}
	out, err := gitRunner.Combined(ctx, *task.WorktreePath, "push", "-u", "origin", "HEAD")
	if err != nil {
		return fmt.Errorf("git push: %s: %w", strings.TrimSpace(out), err)
	}
	return nil
}

// ghPRListEntry decodes one row of `gh pr list --json number,url`.
type ghPRListEntry struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

// ProductionCreateDraftPRFn is the production wiring for
// OrchestratorOptions.CreateDraftPRFn. It is idempotent: an already-open PR
// for branch is returned as-is instead of creating a duplicate.
func ProductionCreateDraftPRFn(ctx context.Context, worktreePath, branch, base, title, prBody string) (int, string, error) {
	if number, url, ok := findExistingPR(ctx, worktreePath, branch); ok {
		return number, url, nil
	}

	out, err := runGH(ctx, worktreePath, "pr", "create", "--draft", "--base", base, "--title", title, "--body", prBody)
	if err != nil {
		return 0, "", fmt.Errorf("gh pr create: %s: %w", strings.TrimSpace(out), err)
	}
	prURL := lastLine(out)
	if prURL == "" {
		return 0, "", fmt.Errorf("gh pr create: no URL in output: %s", strings.TrimSpace(out))
	}

	viewOut, err := runGH(ctx, worktreePath, "pr", "view", prURL, "--json", "number")
	if err != nil {
		return 0, "", fmt.Errorf("gh pr view: %s: %w", strings.TrimSpace(viewOut), err)
	}
	var view struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal([]byte(viewOut), &view); err != nil {
		return 0, "", fmt.Errorf("gh pr view: parse output: %w", err)
	}
	return view.Number, prURL, nil
}

// findExistingPR looks up an already-open PR for branch. A lookup failure or
// empty result is not fatal — the caller falls back to creating a new one.
func findExistingPR(ctx context.Context, worktreePath, branch string) (number int, url string, ok bool) {
	out, err := runGH(ctx, worktreePath, "pr", "list", "--head", branch, "--state", "open", "--json", "number,url", "--limit", "1")
	if err != nil {
		return 0, "", false
	}
	var entries []ghPRListEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil || len(entries) == 0 {
		return 0, "", false
	}
	return entries[0].Number, entries[0].URL, true
}

func runGH(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, ghTimeout)
	defer cancel()
	// #nosec G204 -- args are fixed gh subcommands plus caller-controlled
	// branch/title/body values that reach argv only; exec.CommandContext passes
	// them without a shell.
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func lastLine(s string) string {
	trimmed := strings.TrimSpace(s)
	lines := strings.Split(trimmed, "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// conventionalCommitRE matches a leading Conventional Commits type, optional
// scope, optional "!", and the mandatory ": " separator.
var conventionalCommitRE = regexp.MustCompile(`(?i)^(feat|fix|refactor|chore|docs|test|perf|build|ci|style|revert)(\([^)]*\))?!?:\s`)

// deriveConventionalTitle returns task.Title unchanged when it already carries
// a Conventional Commits prefix, else prefixes it with "feat: ".
func deriveConventionalTitle(task *ent.Task) string {
	if conventionalCommitRE.MatchString(task.Title) {
		return task.Title
	}
	return "feat: " + task.Title
}

// resolveBase returns the PR base branch for task: its TargetBranch when set
// and not a protected trunk name, else "develop".
func resolveBase(task *ent.Task) string {
	if task.TargetBranch != nil {
		b := *task.TargetBranch
		if b != "" && b != "main" && b != "master" {
			return b
		}
	}
	return "develop"
}

// buildFinalizationPRArgs derives the title, base branch, and body for the
// draft PR opened when a worktree task's finalization stage completes.
func buildFinalizationPRArgs(task *ent.Task, finOutput map[string]any, selfRun *ent.StageRun) (title, base, prBody string) {
	title = deriveConventionalTitle(task)
	base = resolveBase(task)
	var selfRunOutput map[string]any
	if selfRun != nil {
		selfRunOutput = selfRun.Output
	}
	prBody = buildPRBody(task, finOutput, selfRunOutput)
	return title, base, prBody
}

// buildPRBody renders the draft PR description from the finalization stage's
// output (summary, testPlan, openTodos) and the self_review stage's findings.
func buildPRBody(task *ent.Task, finOutput map[string]any, selfRunOutput map[string]any) string {
	var b strings.Builder

	b.WriteString("## Summary\n")
	switch {
	case notEmptyString(finOutput["summary"]):
		b.WriteString(finOutput["summary"].(string))
	case task.Description != nil && *task.Description != "":
		b.WriteString(*task.Description)
	default:
		b.WriteString(task.Title)
	}
	b.WriteString("\n")

	b.WriteString("\n## Test Plan\n")
	testPlan := stringSlice(finOutput["testPlan"])
	if len(testPlan) == 0 {
		b.WriteString("- [ ] Manual verification\n")
	} else {
		for _, item := range testPlan {
			fmt.Fprintf(&b, "- [ ] %s\n", item)
		}
	}

	if known := buildKnownIssues(finOutput, selfRunOutput); known != "" {
		b.WriteString("\n## Known Issues\n")
		b.WriteString(known)
	}

	fmt.Fprintf(&b, "\nKontor task: `%s` (%s)\n", task.Slug, task.ID)
	b.WriteString("\n🤖 Generated with [Kontor](https://github.com/lx-wnk/kontor)\n")

	return b.String()
}

// buildKnownIssues lists finalization's openTodos followed by every
// high/medium self_review finding — low-severity findings are noise the PR
// description does not need to carry.
func buildKnownIssues(finOutput, selfRunOutput map[string]any) string {
	var b strings.Builder
	for _, todo := range stringSlice(finOutput["openTodos"]) {
		fmt.Fprintf(&b, "- %s\n", todo)
	}
	for _, f := range findingsSlice(selfRunOutput["findings"]) {
		severity, _ := f["severity"].(string)
		if severity != "high" && severity != "medium" {
			continue
		}
		description, _ := f["description"].(string)
		file, _ := f["file"].(string)
		line := fmt.Sprintf("- [%s] %s", severity, description)
		if file != "" {
			line += fmt.Sprintf(" (%s)", file)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func notEmptyString(v any) bool {
	s, ok := v.(string)
	return ok && s != ""
}

func stringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func findingsSlice(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
