package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_DefaultsAndValidation(t *testing.T) {
	d, ok := Lookup("spawn.rateLimit")
	require.True(t, ok)
	assert.Equal(t, TypeInt, d.Type)
	assert.Equal(t, ApplyRestart, d.Apply)
	assert.Equal(t, "5", d.Default)

	// validator rejects non-int
	require.Error(t, d.Validate("abc"))
	require.NoError(t, d.Validate("10"))

	// enum auth.mode
	a, _ := Lookup("auth.mode")
	require.NoError(t, a.Validate("none"))
	require.NoError(t, a.Validate("plugin"))
	require.Error(t, a.Validate("github"))

	// positive-int constraint
	h, _ := Lookup("hooks.eventsPerSession")
	require.Error(t, h.Validate("0"))
	require.NoError(t, h.Validate("1"))

	_, ok = Lookup("nope")
	assert.False(t, ok)
}
