package upstreampricing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamPricingCredentialSurvivesRestart(t *testing.T) {
	first, err := NewCredentialCipher("synthetic-persisted-secret", nil)
	require.NoError(t, err)
	sealed, err := first.Encrypt("synthetic-panel-token")
	require.NoError(t, err)
	require.NotContains(t, sealed, "synthetic-panel-token")
	second, err := NewCredentialCipher("synthetic-persisted-secret", nil)
	require.NoError(t, err)
	token, err := second.Decrypt(sealed)
	require.NoError(t, err)
	require.Equal(t, "synthetic-panel-token", token)
}
