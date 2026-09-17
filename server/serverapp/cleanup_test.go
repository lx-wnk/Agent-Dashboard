package serverapp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChainCleanup_RunsBothNewestFirst(t *testing.T) {
	var order []string
	prev := func() { order = append(order, "prev") }
	next := func() { order = append(order, "next") }

	chainCleanup(prev, next)()

	require.Equal(t, []string{"next", "prev"}, order, "a later shutdown step must not drop an earlier one")
}
