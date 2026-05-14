package pipeline

import (
	"fmt"
	"os"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

const agentMessageMaxChars = 2000

type ValidationResult struct {
	OK    bool
	Error string
}

func ValidateStageOutput(stage string, output map[string]any) ValidationResult {
	switch stage {
	case "self_review":
		return validateSelfReview(output)
	case "finalization":
		return validateFinalization(output)
	default:
		return ValidationResult{OK: true}
	}
}

func missing(field string) ValidationResult {
	return ValidationResult{OK: false, Error: fmt.Sprintf("missing required field: %s", field)}
}

func validateSelfReview(o map[string]any) ValidationResult {
	if _, ok := o["passed"].(bool); !ok {
		return missing("passed (boolean)")
	}
	if _, ok := o["findings"].([]any); !ok {
		return missing("findings (array)")
	}
	if _, ok := o["summary"].(string); !ok {
		return missing("summary (string)")
	}
	return ValidationResult{OK: true}
}

func validateFinalization(o map[string]any) ValidationResult {
	if _, ok := o["summary"].(string); !ok {
		return missing("summary (string)")
	}
	if _, ok := o["insights"].([]any); !ok {
		return missing("insights (string array)")
	}
	if _, ok := o["openTodos"].([]any); !ok {
		return missing("openTodos (string array)")
	}
	if _, ok := o["testPlan"].([]any); !ok {
		return missing("testPlan (string array)")
	}
	return ValidationResult{OK: true}
}

type CompletionResult struct {
	Kind      string
	Output    map[string]any
	Error     string
	Retryable bool
}

type CompletionDeps struct {
	IsPidAlive  func(pid int) bool
	ReadOutput  func(cwd, sessionID string) (StageOutputRead, error)
	FindSession func(cwd, afterISO string) (string, error)
	PersistSID  func(stageRunID, sessionID string) error
	Validate    func(stage string, output map[string]any) ValidationResult
}

func DetectCompletion(sr *ent.StageRun, cwd string, deps CompletionDeps) (CompletionResult, error) {
	isPidAliveFn := deps.IsPidAlive
	if isPidAliveFn == nil {
		isPidAliveFn = IsPidAlive
	}
	readOutputFn := deps.ReadOutput
	if readOutputFn == nil {
		readOutputFn = ReadLastStageJsonOutput
	}
	findSessionFn := deps.FindSession
	if findSessionFn == nil {
		findSessionFn = FindNewestSessionID
	}
	validateFn := deps.Validate
	if validateFn == nil {
		validateFn = ValidateStageOutput
	}

	pid := 0
	if sr.Pid != nil {
		pid = *sr.Pid
	}
	if isPidAliveFn(pid) {
		return CompletionResult{Kind: "still_running"}, nil
	}

	// Non-Claude adapters store the synthetic JSONL path in stage_run.output.
	// Use it directly instead of scanning ~/.claude/projects/...
	if sr.Output != nil {
		if syntheticFile, ok := sr.Output["synthetic_session_file"].(string); ok && syntheticFile != "" {
			if _, statErr := os.Stat(syntheticFile); statErr == nil {
				read, err := ReadLastStageJsonOutputFromFile(syntheticFile)
				if err != nil {
					return CompletionResult{Kind: "failed", Error: fmt.Sprintf("synthetic session read error: %s", err)}, nil
				}
				if read.Output == nil {
					if read.RawText != "" {
						trimmed := read.RawText
						if len(trimmed) > agentMessageMaxChars {
							trimmed = trimmed[len(trimmed)-agentMessageMaxChars:]
						}
						return CompletionResult{
							Kind:   "failed",
							Error:  "adapter did not produce a ```json output block",
							Output: map[string]any{"agentMessage": trimmed},
						}, nil
					}
					return CompletionResult{Kind: "failed", Error: "no parseable json output in synthetic session"}, nil
				}
				v := validateFn(sr.Stage, read.Output)
				if !v.OK {
					return CompletionResult{Kind: "failed", Error: v.Error, Output: read.Output, Retryable: true}, nil
				}
				return CompletionResult{Kind: "completed", Output: read.Output}, nil
			}
		}
	}

	sessionID := ""
	if sr.SessionID != nil {
		sessionID = *sr.SessionID
	}
	if sessionID == "" {
		if sr.StartedAt == nil {
			return CompletionResult{Kind: "failed", Error: "stage_run never started — cannot locate session"}, nil
		}
		found, err := findSessionFn(cwd, sr.StartedAt.Format("2006-01-02T15:04:05Z"))
		if err != nil {
			return CompletionResult{Kind: "failed", Error: fmt.Sprintf("session lookup error: %s", err)}, nil
		}
		sessionID = found
		if sessionID != "" && deps.PersistSID != nil {
			_ = deps.PersistSID(sr.ID, sessionID)
		}
	}

	if sessionID == "" {
		projectDir, _ := ResolvedProjectDir(cwd)
		return CompletionResult{
			Kind:  "failed",
			Error: fmt.Sprintf("no session JSONL found in %s after %v (cwd=%s)", projectDir, sr.StartedAt, cwd),
		}, nil
	}

	read, err := readOutputFn(cwd, sessionID)
	if err != nil {
		return CompletionResult{Kind: "failed", Error: fmt.Sprintf("session read error: %s", err)}, nil
	}
	if read.Output == nil {
		if read.RawText != "" {
			trimmed := read.RawText
			if len(trimmed) > agentMessageMaxChars {
				trimmed = trimmed[len(trimmed)-agentMessageMaxChars:]
			}
			return CompletionResult{
				Kind:   "failed",
				Error:  "agent did not produce a ```json output block",
				Output: map[string]any{"agentMessage": trimmed},
			}, nil
		}
		return CompletionResult{Kind: "failed", Error: "no parseable json output in session tail"}, nil
	}

	v := validateFn(sr.Stage, read.Output)
	if !v.OK {
		return CompletionResult{Kind: "failed", Error: v.Error, Output: read.Output, Retryable: true}, nil
	}
	return CompletionResult{Kind: "completed", Output: read.Output}, nil
}
