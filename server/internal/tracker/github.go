package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var (
	ghURLRe   = regexp.MustCompile(`(?i)github\.com/([^/]+)/([^/]+)/issues/(\d+)`)
	ghSlashRe = regexp.MustCompile(`^([^/]+)/([^#]+)#(\d+)$`)
	ghBareRe  = regexp.MustCompile(`^#?(\d+)$`)
	// ghNameRe is the GitHub owner/repo name charset. It deliberately excludes
	// path/query characters (/ ? @ #) that would let a crafted ref escape the
	// intended /repos/{owner}/{repo}/issues/{n} path.
	ghNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// validGHComponent reports whether s is a safe GitHub owner/repo path segment:
// the GitHub name charset, and never a traversal segment (. / .. / contains "..").
func validGHComponent(s string) bool {
	if s == "." || s == ".." || strings.Contains(s, "..") {
		return false
	}
	return ghNameRe.MatchString(s)
}

// GitHubClient fetches GitHub issues. Use NewGitHubClient for production;
// NewGitHubClientWithBase lets tests inject an httptest base URL.
type GitHubClient struct {
	baseURL string // "" means "https://api.github.com"
	token   string
	defRepo string // "owner/repo"; may be empty
	client  *http.Client
}

// NewGitHubClient creates a production GitHub client.
func NewGitHubClient(token, defRepo string, client *http.Client) *GitHubClient {
	return &GitHubClient{token: token, defRepo: defRepo, client: client}
}

// NewGitHubClientWithBase creates a client pointing at baseURL (for httptest injection).
func NewGitHubClientWithBase(baseURL, token, defRepo string, client *http.Client) *GitHubClient {
	return &GitHubClient{baseURL: baseURL, token: token, defRepo: defRepo, client: client}
}

func (g *GitHubClient) parseRef(ref string) (owner, repo string, num int, err error) {
	if m := ghURLRe.FindStringSubmatch(ref); m != nil {
		n, _ := strconv.Atoi(m[3])
		return validatedRef(m[1], m[2], n)
	}
	if m := ghSlashRe.FindStringSubmatch(ref); m != nil {
		n, _ := strconv.Atoi(m[3])
		return validatedRef(m[1], m[2], n)
	}
	stripped := strings.TrimPrefix(ref, "#")
	if m := ghBareRe.FindStringSubmatch(stripped); m != nil {
		n, _ := strconv.Atoi(m[1])
		if g.defRepo == "" {
			return "", "", 0, fmt.Errorf("%w: bare ref #%d requires a configured default repo", ErrBadRef, n)
		}
		parts := strings.SplitN(g.defRepo, "/", 2)
		if len(parts) != 2 {
			return "", "", 0, fmt.Errorf("%w: invalid default repo %q", ErrBadRef, g.defRepo)
		}
		return validatedRef(parts[0], parts[1], n)
	}
	return "", "", 0, fmt.Errorf("%w: not a recognized GitHub issue ref: %q", ErrBadRef, ref)
}

// validatedRef rejects owner/repo components that fall outside the GitHub name
// charset or attempt path traversal, so they cannot escape the issues path.
func validatedRef(owner, repo string, num int) (string, string, int, error) {
	if !validGHComponent(owner) || !validGHComponent(repo) {
		return "", "", 0, fmt.Errorf("%w: invalid GitHub owner/repo in ref", ErrBadRef)
	}
	return owner, repo, num, nil
}

func (g *GitHubClient) apiBase() string {
	if g.baseURL != "" {
		return g.baseURL
	}
	return "https://api.github.com"
}

// FetchIssue fetches a GitHub issue by ref. Supports full URL, owner/repo#n, and bare #n.
func (g *GitHubClient) FetchIssue(ctx context.Context, ref string) (Issue, error) {
	owner, repo, num, err := g.parseRef(ref)
	if err != nil {
		return Issue{}, err
	}
	u := fmt.Sprintf("%s/repos/%s/%s/issues/%d", g.apiBase(), owner, repo, num)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Issue{}, fmt.Errorf("%w: build request: %s", ErrTrackerUpstream, err)
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := g.client.Do(req)
	if err != nil {
		return Issue{}, fmt.Errorf("%w: %s", ErrTrackerUpstream, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return Issue{}, fmt.Errorf("%w: HTTP %d", ErrTrackerAuth, resp.StatusCode)
	case http.StatusNotFound:
		return Issue{}, ErrIssueNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Issue{}, fmt.Errorf("%w: HTTP %d", ErrTrackerUpstream, resp.StatusCode)
	}

	var payload struct {
		Title   string `json:"title"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		Number  int    `json:"number"`
		Labels  []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Issue{}, fmt.Errorf("%w: decode: %s", ErrTrackerUpstream, err)
	}
	labels := make([]string, len(payload.Labels))
	for i, l := range payload.Labels {
		labels[i] = l.Name
	}
	return Issue{
		Tracker: "github",
		Key:     fmt.Sprintf("#%d", payload.Number),
		Title:   payload.Title,
		Body:    payload.Body,
		URL:     payload.HTMLURL,
		Labels:  labels,
	}, nil
}
