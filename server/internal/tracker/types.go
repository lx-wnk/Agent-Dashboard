// Package tracker fetches issue metadata from GitHub and Jira.
package tracker

import (
	"context"
	"errors"
)

// Sentinel errors returned by Tracker implementations and Resolve.
var (
	ErrBadRef          = errors.New("tracker: not a recognized issue reference")
	ErrTrackerAuth     = errors.New("tracker: authentication failed")
	ErrIssueNotFound   = errors.New("tracker: issue not found")
	ErrTrackerUpstream = errors.New("tracker: upstream error")
)

// Issue is the normalised issue payload returned by any Tracker.
type Issue struct {
	Tracker string   `json:"tracker"`
	Key     string   `json:"key"`
	Title   string   `json:"title"`
	Body    string   `json:"body"`
	URL     string   `json:"url"`
	Labels  []string `json:"labels"`
}

// Tracker fetches a single issue by ref string.
type Tracker interface {
	FetchIssue(ctx context.Context, ref string) (Issue, error)
}

// Config holds all tracker credentials resolved from the settings store.
type Config struct {
	GitHubToken   string
	GitHubDefRepo string // "owner/repo"; optional — bare #n refs only work when set
	JiraBaseURL   string
	JiraEmail     string
	JiraToken     string
}
