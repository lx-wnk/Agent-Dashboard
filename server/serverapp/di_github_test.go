package serverapp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildGitHubClient_UnconfiguredIsNotAnError: an absent GitHub
// configuration must leave the rest of the server running, exactly as an
// absent Obsidian vault does.
func TestBuildGitHubClient_UnconfiguredIsNotAnError(t *testing.T) {
	svc := newSettingsServiceForTest(t) // nothing set
	client, err := buildGitHubClient(t.Context(), svc)
	require.NoError(t, err)
	assert.Nil(t, client, "an unconfigured GitHub must disable the feature, not fail the boot")
}

// github.token and github.repos are a required PAIR. Each direction gets its
// own test so a regression in one check cannot hide behind the other passing —
// the same shape TestBuildObsidianClient_Missing* uses for its trio.

func TestBuildGitHubClient_TokenWithoutReposIsAnError(t *testing.T) {
	svc := newSettingsServiceForTest(t)
	require.NoError(t, svc.Set(t.Context(), "github.token", "ghp_x"))
	_, err := buildGitHubClient(t.Context(), svc)
	require.Error(t, err, "a token with no repositories must fail loudly, not reach every repository the token can see")
	assert.Contains(t, err.Error(), "github.repos")
}

func TestBuildGitHubClient_ReposWithoutTokenIsAnError(t *testing.T) {
	svc := newSettingsServiceForTest(t)
	require.NoError(t, svc.Set(t.Context(), "github.repos", "lx-wnk/agent-dashboard"))
	_, err := buildGitHubClient(t.Context(), svc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "github.token")
}

// TestBuildGitHubClient_BaseURLIsNotHalfOfThePair pins the one place this
// differs from Obsidian's trio: github.baseURL carries a registry default, so
// it is never unset and can never be a missing half.
func TestBuildGitHubClient_BaseURLIsNotHalfOfThePair(t *testing.T) {
	svc := newSettingsServiceForTest(t)
	require.NoError(t, svc.Set(t.Context(), "github.baseURL", "https://api.github.com"))
	client, err := buildGitHubClient(t.Context(), svc)
	require.NoError(t, err, "a base URL alone must not fail the boot — it always has a value")
	assert.Nil(t, client)
}

// TestBuildGitHubClient_ErrorsNeverCarryTheToken: this function reads the
// decrypted secret, so it is the one place a boot error could leak it.
func TestBuildGitHubClient_ErrorsNeverCarryTheToken(t *testing.T) {
	svc := newSettingsServiceForTest(t)
	require.NoError(t, svc.Set(t.Context(), "github.token", "ghp_supersecret"))
	require.NoError(t, svc.Set(t.Context(), "github.repos", "not-an-owner-name-pair"))
	_, err := buildGitHubClient(t.Context(), svc)
	require.Error(t, err, "a malformed github.repos must fail the boot")
	assert.False(t, strings.Contains(err.Error(), "ghp_supersecret"), "boot error carries the token: %v", err)
}

func TestBuildGitHubClient_FullyConfiguredBuildsAClient(t *testing.T) {
	svc := newSettingsServiceForTest(t)
	require.NoError(t, svc.Set(t.Context(), "github.token", "ghp_x"))
	require.NoError(t, svc.Set(t.Context(), "github.repos", "lx-wnk/agent-dashboard, golang/go"))
	client, err := buildGitHubClient(t.Context(), svc)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, []string{"lx-wnk/agent-dashboard", "golang/go"}, client.Repos(),
		"the allow-list must keep its configured order — the summary panel lists repositories in it")
}
