package blocks

import (
	"errors"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/epoch"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/params"
)

// ProcessLightClientAggregate verifies that the light client signature is correct,
// and rewards every committee member who participated in it.
//
// Spec code:
// def process_light_client_aggregate(state: BeaconState, block_body: BeaconBlockBody) -> None:
//    committee = get_light_client_committee(state, get_current_epoch(state))
//    previous_slot = compute_previous_slot(state.slot)
//    previous_block_root = get_block_root_at_slot(state, previous_slot)
//
//    total_reward = Gwei(0)
//    signer_pubkeys = []
//    for bit_index, participant_index in enumerate(committee):
//        if block_body.light_client_bits[bit_index]:
//            signer_pubkeys.append(state.validators[participant_index].pubkey)
//            if not state.validators[participant_index].slashed:
//                increase_balance(state, participant_index, get_base_reward(state, participant_index))
//                total_reward += get_base_reward(state, participant_index)
//
//    increase_balance(state, get_beacon_proposer_index(state), Gwei(total_reward // PROPOSER_REWARD_QUOTIENT))
//
//    signing_root = compute_signing_root(previous_block_root,
//                                        get_domain(state, DOMAIN_LIGHT_CLIENT, compute_epoch_at_slot(previous_slot)))
//    assert optional_fast_aggregate_verify(signer_pubkeys, signing_root, block_body.light_client_signature)
func ProcessLightClientAggregate(state *state.BeaconState, body *ethpb.BeaconBlockBody) (*state.BeaconState, error) {
	lcc, err := helpers.LightClientCommittee(state, helpers.CurrentEpoch(state))
	if err != nil {
		return nil, err
	}

	totalRewards := uint64(0)
	pubKeys := make([]bls.PublicKey, 0, len(lcc))
	for i, index := range lcc {
		if body.LightClientBits.BitAt(uint64(i)) {
			v, err := state.ValidatorAtIndex(index)
			if err != nil {
				return nil, err
			}
			p, err := bls.PublicKeyFromBytes(v.PublicKey)
			if err != nil {
				return nil, err
			}
			pubKeys = append(pubKeys, p)

			if !v.Slashed {
				br, err := epoch.BaseReward(state, index)
				if err != nil {
					return nil, err
				}
				if err := helpers.IncreaseBalance(state, index, br); err != nil {
					return nil, err
				}
				totalRewards += br
			}
		}
	}

	proposerIndex, err := helpers.BeaconProposerIndex(state)
	if err != nil {
		return nil, err
	}
	if err := helpers.IncreaseBalance(state, proposerIndex, totalRewards/params.BeaconConfig().ProposerRewardQuotient); err != nil {
		return nil, err
	}
	state.GenesisValidatorRoot()
	ps := helpers.PrevSlot(state.Slot())
	pbr, err := helpers.BlockRootAtSlot(state, ps)
	if err != nil {
		return nil, err
	}
	d, err := helpers.Domain(state.Fork(), helpers.SlotToEpoch(ps), params.BeaconConfig().DomainLightClient, state.GenesisValidatorRoot())
	if err != nil {
		return nil, err
	}
	r, err := helpers.ComputeSigningRoot(pbr, d)
	if err != nil {
		return nil, err
	}
	sig, err := bls.SignatureFromBytes(body.LightClientSignature)
	if err != nil {
		return nil, err
	}
	if !sig.FastAggregateVerify(pubKeys, r) {
		return nil, errors.New("could not verify light client signature")
	}
	return state, nil
}
