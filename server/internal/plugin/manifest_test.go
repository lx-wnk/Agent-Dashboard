package plugin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescriptor_ParsesV2(t *testing.T) {
	raw := `{
	  "id":"voice-whisper","name":"Voice (Whisper)","version":"1.2.0",
	  "capabilities":["route_extension"],"addr":"127.0.0.1:19010","command":["./voice-whisper"],
	  "slots":[{"slot":"agent-toolbar","priority":100,"mode":"extend"}],
	  "settings":[{"key":"endpoint","type":"url","label":"Endpoint"},
	              {"key":"apiKey","type":"string","label":"API Key","secret":true}],
	  "lifecycle":{"install":"/lifecycle/install","activate":"/lifecycle/activate"},
	  "permissions":["net"]
	}`
	var d Descriptor
	require.NoError(t, json.Unmarshal([]byte(raw), &d))
	assert.Equal(t, "Voice (Whisper)", d.Name)
	require.Len(t, d.Slots, 1)
	assert.Equal(t, "agent-toolbar", d.Slots[0].Slot)
	assert.Equal(t, 100, d.Slots[0].Priority)
	assert.Equal(t, "extend", d.Slots[0].Mode)
	require.Len(t, d.Settings, 2)
	assert.True(t, d.Settings[1].Secret)
	assert.Equal(t, "/lifecycle/activate", d.Lifecycle.Activate)
}

func TestDescriptor_BackwardCompatV1(t *testing.T) {
	// An old manifest with no v2 fields must still parse, with zero-value v2 fields.
	raw := `{"id":"old","capabilities":["auth_provider"],"addr":"127.0.0.1:9000","command":["./old"]}`
	var d Descriptor
	require.NoError(t, json.Unmarshal([]byte(raw), &d))
	assert.Equal(t, "old", d.ID)
	assert.Empty(t, d.Slots)
	assert.Empty(t, d.Settings)
	assert.Empty(t, d.Name)
}
