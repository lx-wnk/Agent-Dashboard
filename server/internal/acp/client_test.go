package acp

import (
	"testing"

	sdkacp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

func TestClientSatisfiesSDKInterface(t *testing.T) {
	var c any = &Client{}
	_, ok := c.(sdkacp.Client)
	require.True(t, ok, "Client must implement sdkacp.Client")
}
