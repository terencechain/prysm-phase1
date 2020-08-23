package helpers

import (
	"encoding/binary"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	s "github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/hashutil"
	"github.com/prysmaticlabs/prysm/shared/params"
)

// OnlineValidatorIndices returns the online validator indices.
// Spec code (https://github.com/ethereum/eth2.0-specs/blob/7a770186b5ba576bf14ce496dc2b0381d169840e/specs/phase1/beacon-chain.md):
// def get_online_validator_indices(state: BeaconState) -> Set[ValidatorIndex]:
//    active_validators = get_active_validator_indices(state, get_current_epoch(state))
//    return set([i for i in active_validators if state.online_countdown[i] != 0])
func OnlineValidatorIndices(beaconState *s.BeaconState) ([]uint64, error) {
	activeValidators, err := ActiveValidatorIndices(beaconState, CurrentEpoch(beaconState))
	if err != nil {
		return nil, err
	}

	onlineCountdown := beaconState.OnlineCountdowns()
	i := 0
	for _, v := range activeValidators {
		if onlineCountdown[v] != 0 {
			activeValidators[i] = v
			i++
		}
	}

	activeValidators = activeValidators[:i]

	return activeValidators, nil
}

// ShardFromCommitteeIndex returns shard using input slot and the committee index.
// Spec code (https://github.com/ethereum/eth2.0-specs/blob/7a770186b5ba576bf14ce496dc2b0381d169840e/specs/phase1/beacon-chain.md):
// def compute_shard_from_committee_index(state: BeaconState, index: CommitteeIndex, slot: Slot) -> Shard:
//    active_shards = get_active_shard_count(state)
//    return Shard((index + get_start_shard(state, slot)) % active_shards)
func ShardFromCommitteeIndex(beaconState *s.BeaconState, slot uint64, committeeID uint64) (uint64, error) {
	activeShards := ActiveShardCount()
	startShard, err := StartShard(beaconState, slot)
	if err != nil {
		return 0, err
	}
	return (startShard + committeeID) % activeShards, nil
}

// UpdatedGasPrice returns the updated gas price.
// Spec code (https://github.com/ethereum/eth2.0-specs/blob/7a770186b5ba576bf14ce496dc2b0381d169840e/specs/phase1/beacon-chain.md):
// def compute_updated_gasprice(prev_gasprice: Gwei, length: uint8) -> Gwei:
//    if length > TARGET_SHARD_BLOCK_SIZE:
//        delta = (prev_gasprice * (length - TARGET_SHARD_BLOCK_SIZE)
//                 // TARGET_SHARD_BLOCK_SIZE // GASPRICE_ADJUSTMENT_COEFFICIENT)
//        return min(prev_gasprice + delta, MAX_GASPRICE)
//    else:
//        delta = (prev_gasprice * (TARGET_SHARD_BLOCK_SIZE - length)
//                 // TARGET_SHARD_BLOCK_SIZE // GASPRICE_ADJUSTMENT_COEFFICIENT)
//        return max(prev_gasprice, MIN_GASPRICE + delta) - delta
func UpdatedGasPrice(prevGasPrice uint64, shardBlockLength uint64) uint64 {
	targetBlockSize := params.ShardConfig().TargetShardBlockSize
	gasPriceAdjustmentCoefficient := params.ShardConfig().GasPriceAdjustmentCoefficient
	maxGasPrice := params.ShardConfig().MaxGasPrice
	minGasPrice := params.ShardConfig().MinGasPrice
	if shardBlockLength > targetBlockSize {
		delta := prevGasPrice * (shardBlockLength - targetBlockSize) / targetBlockSize / gasPriceAdjustmentCoefficient
		// Max gas price is the upper bound.
		if prevGasPrice+delta > maxGasPrice {
			return maxGasPrice
		}
		return prevGasPrice + delta
	}

	delta := prevGasPrice * (targetBlockSize - shardBlockLength) / targetBlockSize / gasPriceAdjustmentCoefficient
	// Min gas price is the lower bound.
	if prevGasPrice < minGasPrice+delta {
		return minGasPrice
	}
	return prevGasPrice - delta
}

// ShardProposerIndex returns the shard proposer index of a given slot and shard.
// Spec code (https://github.com/ethereum/eth2.0-specs/blob/7a770186b5ba576bf14ce496dc2b0381d169840e/specs/phase1/beacon-chain.md):
// def get_shard_proposer_index(beacon_state: BeaconState, slot: Slot, shard: Shard) -> ValidatorIndex:
//    """
//    Return the proposer's index of shard block at ``slot``.
//    """
//    epoch = compute_epoch_at_slot(slot)
//    committee = get_shard_committee(beacon_state, epoch, shard)
//    seed = hash(get_seed(beacon_state, epoch, DOMAIN_SHARD_COMMITTEE) + int_to_bytes(slot, length=8))
//    r = bytes_to_int(seed[:8])
//    return committee[r % len(committee)]
func ShardProposerIndex(beaconState *s.BeaconState, slot uint64, shard uint64) (uint64, error) {
	shardCommittee, err := ShardCommittee(beaconState, SlotToEpoch(slot), shard)
	if err != nil {
		return 0, err
	}

	seed, err := Seed(beaconState, CurrentEpoch(beaconState), params.ShardConfig().DomainShardCommittee)
	if err != nil {
		return 0, err
	}
	seedWithSlot := append(seed[:], bytesutil.Bytes8(slot)...)
	seedWithSlotHash := hashutil.Hash(seedWithSlot)
	r := binary.LittleEndian.Uint64(seedWithSlotHash[:8])
	i := r % uint64(len(shardCommittee))
	return shardCommittee[i], nil
}

// ShardCommittee returns the shard committee of a given slot and shard.
// Spec code (https://github.com/ethereum/eth2.0-specs/blob/7a770186b5ba576bf14ce496dc2b0381d169840e/specs/phase1/beacon-chain.md):
// def get_shard_committee(beacon_state: BeaconState, epoch: Epoch, shard: Shard) -> Sequence[ValidatorIndex]:
//    """
//    Return the shard committee of the given ``epoch`` of the given ``shard``.
//    """
//    source_epoch = compute_committee_source_epoch(epoch, SHARD_COMMITTEE_PERIOD)
//    active_validator_indices = get_active_validator_indices(beacon_state, source_epoch)
//    seed = get_seed(beacon_state, source_epoch, DOMAIN_SHARD_COMMITTEE)
//    return compute_committee(
//        indices=active_validator_indices,
//        seed=seed,
//        index=shard,
//        count=get_active_shard_count(beacon_state),
//    )
func ShardCommittee(beaconState *s.BeaconState, epoch uint64, shard uint64) ([]uint64, error) {
	se := SourceEpoch(epoch, params.ShardConfig().ShardCommitteePeriod)
	activeValidatorIndices, err := ActiveValidatorIndices(beaconState, se)
	if err != nil {
		return nil, err
	}
	seed, err := Seed(beaconState, se, params.ShardConfig().DomainShardCommittee)
	if err != nil {
		return nil, err
	}
	return ComputeCommittee(activeValidatorIndices, seed, shard, ActiveShardCount())
}

// ShardFromAttestation returns the shard number of a given attestation.
// Spec code (https://github.com/ethereum/eth2.0-specs/blob/7a770186b5ba576bf14ce496dc2b0381d169840e/specs/phase1/beacon-chain.md):
// def compute_shard_from_committee_index(state: BeaconState, index: CommitteeIndex, slot: Slot) -> Shard:
//    active_shards = get_active_shard_count(state)
//    return Shard((index + get_start_shard(state, slot)) % active_shards)
func ShardFromAttestation(beaconState *s.BeaconState, attestation *ethpb.Attestation) (uint64, error) {
	activeShards := ActiveShardCount()
	startShard, err := StartShard(beaconState, attestation.Data.Slot)
	if err != nil {
		return 0, err
	}
	return (startShard + attestation.Data.CommitteeIndex) % activeShards, nil
}

// ShardOffSetSlots returns the offset slot given the beacon state slot and the shard.
// Spec code (https://github.com/ethereum/eth2.0-specs/blob/7a770186b5ba576bf14ce496dc2b0381d169840e/specs/phase1/beacon-chain.md):
// def get_offset_slots(state: BeaconState, shard: Shard) -> Sequence[Slot]:
//    return compute_offset_slots(state.shard_states[shard].slot, state.slot)
func ShardOffSetSlots(beaconState *s.BeaconState, shard uint64) []uint64 {
	currentShardSlot := beaconState.ShardStateAtIndex(shard).Slot
	return ComputeOffsetSlots(currentShardSlot, beaconState.Slot())
}

// ComputeOffsetSlots returns the offset slot given the start slot and the end slot.
// Spec code (https://github.com/ethereum/eth2.0-specs/blob/7a770186b5ba576bf14ce496dc2b0381d169840e/specs/phase1/beacon-chain.md):
// def compute_offset_slots(start_slot: Slot, end_slot: Slot) -> Sequence[Slot]:
//    return [Slot(start_slot + x) for x in SHARD_BLOCK_OFFSETS if start_slot + x < end_slot]
func ComputeOffsetSlots(startSlot uint64, endSlot uint64) []uint64 {
	shardBlockOffsets := params.ShardConfig().ShardBlockOffsets
	filteredShardBlockOffsets := make([]uint64, 0, len(shardBlockOffsets))

	for _, offset := range shardBlockOffsets {
		s := startSlot + offset
		if s < endSlot {
			filteredShardBlockOffsets = append(filteredShardBlockOffsets, s)
		}
	}

	return filteredShardBlockOffsets
}

// IsEmptyShardTransition returns true if the shard transition is empty.
// It's similar to the following line in Python:
//  assert shard_transitions[shard] == ShardTransition()
func IsEmptyShardTransition(transition *ethpb.ShardTransition) bool {
	emptySlot := transition.StartSlot == 0
	emptyShardState := len(transition.ShardStates) == 0
	emptyShardData := len(transition.ShardDataRoots) == 0
	emptyShardBlockLength := len(transition.ShardBlockLengths) == 0

	return emptySlot && emptyShardState && emptyShardData && emptyShardBlockLength
}

// ActiveShardCount returns the active shard count.
// Spec code:
// def get_active_shard_count(state: BeaconState) -> uint64:
//    return len(state.shard_states)  # May adapt in the future, or change over time.
//    """
//    Return the number of active shards.
//    Note that this puts an upper bound on the number of committees per slot.
//    """
//    return INITIAL_ACTIVE_SHARDS
func ActiveShardCount() uint64 {
	return params.ShardConfig().InitialActiveShards
}

// StartShard returns the start shard of a given slot.
// Spec code:
// def get_start_shard(state: BeaconState, slot: Slot) -> Shard:
//    """
//    Return the start shard at ``slot``.
//    """
//    current_epoch_start_slot = compute_start_slot_at_epoch(get_current_epoch(state))
//    active_shard_count = get_active_shard_count(state)
//    if current_epoch_start_slot == slot:
//        return state.current_epoch_start_shard
//    elif current_epoch_start_slot > slot:
//        # Current epoch or the next epoch lookahead
//        shard_delta = get_committee_count_delta(state, start_slot=current_epoch_start_slot, stop_slot=slot)
//        return Shard((state.current_epoch_start_shard + shard_delta) % active_shard_count)
//    else:
//        # Previous epoch
//        shard_delta = get_committee_count_delta(state, start_slot=slot, stop_slot=current_epoch_start_slot)
//        max_committees_per_slot = active_shard_count
//        max_committees_in_span = max_committees_per_slot * (current_epoch_start_slot - slot)
//        return Shard(
//            # Ensure positive
//            (state.current_epoch_start_shard + max_committees_in_span - shard_delta)
//            % active_shard_count
//        )
func StartShard(beaconState *s.BeaconState, slot uint64) (uint64, error) {
	currentEpoch := CurrentEpoch(beaconState)
	currentEpochStartSlot := StartSlot(currentEpoch)
	activeShardCount := ActiveShardCount()
	if slot == currentEpochStartSlot {
		return beaconState.CurrentEpochStartShard(), nil
	} else if slot > currentEpochStartSlot {
		shardDelta, err := CommitteeCountDelta(beaconState, currentEpochStartSlot, slot)
		if err != nil {
			return 0, err
		}
		return (beaconState.CurrentEpochStartShard() + shardDelta) % activeShardCount, nil
	}
	shardDelta, err := CommitteeCountDelta(beaconState, slot, currentEpochStartSlot)
	if err != nil {
		return 0, err
	}
	maxCommitteesInSpan := activeShardCount * (currentEpochStartSlot - shardDelta)
	return (beaconState.CurrentEpochStartShard() + maxCommitteesInSpan - shardDelta) % activeShardCount, nil
}

// CommitteeCountDelta returns the sum of committee counts between start slot and stop slot.
// Spec code:
// def get_committee_count_delta(state: BeaconState, start_slot: Slot, stop_slot: Slot) -> uint64:
//    """
//    Return the sum of committee counts between ``[start_slot, stop_slot)``.
//    """
//    committee_sum = 0
//    for slot in range(start_slot, stop_slot):
//        count = get_committee_count_at_slot(state, Slot(slot))
//        committee_sum += count
//    return committee_sum
func CommitteeCountDelta(beaconState *s.BeaconState, startSlot uint64, endSlot uint64) (uint64, error) {
	sum := uint64(0)
	for i := startSlot; i < endSlot; i++ {
		activeValidatorCount, err := ActiveValidatorCount(beaconState, SlotToEpoch(i))
		if err != nil {
			return 0, err
		}
		sum += SlotCommitteeCount(activeValidatorCount)
	}
	return sum, nil
}

// IsOnTimeAttData returns true if the attestation data's is on time.
func IsOnTimeAttData(data *ethpb.AttestationData, currentSlot uint64) bool {
	return data.Slot == PrevSlot(currentSlot)
}

// OnTimeAttsByCommitteeID returns lists of filtered on time attestations that are indexed by committee IDs.
// The length of lists is defaulted at config MaxCommitteesPerSlot. The list will be empty if there's no committee
// or no attestation for that ID.
func OnTimeAttsByCommitteeID(atts []*ethpb.Attestation, currentSlot uint64) [][]*ethpb.Attestation {
	attsByCid := make([][]*ethpb.Attestation, params.BeaconConfig().MaxCommitteesPerSlot)
	for _, a := range atts {
		if IsOnTimeAttData(a.Data, currentSlot) {
			attsByCid[a.Data.CommitteeIndex] = append(attsByCid[a.Data.CommitteeIndex], a)
		}
	}
	return attsByCid
}

// AttsByTransitionRoot returns a mapping of attestation list that's grouped and keyed by shard transition root.
func AttsByTransitionRoot(atts []*ethpb.Attestation) map[[32]byte][]*ethpb.Attestation {
	attsByTRoot := make(map[[32]byte][]*ethpb.Attestation)
	for _, a := range atts {
		tRoot := bytesutil.ToBytes32(a.Data.ShardTransitionRoot)
		atts, ok := attsByTRoot[tRoot]
		if ok {
			attsByTRoot[tRoot] = []*ethpb.Attestation{a}
		} else {
			attsByTRoot[tRoot] = append(atts, a)
		}
	}
	return attsByTRoot
}

// CustodyPeriodForValidator returns the custody period of a given validator and epoch.
func CustodyPeriodForValidator(epoch uint64, validator uint64) uint64 {
	return 0
}
