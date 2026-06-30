package tracker

import (
	"errors"
	"net/http"
	"regexp"
	"time"
)

var (
	// resolveGHRef matches a github.com URL, owner/repo#n, or a bare #n / n.
	resolveGHRef      = regexp.MustCompile(`(?i)github\.com/|^[^/]+/[^#]+#\d+$|^#?\d+$`)
	resolveJiraKey    = regexp.MustCompile(`^[A-Z][A-Z0-9]*-\d+$`)
	resolveJiraBrowse = regexp.MustCompile(`/browse/[A-Z][A-Z0-9]*-\d+`)
)

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
	if resolveGHRef.MatchString(ref) {
		if cfg.GitHubToken == "" {
			return nil, errors.New("configure the GitHub token in Settings")
		}
		return NewGitHubClient(cfg.GitHubToken, cfg.GitHubDefRepo, client), nil
	}
	return nil, ErrBadRef
}
