package helpers

import (
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/shared/params"
)

// PackCompactValidator packs validator index, slashed status and compressed balance into a single uint value.
//
// Spec pseudocode definition:
// Spec code:
// def pack_compact_validator(index: ValidatorIndex, slashed: bool, balance_in_increments: uint64) -> uint64:
//    """
//    Create a compact validator object representing index, slashed status, and compressed balance.
//    Takes as input balance-in-increments (// EFFECTIVE_BALANCE_INCREMENT) to preserve symmetry with
//    the unpacking function.
//    """
//    return (index << 16) + (slashed << 15) + balance_in_increments
func PackCompactValidator(index uint64, slashed bool, balanceIncrements uint64) uint64 {
	if slashed {
		return (index << 16) + (1 << 15) + balanceIncrements
	}
	return (index << 16) + (0 << 15) + balanceIncrements
}

// UnpackCompactValidator unpacks the compact validator object into index, slashed and balance forms.
//
// Spec code:
//   def unpack_compact_validator(compact_validator: CompactValidator) -> Tuple[ValidatorIndex, bool, uint64]:
//    """
//    Return the index, slashed, effective_balance // EFFECTIVE_BALANCE_INCREMENT of ``compact_validator``.
//    """
//    return (
//        ValidatorIndex(compact_validator >> 16),
//        (compact_validator >> 15) % 2,
//        uint64(compact_validator & (2**15 - 1)),
//    )
func UnpackCompactValidator(compactValidator uint64) (uint64, bool, uint64) {
	index := compactValidator >> 16
	slashed := (compactValidator >> 15) == 1
	balance := uint64(compactValidator & (1<<15 - 1))

	return index, slashed, balance
}

// CommitteeToCompactCommittee converts a committee object to compact committee object.
//
// Spec code:
//   def committee_to_compact_committee(state: BeaconState, committee: Sequence[ValidatorIndex]) -> CompactCommittee:
//    """
//    Given a state and a list of validator indices, outputs the CompactCommittee representing them.
//    """
//    validators = [state.validators[i] for i in committee]
//    compact_validators = [
//        pack_compact_validator(i, v.slashed, v.effective_balance // EFFECTIVE_BALANCE_INCREMENT)
//        for i, v in zip(committee, validators)
//    ]
//    pubkeys = [v.pubkey for v in validators]
//    return CompactCommittee(pubkeys=pubkeys, compact_validators=compact_validators)
func CommitteeToCompactCommittee(state *state.BeaconState, committee []uint64) (*ethpb.CompactCommittee, error) {
	compactValidators := make([]uint64, len(committee))
	pubKeys := make([][]byte, len(committee))

	for i := 0; i < len(committee); i++ {
		v, err := state.ValidatorAtIndex(committee[i])
		if err != nil {
			return nil, err
		}
		compactValidators[i] = PackCompactValidator(committee[i], v.Slashed, v.EffectiveBalance/params.BeaconConfig().EffectiveBalanceIncrement)
		pubKeys[i] = v.PublicKey
	}

	return &ethpb.CompactCommittee{CompactValidators: compactValidators, PubKeys: pubKeys}, nil
}

// LightClientCommittee returns the light client committee of a given epoch.
//
// Spec code:
//   def get_light_client_committee(beacon_state: BeaconState, epoch: Epoch) -> Sequence[ValidatorIndex]:
//    """
//    Return the light client committee of no more than ``LIGHT_CLIENT_COMMITTEE_SIZE`` validators.
//    """
//    source_epoch = compute_committee_source_epoch(epoch, LIGHT_CLIENT_COMMITTEE_PERIOD)
//    active_validator_indices = get_active_validator_indices(beacon_state, source_epoch)
//    seed = get_seed(beacon_state, source_epoch, DOMAIN_LIGHT_CLIENT)
//    return compute_committee(
//        indices=active_validator_indices,
//        seed=seed,
//        index=0,
//        count=get_active_shard_count(beacon_state),
//    )[:LIGHT_CLIENT_COMMITTEE_SIZE]
func LightClientCommittee(state *state.BeaconState, epoch uint64) ([]uint64, error) {
	se := SourceEpoch(epoch, params.ShardConfig().LightClientCommitteePeriod)
	activeValidatorIndices, err := ActiveValidatorIndices(state, se)
	if err != nil {
		return nil, err
	}
	seed, err := Seed(state, se, params.ShardConfig().DomainLightClient)
	if err != nil {
		return nil, err
	}
	activeShardCount := ActiveShardCount()
	lightClientCommittee, err := ComputeCommittee(activeValidatorIndices, seed, 0, activeShardCount)
	if err != nil {
		return nil, err
	}
	return lightClientCommittee[:params.ShardConfig().LightClientCommitteeSize], nil
}
