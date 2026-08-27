package pipeline_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/permissions"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/lx-wnk/agent-dashboard/server/internal/taskcontrol"
	"github.com/stretchr/testify/require"
)

// parityGitPushRE mirrors pipeline's unexported gitPushRE (spawner.go). It
// cannot be imported, so it is deliberately duplicated here: git-push
// containment is a validation rule that belongs to the grant-translation
// layer, not to capability.Decide, and this golden-parity test needs its own
// copy to build grants the same way production code does.
var parityGitPushRE = regexp.MustCompile(`(?i)\bgit push\b`)

// toGrant translates one TaskPermission fixture into the GrantView
// capability.Decide would need to resolve it, applying exactly the
// non-Decide-owned validation BuildAllowList applies before it ever reaches
// the append: tool allow-list membership, blanket-Bash rejection, git-push
// containment, Bash command safety, and bare-WebFetch rejection. Decide has
// no notion of any of these — it only resolves context specificity, mode
// ranking, expiry and revocation — so this helper exists to draw exactly
// that line. ok is false when no allow grant should exist for the
// permission at all.
func toGrant(p *ent.TaskPermission, taskID string, allowGitPush bool) (grant capability.GrantView, value string, ok bool) {
	if !p.Granted {
		return capability.GrantView{}, "", false // rule 1: !p.Granted
	}
	if !permissions.IsAllowedTool(p.Tool) {
		return capability.GrantView{}, "", false // rule 3: tool not on the allow-list
	}

	switch p.Tool {
	case "Bash":
		if p.Pattern == nil || *p.Pattern == "" {
			return capability.GrantView{}, "", false // rule 4: blanket Bash forbidden
		}
		normalized := strings.Join(strings.Fields(*p.Pattern), " ")
		if !p.ManualOverride {
			if !allowGitPush && parityGitPushRE.MatchString(normalized) {
				return capability.GrantView{}, "", false // rule 5: git push forbidden
			}
			if safe, _ := permissions.IsSafeBashPattern(normalized); !safe {
				return capability.GrantView{}, "", false // rule 6: unsafe Bash pattern
			}
		}
		value = normalized
	case "WebFetch":
		if p.Pattern == nil || strings.TrimSpace(*p.Pattern) == "" {
			return capability.GrantView{}, "", false // rule 7: bare WebFetch forbidden
		}
		value = strings.TrimSpace(*p.Pattern)
	default:
		if p.Pattern != nil {
			value = *p.Pattern
		}
	}

	return capability.GrantView{
		ID:          p.ID,
		ContextKind: "task",
		ContextRef:  taskID,
		Pattern:     value,
		Mode:        "allow",
		ExpiresAt:   p.ExpiresAt,
	}, value, true
}

// formatAllowEntry renders a resolved (tool, value) pair the same way
// BuildAllowList formats its --allowedTools entries.
func formatAllowEntry(tool, value string) string {
	switch tool {
	case "Bash":
		return fmt.Sprintf("Bash(%s)", value)
	case "WebFetch":
		return fmt.Sprintf("WebFetch(%s)", value)
	default:
		if value == "" {
			return tool
		}
		return fmt.Sprintf("%s(%s)", tool, value)
	}
}

// buildViaDecide reproduces BuildAllowList's restrictive-path output by
// translating each permission into a grant (toGrant) and resolving it
// through capability.Decide, one capability at a time. Permissions are
// walked in the same order BuildAllowList iterates perms, so list order is
// preserved identically.
func buildViaDecide(perms []*ent.TaskPermission, taskID string, allowGitPush bool) []string {
	type survivor struct{ tool, value string }

	grantsByTool := map[string][]capability.GrantView{}
	var survivors []survivor
	for _, p := range perms {
		g, value, ok := toGrant(p, taskID, allowGitPush)
		if !ok {
			continue
		}
		grantsByTool[p.Tool] = append(grantsByTool[p.Tool], g)
		survivors = append(survivors, survivor{tool: p.Tool, value: value})
	}

	contexts := []capability.Context{{Kind: "task", Ref: taskID}}
	var out []string
	for _, s := range survivors {
		cap := capability.CapabilityView{Name: s.tool, Class: "tool", EnforceableBy: []string{"spawn"}}
		req := capability.Request{Capability: s.tool, Value: s.value, Contexts: contexts}
		if capability.Decide(req, grantsByTool[s.tool], cap).Effect == capability.EffectAllow {
			out = append(out, formatAllowEntry(s.tool, s.value))
		}
	}
	return out
}

// assertParity runs both paths — the legacy filter chain and the
// grant-translation-plus-Decide path — over the same fixtures under
// autonomy "manual" (the only autonomy where BuildAllowList reads perms at
// all, see exception A below) and requires an identical, identically
// ordered result.
func assertParity(t *testing.T, perms []*ent.TaskPermission, allowGitPush bool) {
	t.Helper()
	const taskID = "t1"
	want := pipeline.BuildAllowList("manual", perms, false, allowGitPush)
	got := buildViaDecide(perms, taskID, allowGitPush)
	require.Equal(t, want, got, "capability.Decide must reproduce BuildAllowList's output element for element and order for order")
}

// TestAllowListParity pins parity between BuildAllowList's restrictive
// filter chain (server/internal/pipeline/spawner.go:98) and the same
// fixtures resolved through capability.Decide, before Task 6 rewrites that
// function's body. It covers all seven drop rules the function implements,
// plus the two carve-outs the controller ruling names as deliberate and out
// of scope for the Decider.
func TestAllowListParity(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	safePattern := "pnpm test"
	expiredPattern := "pnpm build"
	overridePattern := "git push origin HEAD"
	gitPushPattern := "git push origin HEAD"
	unsafePattern := "chmod +x ./x.sh"
	webfetchPattern := "https://docs.example.com*"
	grepPattern := "src/**/*.go"

	t.Run("combined fixture set matches element for element and order for order", func(t *testing.T) {
		perms := []*ent.TaskPermission{
			{ID: "p1", Tool: "Bash", Pattern: &safePattern, Granted: true},                           // survives
			{ID: "p2", Tool: "Bash", Pattern: &expiredPattern, Granted: true, ExpiresAt: &past},      // rule 2
			{ID: "p3", Tool: "Bash", Pattern: &overridePattern, Granted: true, ManualOverride: true}, // override bypasses rules 5+6
			{ID: "p4", Tool: "WebFetch", Granted: true},                                              // rule 7
			{ID: "p5", Tool: "Bash", Granted: true},                                                  // rule 4
			{ID: "p6", Tool: "Read", Granted: false},                                                 // rule 1
			{ID: "p7", Tool: "ShellExec", Granted: true},                                             // rule 3
			{ID: "p8", Tool: "Bash", Pattern: &gitPushPattern, Granted: true},                        // rule 5
			{ID: "p9", Tool: "Bash", Pattern: &unsafePattern, Granted: true},                         // rule 6
			{ID: "p10", Tool: "WebFetch", Pattern: &webfetchPattern, Granted: true},                  // survives
			{ID: "p11", Tool: "Read", Granted: true},                                                 // survives, bare tool
			{ID: "p12", Tool: "Grep", Pattern: &grepPattern, Granted: true},                          // survives, tool(pattern)
		}
		assertParity(t, perms, false)
	})

	t.Run("rule 1: ungranted permission is dropped", func(t *testing.T) {
		assertParity(t, []*ent.TaskPermission{{ID: "p1", Tool: "Read", Granted: false}}, false)
	})

	t.Run("rule 2: expired permission is dropped", func(t *testing.T) {
		assertParity(t, []*ent.TaskPermission{
			{ID: "p1", Tool: "Bash", Pattern: &safePattern, Granted: true, ExpiresAt: &past},
		}, false)
	})

	t.Run("rule 3: tool not on the allow-list is dropped", func(t *testing.T) {
		assertParity(t, []*ent.TaskPermission{{ID: "p1", Tool: "ShellExec", Granted: true}}, false)
	})

	t.Run("rule 4: blanket Bash with no pattern is dropped", func(t *testing.T) {
		assertParity(t, []*ent.TaskPermission{{ID: "p1", Tool: "Bash", Granted: true}}, false)
	})

	t.Run("rule 5: git-push Bash pattern is dropped when push is disallowed", func(t *testing.T) {
		assertParity(t, []*ent.TaskPermission{
			{ID: "p1", Tool: "Bash", Pattern: &gitPushPattern, Granted: true},
		}, false)
	})

	t.Run("rule 6: unsafe Bash pattern is dropped", func(t *testing.T) {
		assertParity(t, []*ent.TaskPermission{
			{ID: "p1", Tool: "Bash", Pattern: &unsafePattern, Granted: true},
		}, false)
	})

	t.Run("rule 7: bare WebFetch with no pattern is dropped", func(t *testing.T) {
		assertParity(t, []*ent.TaskPermission{{ID: "p1", Tool: "WebFetch", Granted: true}}, false)
	})

	t.Run("manual_override bypasses the git-push and Bash-safety gates", func(t *testing.T) {
		assertParity(t, []*ent.TaskPermission{
			{ID: "p1", Tool: "Bash", Pattern: &overridePattern, Granted: true, ManualOverride: true},
		}, false)
	})

	// Exception A (controller ruling): the allow-all short-circuit is out of
	// scope for the Decider. BuildAllowList never reads perms on this path,
	// so there is nothing to translate into grants — synthesising wildcard
	// grants to make it comparable would fabricate policy nobody authored.
	// Asserted here as its own claim: the perms argument has zero effect on
	// the result for spec_gated/full autonomy.
	t.Run("exception A: allow-all autonomy is out of scope for the Decider", func(t *testing.T) {
		perms := []*ent.TaskPermission{{ID: "p1", Tool: "Bash", Pattern: &safePattern, Granted: true}}
		for _, autonomy := range []string{"spec_gated", "full"} {
			got := pipeline.BuildAllowList(autonomy, perms, false, false)
			require.Equal(t, taskcontrol.PermissiveAllowList(false), got,
				"autonomy=%s must return the permissive allow-list untouched by perms", autonomy)
		}
	})

	// Exception B (controller ruling): channel entries bypass the gate by
	// design — enableChannel prepends them unconditionally, consulting no
	// grant. Asserted separately; only the tail is compared against the
	// grant-resolved list.
	t.Run("exception B: channel tools bypass the gate and are asserted separately", func(t *testing.T) {
		perms := []*ent.TaskPermission{{ID: "p1", Tool: "Bash", Pattern: &safePattern, Granted: true}}
		got := pipeline.BuildAllowList("manual", perms, true, false)
		require.GreaterOrEqual(t, len(got), 2)
		require.Equal(t, []string{
			"mcp__dashboard-channel__dashboard_reply",
			"mcp__dashboard-channel__request_permission",
		}, got[:2], "channel entries are prepended unconditionally, consulting no grant")
		require.Equal(t, buildViaDecide(perms, "t1", false), got[2:],
			"the remainder must still match the grant-resolved list")
	})
}
