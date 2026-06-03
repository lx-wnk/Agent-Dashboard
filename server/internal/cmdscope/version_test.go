package cmdscope

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseVersion(t *testing.T) {
	cases := map[string]string{
		"2.1.161 (Claude Code)": "2.1.161",
		"claude 2.0.5\n":        "2.0.5",
		"v10.20.30":             "10.20.30",
		"no version here":       "",
		"":                      "",
	}
	for in, want := range cases {
		require.Equal(t, want, parseVersion(in), "input %q", in)
	}
}

func TestBuiltinsMayBeStale(t *testing.T) {
	require.False(t, BuiltinsMayBeStale("", false), "no probe → not stale")
	require.False(t, BuiltinsMayBeStale(CuratedBuiltinsVersion, true), "matching version → not stale")
	require.False(t, BuiltinsMayBeStale("", true), "empty version → not stale")
	require.True(t, BuiltinsMayBeStale("9.9.9", true), "differing version → stale")
}

func TestProbeEngineVersion_EmptyCommand(t *testing.T) {
	_, ok := ProbeEngineVersion("")
	require.False(t, ok)
}

func TestProbeEngineVersion_StubbedAndCached(t *testing.T) {
	// stub the runner to return a fixed version and count invocations — no subprocess
	calls := 0
	origRun := runVersion
	origNow := nowFn
	t.Cleanup(func() { runVersion = origRun; nowFn = origNow })

	runVersion = func(_ context.Context, _ string) ([]byte, error) {
		calls++
		return []byte("3.2.1 (Claude Code)\n"), nil
	}
	base := time.Now()
	nowFn = func() time.Time { return base }

	// reset cache for the command under test
	versionCacheMu.Lock()
	delete(versionCache, "fake-claude")
	versionCacheMu.Unlock()

	v, ok := ProbeEngineVersion("fake-claude")
	require.True(t, ok)
	require.Equal(t, "3.2.1", v)
	require.Equal(t, 1, calls)

	// second call within TTL → served from cache, no new exec
	v2, ok2 := ProbeEngineVersion("fake-claude")
	require.True(t, ok2)
	require.Equal(t, "3.2.1", v2)
	require.Equal(t, 1, calls, "cached within TTL")

	// advance past TTL → re-probe
	nowFn = func() time.Time { return base.Add(versionCacheTTL + time.Second) }
	_, _ = ProbeEngineVersion("fake-claude")
	require.Equal(t, 2, calls, "re-probe after TTL")
}
