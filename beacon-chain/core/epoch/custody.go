package epoch

import (
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/validators"
	stateTrie "github.com/prysmaticlabs/prysm/beacon-chain/state"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/params"
)

// ProcessRevealDeadline processes reveal deadline and slashes validators that did not reveal.
func ProcessRevealDeadline(state *stateTrie.BeaconState) (*stateTrie.BeaconState, error) {
	ce := helpers.CurrentEpoch(state)
	vals := state.Validators()
	var err error
	for i, val := range vals {
		deadLine := val.NextCustodySecretRevealEpoch + 1
		if helpers.CustodyPeriodForValidator(ce, uint64(i)) > deadLine {
			state, err = validators.SlashValidator(state, uint64(i))
			if err != nil {
				return nil, err
			}
		}
	}
	return state, nil
}

// ProcessRevealDeadline processes challenge deadline and slashes validators that did not reveal.
func ProcessChallengeDeadline(state *stateTrie.BeaconState) (*stateTrie.BeaconState, error) {
	ce := helpers.CurrentEpoch(state)
	records := state.CustodyChunkChallengeRecords()
	var err error
	for _, r := range records {
		if ce > r.InclusionEpoch+params.ShardConfig().EpochsPerCustodyPeriod {
			state, err = validators.SlashValidatorForWhistleBlower(state, r.ResponderIndex, r.ChallengerIndex)
			if err != nil {
				return nil, err
			}
			r = &pb.CustodyChunkChallengeRecord{}
		}
	}
	if err := state.SetCustodyChunkChallengeRecords(records); err != nil {
		return nil, err
	}
	return state, nil
}
