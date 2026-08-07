package cmdscope

import (
	"context"
	"log/slog"
	"os/exec"
	"regexp"
	"sync"
	"time"
)

// CuratedBuiltinsVersion is the Claude Code version the built-in command list in
// enumerate.go was curated against. The CLI exposes no machine-readable command
// listing, so when a spawner's engine reports a different version the built-in
// list may be stale — surfaced via BuiltinsMayBeStale and logged once.
const CuratedBuiltinsVersion = "2.1.224"

// versionProbeTimeout bounds a single `<command> --version` invocation.
const versionProbeTimeout = 2 * time.Second

// versionCacheTTL is how long a probed version is reused before re-probing.
const versionCacheTTL = 5 * time.Minute

var semverRE = regexp.MustCompile(`\d+\.\d+\.\d+`)

// parseVersion extracts the semantic version from `claude --version` output,
// e.g. "2.1.161 (Claude Code)" → "2.1.161". Returns "" when no semver is found.
func parseVersion(output string) string {
	return semverRE.FindString(output)
}

type cachedVersion struct {
	version string
	ok      bool
	at      time.Time
}

var (
	versionCacheMu sync.RWMutex
	versionCache   = map[string]cachedVersion{}

	staleWarnedMu sync.Mutex
	staleWarned   = map[string]bool{}
)

// runVersion executes `<command> --version` and returns its combined stdout.
// Indirected so tests can stub the probe without spawning a real binary.
var runVersion = func(ctx context.Context, command string) ([]byte, error) {
	return exec.CommandContext(ctx, command, "--version").Output()
}

// nowFn is indirected for deterministic cache tests.
var nowFn = time.Now

// ProbeEngineVersion runs `<command> --version`, parses the semver, and caches
// the result by command for versionCacheTTL. Returns ok=false on empty command
// or any execution/parse failure.
func ProbeEngineVersion(command string) (string, bool) {
	if command == "" {
		return "", false
	}

	now := nowFn()
	versionCacheMu.RLock()
	if c, found := versionCache[command]; found && now.Sub(c.at) < versionCacheTTL {
		versionCacheMu.RUnlock()
		return c.version, c.ok
	}
	versionCacheMu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), versionProbeTimeout)
	defer cancel()

	version, ok := "", false
	out, err := runVersion(ctx, command)
	if err == nil {
		if v := parseVersion(string(out)); v != "" {
			version, ok = v, true
		}
	}

	versionCacheMu.Lock()
	versionCache[command] = cachedVersion{version: version, ok: ok, at: now}
	versionCacheMu.Unlock()

	if BuiltinsMayBeStale(version, ok) {
		warnStaleOnce(version)
	}
	return version, ok
}

// BuiltinsMayBeStale reports whether a probed engine version differs from the
// version the built-in command list was curated against.
func BuiltinsMayBeStale(probedVersion string, ok bool) bool {
	return ok && probedVersion != "" && probedVersion != CuratedBuiltinsVersion
}

func warnStaleOnce(version string) {
	staleWarnedMu.Lock()
	defer staleWarnedMu.Unlock()
	if staleWarned[version] {
		return
	}
	staleWarned[version] = true
	slog.Warn("claude built-in command list may be stale",
		"probedVersion", version,
		"curatedForVersion", CuratedBuiltinsVersion,
	)
}
