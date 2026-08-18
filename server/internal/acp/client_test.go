package acp

import (
	"context"
	"errors"
	"testing"

	sdkacp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

func TestClientSatisfiesSDKInterface(t *testing.T) {
	var c any = &Client{}
	_, ok := c.(sdkacp.Client)
	require.True(t, ok, "Client must implement sdkacp.Client")
}

func TestSessionUpdateMapsEveryKind(t *testing.T) {
	inProgress := sdkacp.ToolCallStatusInProgress

	tests := []struct {
		name   string
		update sdkacp.SessionUpdate
		want   Event
	}{
		{
			"agent message",
			sdkacp.SessionUpdate{AgentMessageChunk: &sdkacp.SessionUpdateAgentMessageChunk{
				Content: sdkacp.TextBlock("hello"),
			}},
			Event{SessionID: "sess-1", Kind: KindAgentMessage, Text: "hello"},
		},
		{
			"agent thought",
			sdkacp.SessionUpdate{AgentThoughtChunk: &sdkacp.SessionUpdateAgentThoughtChunk{
				Content: sdkacp.TextBlock("thinking"),
			}},
			Event{SessionID: "sess-1", Kind: KindAgentThought, Text: "thinking"},
		},
		{
			"tool call",
			sdkacp.SessionUpdate{ToolCall: &sdkacp.SessionUpdateToolCall{
				ToolCallId: "tc-1", Title: "Write file", Status: sdkacp.ToolCallStatusPending,
			}},
			Event{
				SessionID: "sess-1", Kind: KindToolCall, Text: "Write file",
				ToolCallID: "tc-1", Status: sdkacp.ToolCallStatusPending,
			},
		},
		{
			"tool call update",
			sdkacp.SessionUpdate{ToolCallUpdate: &sdkacp.SessionToolCallUpdate{
				ToolCallId: "tc-1", Status: &inProgress,
			}},
			Event{
				SessionID: "sess-1", Kind: KindToolCallUpdate,
				ToolCallID: "tc-1", Status: sdkacp.ToolCallStatusInProgress,
			},
		},
		{
			"tool call update without status",
			sdkacp.SessionUpdate{ToolCallUpdate: &sdkacp.SessionToolCallUpdate{ToolCallId: "tc-1"}},
			Event{SessionID: "sess-1", Kind: KindToolCallUpdate, ToolCallID: "tc-1"},
		},
		{
			"plan",
			sdkacp.SessionUpdate{Plan: &sdkacp.SessionUpdatePlan{}},
			Event{SessionID: "sess-1", Kind: KindPlan},
		},
		{
			"mode",
			sdkacp.SessionUpdate{CurrentModeUpdate: &sdkacp.SessionCurrentModeUpdate{
				CurrentModeId: "acceptEdits",
			}},
			Event{SessionID: "sess-1", Kind: KindMode, Text: "acceptEdits"},
		},
		{
			"unmapped variant",
			sdkacp.SessionUpdate{UserMessageChunk: &sdkacp.SessionUpdateUserMessageChunk{
				Content: sdkacp.TextBlock("typed by the user"),
			}},
			Event{SessionID: "sess-1", Kind: KindOther},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []Event
			c := &Client{OnEvent: func(e Event) { got = append(got, e) }}

			err := c.SessionUpdate(context.Background(), sdkacp.SessionNotification{
				SessionId: "sess-1", Update: tt.update,
			})

			require.NoError(t, err)
			require.Equal(t, []Event{tt.want}, got)
		})
	}
}

func TestSessionUpdateContainsAPanickingCallback(t *testing.T) {
	c := &Client{OnEvent: func(Event) { panic("consumer bug") }}

	require.NotPanics(t, func() {
		err := c.SessionUpdate(context.Background(), sdkacp.SessionNotification{
			SessionId: "sess-1",
			Update: sdkacp.SessionUpdate{
				AgentMessageChunk: &sdkacp.SessionUpdateAgentMessageChunk{
					Content: sdkacp.TextBlock("hello"),
				},
			},
		})
		require.NoError(t, err)
	})
}

func TestSessionUpdateWithoutCallbackDoesNotPanic(t *testing.T) {
	c := &Client{}
	err := c.SessionUpdate(context.Background(), sdkacp.SessionNotification{
		SessionId: "sess-1",
		Update: sdkacp.SessionUpdate{
			AgentMessageChunk: &sdkacp.SessionUpdateAgentMessageChunk{
				Content: sdkacp.TextBlock("hello"),
			},
		},
	})
	require.NoError(t, err)
}

func permissionOptions() []sdkacp.PermissionOption {
	return []sdkacp.PermissionOption{
		{Kind: sdkacp.PermissionOptionKindAllowOnce, Name: "Allow", OptionId: "allow"},
		{Kind: sdkacp.PermissionOptionKindRejectOnce, Name: "Reject", OptionId: "reject"},
	}
}

func TestRequestPermissionAllowSelectsAllowOption(t *testing.T) {
	c := &Client{OnPermission: func(context.Context, PermissionRequest) (PermissionDecision, error) {
		return DecisionAllow, nil
	}}

	resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		Options:   permissionOptions(),
		ToolCall:  sdkacp.ToolCallUpdate{ToolCallId: "tc-1"},
	})

	require.NoError(t, err)
	require.NotNil(t, resp.Outcome.Selected)
	require.Equal(t, sdkacp.PermissionOptionId("allow"), resp.Outcome.Selected.OptionId)
}

func TestRequestPermissionMapsToolCallDetail(t *testing.T) {
	var got PermissionRequest
	c := &Client{OnPermission: func(_ context.Context, req PermissionRequest) (PermissionDecision, error) {
		got = req
		return DecisionAllow, nil
	}}

	kind := sdkacp.ToolKindEdit
	name := "edit_file"
	rawInput := map[string]any{"command": "rm -rf /"}

	_, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		Options:   permissionOptions(),
		ToolCall: sdkacp.ToolCallUpdate{
			ToolCallId: "tc-1",
			Kind:       &kind,
			Name:       &name,
			Locations: []sdkacp.ToolCallLocation{
				{Path: "/tmp/a.txt"},
				{Path: "/tmp/b.txt"},
			},
			RawInput: rawInput,
		},
	})

	require.NoError(t, err)
	require.Equal(t, "edit", got.ToolKind)
	require.Equal(t, "edit_file", got.ToolName)
	require.Equal(t, []string{"/tmp/a.txt", "/tmp/b.txt"}, got.Locations)
	require.Equal(t, rawInput, got.RawInput)
}

func TestRequestPermissionMapsEmptyToolCallDetailWithoutPanic(t *testing.T) {
	var got PermissionRequest
	called := false
	c := &Client{OnPermission: func(_ context.Context, req PermissionRequest) (PermissionDecision, error) {
		called = true
		got = req
		return DecisionAllow, nil
	}}

	_, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		Options:   permissionOptions(),
		ToolCall:  sdkacp.ToolCallUpdate{ToolCallId: "tc-1"},
	})

	require.NoError(t, err)
	require.True(t, called)
	require.Empty(t, got.ToolKind)
	require.Empty(t, got.ToolName)
	require.Empty(t, got.Locations)
	require.Nil(t, got.RawInput)
}

func TestRequestPermissionCarriesLocationWithEmptyPath(t *testing.T) {
	var got PermissionRequest
	c := &Client{OnPermission: func(_ context.Context, req PermissionRequest) (PermissionDecision, error) {
		got = req
		return DecisionAllow, nil
	}}

	_, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		Options:   permissionOptions(),
		ToolCall: sdkacp.ToolCallUpdate{
			ToolCallId: "tc-1",
			Locations:  []sdkacp.ToolCallLocation{{Path: ""}},
		},
	})

	require.NoError(t, err)
	require.Equal(t, []string{""}, got.Locations)
}

func TestRequestPermissionWithoutCallbackDenies(t *testing.T) {
	c := &Client{}

	resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		Options:   permissionOptions(),
	})

	require.NoError(t, err)
	require.NotNil(t, resp.Outcome.Selected)
	require.Equal(t, sdkacp.PermissionOptionId("reject"), resp.Outcome.Selected.OptionId)
}

func TestRequestPermissionCallbackErrorDenies(t *testing.T) {
	c := &Client{OnPermission: func(context.Context, PermissionRequest) (PermissionDecision, error) {
		return DecisionAllow, errors.New("gate unreachable")
	}}

	resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		Options:   permissionOptions(),
	})

	require.NoError(t, err)
	require.NotNil(t, resp.Outcome.Selected)
	require.Equal(t, sdkacp.PermissionOptionId("reject"), resp.Outcome.Selected.OptionId)
}

func TestRequestPermissionWithoutRejectOptionCancels(t *testing.T) {
	c := &Client{}

	resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		Options:   []sdkacp.PermissionOption{{Kind: sdkacp.PermissionOptionKindAllowOnce, Name: "Allow", OptionId: "allow"}},
	})

	require.NoError(t, err)
	require.Nil(t, resp.Outcome.Selected)
	require.NotNil(t, resp.Outcome.Cancelled)
}

func TestRequestPermissionDenyFallsBackToRejectAlways(t *testing.T) {
	// A gate that reached a verdict: remembering it is accurate. The unwired
	// case is a different thing and is covered separately.
	c := &Client{OnPermission: func(context.Context, PermissionRequest) (PermissionDecision, error) {
		return DecisionDeny, nil
	}}

	resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		Options: []sdkacp.PermissionOption{
			{Kind: sdkacp.PermissionOptionKindAllowOnce, Name: "Allow", OptionId: "allow"},
			{Kind: sdkacp.PermissionOptionKindRejectAlways, Name: "Always reject", OptionId: "reject-always"},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp.Outcome.Selected)
	require.Equal(t, sdkacp.PermissionOptionId("reject-always"), resp.Outcome.Selected.OptionId)
}

func TestRequestPermissionPanickingGateDenies(t *testing.T) {
	c := &Client{OnPermission: func(context.Context, PermissionRequest) (PermissionDecision, error) {
		panic("gate bug")
	}}

	require.NotPanics(t, func() {
		resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
			SessionId: "sess-1",
			Options:   permissionOptions(),
		})

		require.NoError(t, err)
		require.NotNil(t, resp.Outcome.Selected)
		require.Equal(t, sdkacp.PermissionOptionId("reject"), resp.Outcome.Selected.OptionId)
	})
}

// A gate that reached a verdict may be remembered for the session; a gate that
// never answered must not be, so it takes reject_once or nothing.
func TestRequestPermissionRejectAlwaysOnlyOnASubstantiveDeny(t *testing.T) {
	denied := func(context.Context, PermissionRequest) (PermissionDecision, error) {
		return DecisionDeny, nil
	}
	errored := func(context.Context, PermissionRequest) (PermissionDecision, error) {
		return DecisionAllow, errors.New("gate unreachable")
	}
	panicking := func(context.Context, PermissionRequest) (PermissionDecision, error) {
		panic("gate bug")
	}
	rejectAlwaysOnly := []sdkacp.PermissionOption{
		{Kind: sdkacp.PermissionOptionKindAllowOnce, Name: "Allow", OptionId: "allow"},
		{Kind: sdkacp.PermissionOptionKindRejectAlways, Name: "Always reject", OptionId: "reject-always"},
	}

	tests := []struct {
		name    string
		gate    func(context.Context, PermissionRequest) (PermissionDecision, error)
		options []sdkacp.PermissionOption
		want    sdkacp.PermissionOptionId // empty means the outcome must be Cancelled
	}{
		{"substantive deny takes reject_always", denied, rejectAlwaysOnly, "reject-always"},
		{"gate error cancels rather than remembering", errored, rejectAlwaysOnly, ""},
		{"gate panic cancels rather than remembering", panicking, rejectAlwaysOnly, ""},
		{"gate error takes reject_once when offered", errored, permissionOptions(), "reject"},
		{"substantive deny prefers reject_once", denied, permissionOptions(), "reject"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{OnPermission: tt.gate}

			resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
				SessionId: "sess-1", Options: tt.options,
			})

			require.NoError(t, err)
			if tt.want == "" {
				require.Nil(t, resp.Outcome.Selected)
				require.NotNil(t, resp.Outcome.Cancelled)
				return
			}
			require.NotNil(t, resp.Outcome.Selected)
			require.Equal(t, tt.want, resp.Outcome.Selected.OptionId)
		})
	}
}

func TestRequestPermissionAllowDoesNotFallBackToAllowAlways(t *testing.T) {
	c := &Client{OnPermission: func(context.Context, PermissionRequest) (PermissionDecision, error) {
		return DecisionAllow, nil
	}}

	resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		Options: []sdkacp.PermissionOption{
			{Kind: sdkacp.PermissionOptionKindAllowAlways, Name: "Always allow", OptionId: "allow-always"},
			{Kind: sdkacp.PermissionOptionKindRejectOnce, Name: "Reject", OptionId: "reject"},
		},
	})

	require.NoError(t, err)
	require.Nil(t, resp.Outcome.Selected)
	require.NotNil(t, resp.Outcome.Cancelled)
}

func TestUnsupportedCapabilitiesRefuse(t *testing.T) {
	c := &Client{}
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{"ReadTextFile", func() error {
			_, err := c.ReadTextFile(ctx, sdkacp.ReadTextFileRequest{})
			return err
		}},
		{"WriteTextFile", func() error {
			_, err := c.WriteTextFile(ctx, sdkacp.WriteTextFileRequest{})
			return err
		}},
		{"CreateTerminal", func() error {
			_, err := c.CreateTerminal(ctx, sdkacp.CreateTerminalRequest{})
			return err
		}},
		{"KillTerminal", func() error {
			_, err := c.KillTerminal(ctx, sdkacp.KillTerminalRequest{})
			return err
		}},
		{"TerminalOutput", func() error {
			_, err := c.TerminalOutput(ctx, sdkacp.TerminalOutputRequest{})
			return err
		}},
		{"ReleaseTerminal", func() error {
			_, err := c.ReleaseTerminal(ctx, sdkacp.ReleaseTerminalRequest{})
			return err
		}},
		{"WaitForTerminalExit", func() error {
			_, err := c.WaitForTerminalExit(ctx, sdkacp.WaitForTerminalExitRequest{})
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			require.ErrorIs(t, err, errUnsupported)
		})
	}
}

func widening(id string, kind sdkacp.PermissionOptionKind) sdkacp.PermissionOption {
	return sdkacp.PermissionOption{
		Kind: kind, Name: "Allow and don't ask again", OptionId: sdkacp.PermissionOptionId(id),
		Meta: map[string]any{"permission": map[string]any{"changes": []any{
			map[string]any{"operation": "set", "mode": "acceptEdits",
				"lifetime": map[string]any{"scope": "session"}},
		}}},
	}
}

func TestRequestPermissionSkipsAWideningAllowOption(t *testing.T) {
	c := &Client{OnPermission: func(context.Context, PermissionRequest) (PermissionDecision, error) {
		return DecisionAllow, nil
	}}

	resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		Options: []sdkacp.PermissionOption{
			widening("wide", sdkacp.PermissionOptionKindAllowOnce),
			{Kind: sdkacp.PermissionOptionKindAllowOnce, Name: "Allow Once", OptionId: "narrow"},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp.Outcome.Selected)
	require.Equal(t, sdkacp.PermissionOptionId("narrow"), resp.Outcome.Selected.OptionId)
}

func TestRequestPermissionCancelsWhenOnlyWideningOptionsRemain(t *testing.T) {
	c := &Client{OnPermission: func(context.Context, PermissionRequest) (PermissionDecision, error) {
		return DecisionAllow, nil
	}}

	resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		Options:   []sdkacp.PermissionOption{widening("wide", sdkacp.PermissionOptionKindAllowOnce)},
	})

	require.NoError(t, err)
	require.Nil(t, resp.Outcome.Selected)
	require.NotNil(t, resp.Outcome.Cancelled)
}

func TestRequestPermissionSkipsAWideningDenyOption(t *testing.T) {
	c := &Client{}

	resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		Options: []sdkacp.PermissionOption{
			widening("wide", sdkacp.PermissionOptionKindRejectOnce),
			{Kind: sdkacp.PermissionOptionKindRejectOnce, Name: "Deny", OptionId: "narrow"},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp.Outcome.Selected)
	require.Equal(t, sdkacp.PermissionOptionId("narrow"), resp.Outcome.Selected.OptionId)
}

func TestRequestPermissionKeepsOptionsWithUnrelatedMeta(t *testing.T) {
	c := &Client{OnPermission: func(context.Context, PermissionRequest) (PermissionDecision, error) {
		return DecisionAllow, nil
	}}

	opt := sdkacp.PermissionOption{
		Kind: sdkacp.PermissionOptionKindAllowOnce, Name: "Allow Once", OptionId: "narrow",
		Meta: map[string]any{"somethingElse": true},
	}
	resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1", Options: []sdkacp.PermissionOption{opt},
	})

	require.NoError(t, err)
	require.NotNil(t, resp.Outcome.Selected)
	require.Equal(t, sdkacp.PermissionOptionId("narrow"), resp.Outcome.Selected.OptionId)
}

func TestRequestPermissionWideningMetaShapes(t *testing.T) {
	tests := []struct {
		name     string
		meta     map[string]any
		selected bool
	}{
		{"no meta", nil, true},
		{"meta without permission key", map[string]any{"somethingElse": true}, true},
		{"permission not a map", map[string]any{"permission": "acceptEdits"}, false},
		{
			"changes not a slice (bypass shape: object instead of array)",
			map[string]any{"permission": map[string]any{"changes": map[string]any{
				"0": map[string]any{"operation": "set", "mode": "acceptEdits",
					"lifetime": map[string]any{"scope": "session"}},
			}}},
			false,
		},
		{
			"changes non-empty slice",
			map[string]any{"permission": map[string]any{"changes": []any{
				map[string]any{"operation": "set", "mode": "acceptEdits"},
			}}},
			false,
		},
		{"changes empty slice", map[string]any{"permission": map[string]any{"changes": []any{}}}, true},
		{"changes absent, permission map", map[string]any{"permission": map[string]any{"mode": "acceptEdits"}}, false},
		{"permission is an empty map", map[string]any{"permission": map[string]any{}}, true},
		{
			"capital P key (bypass shape: case-varied top-level key)",
			map[string]any{"Permission": map[string]any{"changes": []any{
				map[string]any{"operation": "set", "mode": "acceptEdits"},
			}}},
			false,
		},
		{
			"sibling key beside empty changes (bypass shape: mode rides along)",
			map[string]any{"permission": map[string]any{"mode": "acceptEdits", "changes": []any{}}},
			false,
		},
		{
			"two case-differing permission keys both present",
			map[string]any{
				"permission": map[string]any{"changes": []any{}},
				"Permission": map[string]any{"changes": []any{}},
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{OnPermission: func(context.Context, PermissionRequest) (PermissionDecision, error) {
				return DecisionAllow, nil
			}}

			opt := sdkacp.PermissionOption{
				Kind: sdkacp.PermissionOptionKindAllowOnce, Name: "Allow Once", OptionId: "opt", Meta: tt.meta,
			}
			resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
				SessionId: "sess-1", Options: []sdkacp.PermissionOption{opt},
			})

			require.NoError(t, err)
			if tt.selected {
				require.NotNil(t, resp.Outcome.Selected)
				require.Equal(t, sdkacp.PermissionOptionId("opt"), resp.Outcome.Selected.OptionId)
			} else {
				require.Nil(t, resp.Outcome.Selected)
				require.NotNil(t, resp.Outcome.Cancelled)
			}
		})
	}
}

func TestRequestPermissionWithoutGateDeniesTransiently(t *testing.T) {
	// A missing gate is a wiring fault, not a verdict: reject_always would let
	// the agent remember it and stop asking for the rest of the session.
	c := &Client{}

	resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		// Only reject_always is offered: a transient deny must refuse it and
		// cancel, while a substantive one would take it. Offering reject_once
		// too makes both classes pick that, which discriminates nothing.
		Options: []sdkacp.PermissionOption{
			{Kind: sdkacp.PermissionOptionKindRejectAlways, Name: "Always reject", OptionId: "reject-always"},
		},
		ToolCall: sdkacp.ToolCallUpdate{ToolCallId: "tc-1"},
	})

	require.NoError(t, err)
	require.Nil(t, resp.Outcome.Selected, "a transient deny must not select reject_always")
	require.NotNil(t, resp.Outcome.Cancelled)
}
