package blockchain

import (
	"bytes"
	"context"
	"errors"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-ssz"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/blocks"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/beacon-chain/state/stateutil"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/params"
)

// ReceiveShardBlock receives and processes an incoming shard block.
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
//    assert verify_shard_block_message(beacon_parent_state, shard_parent_state, shard_block)
//    assert verify_shard_block_signature(beacon_parent_state, signed_shard_block)
//
//    post_state = get_post_shard_state(shard_parent_state, shard_block)
//
//    # Add new block to the store
//    shard_store.blocks[hash_tree_root(shard_block)] = shard_block
//
//    # Add new state for this block to the store
//    shard_store.block_states[hash_tree_root(shard_block)] = post_state
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

	bps, err := s.beaconDB.State(ctx, bpr)
	if err != nil {
		return err
	}
	sps, err := s.beaconDB.ShardState(ctx, spr)
	if err != nil {
		return err
	}
	verified, err := verifyShardBlockMessage(ctx, bps, sps, block.Message)
	if err != nil {
		return err
	}
	if !verified {
		return errors.New("could not verify shard block message")
	}
	verified, err = verifyShardBlockSignature(bps, block)
	if err != nil {
		return err
	}
	if !verified {
		return errors.New("could not verify shard block signature")
	}

	shardState, err := blocks.PostShardState(sps, block.Message)
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

// This validates shard block contents.
//
// Spec code (https://github.com/ethereum/eth2.0-specs/blob/7a770186b5ba576bf14ce496dc2b0381d169840e/specs/phase1/shard-transition.md):
// def verify_shard_block_message(beacon_parent_state: BeaconState,
//                               shard_parent_state: ShardState,
//                               block: ShardBlock) -> bool:
//    # Check `shard_parent_root` field
//    assert block.shard_parent_root == shard_parent_state.latest_block_root
//    # Check `beacon_parent_root` field
//    beacon_parent_block_header = beacon_parent_state.latest_block_header.copy()
//    if beacon_parent_block_header.state_root == Root():
//        beacon_parent_block_header.state_root = hash_tree_root(beacon_parent_state)
//    beacon_parent_root = hash_tree_root(beacon_parent_block_header)
//    assert block.beacon_parent_root == beacon_parent_root
//    # Check `slot` field
//    shard = block.shard
//    next_slot = Slot(block.slot + 1)
//    offset_slots = compute_offset_slots(get_latest_slot_for_shard(beacon_parent_state, shard), next_slot)
//    assert block.slot in offset_slots
//    # Check `shard` field
//    assert block.shard == shard
//    # Check `proposer_index` field
//    assert block.proposer_index == get_shard_proposer_index(beacon_parent_state, block.slot, shard)
//    # Check `body` field
//    assert 0 < len(block.body) <= MAX_SHARD_BLOCK_SIZE
//    return True
func verifyShardBlockMessage(ctx context.Context, beaconParentState *state.BeaconState, shardParentState *ethpb.ShardState, shardBlock *ethpb.ShardBlock) (bool, error) {
	if !bytes.Equal(shardParentState.LatestBlockRoot, shardBlock.ShardParentRoot) {
		return false, nil
	}

	bh := state.CopyBeaconBlockHeader(beaconParentState.LatestBlockHeader())
	if bytesutil.ToBytes32(bh.StateRoot) == params.BeaconConfig().ZeroHash {
		r, err := beaconParentState.HashTreeRoot(ctx)
		if err != nil {
			return false, err
		}
		bh.StateRoot = r[:]
	}
	bhr, err := stateutil.BlockHeaderRoot(bh)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(bhr[:], shardBlock.BeaconParentRoot) {
		return false, nil
	}

	offsetSlots := helpers.ComputeOffsetSlots(beaconParentState.Slot(), shardBlock.Slot+1)
	valid := false
	for _, s := range offsetSlots {
		if shardBlock.Slot == s {
			valid = true
		}
	}
	if !valid {
		return false, nil
	}

	shardProposerIndex, err := helpers.ShardProposerIndex(beaconParentState, shardBlock.Slot, shardBlock.Shard)
	if err != nil {
		return false, err
	}
	if shardBlock.ProposerIndex != shardProposerIndex {
		return false, nil
	}

	if len(shardBlock.Body) == 0 || uint64(len(shardBlock.Body)) > params.ShardConfig().MaxShardBlockSize {
		return false, nil
	}

	return true, nil
}

// This validates shard block signature.
//
// Spec code (https://github.com/ethereum/eth2.0-specs/blob/7a770186b5ba576bf14ce496dc2b0381d169840e/specs/phase1/shard-transition.md):
// def verify_shard_block_signature(beacon_state: BeaconState,
//                                 signed_block: SignedShardBlock) -> bool:
//    proposer = beacon_state.validators[signed_block.message.proposer_index]
//    domain = get_domain(beacon_state, DOMAIN_SHARD_PROPOSAL, compute_epoch_at_slot(signed_block.message.slot))
//    signing_root = compute_signing_root(signed_block.message, domain)
//    return bls.Verify(proposer.pubkey, signing_root, signed_block.signature)
func verifyShardBlockSignature(beaconState *state.BeaconState, shardBlock *ethpb.SignedShardBlock) (bool, error) {
	proposer, err := beaconState.ValidatorAtIndex(shardBlock.Message.ProposerIndex)
	if err != nil {
		return false, err
	}
	domain, err := helpers.Domain(beaconState.Fork(), helpers.SlotToEpoch(shardBlock.Message.Slot), params.ShardConfig().DomainShardProposal, beaconState.GenesisValidatorRoot())
	if err != nil {
		return false, err
	}
	signingRoot, err := helpers.ComputeSigningRoot(shardBlock.Message, domain)
	if err != nil {
		return false, err
	}
	pubKey, err := bls.PublicKeyFromBytes(proposer.PublicKey)
	if err != nil {
		return false, err
	}
	sig, err := bls.SignatureFromBytes(shardBlock.Signature)
	if err != nil {
		return false, err
	}

	return sig.Verify(pubKey, signingRoot[:]), nil
}
