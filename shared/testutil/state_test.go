package testutil

import (
	"context"
	"reflect"
	"testing"

	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
)

func TestNewBeaconState(t *testing.T) {
	st := NewBeaconState()
	b, err := st.InnerStateUnsafe().MarshalSSZ()
	require.NoError(t, err)
	got := &pb.BeaconState{}
	require.NoError(t, got.UnmarshalSSZ(b))
	if !reflect.DeepEqual(st.InnerStateUnsafe(), got) {
		t.Fatal("State did not match after round trip marshal")
	}
}

func TestNewBeaconState_HashTreeRoot(t *testing.T) {
	_, err := st.HashTreeRoot(context.Background())
	require.NoError(t, err)
	state := NewBeaconState()
	_, err = state.HashTreeRoot(context.Background())
	require.NoError(t, err)
}
