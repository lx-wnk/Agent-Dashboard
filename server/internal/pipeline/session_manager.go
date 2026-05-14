package pipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/platform"
)

func isPidZombie(pid int) bool {
	if platform.IsLinux {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if err != nil {
			return false
		}
		s := string(data)
		return strings.Contains(s, "State:\tZ") || strings.Contains(s, "State: Z")
	}
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=") //nolint:gosec
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(string(out)), "Z")
}

func IsPidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return !isPidZombie(pid)
	}
	if err == syscall.EPERM {
		return true
	}
	return false
}

type RecoveryDecision struct {
	Kind   string
	Reason string
}

func DecideRecovery(sr *ent.StageRun) RecoveryDecision {
	pid := 0
	if sr.Pid != nil {
		pid = *sr.Pid
	}
	if IsPidAlive(pid) {
		return RecoveryDecision{Kind: "alive", Reason: fmt.Sprintf("PID %d still running", pid)}
	}
	if sr.SessionID != nil && *sr.SessionID != "" {
		return RecoveryDecision{Kind: "resume", Reason: fmt.Sprintf("session %s available for --resume", *sr.SessionID)}
	}
	return RecoveryDecision{Kind: "restart", Reason: "no live PID and no session — must start fresh"}
}

func BuildSessionName(taskSlug, stage string, iteration int) string {
	return fmt.Sprintf("%s-%s-iter-%d", taskSlug, stage, iteration)
}

func AttachSessionID(ctx context.Context, stageRunID, sessionID string, srRepo repo.StageRunRepo) error {
	existing, err := srRepo.GetBySessionID(ctx, sessionID)
	if err == nil && existing != nil {
		return nil
	}
	sid := sessionID
	_, err = srRepo.Update(ctx, stageRunID, repo.UpdateStageRunInput{SessionID: &sid})
	if err != nil {
		return fmt.Errorf("AttachSessionID: %w", err)
	}
	return nil
}
