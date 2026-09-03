package serverapp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lx-wnk/agent-dashboard/server/internal/apps/github"
	"github.com/lx-wnk/agent-dashboard/server/internal/settings"
)

// buildGitHubClient returns nil, nil when GitHub is unconfigured: the
// application is optional, and an absent configuration must leave the rest of
// the server running.
//
// github.token and github.repos are a required PAIR — if one is set, both must
// be, because a client built from half of it would look configured and then
// fail every request instead of refusing to boot. This is the rule
// buildObsidianClient follows for its trio, with one difference forced by the
// settings registry: github.baseURL is NOT part of the pair. It carries a
// Default ("https://api.github.com"), so it is never unset and can never be a
// missing half of anything; a Secret definition may not carry a Default at all
// (settings.Definition's own doc comment), which is why the token has none.
//
// Nothing in this function may put the token in an error or a log line: it is
// the one place that holds the decrypted value.
func buildGitHubClient(ctx context.Context, settingsSvc *settings.Service) (*github.Client, error) {
	reposRaw := strings.TrimSpace(settingsSvc.String("github.repos"))
	token, err := settingsSvc.Secret(ctx, "github.token")
	if err != nil {
		return nil, fmt.Errorf("read github.token: %w", err)
	}
	token = strings.TrimSpace(token)

	if token == "" && reposRaw == "" {
		slog.Info("github: not configured, integration disabled")
		return nil, nil
	}

	var missing []string
	if token == "" {
		missing = append(missing, "github.token")
	}
	if reposRaw == "" {
		missing = append(missing, "github.repos")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required settings: %s", strings.Join(missing, ", "))
	}

	repos, err := github.ParseRepos(reposRaw)
	if err != nil {
		return nil, err
	}

	client, err := github.NewClient(github.Config{
		Token:   token,
		BaseURL: settingsSvc.String("github.baseURL"),
		Repos:   repos,
	})
	if err != nil {
		return nil, err
	}
	slog.Info("github: integration enabled", "repositories", len(repos))
	return client, nil
}
