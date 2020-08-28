package kv

import (
	"context"
	"flag"
	"testing"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/testutil/assert"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
	"github.com/urfave/cli/v2"
)

func TestNilDBHistoryBlkHdr(t *testing.T) {
	app := cli.App{}
	set := flag.NewFlagSet("test", 0)
	db := setupDB(t, cli.NewContext(&app, set, nil))
	ctx := context.Background()

	slot := uint64(1)
	validatorID := uint64(1)

	require.Equal(t, false, db.HasBlockHeader(ctx, slot, validatorID))

	bPrime, err := db.BlockHeaders(ctx, slot, validatorID)
	require.NoError(t, err, "Failed to get block")
	require.DeepEqual(t, ([]*ethpb.SignedBeaconBlockHeader)(nil), bPrime, "Should return nil for a non existent key")
}

func TestSaveHistoryBlkHdr(t *testing.T) {
	app := cli.App{}
	set := flag.NewFlagSet("test", 0)
	db := setupDB(t, cli.NewContext(&app, set, nil))
	ctx := context.Background()

	tests := []struct {
		bh *ethpb.SignedBeaconBlockHeader
	}{
		{
			bh: &ethpb.SignedBeaconBlockHeader{Signature: bytesutil.PadTo([]byte("let me in"), 96), Header: &ethpb.BeaconBlockHeader{Slot: 0, ProposerIndex: 0}},
		},
		{
			bh: &ethpb.SignedBeaconBlockHeader{Signature: bytesutil.PadTo([]byte("let me in 2nd"), 96), Header: &ethpb.BeaconBlockHeader{Slot: 0, ProposerIndex: 1}},
		},
		{
			bh: &ethpb.SignedBeaconBlockHeader{Signature: bytesutil.PadTo([]byte("let me in 3rd"), 96), Header: &ethpb.BeaconBlockHeader{Slot: params.BeaconConfig().SlotsPerEpoch + 1, ProposerIndex: 0}},
		},
		{
			bh: &ethpb.SignedBeaconBlockHeader{Signature: bytesutil.PadTo([]byte("let me in 3rd"), 96), Header: &ethpb.BeaconBlockHeader{Slot: 1, ProposerIndex: 0}},
		},
	}

	for _, tt := range tests {
		err := db.SaveBlockHeader(ctx, tt.bh)
		require.NoError(t, err, "Save block failed")

		bha, err := db.BlockHeaders(ctx, tt.bh.Header.Slot, tt.bh.Header.ProposerIndex)
		require.NoError(t, err, "Failed to get block")
		require.NotNil(t, bha)
		require.DeepEqual(t, tt.bh, bha[0], "Should return bh")
	}
}

func TestDeleteHistoryBlkHdr(t *testing.T) {
	app := cli.App{}
	set := flag.NewFlagSet("test", 0)
	db := setupDB(t, cli.NewContext(&app, set, nil))
	ctx := context.Background()

	tests := []struct {
		bh *ethpb.SignedBeaconBlockHeader
	}{
		{
			bh: &ethpb.SignedBeaconBlockHeader{Signature: bytesutil.PadTo([]byte("let me in"), 96), Header: &ethpb.BeaconBlockHeader{Slot: 0, ProposerIndex: 0}},
		},
		{
			bh: &ethpb.SignedBeaconBlockHeader{Signature: bytesutil.PadTo([]byte("let me in 2nd"), 96), Header: &ethpb.BeaconBlockHeader{Slot: 0, ProposerIndex: 1}},
		},
		{
			bh: &ethpb.SignedBeaconBlockHeader{Signature: bytesutil.PadTo([]byte("let me in 3rd"), 96), Header: &ethpb.BeaconBlockHeader{Slot: params.BeaconConfig().SlotsPerEpoch + 1, ProposerIndex: 0}},
		},
	}
	for _, tt := range tests {
		err := db.SaveBlockHeader(ctx, tt.bh)
		require.NoError(t, err, "Save block failed")
	}

	for _, tt := range tests {
		bha, err := db.BlockHeaders(ctx, tt.bh.Header.Slot, tt.bh.Header.ProposerIndex)
		require.NoError(t, err, "Failed to get block")
		require.NotNil(t, bha)
		require.DeepEqual(t, tt.bh, bha[0], "Should return bh")

		err = db.DeleteBlockHeader(ctx, tt.bh)
		require.NoError(t, err, "Save block failed")
		bh, err := db.BlockHeaders(ctx, tt.bh.Header.Slot, tt.bh.Header.ProposerIndex)
		require.NoError(t, err)
		assert.DeepEqual(t, ([]*ethpb.SignedBeaconBlockHeader)(nil), bh, "Expected block to have been deleted")
	}
}

func TestHasHistoryBlkHdr(t *testing.T) {
	app := cli.App{}
	set := flag.NewFlagSet("test", 0)
	db := setupDB(t, cli.NewContext(&app, set, nil))
	ctx := context.Background()

	tests := []struct {
		bh *ethpb.SignedBeaconBlockHeader
	}{
		{
			bh: &ethpb.SignedBeaconBlockHeader{Signature: bytesutil.PadTo([]byte("let me in"), 96), Header: &ethpb.BeaconBlockHeader{Slot: 0, ProposerIndex: 0}},
		},
		{
			bh: &ethpb.SignedBeaconBlockHeader{Signature: bytesutil.PadTo([]byte("let me in 2nd"), 96), Header: &ethpb.BeaconBlockHeader{Slot: 0, ProposerIndex: 1}},
		},
		{
			bh: &ethpb.SignedBeaconBlockHeader{Signature: bytesutil.PadTo([]byte("let me in 3rd"), 96), Header: &ethpb.BeaconBlockHeader{Slot: params.BeaconConfig().SlotsPerEpoch + 1, ProposerIndex: 0}},
		},
		{
			bh: &ethpb.SignedBeaconBlockHeader{Signature: bytesutil.PadTo([]byte("let me in 4th"), 96), Header: &ethpb.BeaconBlockHeader{Slot: 1, ProposerIndex: 0}},
		},
	}
	for _, tt := range tests {
		found := db.HasBlockHeader(ctx, tt.bh.Header.Slot, tt.bh.Header.ProposerIndex)
		require.Equal(t, false, found, "Has block header should return false for block headers that are not in db")
		err := db.SaveBlockHeader(ctx, tt.bh)
		require.NoError(t, err, "Save block failed")
	}
	for _, tt := range tests {
		err := db.SaveBlockHeader(ctx, tt.bh)
		require.NoError(t, err, "Save block failed")

		found := db.HasBlockHeader(ctx, tt.bh.Header.Slot, tt.bh.Header.ProposerIndex)
		require.Equal(t, true, found, "Block header should exist")
	}
}

func TestPruneHistoryBlkHdr(t *testing.T) {
	app := cli.App{}
	set := flag.NewFlagSet("test", 0)
	db := setupDB(t, cli.NewContext(&app, set, nil))
	ctx := context.Background()

	tests := []struct {
		bh *ethpb.SignedBeaconBlockHeader
	}{
		{
			bh: &ethpb.SignedBeaconBlockHeader{Signature: bytesutil.PadTo([]byte("let me in"), 96), Header: &ethpb.BeaconBlockHeader{Slot: 0, ProposerIndex: 0}},
		},
		{
			bh: &ethpb.SignedBeaconBlockHeader{Signature: bytesutil.PadTo([]byte("let me in 2nd"), 96), Header: &ethpb.BeaconBlockHeader{Slot: 0, ProposerIndex: 1}},
		},
		{
			bh: &ethpb.SignedBeaconBlockHeader{Signature: bytesutil.PadTo([]byte("let me in 3rd"), 96), Header: &ethpb.BeaconBlockHeader{Slot: params.BeaconConfig().SlotsPerEpoch + 1, ProposerIndex: 0}},
		},
		{
			bh: &ethpb.SignedBeaconBlockHeader{Signature: bytesutil.PadTo([]byte("let me in 4th"), 96), Header: &ethpb.BeaconBlockHeader{Slot: params.BeaconConfig().SlotsPerEpoch*2 + 1, ProposerIndex: 0}},
		},
		{
			bh: &ethpb.SignedBeaconBlockHeader{Signature: bytesutil.PadTo([]byte("let me in 5th"), 96), Header: &ethpb.BeaconBlockHeader{Slot: params.BeaconConfig().SlotsPerEpoch*3 + 1, ProposerIndex: 0}},
		},
	}

	for _, tt := range tests {
		err := db.SaveBlockHeader(ctx, tt.bh)
		require.NoError(t, err, "Save block header failed")

		bha, err := db.BlockHeaders(ctx, tt.bh.Header.Slot, tt.bh.Header.ProposerIndex)
		require.NoError(t, err, "Failed to get block header")
		require.NotNil(t, bha)
		require.DeepEqual(t, tt.bh, bha[0], "Should return bh")
	}
	currentEpoch := uint64(3)
	historyToKeep := uint64(2)
	err := db.PruneBlockHistory(ctx, currentEpoch, historyToKeep)
	require.NoError(t, err, "Failed to prune")

	for _, tt := range tests {
		bha, err := db.BlockHeaders(ctx, tt.bh.Header.Slot, tt.bh.Header.ProposerIndex)
		require.NoError(t, err, "Failed to get block header")
		if helpers.SlotToEpoch(tt.bh.Header.Slot) >= currentEpoch-historyToKeep {
			require.NotNil(t, bha)
			require.DeepEqual(t, tt.bh, bha[0], "Should return bh")
		} else {
			require.NotNil(t, bha, "Block header should have been pruned")
		}
	}
}
