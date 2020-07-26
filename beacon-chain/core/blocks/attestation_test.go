package blocks_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/blocks"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	stateTrie "github.com/prysmaticlabs/prysm/beacon-chain/state"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/aggregation"
	attaggregation "github.com/prysmaticlabs/prysm/shared/aggregation/attestations"
	"github.com/prysmaticlabs/prysm/shared/attestationutil"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/testutil"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
)

func TestProcessAttestations_InclusionDelayFailure(t *testing.T) {
	attestations := []*ethpb.Attestation{
		{
			Data: &ethpb.AttestationData{
				Target: &ethpb.Checkpoint{Epoch: 0},
				Slot:   5,
			},
		},
	}
	block := &ethpb.BeaconBlock{
		Body: &ethpb.BeaconBlockBody{
			Attestations: attestations,
		},
	}
	beaconState, _ := testutil.DeterministicGenesisState(t, 100)

	want := fmt.Sprintf(
		"attestation slot %d + inclusion delay %d > state slot %d",
		attestations[0].Data.Slot,
		params.BeaconConfig().MinAttestationInclusionDelay,
		beaconState.Slot(),
	)
	_, err := blocks.ProcessAttestations(context.Background(), beaconState, block.Body)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Expected %s, received %v", want, err)
	}
}

func TestProcessAttestations_NeitherCurrentNorPrevEpoch(t *testing.T) {
	att := &ethpb.Attestation{
		Data: &ethpb.AttestationData{
			Source: &ethpb.Checkpoint{Epoch: 0, Root: []byte("hello-world")},
			Target: &ethpb.Checkpoint{Epoch: 0}}}

	block := &ethpb.BeaconBlock{
		Body: &ethpb.BeaconBlockBody{
			Attestations: []*ethpb.Attestation{att},
		},
	}
	beaconState, _ := testutil.DeterministicGenesisState(t, 100)
	err := beaconState.SetSlot(beaconState.Slot() + params.BeaconConfig().SlotsPerEpoch*4 + params.BeaconConfig().MinAttestationInclusionDelay)
	if err != nil {
		t.Fatal(err)
	}
	pfc := beaconState.PreviousJustifiedCheckpoint()
	pfc.Root = []byte("hello-world")
	if err := beaconState.SetPreviousJustifiedCheckpoint(pfc); err != nil {
		t.Fatal(err)
	}
	if err := beaconState.SetPreviousEpochAttestations([]*pb.PendingAttestation{}); err != nil {
		t.Fatal(err)
	}

	want := fmt.Sprintf(
		"expected target epoch (%d) to be the previous epoch (%d) or the current epoch (%d)",
		att.Data.Target.Epoch,
		helpers.PrevEpoch(beaconState),
		helpers.CurrentEpoch(beaconState),
	)
	_, err = blocks.ProcessAttestations(context.Background(), beaconState, block.Body)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Expected %s, received %v", want, err)
	}
}

func TestProcessAttestations_CurrentEpochFFGDataMismatches(t *testing.T) {
	aggBits := bitfield.NewBitlist(3)
	attestations := []*ethpb.Attestation{
		{
			Data: &ethpb.AttestationData{
				Target: &ethpb.Checkpoint{Epoch: 0},
				Source: &ethpb.Checkpoint{Epoch: 1},
			},
			AggregationBits: aggBits,
		},
	}
	block := &ethpb.BeaconBlock{
		Body: &ethpb.BeaconBlockBody{
			Attestations: attestations,
		},
	}
	beaconState, _ := testutil.DeterministicGenesisState(t, 100)
	if err := beaconState.SetSlot(beaconState.Slot() + params.BeaconConfig().MinAttestationInclusionDelay); err != nil {
		t.Fatal(err)
	}
	cfc := beaconState.CurrentJustifiedCheckpoint()
	cfc.Root = []byte("hello-world")
	if err := beaconState.SetCurrentJustifiedCheckpoint(cfc); err != nil {
		t.Fatal(err)
	}
	if err := beaconState.SetCurrentEpochAttestations([]*pb.PendingAttestation{}); err != nil {
		t.Fatal(err)
	}

	want := fmt.Sprintf(
		"expected source epoch %d, received %d",
		helpers.CurrentEpoch(beaconState),
		attestations[0].Data.Source.Epoch,
	)
	_, err := blocks.ProcessAttestations(context.Background(), beaconState, block.Body)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Expected %s, received %v", want, err)
	}

	block.Body.Attestations[0].Data.Source.Epoch = helpers.CurrentEpoch(beaconState)
	block.Body.Attestations[0].Data.Source.Root = []byte{}

	want = fmt.Sprintf(
		"expected source root %#x, received %#x",
		beaconState.CurrentJustifiedCheckpoint().Root,
		attestations[0].Data.Source.Root,
	)
	_, err = blocks.ProcessAttestations(context.Background(), beaconState, block.Body)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Expected %s, received %v", want, err)
	}
}

func TestProcessAttestations_PrevEpochFFGDataMismatches(t *testing.T) {
	beaconState, _ := testutil.DeterministicGenesisState(t, 100)

	aggBits := bitfield.NewBitlist(3)
	aggBits.SetBitAt(0, true)
	attestations := []*ethpb.Attestation{
		{
			Data: &ethpb.AttestationData{
				Source: &ethpb.Checkpoint{Epoch: 1},
				Target: &ethpb.Checkpoint{Epoch: 1},
				Slot:   params.BeaconConfig().SlotsPerEpoch,
			},
			AggregationBits: aggBits,
		},
	}
	block := &ethpb.BeaconBlock{
		Body: &ethpb.BeaconBlockBody{
			Attestations: attestations,
		},
	}

	err := beaconState.SetSlot(beaconState.Slot() + params.BeaconConfig().SlotsPerEpoch + params.BeaconConfig().MinAttestationInclusionDelay)
	if err != nil {
		t.Fatal(err)
	}
	pfc := beaconState.PreviousJustifiedCheckpoint()
	pfc.Root = []byte("hello-world")
	if err := beaconState.SetPreviousJustifiedCheckpoint(pfc); err != nil {
		t.Fatal(err)
	}
	if err := beaconState.SetPreviousEpochAttestations([]*pb.PendingAttestation{}); err != nil {
		t.Fatal(err)
	}

	want := fmt.Sprintf(
		"expected source epoch %d, received %d",
		helpers.PrevEpoch(beaconState),
		attestations[0].Data.Source.Epoch,
	)
	_, err = blocks.ProcessAttestations(context.Background(), beaconState, block.Body)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Expected %s, received %v", want, err)
	}

	block.Body.Attestations[0].Data.Source.Epoch = helpers.PrevEpoch(beaconState)
	block.Body.Attestations[0].Data.Target.Epoch = helpers.CurrentEpoch(beaconState)
	block.Body.Attestations[0].Data.Source.Root = []byte{}

	want = fmt.Sprintf(
		"expected source root %#x, received %#x",
		beaconState.CurrentJustifiedCheckpoint().Root,
		attestations[0].Data.Source.Root,
	)
	_, err = blocks.ProcessAttestations(context.Background(), beaconState, block.Body)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Expected %s, received %v", want, err)
	}
}

func TestProcessAttestations_InvalidAggregationBitsLength(t *testing.T) {
	beaconState, _ := testutil.DeterministicGenesisState(t, 100)

	aggBits := bitfield.NewBitlist(4)
	att := &ethpb.Attestation{
		Data: &ethpb.AttestationData{
			Source: &ethpb.Checkpoint{Epoch: 0, Root: []byte("hello-world")},
			Target: &ethpb.Checkpoint{Epoch: 0}},
		AggregationBits: aggBits,
	}

	block := &ethpb.BeaconBlock{
		Body: &ethpb.BeaconBlockBody{
			Attestations: []*ethpb.Attestation{att},
		},
	}

	err := beaconState.SetSlot(beaconState.Slot() + params.BeaconConfig().MinAttestationInclusionDelay)
	if err != nil {
		t.Fatal(err)
	}

	cfc := beaconState.CurrentJustifiedCheckpoint()
	cfc.Root = []byte("hello-world")
	if err := beaconState.SetCurrentJustifiedCheckpoint(cfc); err != nil {
		t.Fatal(err)
	}
	if err := beaconState.SetCurrentEpochAttestations([]*pb.PendingAttestation{}); err != nil {
		t.Fatal(err)
	}

	expected := "failed to verify aggregation bitfield: wanted participants bitfield length 3, got: 4"
	_, err = blocks.ProcessAttestations(context.Background(), beaconState, block.Body)
	if err == nil || !strings.Contains(err.Error(), expected) {
		t.Errorf("Did not receive wanted error")
	}
}

func TestProcessAttestations_OK(t *testing.T) {
	beaconState, privKeys := testutil.DeterministicGenesisState(t, 100)

	aggBits := bitfield.NewBitlist(3)
	aggBits.SetBitAt(0, true)
	var mockRoot [32]byte
	copy(mockRoot[:], "hello-world")
	att := &ethpb.Attestation{
		Data: &ethpb.AttestationData{
			Source: &ethpb.Checkpoint{Epoch: 0, Root: mockRoot[:]},
			Target: &ethpb.Checkpoint{Epoch: 0, Root: mockRoot[:]},
		},
		AggregationBits: aggBits,
	}

	cfc := beaconState.CurrentJustifiedCheckpoint()
	cfc.Root = mockRoot[:]
	if err := beaconState.SetCurrentJustifiedCheckpoint(cfc); err != nil {
		t.Fatal(err)
	}
	if err := beaconState.SetCurrentEpochAttestations([]*pb.PendingAttestation{}); err != nil {
		t.Fatal(err)
	}

	committee, err := helpers.BeaconCommitteeFromState(beaconState, att.Data.Slot, att.Data.CommitteeIndex)
	if err != nil {
		t.Error(err)
	}
	attestingIndices := attestationutil.AttestingIndices(att.AggregationBits, committee)
	if err != nil {
		t.Error(err)
	}
	domain, err := helpers.Domain(beaconState.Fork(), 0, params.BeaconConfig().DomainBeaconAttester, beaconState.GenesisValidatorRoot())
	if err != nil {
		t.Fatal(err)
	}
	hashTreeRoot, err := helpers.ComputeSigningRoot(att.Data, domain)
	if err != nil {
		t.Error(err)
	}
	sigs := make([]bls.Signature, len(attestingIndices))
	for i, indice := range attestingIndices {
		sig := privKeys[indice].Sign(hashTreeRoot[:])
		sigs[i] = sig
	}
	att.Signature = bls.AggregateSignatures(sigs).Marshal()[:]

	block := &ethpb.BeaconBlock{
		Body: &ethpb.BeaconBlockBody{
			Attestations: []*ethpb.Attestation{att},
		},
	}

	err = beaconState.SetSlot(beaconState.Slot() + params.BeaconConfig().MinAttestationInclusionDelay)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := blocks.ProcessAttestations(context.Background(), beaconState, block.Body); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestProcessAggregatedAttestation_OverlappingBits(t *testing.T) {
	beaconState, privKeys := testutil.DeterministicGenesisState(t, 100)

	domain, err := helpers.Domain(beaconState.Fork(), 0, params.BeaconConfig().DomainBeaconAttester, beaconState.GenesisValidatorRoot())
	if err != nil {
		t.Fatal(err)
	}
	data := &ethpb.AttestationData{
		Source: &ethpb.Checkpoint{Epoch: 0, Root: []byte("hello-world")},
		Target: &ethpb.Checkpoint{Epoch: 0, Root: []byte("hello-world")},
	}
	aggBits1 := bitfield.NewBitlist(4)
	aggBits1.SetBitAt(0, true)
	aggBits1.SetBitAt(1, true)
	aggBits1.SetBitAt(2, true)
	att1 := &ethpb.Attestation{
		Data:            data,
		AggregationBits: aggBits1,
	}

	cfc := beaconState.CurrentJustifiedCheckpoint()
	cfc.Root = []byte("hello-world")
	if err := beaconState.SetCurrentJustifiedCheckpoint(cfc); err != nil {
		t.Fatal(err)
	}
	if err := beaconState.SetCurrentEpochAttestations([]*pb.PendingAttestation{}); err != nil {
		t.Fatal(err)
	}

	committee, err := helpers.BeaconCommitteeFromState(beaconState, att1.Data.Slot, att1.Data.CommitteeIndex)
	if err != nil {
		t.Error(err)
	}
	attestingIndices1 := attestationutil.AttestingIndices(att1.AggregationBits, committee)
	if err != nil {
		t.Fatal(err)
	}
	hashTreeRoot, err := helpers.ComputeSigningRoot(att1.Data, domain)
	if err != nil {
		t.Error(err)
	}
	sigs := make([]bls.Signature, len(attestingIndices1))
	for i, indice := range attestingIndices1 {
		sig := privKeys[indice].Sign(hashTreeRoot[:])
		sigs[i] = sig
	}
	att1.Signature = bls.AggregateSignatures(sigs).Marshal()[:]

	aggBits2 := bitfield.NewBitlist(4)
	aggBits2.SetBitAt(1, true)
	aggBits2.SetBitAt(2, true)
	aggBits2.SetBitAt(3, true)
	att2 := &ethpb.Attestation{
		Data:            data,
		AggregationBits: aggBits2,
	}

	committee, err = helpers.BeaconCommitteeFromState(beaconState, att2.Data.Slot, att2.Data.CommitteeIndex)
	if err != nil {
		t.Error(err)
	}
	attestingIndices2 := attestationutil.AttestingIndices(att2.AggregationBits, committee)
	if err != nil {
		t.Fatal(err)
	}
	hashTreeRoot, err = helpers.ComputeSigningRoot(data, domain)
	if err != nil {
		t.Error(err)
	}
	sigs = make([]bls.Signature, len(attestingIndices2))
	for i, indice := range attestingIndices2 {
		sig := privKeys[indice].Sign(hashTreeRoot[:])
		sigs[i] = sig
	}
	att2.Signature = bls.AggregateSignatures(sigs).Marshal()[:]

	if _, err = attaggregation.AggregatePair(att1, att2); err != aggregation.ErrBitsOverlap {
		t.Error("Did not receive wanted error")
	}
}

func TestProcessAggregatedAttestation_NoOverlappingBits(t *testing.T) {
	beaconState, privKeys := testutil.DeterministicGenesisState(t, 300)

	domain, err := helpers.Domain(beaconState.Fork(), 0, params.BeaconConfig().DomainBeaconAttester, beaconState.GenesisValidatorRoot())
	if err != nil {
		t.Fatal(err)
	}
	var mockRoot [32]byte
	copy(mockRoot[:], "hello-world")
	data := &ethpb.AttestationData{
		Source: &ethpb.Checkpoint{Epoch: 0, Root: mockRoot[:]},
		Target: &ethpb.Checkpoint{Epoch: 0, Root: mockRoot[:]},
	}
	aggBits1 := bitfield.NewBitlist(9)
	aggBits1.SetBitAt(0, true)
	aggBits1.SetBitAt(1, true)
	att1 := &ethpb.Attestation{
		Data:            data,
		AggregationBits: aggBits1,
	}

	cfc := beaconState.CurrentJustifiedCheckpoint()
	cfc.Root = mockRoot[:]
	if err := beaconState.SetCurrentJustifiedCheckpoint(cfc); err != nil {
		t.Fatal(err)
	}
	if err := beaconState.SetCurrentEpochAttestations([]*pb.PendingAttestation{}); err != nil {
		t.Fatal(err)
	}

	committee, err := helpers.BeaconCommitteeFromState(beaconState, att1.Data.Slot, att1.Data.CommitteeIndex)
	if err != nil {
		t.Error(err)
	}
	attestingIndices1 := attestationutil.AttestingIndices(att1.AggregationBits, committee)
	if err != nil {
		t.Fatal(err)
	}
	hashTreeRoot, err := helpers.ComputeSigningRoot(data, domain)
	if err != nil {
		t.Error(err)
	}
	sigs := make([]bls.Signature, len(attestingIndices1))
	for i, indice := range attestingIndices1 {
		sig := privKeys[indice].Sign(hashTreeRoot[:])
		sigs[i] = sig
	}
	att1.Signature = bls.AggregateSignatures(sigs).Marshal()[:]

	aggBits2 := bitfield.NewBitlist(9)
	aggBits2.SetBitAt(2, true)
	aggBits2.SetBitAt(3, true)
	att2 := &ethpb.Attestation{
		Data:            data,
		AggregationBits: aggBits2,
	}

	committee, err = helpers.BeaconCommitteeFromState(beaconState, att2.Data.Slot, att2.Data.CommitteeIndex)
	if err != nil {
		t.Error(err)
	}
	attestingIndices2 := attestationutil.AttestingIndices(att2.AggregationBits, committee)
	if err != nil {
		t.Fatal(err)
	}
	hashTreeRoot, err = helpers.ComputeSigningRoot(data, domain)
	if err != nil {
		t.Error(err)
	}
	sigs = make([]bls.Signature, len(attestingIndices2))
	for i, indice := range attestingIndices2 {
		sig := privKeys[indice].Sign(hashTreeRoot[:])
		sigs[i] = sig
	}
	att2.Signature = bls.AggregateSignatures(sigs).Marshal()[:]

	aggregatedAtt, err := attaggregation.AggregatePair(att1, att2)
	if err != nil {
		t.Fatal(err)
	}
	block := &ethpb.BeaconBlock{
		Body: &ethpb.BeaconBlockBody{
			Attestations: []*ethpb.Attestation{aggregatedAtt},
		},
	}

	err = beaconState.SetSlot(beaconState.Slot() + params.BeaconConfig().MinAttestationInclusionDelay)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := blocks.ProcessAttestations(context.Background(), beaconState, block.Body); err != nil {
		t.Error(err)
	}
}

func TestProcessAttestationsNoVerify_IncorrectSlotTargetEpoch(t *testing.T) {
	beaconState, _ := testutil.DeterministicGenesisState(t, 1)

	att := &ethpb.Attestation{
		Data: &ethpb.AttestationData{
			Slot:   params.BeaconConfig().SlotsPerEpoch,
			Target: &ethpb.Checkpoint{},
		},
	}
	wanted := fmt.Sprintf("data slot is not in the same epoch as target %d != %d", helpers.SlotToEpoch(att.Data.Slot), att.Data.Target.Epoch)
	_, err := blocks.ProcessAttestationNoVerify(context.TODO(), beaconState, att)
	if err == nil || err.Error() != wanted {
		t.Error("Did not get wanted error")
	}
}

func TestProcessAttestationsNoVerify_OK(t *testing.T) {
	// Attestation with an empty signature

	beaconState, _ := testutil.DeterministicGenesisState(t, 100)

	aggBits := bitfield.NewBitlist(3)
	aggBits.SetBitAt(1, true)
	var mockRoot [32]byte
	copy(mockRoot[:], "hello-world")
	att := &ethpb.Attestation{
		Data: &ethpb.AttestationData{
			Source: &ethpb.Checkpoint{Epoch: 0, Root: mockRoot[:]},
			Target: &ethpb.Checkpoint{Epoch: 0},
		},
		AggregationBits: aggBits,
	}

	zeroSig := [96]byte{}
	att.Signature = zeroSig[:]

	err := beaconState.SetSlot(beaconState.Slot() + params.BeaconConfig().MinAttestationInclusionDelay)
	if err != nil {
		t.Fatal(err)
	}
	ckp := beaconState.CurrentJustifiedCheckpoint()
	copy(ckp.Root, "hello-world")
	if err := beaconState.SetCurrentJustifiedCheckpoint(ckp); err != nil {
		t.Fatal(err)
	}
	if err := beaconState.SetCurrentEpochAttestations([]*pb.PendingAttestation{}); err != nil {
		t.Fatal(err)
	}

	if _, err := blocks.ProcessAttestationNoVerify(context.TODO(), beaconState, att); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestProcessAttestationsNoVerify_BadAttIdx(t *testing.T) {
	beaconState, _ := testutil.DeterministicGenesisState(t, 100)
	aggBits := bitfield.NewBitlist(3)
	aggBits.SetBitAt(1, true)
	var mockRoot [32]byte
	copy(mockRoot[:], "hello-world")
	att := &ethpb.Attestation{
		Data: &ethpb.AttestationData{
			CommitteeIndex: 100,
			Source:         &ethpb.Checkpoint{Epoch: 0, Root: mockRoot[:]},
			Target:         &ethpb.Checkpoint{Epoch: 0},
		},
		AggregationBits: aggBits,
	}
	zeroSig := [96]byte{}
	att.Signature = zeroSig[:]
	require.NoError(t, beaconState.SetSlot(beaconState.Slot()+params.BeaconConfig().MinAttestationInclusionDelay))
	ckp := beaconState.CurrentJustifiedCheckpoint()
	copy(ckp.Root, "hello-world")
	require.NoError(t, beaconState.SetCurrentJustifiedCheckpoint(ckp))
	require.NoError(t, beaconState.SetCurrentEpochAttestations([]*pb.PendingAttestation{}))
	_, err := blocks.ProcessAttestationNoVerify(context.TODO(), beaconState, att)
	require.ErrorContains(t, "committee index 100 >= committee count 1", err)
}

func TestConvertToIndexed_OK(t *testing.T) {
	helpers.ClearCache()
	validators := make([]*ethpb.Validator, 2*params.BeaconConfig().SlotsPerEpoch)
	for i := 0; i < len(validators); i++ {
		validators[i] = &ethpb.Validator{
			ExitEpoch: params.BeaconConfig().FarFutureEpoch,
		}
	}

	state, err := stateTrie.InitializeFromProto(&pb.BeaconState{
		Slot:        5,
		Validators:  validators,
		RandaoMixes: make([][]byte, params.BeaconConfig().EpochsPerHistoricalVector),
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		aggregationBitfield    bitfield.Bitlist
		wantedAttestingIndices []uint64
	}{
		{
			aggregationBitfield:    bitfield.Bitlist{0x07},
			wantedAttestingIndices: []uint64{43, 47},
		},
		{
			aggregationBitfield:    bitfield.Bitlist{0x03},
			wantedAttestingIndices: []uint64{47},
		},
		{
			aggregationBitfield:    bitfield.Bitlist{0x01},
			wantedAttestingIndices: []uint64{},
		},
	}

	var sig [96]byte
	copy(sig[:], "signed")
	attestation := &ethpb.Attestation{
		Signature: sig[:],
		Data: &ethpb.AttestationData{
			Source: &ethpb.Checkpoint{Epoch: 0},
			Target: &ethpb.Checkpoint{Epoch: 0},
		},
	}
	for _, tt := range tests {
		attestation.AggregationBits = tt.aggregationBitfield
		wanted := &ethpb.IndexedAttestation{
			AttestingIndices: tt.wantedAttestingIndices,
			Data:             attestation.Data,
			Signature:        attestation.Signature,
		}

		committee, err := helpers.BeaconCommitteeFromState(state, attestation.Data.Slot, attestation.Data.CommitteeIndex)
		if err != nil {
			t.Error(err)
		}
		ia := attestationutil.ConvertToIndexed(context.Background(), attestation, committee)
		if !reflect.DeepEqual(wanted, ia) {
			t.Error("convert attestation to indexed attestation didn't result as wanted")
		}
	}
}

func TestVerifyIndexedAttestation_OK(t *testing.T) {
	numOfValidators := 4 * params.BeaconConfig().SlotsPerEpoch
	validators := make([]*ethpb.Validator, numOfValidators)
	_, keys, err := testutil.DeterministicDepositsAndKeys(numOfValidators)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(validators); i++ {
		validators[i] = &ethpb.Validator{
			ExitEpoch: params.BeaconConfig().FarFutureEpoch,
			PublicKey: keys[i].PublicKey().Marshal(),
		}
	}

	state, err := stateTrie.InitializeFromProto(&pb.BeaconState{
		Slot:       5,
		Validators: validators,
		Fork: &pb.Fork{
			Epoch:           0,
			CurrentVersion:  params.BeaconConfig().GenesisForkVersion,
			PreviousVersion: params.BeaconConfig().GenesisForkVersion,
		},
		RandaoMixes: make([][]byte, params.BeaconConfig().EpochsPerHistoricalVector),
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		attestation *ethpb.IndexedAttestation
	}{
		{attestation: &ethpb.IndexedAttestation{
			Data: &ethpb.AttestationData{
				Target: &ethpb.Checkpoint{
					Epoch: 2,
				},
			},
			AttestingIndices: []uint64{1},
		}},
		{attestation: &ethpb.IndexedAttestation{
			Data: &ethpb.AttestationData{
				Target: &ethpb.Checkpoint{
					Epoch: 1,
				},
			},
			AttestingIndices: []uint64{47, 99, 101},
		}},
		{attestation: &ethpb.IndexedAttestation{
			Data: &ethpb.AttestationData{
				Target: &ethpb.Checkpoint{
					Epoch: 4,
				},
			},
			AttestingIndices: []uint64{21, 72},
		}},
		{attestation: &ethpb.IndexedAttestation{
			Data: &ethpb.AttestationData{
				Target: &ethpb.Checkpoint{
					Epoch: 7,
				},
			},
			AttestingIndices: []uint64{100, 121, 122},
		}},
	}

	for _, tt := range tests {
		domain, err := helpers.Domain(state.Fork(), tt.attestation.Data.Target.Epoch, params.BeaconConfig().DomainBeaconAttester, state.GenesisValidatorRoot())
		if err != nil {
			t.Fatal(err)
		}
		root, err := helpers.ComputeSigningRoot(tt.attestation.Data, domain)
		if err != nil {
			t.Error(err)
		}
		var sig []bls.Signature
		for _, idx := range tt.attestation.AttestingIndices {
			validatorSig := keys[idx].Sign(root[:])
			sig = append(sig, validatorSig)
		}
		aggSig := bls.AggregateSignatures(sig)
		marshalledSig := aggSig.Marshal()

		tt.attestation.Signature = marshalledSig

		err = blocks.VerifyIndexedAttestation(context.Background(), state, tt.attestation)
		if err != nil {
			t.Errorf("failed to verify indexed attestation: %v", err)
		}
	}
}

func TestValidateIndexedAttestation_AboveMaxLength(t *testing.T) {
	indexedAtt1 := &ethpb.IndexedAttestation{
		AttestingIndices: make([]uint64, params.BeaconConfig().MaxValidatorsPerCommittee+5),
	}

	for i := uint64(0); i < params.BeaconConfig().MaxValidatorsPerCommittee+5; i++ {
		indexedAtt1.AttestingIndices[i] = i
		indexedAtt1.Data = &ethpb.AttestationData{
			Target: &ethpb.Checkpoint{
				Epoch: i,
			},
		}
	}

	want := "validator indices count exceeds MAX_VALIDATORS_PER_COMMITTEE"
	err := blocks.VerifyIndexedAttestation(context.Background(), &stateTrie.BeaconState{}, indexedAtt1)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Expected verification to fail return false, received: %v", err)
	}
}

func TestVerifyAttestations_VerifiesMultipleAttestations(t *testing.T) {
	ctx := context.Background()
	numOfValidators := 4 * params.BeaconConfig().SlotsPerEpoch
	validators := make([]*ethpb.Validator, numOfValidators)
	_, keys, err := testutil.DeterministicDepositsAndKeys(numOfValidators)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(validators); i++ {
		validators[i] = &ethpb.Validator{
			ExitEpoch: params.BeaconConfig().FarFutureEpoch,
			PublicKey: keys[i].PublicKey().Marshal(),
		}
	}

	st, err := stateTrie.InitializeFromProto(&pb.BeaconState{
		Slot:       5,
		Validators: validators,
		Fork: &pb.Fork{
			Epoch:           0,
			CurrentVersion:  params.BeaconConfig().GenesisForkVersion,
			PreviousVersion: params.BeaconConfig().GenesisForkVersion,
		},
		RandaoMixes: make([][]byte, params.BeaconConfig().EpochsPerHistoricalVector),
	})

	comm1, err := helpers.BeaconCommitteeFromState(st, 1 /*slot*/, 0 /*committeeIndex*/)
	if err != nil {
		t.Fatal(err)
	}
	att1 := &ethpb.Attestation{
		AggregationBits: bitfield.NewBitlist(uint64(len(comm1))),
		Data: &ethpb.AttestationData{
			Slot:           1,
			CommitteeIndex: 0,
		},
		Signature: nil,
	}
	domain, err := helpers.Domain(st.Fork(), st.Fork().Epoch, params.BeaconConfig().DomainBeaconAttester, st.GenesisValidatorRoot())
	if err != nil {
		t.Fatal(err)
	}
	root, err := helpers.ComputeSigningRoot(att1.Data, domain)
	if err != nil {
		t.Fatal(err)
	}
	var sigs []bls.Signature
	for i, u := range comm1 {
		att1.AggregationBits.SetBitAt(uint64(i), true)
		sigs = append(sigs, keys[u].Sign(root[:]))
	}
	att1.Signature = bls.AggregateSignatures(sigs).Marshal()

	comm2, err := helpers.BeaconCommitteeFromState(st, 1 /*slot*/, 1 /*committeeIndex*/)
	if err != nil {
		t.Fatal(err)
	}
	att2 := &ethpb.Attestation{
		AggregationBits: bitfield.NewBitlist(uint64(len(comm2))),
		Data: &ethpb.AttestationData{
			Slot:           1,
			CommitteeIndex: 1,
		},
		Signature: nil,
	}
	root, err = helpers.ComputeSigningRoot(att2.Data, domain)
	if err != nil {
		t.Fatal(err)
	}
	sigs = nil
	for i, u := range comm2 {
		att2.AggregationBits.SetBitAt(uint64(i), true)
		sigs = append(sigs, keys[u].Sign(root[:]))
	}
	att2.Signature = bls.AggregateSignatures(sigs).Marshal()

	if err := blocks.VerifyAttestations(ctx, st, []*ethpb.Attestation{att1, att2}); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyAttestations_HandlesPlannedFork(t *testing.T) {
	// In this test, att1 is from the prior fork and att2 is from the new fork.
	ctx := context.Background()
	numOfValidators := 4 * params.BeaconConfig().SlotsPerEpoch
	validators := make([]*ethpb.Validator, numOfValidators)
	_, keys, err := testutil.DeterministicDepositsAndKeys(numOfValidators)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(validators); i++ {
		validators[i] = &ethpb.Validator{
			ExitEpoch: params.BeaconConfig().FarFutureEpoch,
			PublicKey: keys[i].PublicKey().Marshal(),
		}
	}

	st, err := stateTrie.InitializeFromProto(&pb.BeaconState{
		Slot:       35,
		Validators: validators,
		Fork: &pb.Fork{
			Epoch:           1,
			CurrentVersion:  []byte{0, 1, 2, 3},
			PreviousVersion: params.BeaconConfig().GenesisForkVersion,
		},
		RandaoMixes: make([][]byte, params.BeaconConfig().EpochsPerHistoricalVector),
	})

	comm1, err := helpers.BeaconCommitteeFromState(st, 1 /*slot*/, 0 /*committeeIndex*/)
	if err != nil {
		t.Fatal(err)
	}
	att1 := &ethpb.Attestation{
		AggregationBits: bitfield.NewBitlist(uint64(len(comm1))),
		Data: &ethpb.AttestationData{
			Slot:           1,
			CommitteeIndex: 0,
		},
		Signature: nil,
	}
	prevDomain, err := helpers.Domain(st.Fork(), st.Fork().Epoch-1, params.BeaconConfig().DomainBeaconAttester, st.GenesisValidatorRoot())
	if err != nil {
		t.Fatal(err)
	}
	root, err := helpers.ComputeSigningRoot(att1.Data, prevDomain)
	if err != nil {
		t.Fatal(err)
	}
	var sigs []bls.Signature
	for i, u := range comm1 {
		att1.AggregationBits.SetBitAt(uint64(i), true)
		sigs = append(sigs, keys[u].Sign(root[:]))
	}
	att1.Signature = bls.AggregateSignatures(sigs).Marshal()

	comm2, err := helpers.BeaconCommitteeFromState(st, 1*params.BeaconConfig().SlotsPerEpoch+1 /*slot*/, 1 /*committeeIndex*/)
	if err != nil {
		t.Fatal(err)
	}
	att2 := &ethpb.Attestation{
		AggregationBits: bitfield.NewBitlist(uint64(len(comm2))),
		Data: &ethpb.AttestationData{
			Slot:           1*params.BeaconConfig().SlotsPerEpoch + 1,
			CommitteeIndex: 1,
		},
		Signature: nil,
	}
	currDomain, err := helpers.Domain(st.Fork(), st.Fork().Epoch, params.BeaconConfig().DomainBeaconAttester, st.GenesisValidatorRoot())
	root, err = helpers.ComputeSigningRoot(att2.Data, currDomain)
	if err != nil {
		t.Fatal(err)
	}
	sigs = nil
	for i, u := range comm2 {
		att2.AggregationBits.SetBitAt(uint64(i), true)
		sigs = append(sigs, keys[u].Sign(root[:]))
	}
	att2.Signature = bls.AggregateSignatures(sigs).Marshal()

	if err := blocks.VerifyAttestations(ctx, st, []*ethpb.Attestation{att1, att2}); err != nil {
		t.Fatal(err)
	}
}

func TestRetrieveAttestationSignatureSet_VerifiesMultipleAttestations(t *testing.T) {
	ctx := context.Background()
	numOfValidators := 4 * params.BeaconConfig().SlotsPerEpoch
	validators := make([]*ethpb.Validator, numOfValidators)
	_, keys, err := testutil.DeterministicDepositsAndKeys(numOfValidators)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(validators); i++ {
		validators[i] = &ethpb.Validator{
			ExitEpoch: params.BeaconConfig().FarFutureEpoch,
			PublicKey: keys[i].PublicKey().Marshal(),
		}
	}

	st, err := stateTrie.InitializeFromProto(&pb.BeaconState{
		Slot:       5,
		Validators: validators,
		Fork: &pb.Fork{
			Epoch:           0,
			CurrentVersion:  params.BeaconConfig().GenesisForkVersion,
			PreviousVersion: params.BeaconConfig().GenesisForkVersion,
		},
		RandaoMixes: make([][]byte, params.BeaconConfig().EpochsPerHistoricalVector),
	})

	comm1, err := helpers.BeaconCommitteeFromState(st, 1 /*slot*/, 0 /*committeeIndex*/)
	if err != nil {
		t.Fatal(err)
	}
	att1 := &ethpb.Attestation{
		AggregationBits: bitfield.NewBitlist(uint64(len(comm1))),
		Data: &ethpb.AttestationData{
			Slot:           1,
			CommitteeIndex: 0,
		},
		Signature: nil,
	}
	domain, err := helpers.Domain(st.Fork(), st.Fork().Epoch, params.BeaconConfig().DomainBeaconAttester, st.GenesisValidatorRoot())
	if err != nil {
		t.Fatal(err)
	}
	root, err := helpers.ComputeSigningRoot(att1.Data, domain)
	if err != nil {
		t.Fatal(err)
	}
	var sigs []bls.Signature
	for i, u := range comm1 {
		att1.AggregationBits.SetBitAt(uint64(i), true)
		sigs = append(sigs, keys[u].Sign(root[:]))
	}
	att1.Signature = bls.AggregateSignatures(sigs).Marshal()

	comm2, err := helpers.BeaconCommitteeFromState(st, 1 /*slot*/, 1 /*committeeIndex*/)
	if err != nil {
		t.Fatal(err)
	}
	att2 := &ethpb.Attestation{
		AggregationBits: bitfield.NewBitlist(uint64(len(comm2))),
		Data: &ethpb.AttestationData{
			Slot:           1,
			CommitteeIndex: 1,
		},
		Signature: nil,
	}
	root, err = helpers.ComputeSigningRoot(att2.Data, domain)
	if err != nil {
		t.Fatal(err)
	}
	sigs = nil
	for i, u := range comm2 {
		att2.AggregationBits.SetBitAt(uint64(i), true)
		sigs = append(sigs, keys[u].Sign(root[:]))
	}
	att2.Signature = bls.AggregateSignatures(sigs).Marshal()

	set, err := blocks.AttestationSignatureSet(ctx, st, []*ethpb.Attestation{att1, att2})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := set.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !verified {
		t.Error("Multiple signatures were unable to be verified.")
	}
}

func TestVerifyAttestationForShard(t *testing.T) {
	type args struct {
		bs  *pb.BeaconState
		att *ethpb.Attestation
	}
	tests := []struct {
		name    string
		args    args
		error   bool
		wantErr string
	}{
		{
			name: "On time attestation is correct",
			args: args{
				bs: &pb.BeaconState{Slot: 1, BlockRoots: [][]byte{bytesutil.PadTo([]byte{'a'}, 32)}},
				att: &ethpb.Attestation{Data: &ethpb.AttestationData{
					BeaconBlockRoot: bytesutil.PadTo([]byte{'a'}, 32),
				}},
			},
		},
		{
			name: "On time attestation with incorrect shard",
			args: args{
				bs: &pb.BeaconState{Slot: 1, BlockRoots: [][]byte{bytesutil.PadTo([]byte{'a'}, 32)}},
				att: &ethpb.Attestation{Data: &ethpb.AttestationData{
					BeaconBlockRoot: bytesutil.PadTo([]byte{'a'}, 32),
					Shard:           1,
				}},
			},
			error:   true,
			wantErr: "att.Data.Shard != shard",
		},
		{
			name: "On time attestation with incorrect shard block root",
			args: args{
				bs: &pb.BeaconState{Slot: 1, BlockRoots: [][]byte{bytesutil.PadTo([]byte{'a'}, 32)}},
				att: &ethpb.Attestation{Data: &ethpb.AttestationData{
					BeaconBlockRoot: bytesutil.PadTo([]byte{'b'}, 32),
				}},
			},
			error:   true,
			wantErr: "att.Data.BeaconBlockRoot != beaconBlockRootAtSlot",
		},
		{
			name: "Delay attestation with incorrect slot",
			args: args{
				bs: &pb.BeaconState{Slot: 1},
				att: &ethpb.Attestation{Data: &ethpb.AttestationData{
					Slot: 1,
				}},
			},
			error:   true,
			wantErr: "attSlot >= stateSlot",
		},
		{
			name: "Delay attestation with incorrect shard transition root",
			args: args{
				bs:  &pb.BeaconState{Slot: 2},
				att: &ethpb.Attestation{Data: &ethpb.AttestationData{ShardTransitionRoot: []byte{'a'}}},
			},
			error:   true,
			wantErr: "ShardTransitionRoot transition root is not empty",
		},
	}
	testutil.ResetCache()
	s, _ := testutil.DeterministicGenesisState(t, 100)
	for _, tt := range tests {
		if err := s.SetSlot(tt.args.bs.Slot); err != nil {
			t.Fatal(err)
		}
		if err := s.SetBlockRoots(tt.args.bs.BlockRoots); err != nil {
			t.Fatal(err)
		}
		err := blocks.VerifyAttestationForShard(context.Background(), s, tt.args.att)
		if tt.error {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Error("Did not get wanted error")
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
