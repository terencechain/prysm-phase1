package testing

import (
	"testing"

	"github.com/prysmaticlabs/prysm/shared/testutil/require"
)

func TestGetBeaconFuzzState(t *testing.T) {
<<<<<<< HEAD
	t.Skip("Skipping for phase 1")
	if _, err := GetBeaconFuzzState(1); err != nil {
		t.Fatal(err)
	}
=======
	_, err := GetBeaconFuzzState(1)
	require.NoError(t, err)
>>>>>>> upstream/master
}
