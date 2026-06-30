package tracker

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var (
	resolveJiraKey    = regexp.MustCompile(`^[A-Z][A-Z0-9]*-\d+$`)
	resolveJiraBrowse = regexp.MustCompile(`/browse/[A-Z][A-Z0-9]*-\d+`)
)

// githubRefComponents reports whether ref is a GitHub issue ref (full URL,
// owner/repo#n, or bare #n) and returns its owner/repo components. For a bare
// ref the components are empty (the default repo applies later). ok is false
// when the ref is not a GitHub shape at all.
func githubRefComponents(ref string) (owner, repo string, ok bool) {
	if m := ghURLRe.FindStringSubmatch(ref); m != nil {
		return m[1], m[2], true
	}
	if m := ghSlashRe.FindStringSubmatch(ref); m != nil {
		return m[1], m[2], true
	}
	if ghBareRe.MatchString(strings.TrimPrefix(ref, "#")) {
		return "", "", true
	}
	return "", "", false
}

// Resolve selects the right Tracker by inspecting the ref shape and returns
// a configured client. Returns ErrBadRef for unrecognized ref shapes.
// client may be nil — a 30-second default is used in that case.
func Resolve(ref string, cfg Config, client *http.Client) (Tracker, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	// Jira: KEY-123 pattern or a browse URL.
	if resolveJiraKey.MatchString(ref) || resolveJiraBrowse.MatchString(ref) {
		if cfg.JiraBaseURL == "" || cfg.JiraToken == "" {
			return nil, errors.New("configure the Jira token and base URL in Settings")
		}
		return NewJiraClient(cfg.JiraBaseURL, cfg.JiraEmail, cfg.JiraToken, client), nil
	}
	// GitHub: full URL, owner/repo#n, bare #n.
	if owner, repo, ok := githubRefComponents(ref); ok {
		if owner != "" && !validGHComponent(owner) || repo != "" && !validGHComponent(repo) {
			return nil, ErrBadRef
		}
		if cfg.GitHubToken == "" {
			return nil, errors.New("configure the GitHub token in Settings")
		}
		return NewGitHubClient(cfg.GitHubToken, cfg.GitHubDefRepo, client), nil
	}
	return nil, ErrBadRef
}
