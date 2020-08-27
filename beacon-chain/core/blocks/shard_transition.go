package blocks

import (
	"bytes"
	"context"
	"fmt"

	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-ssz"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/epoch"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	stateTrie "github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/beacon-chain/state/stateutil"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/attestationutil"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/sliceutil"
)

// ShardStateTransition processes shard state transition.
// Note: This diverges from spec implementation. There's no `validate_result`.
//
// Spec code:
// def shard_state_transition(shard_state: ShardState,
//                           signed_block: SignedShardBlock,
//                           beacon_parent_state: BeaconState,
//                           validate_result: bool = True) -> ShardState:
//    assert verify_shard_block_message(beacon_parent_state, shard_state, signed_block.message)
//
//    if validate_result:
//        assert verify_shard_block_signature(beacon_parent_state, signed_block)
//
//    process_shard_block(shard_state, signed_block.message)
//    return shard_state
func ShardStateTransition(ctx context.Context, bps *stateTrie.BeaconState, shardState *ethpb.ShardState, block *ethpb.SignedShardBlock) (*ethpb.ShardState, error) {
	verified, err := verifyShardBlockMessage(ctx, bps, shardState, block.Message)
	if err != nil {
		return nil, err
	}
	if !verified {
		return nil, errors.New("could not verify shard block message")
	}
	if err := verifyShardBlockSignature(bps, block); err != nil {
		return nil, errors.Wrap(err, "could not verify shard block signature")
	}

	return ProcessShardBlock(shardState, block.Message)
}

// ProcessShardBlock processes shard state transition and returns the mutated post state.
//
// Spec code:
// def process_shard_block(shard_state: ShardState,
//                        block: ShardBlock) -> None:
//    """
//    Update ``shard_state`` with shard ``block``.
//    """
//    shard_state.slot = block.slot
//    prev_gasprice = shard_state.gasprice
//    shard_block_length = len(block.body)
//    shard_state.gasprice = compute_updated_gasprice(prev_gasprice, uint64(shard_block_length))
//    if shard_block_length != 0:
//        shard_state.latest_block_root = hash_tree_root(block)
func ProcessShardBlock(shardState *ethpb.ShardState, shardBlock *ethpb.ShardBlock) (*ethpb.ShardState, error) {
	shardState.Slot = shardBlock.Slot
	shardState.GasPrice = helpers.UpdatedGasPrice(shardState.GasPrice, uint64(len(shardBlock.Body)))

	if len(shardBlock.Body) != 0 {
		root, err := ssz.HashTreeRoot(shardBlock)
		if err != nil {
			return nil, err
		}
		shardState.LatestBlockRoot = root[:]
	}

	return shardState, nil
}

// ProcessShardTransitions processes shard transitions of the beacon block,
// the attestations are used to determine shard transition validity.
//
// Spec code:
// def process_shard_transitions(state: BeaconState,
//                              shard_transitions: Sequence[ShardTransition],
//                              attestations: Sequence[Attestation]) -> None:
//    # Process crosslinks
//    process_crosslinks(state, shard_transitions, attestations)
//    # Verify the empty proposal shard states
//    assert verify_empty_shard_transition(state, shard_transitions)
func ProcessShardTransitions(
	beaconState *stateTrie.BeaconState,
	shardTransitions []*ethpb.ShardTransition,
	attestations []*ethpb.Attestation) (
	*stateTrie.BeaconState, error) {

	beaconState, err := processCrosslinks(beaconState, shardTransitions, attestations)
	if err != nil {
		return nil, err
	}

	if !verifyEmptyShardTransition(beaconState, shardTransitions) {
		return nil, errors.New("failed to verify empty shard transition")
	}

	return beaconState, nil
}

// This applies shard transition to the beacon state.
//
// Spec code:
// def apply_shard_transition(state: BeaconState, shard: Shard, transition: ShardTransition) -> None:
//    # TODO: only need to check it once when phase 1 starts
//    assert state.slot > PHASE_1_FORK_SLOT
//
//    # Correct data root count
//    offset_slots = get_offset_slots(state, shard)
//    assert (
//        len(transition.shard_data_roots)
//        == len(transition.shard_states)
//        == len(transition.shard_block_lengths)
//        == len(offset_slots)
//    )
//    assert transition.start_slot == offset_slots[0]
//
//    headers = []
//    proposers = []
//    prev_gasprice = state.shard_states[shard].gasprice
//    shard_parent_root = state.shard_states[shard].latest_block_root
//    for i, offset_slot in enumerate(offset_slots):
//        shard_block_length = transition.shard_block_lengths[i]
//        shard_state = transition.shard_states[i]
//        # Verify correct calculation of gas prices and slots
//        assert shard_state.gasprice == compute_updated_gasprice(prev_gasprice, shard_block_length)
//        assert shard_state.slot == offset_slot
//        # Collect the non-empty proposals result
//        is_empty_proposal = shard_block_length == 0
//        if not is_empty_proposal:
//            proposal_index = get_shard_proposer_index(state, offset_slot, shard)
//            # Reconstruct shard headers
//            header = ShardBlockHeader(
//                shard_parent_root=shard_parent_root,
//                beacon_parent_root=get_block_root_at_slot(state, offset_slot),
//                slot=offset_slot,
//                shard=shard,
//                proposer_index=proposal_index,
//                body_root=transition.shard_data_roots[i]
//            )
//            shard_parent_root = hash_tree_root(header)
//            headers.append(header)
//            proposers.append(proposal_index)
//        else:
//            # Must have a stub for `shard_data_root` if empty slot
//            assert transition.shard_data_roots[i] == Root()
//
//        prev_gasprice = shard_state.gasprice
//
//    pubkeys = [state.validators[proposer].pubkey for proposer in proposers]
//    signing_roots = [
//        compute_signing_root(header, get_domain(state, DOMAIN_SHARD_PROPOSAL, compute_epoch_at_slot(header.slot)))
//        for header in headers
//    ]
//    # Verify combined proposer signature
//    assert optional_aggregate_verify(pubkeys, signing_roots, transition.proposer_signature_aggregate)
//
//    # Save updated state
//    state.shard_states[shard] = transition.shard_states[len(transition.shard_states) - 1]
//    state.shard_states[shard].slot = compute_previous_slot(state.slot)
func applyShardTransition(beaconState *stateTrie.BeaconState, transition *ethpb.ShardTransition, shard uint64) (*stateTrie.BeaconState, error) {
	if params.BeaconConfig().Phase1GenesisSlot >= beaconState.Slot() {
		return nil, errors.New("phase1 genesis slot can not be greater than beacon slot")
	}

	offsetSlots := helpers.ShardOffSetSlots(beaconState, shard)
	if err := verifyShardDataRootLength(offsetSlots, transition); err != nil {
		return nil, err
	}

	headers, pIndices, err := shardBlockProposersAndHeaders(beaconState, transition, offsetSlots, shard)
	if err != nil {
		return nil, err
	}

	// Verify aggregated proposer signatures.
	if err := verifyProposerSignature(beaconState, headers, pIndices, transition.ProposerSignatureAggregate); err != nil {
		return nil, err
	}

	// Save shard state in beacon state and handle shard skip slot scenario.
	currentShardState := transition.ShardStates[len(transition.ShardStates)-1]
	// TODO(0): https://github.com/ethereum/eth2.0-specs/pull/2013
	currentShardState.Slot = beaconState.Slot() - 1
	if err := beaconState.SetShardStateAtIndex(shard, currentShardState); err != nil {
		return nil, err
	}

	return beaconState, nil
}

func verifyProposerSignature(beaconState *stateTrie.BeaconState, headers []*ethpb.ShardBlockHeader, pIndices []uint64, sig []byte) error {
	pks := make([]bls.PublicKey, len(pIndices))
	for i, index := range pIndices {
		pkAtIndex := beaconState.PubkeyAtIndex(index)
		p, err := bls.PublicKeyFromBytes(pkAtIndex[:])
		if err != nil {
			return err
		}
		pks[i] = p
	}
	msgs := make([][32]byte, len(pIndices))
	for i, header := range headers {
		d, err := helpers.Domain(beaconState.Fork(), helpers.SlotToEpoch(header.Slot), params.BeaconConfig().DomainShardProposal, beaconState.GenesisValidatorRoot())
		if err != nil {
			return err
		}
		r, err := helpers.ComputeSigningRoot(header, d)
		if err != nil {
			return err
		}
		msgs[i] = r
	}
	s, err := bls.SignatureFromBytes(sig)
	if err != nil {
		return err
	}
	if !s.AggregateVerify(pks, msgs) {
		return errors.New("could not verify aggregated proposer signature")
	}
	return nil
}

// verifyShardDataRootLength verifies shard transition is consistent with offset slots for length.
func verifyShardDataRootLength(offsetSlots []uint64, transition *ethpb.ShardTransition) error {
	if len(transition.ShardDataRoots) != len(transition.ShardBlockLengths) {
		return errors.New("data roots length != shard blocks length")
	}
	if len(transition.ShardDataRoots) != len(transition.ShardStates) {
		return errors.New("data roots length != shard states length")
	}
	if len(transition.ShardDataRoots) != len(offsetSlots) {
		return errors.New("data roots length != offset length")
	}
	if offsetSlots[0] != transition.StartSlot {
		return errors.New("offset start slot != transition start slot")
	}
	return nil
}

// This processes the crosslinks of a given shard, if there's a successful crosslink, the winning transition root will be return.
// If there's no successful crosslink, an empty root will be returned.
//
// processCrosslinkForShard processes crosslink of an individual shard.
//
// Spec code:
// def process_crosslink_for_shard(state: BeaconState,
//                                committee_index: CommitteeIndex,
//                                shard_transition: ShardTransition,
//                                attestations: Sequence[Attestation]) -> Root:
//    on_time_attestation_slot = compute_previous_slot(state.slot)
//    committee = get_beacon_committee(state, on_time_attestation_slot, committee_index)
//    online_indices = get_online_validator_indices(state)
//    shard = compute_shard_from_committee_index(state, committee_index, on_time_attestation_slot)
//
//    # Loop over all shard transition roots
//    shard_transition_roots = set([a.data.shard_transition_root for a in attestations])
//    for shard_transition_root in sorted(shard_transition_roots):
//        transition_attestations = [a for a in attestations if a.data.shard_transition_root == shard_transition_root]
//        transition_participants: Set[ValidatorIndex] = set()
//        for attestation in transition_attestations:
//            participants = get_attesting_indices(state, attestation.data, attestation.aggregation_bits)
//            transition_participants = transition_participants.union(participants)
//
//        enough_online_stake = (
//            get_total_balance(state, online_indices.intersection(transition_participants)) * 3 >=
//            get_total_balance(state, online_indices.intersection(committee)) * 2
//        )
//        # If not enough stake, try next transition root
//        if not enough_online_stake:
//            continue
//
//        # Attestation <-> shard transition consistency
//        assert shard_transition_root == hash_tree_root(shard_transition)
//
//        # Check `shard_head_root` of the winning root
//        last_offset_index = len(shard_transition.shard_states) - 1
//        shard_head_root = shard_transition.shard_states[last_offset_index].latest_block_root
//        for attestation in transition_attestations:
//            assert attestation.data.shard_head_root == shard_head_root
//
//        # Apply transition
//        apply_shard_transition(state, shard, shard_transition)
//        # Apply proposer reward and cost
//        beacon_proposer_index = get_beacon_proposer_index(state)
//        estimated_attester_reward = sum([get_base_reward(state, attester) for attester in transition_participants])
//        proposer_reward = Gwei(estimated_attester_reward // PROPOSER_REWARD_QUOTIENT)
//        increase_balance(state, beacon_proposer_index, proposer_reward)
//        states_slots_lengths = zip(
//            shard_transition.shard_states,
//            get_offset_slots(state, shard),
//            shard_transition.shard_block_lengths
//        )
//        for shard_state, slot, length in states_slots_lengths:
//            proposer_index = get_shard_proposer_index(state, slot, shard)
//            decrease_balance(state, proposer_index, shard_state.gasprice * length)
//
//        # Return winning transition root
//        return shard_transition_root
//
//    # No winning transition root, ensure empty and return empty root
//    assert shard_transition == ShardTransition()
//    return Root()
func processCrosslinkForShard(bs *stateTrie.BeaconState, attestations []*ethpb.Attestation, transition *ethpb.ShardTransition, committeeID uint64) ([32]byte, error) {
	onTimeSlot := helpers.PrevSlot(bs.Slot())
	shard, err := helpers.ShardFromCommitteeIndex(bs, bs.Slot(), committeeID)
	if err != nil {
		return [32]byte{}, err
	}

	bc, err := helpers.BeaconCommitteeFromState(bs, onTimeSlot, committeeID)
	if err != nil {
		return [32]byte{}, err
	}

	attsByTRoot := helpers.AttsByTransitionRoot(attestations)
	for tRoot, atts := range attsByTRoot {
		enough, tParticipants, err := CanCrosslink(bs, atts, bc)
		if err != nil {
			return [32]byte{}, err
		}
		if !enough {
			continue
		}
		if err := verifyAttShardHeadRoot(transition, atts); err != nil {
			return [32]byte{}, err
		}
		if err := verifyAttTransitionRoot(transition, tRoot); err != nil {
			return [32]byte{}, err
		}
		bs, err = applyShardTransition(bs, transition, shard)
		if err != nil {
			return [32]byte{}, err
		}
		bs, err = incBeaconProposerBal(bs, tParticipants)
		if err != nil {
			return [32]byte{}, err
		}
		bs, err = decShardProposerBal(bs, transition, shard)
		if err != nil {
			return [32]byte{}, err
		}
		return tRoot, nil
	}

	if helpers.IsEmptyShardTransition(transition) {
		return [32]byte{}, errors.New("did not get empty transition with no winning root")
	}

	return [32]byte{}, nil
}

func verifyAttTransitionRoot(transition *ethpb.ShardTransition, tRoot [32]byte) error {
	r, err := ssz.HashTreeRoot(transition)
	if err != nil {
		return err
	}
	if tRoot != r {
		return errors.New("transition root missmatch")
	}
	return nil
}

func verifyAttShardHeadRoot(transition *ethpb.ShardTransition, atts []*ethpb.Attestation) error {
	lastOffset := len(transition.ShardStates) - 1
	lbr := transition.ShardStates[lastOffset].LatestBlockRoot
	for _, a := range atts {
		if !bytes.Equal(a.Data.ShardHeadRoot, lbr) {
			return errors.New("attestation shard head root is not consistent with shard state")
		}
	}
	return nil
}

// This processes crosslinks for beacon block's shard transitions and attestations.
//
// Spec code:
// def process_crosslinks(state: BeaconState,
//                       shard_transitions: Sequence[ShardTransition],
//                       attestations: Sequence[Attestation]) -> None:
//    on_time_attestation_slot = compute_previous_slot(state.slot)
//    committee_count = get_committee_count_at_slot(state, on_time_attestation_slot)
//    for committee_index in map(CommitteeIndex, range(committee_count)):
//        # All attestations in the block for this committee/shard and current slot
//        shard_attestations = [
//            attestation for attestation in attestations
//            if is_on_time_attestation(state, attestation) and attestation.data.index == committee_index
//        ]
//        shard = compute_shard_from_committee_index(state, committee_index, on_time_attestation_slot)
//        winning_root = process_crosslink_for_shard(state, committee_index, shard_transitions[shard], shard_attestations)
//        if winning_root != Root():
//            # Mark relevant pending attestations as creating a successful crosslink
//            for pending_attestation in state.current_epoch_attestations:
//                if is_winning_attestation(state, pending_attestation, committee_index, winning_root):
//                    pending_attestation.crosslink_success = True
func processCrosslinks(
	bs *stateTrie.BeaconState,
	sts []*ethpb.ShardTransition,
	atts []*ethpb.Attestation) (
	*stateTrie.BeaconState, error) {
	onTimeSlot := helpers.PrevSlot(bs.Slot())
	vCount, err := helpers.ActiveValidatorCount(bs, helpers.SlotToEpoch(onTimeSlot))
	if err != nil {
		return nil, err
	}
	cCount := helpers.SlotCommitteeCount(vCount)
	attsByCommitteeId := helpers.OnTimeAttsByCommitteeID(atts, bs.Slot())
	for cID := uint64(0); cID < cCount; cID++ {
		if len(attsByCommitteeId[cID]) == 0 {
			continue
		}
		shard, err := helpers.ShardFromCommitteeIndex(bs, onTimeSlot, cID)
		if err != nil {
			return nil, err
		}
		st := sts[shard]
		wRoot, err := processCrosslinkForShard(bs, attsByCommitteeId[cID], st, cID)
		if err != nil {
			return nil, err
		}

		if wRoot != [32]byte{} {
			pendingAtts := bs.CurrentEpochAttestations()
			for _, pendingAtt := range pendingAtts {
				if isWinningAttestation(pendingAtt, onTimeSlot, cID, wRoot) {
					pendingAtt.CrosslinkSuccess = true
				}
			}
			if err := bs.SetCurrentEpochAttestations(pendingAtts); err != nil {
				return nil, err
			}
		}
	}

	return bs, nil
}

// This verifies the shard transition is empty in the event of a skip slot between beacon chain and shard chain.
//
// Spec code:
// def verify_empty_shard_transition(state: BeaconState, shard_transitions: Sequence[ShardTransition]) -> bool:
//    """
//    Verify that a `shard_transition` in a block is empty if an attestation was not processed for it.
//    """
//    for shard in range(get_active_shard_count(state)):
//        if state.shard_states[shard].slot != compute_previous_slot(state.slot):
//            if shard_transitions[shard] != ShardTransition():
//                return False
//    return True
func verifyEmptyShardTransition(beaconState *stateTrie.BeaconState, transitions []*ethpb.ShardTransition) bool {
	activeShardCount := helpers.ActiveShardCount()
	for i := uint64(0); i < activeShardCount; i++ {
		shardState := beaconState.ShardStateAtIndex(i)
		if shardState.Slot != helpers.PrevSlot(beaconState.Slot()) {
			if !helpers.IsEmptyShardTransition(transitions[i]) {
				return false
			}
		}
	}
	return true
}

// shardBlockProposersAndHeaders returns the shard block header and proposer indices given the shard transition object.
func shardBlockProposersAndHeaders(
	beaconState *stateTrie.BeaconState,
	transition *ethpb.ShardTransition,
	offsetSlots []uint64,
	shard uint64) ([]*ethpb.ShardBlockHeader, []uint64, error) {
	shardState := beaconState.ShardStateAtIndex(shard)
	prevGasPrice := shardState.GasPrice
	shardParentRoot := bytesutil.ToBytes32(shardState.LatestBlockRoot)
	proposerIndices := make([]uint64, 0, len(offsetSlots))
	headers := make([]*ethpb.ShardBlockHeader, 0, len(offsetSlots))
	for i, slot := range offsetSlots {
		shardBlockLength := transition.ShardBlockLengths[i]
		shardState := transition.ShardStates[i]
		if shardState.GasPrice != helpers.UpdatedGasPrice(prevGasPrice, shardBlockLength) {
			return nil, nil, fmt.Errorf("prev gas price %d != updated gas price %d", shardState.GasPrice, helpers.UpdatedGasPrice(prevGasPrice, shardBlockLength))
		}
		if shardState.Slot != slot {
			return nil, nil, errors.New("shard state slot != off set slot")
		}

		emptyProposal := shardBlockLength == 0
		if !emptyProposal {
			beaconParentRoot, err := helpers.BlockRootAtSlot(beaconState, slot)
			if err != nil {
				return nil, nil, err
			}
			proposerIndex, err := helpers.ShardProposerIndex(beaconState, slot, shard)
			if err != nil {
				return nil, nil, err
			}
			spr := make([]byte, 32)
			copy(spr[:], shardParentRoot[:])
			shardBlockHeader := &ethpb.ShardBlockHeader{
				ShardParentRoot:  spr[:], // TODO(0): We should verify here
				BeaconParentRoot: beaconParentRoot,
				Shard:            shard,
				Slot:             slot,
				ProposerIndex:    proposerIndex,
				BodyRoot:         transition.ShardDataRoots[i],
			}
			shardParentRoot, err = ssz.HashTreeRoot(shardBlockHeader)
			if err != nil {
				return nil, nil, err
			}
			headers = append(headers, shardBlockHeader)
			proposerIndices = append(proposerIndices, proposerIndex)
		} else {
			if len(transition.ShardDataRoots[i]) != 0 {
				return nil, nil, errors.New("empty proposal must have empty shard data root")
			}
		}
		prevGasPrice = shardState.GasPrice
	}

	return headers, proposerIndices, nil
}

func incBeaconProposerBal(beaconState *stateTrie.BeaconState, votedIndices []uint64) (*stateTrie.BeaconState, error) {
	beaconProposerIndex, err := helpers.BeaconProposerIndex(beaconState)
	if err != nil {
		return nil, err
	}
	attesterReward := uint64(0)
	for _, index := range votedIndices {
		reward, err := epoch.BaseReward(beaconState, index)
		if err != nil {
			return nil, err
		}
		attesterReward += reward
	}
	proposerReward := attesterReward / params.BeaconConfig().ProposerRewardQuotient
	if err := helpers.IncreaseBalance(beaconState, beaconProposerIndex, proposerReward); err != nil {
		return nil, err
	}
	return beaconState, nil
}

func decShardProposerBal(beaconState *stateTrie.BeaconState, transition *ethpb.ShardTransition, shard uint64) (*stateTrie.BeaconState, error) {
	offsetSlots := helpers.ShardOffSetSlots(beaconState, shard)
	for i, slot := range offsetSlots {
		shardProposerIndex, err := helpers.ShardProposerIndex(beaconState, slot, shard)
		if err != nil {
			return nil, err
		}

		if err := helpers.DecreaseBalance(beaconState, shardProposerIndex, transition.ShardStates[i].GasPrice*transition.ShardBlockLengths[i]); err != nil {
			return nil, err
		}
	}
	return beaconState, nil
}

// isCorrectIndexAttestation returns true if the attestation has the correct index.
func isCorrectIndexAttestation(attestation *ethpb.Attestation, committeeIndex uint64) bool {
	return attestation.Data.CommitteeIndex == committeeIndex
}

// isWinningAttestation returns true if the pending attestation has the correct winning root, slot, and the committee index.
func isWinningAttestation(pendingAttestation *pb.PendingAttestation, slot uint64, committeeIndex uint64, winningRoot [32]byte) bool {
	sameSlot := pendingAttestation.Data.Slot == slot
	sameCommittee := pendingAttestation.Data.CommitteeIndex == committeeIndex
	sameRoot := bytesutil.ToBytes32(pendingAttestation.Data.ShardTransitionRoot) == winningRoot
	return sameSlot && sameCommittee && sameRoot
}

// This validates shard block contents.
//
// Spec code:
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
//    # Check `proposer_index` field
//    assert block.proposer_index == get_shard_proposer_index(beacon_parent_state, block.slot, shard)
//    # Check `body` field
//    assert 0 < len(block.body) <= MAX_SHARD_BLOCK_SIZE
//    return True
func verifyShardBlockMessage(ctx context.Context, beaconParentState *stateTrie.BeaconState, shardParentState *ethpb.ShardState, shardBlock *ethpb.ShardBlock) (bool, error) {
	// TODO(0): These should be errors
	if !bytes.Equal(shardParentState.LatestBlockRoot, shardBlock.ShardParentRoot) {
		return false, nil
	}

	bh := stateTrie.CopyBeaconBlockHeader(beaconParentState.LatestBlockHeader())
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
	nextShardSlot := shardBlock.Slot + 1
	offsetSlots := helpers.ComputeOffsetSlots(beaconParentState.Slot(), nextShardSlot)
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

	if len(shardBlock.Body) == 0 || uint64(len(shardBlock.Body)) > params.BeaconConfig().MaxShardBlockSize {
		return false, nil
	}

	return true, nil
}

// This validates shard block signature.
//
// Spec code:
// def verify_shard_block_signature(beacon_parent_state: BeaconState,
//                                 signed_block: SignedShardBlock) -> bool:
//    proposer = beacon_parent_state.validators[signed_block.message.proposer_index]
//    domain = get_domain(beacon_parent_state, DOMAIN_SHARD_PROPOSAL, compute_epoch_at_slot(signed_block.message.slot))
//    signing_root = compute_signing_root(signed_block.message, domain)
//    return bls.Verify(proposer.pubkey, signing_root, signed_block.signature)
func verifyShardBlockSignature(beaconState *stateTrie.BeaconState, shardBlock *ethpb.SignedShardBlock) error {
	i := shardBlock.Message.ProposerIndex
	e := helpers.SlotToEpoch(shardBlock.Message.Slot)
	s := shardBlock.Signature
	return helpers.ComputeDomainVerifySigningRoot(beaconState, i, e, shardBlock.Message, params.BeaconConfig().DomainShardProposal, s)
}

// CanCrosslink returns true if more than 2/3 participants voted on input attestations. The voted
// indices are compared against the input committee indices to see if it reaches 2/3 balance threshold.
// The voted indices are also returned in the end.
func CanCrosslink(beaconState *stateTrie.BeaconState, atts []*ethpb.Attestation, committee []uint64) (bool, []uint64, error) {
	votedIndices := make([]uint64, 0, params.BeaconConfig().MaxValidatorsPerCommittee)
	m := make(map[uint64]bool)
	for _, a := range atts {
		indices := attestationutil.AttestingIndices(a.AggregationBits, committee)
		for _, i := range indices {
			if ok := m[i]; ok {
				continue
			}
			m[i] = true
			votedIndices = append(votedIndices, i)
		}
	}
	onlineIndices, err := helpers.OnlineValidatorIndices(beaconState)
	if err != nil {
		return false, []uint64{}, err
	}

	onlineCommitteeIndices := sliceutil.IntersectionUint64(onlineIndices, committee)
	onlineVotedIndices := sliceutil.IntersectionUint64(onlineIndices, votedIndices)
	enoughStaked := helpers.TotalBalance(beaconState, onlineVotedIndices)*3 >= helpers.TotalBalance(beaconState, onlineCommitteeIndices)*2

	return enoughStaked, votedIndices, nil
}
