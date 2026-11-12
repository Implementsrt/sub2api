package upstreampricing

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestComparisonEffectivePriceExactAndUnknown(t *testing.T) {
	input, cache, request := "0.000000000000000001", "0", "2.5"
	p := Price{Mode: "token", Input: &input, CacheRead: &cache, Request: &request}
	effective, err := effectivePrice(p, "0.000000000000000001")
	require.NoError(t, err)
	require.Equal(t, "0.000000000000000000000000000000000001", *effective.Input)
	require.Equal(t, "0", *effective.CacheRead)
	require.Nil(t, effective.Output)
	require.Equal(t, "0.0000000000000000025", *effective.Request)
	require.Equal(t, input, *p.Input)
	_, err = effectivePrice(p, "invalid")
	require.Error(t, err)
	unavailable, err := effectivePrice(Price{Mode: "unavailable", Reason: "missing price"}, "0.5")
	require.NoError(t, err)
	require.Nil(t, unavailable.Input)
}
