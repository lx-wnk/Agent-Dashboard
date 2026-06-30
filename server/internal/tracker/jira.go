package tracker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

var (
	jiraKeyRe  = regexp.MustCompile(`^([A-Z][A-Z0-9]*-\d+)$`)
	jiraBrowse = regexp.MustCompile(`/browse/([A-Z][A-Z0-9]*-\d+)`)
)

// JiraClient fetches Jira issues using REST API v3 with Basic auth.
type JiraClient struct {
	baseURL string
	email   string
	token   string
	client  *http.Client
}

// NewJiraClient creates a Jira client.
func NewJiraClient(baseURL, email, token string, client *http.Client) *JiraClient {
	return &JiraClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		email:   email,
		token:   token,
		client:  client,
	}
}

func (j *JiraClient) parseRef(ref string) (string, error) {
	if m := jiraKeyRe.FindStringSubmatch(ref); m != nil {
		return m[1], nil
	}
	if m := jiraBrowse.FindStringSubmatch(ref); m != nil {
		return m[1], nil
	}
	return "", fmt.Errorf("%w: not a recognized Jira issue ref: %q", ErrBadRef, ref)
}

// flattenADF extracts plain text from an Atlassian Document Format JSON node.
// On any parse error it returns an empty string (non-fatal per spec).
func flattenADF(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var node map[string]json.RawMessage
	if err := json.Unmarshal(raw, &node); err != nil {
		return ""
	}
	var sb strings.Builder
	walkADF(&sb, node, false)
	return strings.TrimSpace(sb.String())
}

func walkADF(sb *strings.Builder, node map[string]json.RawMessage, addNewline bool) {
	// Leaf: text node.
	if raw, ok := node["text"]; ok {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			if addNewline && sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(s)
		}
		return
	}
	// Container: recurse into the content array.
	contentRaw, ok := node["content"]
	if !ok {
		return
	}
	var children []map[string]json.RawMessage
	if json.Unmarshal(contentRaw, &children) != nil {
		return
	}
	var nodeType string
	if tr, ok := node["type"]; ok {
		_ = json.Unmarshal(tr, &nodeType)
	}
	// Block-level nodes prefix their first text child with a blank-line separator.
	isBlock := nodeType == "paragraph" || nodeType == "heading"
	for _, child := range children {
		walkADF(sb, child, isBlock)
	}
}

// FetchIssue fetches a Jira issue by KEY-123 reference or browse URL.
func (j *JiraClient) FetchIssue(ctx context.Context, ref string) (Issue, error) {
	key, err := j.parseRef(ref)
	if err != nil {
		return Issue{}, err
	}
	u := fmt.Sprintf("%s/rest/api/3/issue/%s?fields=summary,description,labels", j.baseURL, key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Issue{}, fmt.Errorf("%w: build request: %s", ErrTrackerUpstream, err)
	}
	creds := base64.StdEncoding.EncodeToString([]byte(j.email + ":" + j.token))
	req.Header.Set("Authorization", "Basic "+creds)
	req.Header.Set("Accept", "application/json")

	resp, err := j.client.Do(req)
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
		Fields struct {
			Summary     string          `json:"summary"`
			Description json.RawMessage `json:"description"`
			Labels      []string        `json:"labels"`
		} `json:"fields"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Issue{}, fmt.Errorf("%w: decode: %s", ErrTrackerUpstream, err)
	}
	body := flattenADF(payload.Fields.Description)
	labels := payload.Fields.Labels
	if labels == nil {
		labels = []string{}
	}
	return Issue{
		Tracker: "jira",
		Key:     key,
		Title:   payload.Fields.Summary,
		Body:    body,
		URL:     fmt.Sprintf("%s/browse/%s", j.baseURL, key),
		Labels:  labels,
	}, nil
}
