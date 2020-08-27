package blockchain

import (
	"context"
	"errors"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-ssz"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/blocks"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
)

// ReceiveShardBlock receives and processes an incoming shard block.
//
// Spec code:
// def on_shard_block(store: Store, shard_store: ShardStore, signed_shard_block: SignedShardBlock) -> None:
//    shard_block = signed_shard_block.message
//    shard = shard_store.shard
//
//    # Check shard
//    # TODO: check it in networking spec
//    assert shard_block.shard == shard
//
//    # Check shard parent exists
//    assert shard_block.shard_parent_root in shard_store.block_states
//    shard_parent_state = shard_store.block_states[shard_block.shard_parent_root]
//
//    # Check beacon parent exists
//    assert shard_block.beacon_parent_root in store.block_states
//    beacon_parent_state = store.block_states[shard_block.beacon_parent_root]
//
//    # Check that block is later than the finalized shard state slot (optimization to reduce calls to get_ancestor)
//    finalized_beacon_state = store.block_states[store.finalized_checkpoint.root]
//    finalized_shard_state = finalized_beacon_state.shard_states[shard]
//    assert shard_block.slot > finalized_shard_state.slot
//
//    # Check block is a descendant of the finalized block at the checkpoint finalized slot
//    finalized_slot = compute_start_slot_at_epoch(store.finalized_checkpoint.epoch)
//    assert (
//        get_ancestor(store, shard_block.beacon_parent_root, finalized_slot) == store.finalized_checkpoint.root
//    )
//
//    # Check the block is valid and compute the post-state
//    shard_state = shard_parent_state.copy()
//    shard_state_transition(
//        shard_state, signed_shard_block,
//        validate=True, beacon_parent_state=beacon_parent_state)
//
//    # Add new block to the store
//    shard_store.blocks[hash_tree_root(shard_block)] = shard_block
//
//    # Add new state for this block to the store
//    shard_store.block_states[hash_tree_root(shard_block)] = shard_state
func (s *Service) ReceiveShardBlock(ctx context.Context, block *ethpb.SignedShardBlock) error {
	spr := bytesutil.ToBytes32(block.Message.ShardParentRoot)
	if !s.beaconDB.HasShardState(ctx, spr) {
		return errors.New("could not find shard parent state")
	}
	bpr := bytesutil.ToBytes32(block.Message.BeaconParentRoot)
	if !s.beaconDB.HasState(ctx, bpr) {
		return errors.New("could not find beacon parent state")
	}
	fRoot := bytesutil.ToBytes32(s.finalizedCheckpt.Root)
	// TODO(0): If this is too slow, we can consider block.
	fState, err := s.stateGen.StateByRoot(ctx, fRoot)
	if err != nil {
		return err
	}
	if fState.Slot() >= block.Message.Slot {
		return errors.New("shard block slot is less than finalized slot")
	}

	fSlot := helpers.StartSlot(s.finalizedCheckpt.Epoch)
	aRoot, err := s.ancestor(ctx, bpr[:], fSlot)
	if err != nil {
		return err
	}
	if bytesutil.ToBytes32(aRoot) != fRoot {
		return errors.New("finalized block roots don't match")
	}

	sps, err := s.beaconDB.ShardState(ctx, spr)
	if err != nil {
		return err
	}
	copiedSps := state.CopyShardState(sps)
	bps, err := s.beaconDB.State(ctx, bpr)
	if err != nil {
		return err
	}
	shardState, err := blocks.ShardStateTransition(ctx, bps, copiedSps, block)
	if err != nil {
		return err
	}

	if err := s.beaconDB.SaveShardBlock(ctx, block); err != nil {
		return err
	}
	sbr, err := ssz.HashTreeRoot(block.Message)
	if err != nil {
		return err
	}
	if err := s.beaconDB.SaveShardState(ctx, shardState, sbr); err != nil {
		return err
	}

	return nil
}
