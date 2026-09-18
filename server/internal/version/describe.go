package version

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// describeTimeout bounds the git call. It is short on purpose: every caller
// has a working answer for "unknown", and none of them may block on git.
const describeTimeout = 2 * time.Second

// DescribeIn runs "git describe --tags --always --dirty" in dir and returns
// its trimmed output, or "" when dir is not a git repository, git is missing,
// or the command fails. An empty dir means the process's working directory.
//
// It is the one place that spells this command, because two callers need it
// for different reasons and must not drift apart: the health endpoint reports
// the revision the server is running beside, and a rebuild stamps the revision
// it is compiling into the new binary. Both have to name a revision the same
// way, or a rebuilt binary would compare unequal to its own source forever.
//
// The result is deliberately not cached here: the two callers need different
// policies -- health polls and caches, a build runs once and must not be
// served a stale answer.
func DescribeIn(ctx context.Context, dir string) string {
	ctx, cancel := context.WithTimeout(ctx, describeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "describe", "--tags", "--always", "--dirty")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
