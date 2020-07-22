package blocks

import (
	"context"
	"errors"

	"github.com/prysmaticlabs/go-ssz"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/epoch"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/state"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/attestationutil"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/params"
)

// CustodyAtoms returns the custody atoms of the input data. Each atom will be
// combined with one legendre bit.
func CustodyAtoms() {

}

// CustodySecrets extracts the custody secrets from the input signature.
func CustodySecrets() {

}

// UniversalHash hashes the input data chunks and secrets and returns an uint
// representation.
// TODO(0): Move to shared
func UniversalHash() {

}

// ComputeCustodyBit returns the custody bit of input signature and data.
func ComputeCustodyBit() {

}

// RandaoEpochForCustodyPeriod returns the randao epoch of a given validator and the custody period.
func RandaoEpochForCustodyPeriod(period uint64, validator uint64) uint64 {
	return 0
}

// CustodyPeriodForValidator returns the custody period of a given validator and epoch.
func CustodyPeriodForValidator(epoch uint64, validator uint64) uint64 {
	return 0
}

func ProcessChunkChallenge(ctx context.Context, state *state.BeaconState, challenge *pb.CustodyChunkChallenge) (*state.BeaconState, error) {
	a := challenge.Attestation
	// Verify challenge has a valid attestation.
	if err := VerifyAttestation(ctx, state, a); err != nil {
		return nil, err
	}

	// Verify it's not too late to challenge the attestation.
	maxChallengeEpoch := a.Data.Target.Epoch + params.ShardConfig().MaxChunkChallengeDelay
	if helpers.CurrentEpoch(state) > maxChallengeEpoch {
		return nil, errors.New("too late to challenge the attestation")
	}

	// Verify it's not too late to challenge the responder.
	responder, err := state.ValidatorAtIndex(challenge.ResponderIndex)
	if err != nil {
		return nil, err
	}
	if responder.ExitEpoch < params.BeaconConfig().FarFutureEpoch {
		if helpers.CurrentEpoch(state) > responder.ExitEpoch+params.ShardConfig().MaxChunkChallengeDelay {
			return nil, errors.New("too late to challenge the responder")
		}
	}

	// Verify the responder is slashable.
	if !helpers.IsSlashableValidator(responder, helpers.CurrentEpoch(state)) {
		return nil, errors.New("not slashable attestation")
	}

	// Verify the responder has participated in the attestation.
	c, err := helpers.BeaconCommitteeFromState(state, a.Data.Slot, a.Data.CommitteeIndex)
	if err != nil {
		return nil, err
	}
	indices := attestationutil.AttestingIndices(a.AggregationBits, c)
	voted := false
	for _, i := range indices {
		if i == challenge.ResponderIndex {
			voted = true
		}
	}
	if !voted {
		return nil, errors.New("responder did not participate")
	}

	// Verify the shard transition is correct.
	str, err := ssz.HashTreeRoot(challenge.ShardTransition)
	if err != nil {
		return nil, err
	}
	if str != bytesutil.ToBytes32(a.Data.ShardTransitionRoot) {
		return nil, errors.New("incorrect shard transition root")
	}

	// Verify the challenge is not a duplicate.
	dr := challenge.ShardTransition.ShardDataRoots[challenge.DataIndex]
	// TODO(0): Add custody_chunk_challenge_records in beacon state

	// TODO(0): Verify depth

	// Add new chunk challenge record
	proposer, err := helpers.BeaconProposerIndex(state)
	if err != nil {
		return nil, err
	}

	r := &pb.CustodyChunkChallengeRecord{
		ChallengeIndex:  state.CustodyChallengeIndex(),
		ChallengerIndex: proposer,
		ResponderIndex:  challenge.ResponderIndex,
		InclusionEpoch:  helpers.CurrentEpoch(state),
		DataRoot:        dr,
		ChunkIndex:      challenge.ChunkIndex,
	}
	if err := state.ReplaceEmptyCustodyChunkChallengeRecord(r); err != nil {
		return nil, err
	}

	if err := state.IncChallengeIndex(); err != nil {
		return nil, err
	}
	responder.WithdrawableEpoch = params.BeaconConfig().FarFutureEpoch
	if err := state.UpdateValidatorAtIndex(challenge.ResponderIndex, responder); err != nil {
		return nil, err
	}
	return state, nil
}

func ProcessChunkChallengeResponse(ctx context.Context, state *state.BeaconState, response *pb.CustodyChunkRespond) (*state.BeaconState, error) {
	// Check matching challenge exists in state.
	matchingChallenge := make([]*pb.CustodyChunkChallengeRecord, 1)
	if len(matchingChallenge) != 1 {
		return nil, errors.New("incorrect challenge record in state")
	}
	c := matchingChallenge[0]

	// Verify chunk index.
	if c.ChunkIndex != response.ChunkIndex {
		return nil, errors.New("incorrect chunk index")
	}

	// Verify merkle branch matches.

	// Clear the index.

	// Reward the proposer.
	proposer, err := helpers.BeaconProposerIndex(state)
	if err != nil {
		return nil, err
	}
	b, err := epoch.BaseReward(state, proposer)
	if err != nil {
		return nil, err
	}
	if err := helpers.IncreaseBalance(state, proposer, b/params.ShardConfig().MinorRewardQuotient); err != nil {
		return nil, err
	}
	return state, nil
}

func ProcessCustodyKeyReveal(ctx context.Context, state *state.BeaconState, r *pb.CustodyKeyReveal) (*state.BeaconState, error) {
	rVal, err := state.ValidatorAtIndex(r.RevealerIndex)
	if err != nil {
		return nil, err
	}
	ce := helpers.CurrentEpoch(state)
	epochToSign := RandaoEpochForCustodyPeriod(rVal.NextCustodySecretRevealEpoch, r.RevealerIndex)
	custodyRevealPeriod := CustodyPeriodForValidator(ce, r.RevealerIndex)

	// Verify timing that validator can reveal.
	pastReveal := rVal.NextCustodySecretRevealEpoch < custodyRevealPeriod
	exited := rVal.ExitEpoch < ce
	exitPeriodReveal := rVal.ExitEpoch == CustodyPeriodForValidator(rVal.ExitEpoch, r.RevealerIndex)
	if !(pastReveal || (exited && exitPeriodReveal)) {
		return nil, errors.New("validator can not reveal yet")
	}

	// Verify validator is slashable.
	if !helpers.IsSlashableValidator(rVal, ce) {
		return nil, errors.New("validator is not slashable")
	}

	// Verify validator revealed signature.
	domain, err := helpers.Domain(state.Fork(), epochToSign, params.BeaconConfig().DomainRandao, state.GenesisValidatorRoot())
	if err != nil {
		return nil, err
	}
	sr, err := helpers.ComputeSigningRoot(epochToSign, domain)
	if err != nil {
		return nil, err
	}
	sig, err := bls.SignatureFromBytes(r.Signature)
	if err != nil {
		return nil, err
	}
	pk, err := bls.PublicKeyFromBytes(rVal.PublicKey)
	if err != nil {
		return nil, err
	}
	if !sig.Verify(pk, sr[:]) {
		return nil, errors.New("could not verify reveal signature")
	}

	// Process reveal.
	if exited && exitPeriodReveal {
		rVal.AllCustodySecretsRevealedEpoch = ce
	}
	rVal.NextCustodySecretRevealEpoch++

	// Reward block proposer.
	proposer, err := helpers.BeaconProposerIndex(state)
	if err != nil {
		return nil, err
	}
	b, err := epoch.BaseReward(state, r.RevealerIndex)
	if err != nil {
		return nil, err
	}
	if err := helpers.IncreaseBalance(state, proposer, b/params.ShardConfig().MinorRewardQuotient); err != nil {
		return nil, err
	}

	if err := state.UpdateValidatorAtIndex(r.RevealerIndex, rVal); err != nil {
		return nil, err
	}

	return state, nil
}
