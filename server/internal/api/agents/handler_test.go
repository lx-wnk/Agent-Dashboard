package agents_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/agents"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

func TestAgentHandler_List_ReturnsJSON(t *testing.T) {
	want := []sdk.Agent{{SessionID: "abc", Status: sdk.AgentStatusActive}}
	getAgents := func(_ context.Context) ([]sdk.Agent, error) { return want, nil }
	broadcaster := sse.NewBroadcaster()
	h := agents.NewHandler(getAgents, broadcaster)

	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, h.List(rec, req))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var got []sdk.Agent
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	require.Len(t, got, 1)
	assert.Equal(t, "abc", got[0].SessionID)
}

func TestAgentHandler_List_EmptyReturnsArray(t *testing.T) {
	getAgents := func(_ context.Context) ([]sdk.Agent, error) { return nil, nil }
	broadcaster := sse.NewBroadcaster()
	h := agents.NewHandler(getAgents, broadcaster)

	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, h.List(rec, req))

	assert.Equal(t, http.StatusOK, rec.Code)
	// Body must be a JSON array [], not null.
	var got []sdk.Agent
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.NotNil(t, got, "handler must return [] not null when no agents exist")
	assert.Empty(t, got)
}
