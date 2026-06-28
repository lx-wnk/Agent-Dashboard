package restart_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
	"github.com/lx-wnk/agent-dashboard/server/internal/restart"
)

type fakeAuthProbe struct {
	entries []plugin.Entry
}

func (f fakeAuthProbe) AllWithCapability(string) []plugin.Entry { return f.entries }

func TestValidatePassesWithNoAuthProvider(t *testing.T) {
	v := restart.NewAuthProviderValidator(fakeAuthProbe{})
	require.NoError(t, v.Validate(context.Background()))
}

func TestValidateFailsWhenAuthProviderUnhealthy(t *testing.T) {
	dead := plugin.NewEntryForTest(plugin.Descriptor{ID: "oauth", Capabilities: []string{plugin.CapAuthProvider}}, false)
	v := restart.NewAuthProviderValidator(fakeAuthProbe{entries: []plugin.Entry{dead}})
	require.Error(t, v.Validate(context.Background()))
}
