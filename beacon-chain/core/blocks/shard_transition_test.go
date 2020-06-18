package blocks

import (
	"testing"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	stateTrie "github.com/prysmaticlabs/prysm/beacon-chain/state"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
)

func TestVerifyShardTransitionFalsePositive(t *testing.T) {
	tests := []struct {
		skippedShards   map[int]bool
		badSkippedShard bool
		wantedResult    bool
	}{
		{
			skippedShards: map[int]bool{},
			wantedResult:  true,
		},
		{
			skippedShards: map[int]bool{0: true},
			wantedResult:  false,
		},
		{
			skippedShards: map[int]bool{0: true, 1: true, 63: true, 64: true},
			wantedResult:  false,
		},
	}

	activeShardCount := 64
	for _, tt := range tests {
		currentSlot := uint64(10)
		shards := make([]*ethpb.ShardState, activeShardCount)
		for i := 0; i < len(shards); i++ {
			shards[i] = &ethpb.ShardState{}
			if tt.skippedShards[i] {
				shards[i].Slot = currentSlot - 2
			} else {
				shards[i].Slot = currentSlot - 1
			}
		}

		transitions := make([]*ethpb.ShardTransition, activeShardCount)
		for i := 0; i < len(transitions); i++ {
			transitions[i] = &ethpb.ShardTransition{
				ShardDataRoots:             make([][]byte, 0),
				ShardStates:                make([]*ethpb.ShardState, 0),
				ProposerSignatureAggregate: []byte{},
			}
			if tt.skippedShards[i] {
				transitions[i] = &ethpb.ShardTransition{StartSlot: 1}
			}
		}

		s := &pb.BeaconState{Slot: currentSlot, ShardStates: shards}
		state, err := stateTrie.InitializeFromProto(s)
		if err != nil {
			t.Fatal(err)
		}

		if verifyEmptyShardTransition(state, transitions) != tt.wantedResult {
			t.Error("Did not get false positive result")
		}
	}
}

func TestIsCommitteeAttestation(t *testing.T) {
	tests := []struct {
		att            *ethpb.Attestation
		committeeIndex uint64
		wanted         bool
	}{
		{&ethpb.Attestation{Data: &ethpb.AttestationData{CommitteeIndex: 2}}, 2, true},
		{&ethpb.Attestation{Data: &ethpb.AttestationData{CommitteeIndex: 2}}, 1, false},
		{&ethpb.Attestation{Data: &ethpb.AttestationData{}}, 0, true},
		{&ethpb.Attestation{Data: &ethpb.AttestationData{CommitteeIndex: 1}}, 0, false},
	}
	for _, tt := range tests {
		if isCorrectIndexAttestation(tt.att, tt.committeeIndex) != tt.wanted {
			t.Errorf("isCorrectIndexAttestation verification fails: %v", isCorrectIndexAttestation(tt.att, tt.committeeIndex))
		}
	}
}

func TestIsWinningAttestation(t *testing.T) {
	tests := []struct {
		att            *pb.PendingAttestation
		slot           uint64
		committeeIndex uint64
		winningRoot    [32]byte
		wanted         bool
	}{
		{&pb.PendingAttestation{Data: &ethpb.AttestationData{}}, 0, 0, [32]byte{}, true},
		{&pb.PendingAttestation{Data: &ethpb.AttestationData{ShardTransitionRoot: []byte{'a'}}}, 0, 0, [32]byte{'a'}, true},
		{&pb.PendingAttestation{Data: &ethpb.AttestationData{ShardTransitionRoot: []byte{}}}, 0, 0, [32]byte{'a'}, false},
		{&pb.PendingAttestation{Data: &ethpb.AttestationData{ShardTransitionRoot: []byte{'a'}}}, 1, 0, [32]byte{'a'}, false},
		{&pb.PendingAttestation{Data: &ethpb.AttestationData{ShardTransitionRoot: []byte{'a'}}}, 0, 1, [32]byte{'a'}, false},
	}
	for _, tt := range tests {
		if isWinningAttestation(tt.att, tt.slot, tt.committeeIndex, tt.winningRoot) != tt.wanted {
			t.Errorf("isWinningAttestation verification fails: %v", isWinningAttestation(tt.att, tt.slot, tt.committeeIndex, tt.winningRoot))
		}
	}
}
