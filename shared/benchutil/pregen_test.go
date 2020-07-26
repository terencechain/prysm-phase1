package benchutil

import (
	"testing"

	"github.com/prysmaticlabs/prysm/shared/testutil/require"
)

func TestPreGenFullBlock(t *testing.T) {
	t.Skip("Skipping for phase 1")
	_, err := PreGenFullBlock()
	require.NoError(t, err)
}

func TestPreGenState1Epoch(t *testing.T) {
	t.Skip("Skipping for phase 1")
	_, err := PreGenFullBlock()
	require.NoError(t, err)
}

func TestPreGenState2FullEpochs(t *testing.T) {
	t.Skip("Skipping for phase 1")
	_, err := PreGenFullBlock()
	require.NoError(t, err)
}
