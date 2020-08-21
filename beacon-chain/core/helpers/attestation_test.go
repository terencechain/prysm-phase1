package helpers_test

import (
	"strconv"
	"testing"
	"time"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	beaconstate "github.com/prysmaticlabs/prysm/beacon-chain/state"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/roughtime"
	"github.com/prysmaticlabs/prysm/shared/testutil"
	"github.com/prysmaticlabs/prysm/shared/testutil/assert"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
)

func TestAttestation_SlotSignature(t *testing.T) {
	priv := bls.RandKey()
	pub := priv.PublicKey()
	state, err := beaconstate.InitializeFromProto(&pb.BeaconState{
		Validators: []*ethpb.Validator{{PublicKey: pub.Marshal()}},
		Fork: &pb.Fork{
			CurrentVersion:  params.BeaconConfig().GenesisForkVersion,
			PreviousVersion: params.BeaconConfig().GenesisForkVersion,
			Epoch:           0,
		},
		Slot: 100,
	})
	require.NoError(t, err)
	slot := uint64(101)

	sig, err := helpers.SlotSignature(state, slot, priv)
	require.NoError(t, err)
	require.NoError(t, helpers.ComputeDomainVerifySigningRoot(state, 0, helpers.CurrentEpoch(state), slot,
		params.BeaconConfig().DomainBeaconAttester, sig.Marshal()))
}

func TestAttestation_IsAggregator(t *testing.T) {
	t.Run("aggregator", func(t *testing.T) {
		beaconState, privKeys := testutil.DeterministicGenesisState(t, 100)
		committee, err := helpers.BeaconCommitteeFromState(beaconState, 0, 0)
		require.NoError(t, err)
		sig := privKeys[0].Sign([]byte{'A'})
		agg, err := helpers.IsAggregator(uint64(len(committee)), sig.Marshal())
		require.NoError(t, err)
		assert.Equal(t, true, agg, "Wanted aggregator true")
	})

	t.Run("not aggregator", func(t *testing.T) {
		params.UseMinimalConfig()
		defer params.UseMainnetConfig()
		beaconState, privKeys := testutil.DeterministicGenesisState(t, 2048)

		committee, err := helpers.BeaconCommitteeFromState(beaconState, 0, 0)
		require.NoError(t, err)
		sig := privKeys[0].Sign([]byte{'A'})
		agg, err := helpers.IsAggregator(uint64(len(committee)), sig.Marshal())
		require.NoError(t, err)
		assert.Equal(t, false, agg, "Wanted aggregator false")
	})
}

func TestAttestation_AggregateSignature(t *testing.T) {
	t.Run("verified", func(t *testing.T) {
		pubkeys := make([]bls.PublicKey, 0, 100)
		atts := make([]*ethpb.Attestation, 0, 100)
		msg := bytesutil.ToBytes32([]byte("hello"))
		for i := 0; i < 100; i++ {
			priv := bls.RandKey()
			pub := priv.PublicKey()
			sig := priv.Sign(msg[:])
			pubkeys = append(pubkeys, pub)
			att := &ethpb.Attestation{Signature: sig.Marshal()}
			atts = append(atts, att)
		}
		aggSig, err := helpers.AggregateSignature(atts)
		require.NoError(t, err)
		assert.Equal(t, true, aggSig.FastAggregateVerify(pubkeys, msg), "Signature did not verify")
	})

	t.Run("not verified", func(t *testing.T) {
		pubkeys := make([]bls.PublicKey, 0, 100)
		atts := make([]*ethpb.Attestation, 0, 100)
		msg := []byte("hello")
		for i := 0; i < 100; i++ {
			priv := bls.RandKey()
			pub := priv.PublicKey()
			sig := priv.Sign(msg[:])
			pubkeys = append(pubkeys, pub)
			att := &ethpb.Attestation{Signature: sig.Marshal()}
			atts = append(atts, att)
		}
		aggSig, err := helpers.AggregateSignature(atts[0 : len(atts)-2])
		require.NoError(t, err)
		assert.Equal(t, false, aggSig.FastAggregateVerify(pubkeys, bytesutil.ToBytes32(msg)), "Signature not suppose to verify")
	})
}

func TestAttestation_ComputeSubnetForAttestation(t *testing.T) {
	// Create 10 committees
	committeeCount := uint64(10)
	validatorCount := committeeCount * params.BeaconConfig().TargetCommitteeSize
	validators := make([]*ethpb.Validator, validatorCount)

	for i := 0; i < len(validators); i++ {
		k := make([]byte, 48)
		copy(k, strconv.Itoa(i))
		validators[i] = &ethpb.Validator{
			PublicKey:             k,
			WithdrawalCredentials: make([]byte, 32),
			ExitEpoch:             params.BeaconConfig().FarFutureEpoch,
		}
	}

	state, err := beaconstate.InitializeFromProto(&pb.BeaconState{
		Validators:  validators,
		Slot:        200,
		BlockRoots:  make([][]byte, params.BeaconConfig().SlotsPerHistoricalRoot),
		StateRoots:  make([][]byte, params.BeaconConfig().SlotsPerHistoricalRoot),
		RandaoMixes: make([][]byte, params.BeaconConfig().EpochsPerHistoricalVector),
	})
	require.NoError(t, err)
	att := &ethpb.Attestation{
		AggregationBits: []byte{'A'},
		Data: &ethpb.AttestationData{
			Slot:            34,
			CommitteeIndex:  4,
			BeaconBlockRoot: []byte{'C'},
			Source:          nil,
			Target:          nil,
		},
		Signature:            []byte{'B'},
		XXX_NoUnkeyedLiteral: struct{}{},
		XXX_unrecognized:     nil,
		XXX_sizecache:        0,
	}
	valCount, err := helpers.ActiveValidatorCount(state, helpers.SlotToEpoch(att.Data.Slot))
	require.NoError(t, err)
	sub := helpers.ComputeSubnetForAttestation(valCount, att)
	assert.Equal(t, uint64(6), sub, "Did not get correct subnet for attestation")
}

func Test_ValidateAttestationTime(t *testing.T) {
	if params.BeaconNetworkConfig().MaximumGossipClockDisparity < 200*time.Millisecond {
		t.Fatal("This test expects the maximum clock disparity to be at least 200ms")
	}

	type args struct {
		attSlot     uint64
		genesisTime time.Time
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "attestation.slot == current_slot",
			args: args{
				attSlot:     15,
				genesisTime: roughtime.Now().Add(-15 * time.Duration(params.BeaconConfig().SecondsPerSlot) * time.Second),
			},
			wantErr: false,
		},
		{
			name: "attestation.slot == current_slot, received in middle of slot",
			args: args{
				attSlot: 15,
				genesisTime: roughtime.Now().Add(
					-15 * time.Duration(params.BeaconConfig().SecondsPerSlot) * time.Second,
				).Add(-(time.Duration(params.BeaconConfig().SecondsPerSlot/2) * time.Second)),
			},
			wantErr: false,
		},
		{
			name: "attestation.slot == current_slot, received 200ms early",
			args: args{
				attSlot: 16,
				genesisTime: roughtime.Now().Add(
					-16 * time.Duration(params.BeaconConfig().SecondsPerSlot) * time.Second,
				).Add(-200 * time.Millisecond),
			},
			wantErr: false,
		},
		{
			name: "attestation.slot > current_slot",
			args: args{
				attSlot:     16,
				genesisTime: roughtime.Now().Add(-15 * time.Duration(params.BeaconConfig().SecondsPerSlot) * time.Second),
			},
			wantErr: true,
		},
		{
			name: "attestation.slot < current_slot-ATTESTATION_PROPAGATION_SLOT_RANGE",
			args: args{
				attSlot:     100 - params.BeaconNetworkConfig().AttestationPropagationSlotRange - 1,
				genesisTime: roughtime.Now().Add(-100 * time.Duration(params.BeaconConfig().SecondsPerSlot) * time.Second),
			},
			wantErr: true,
		},
		{
			name: "attestation.slot = current_slot-ATTESTATION_PROPAGATION_SLOT_RANGE",
			args: args{
				attSlot:     100 - params.BeaconNetworkConfig().AttestationPropagationSlotRange,
				genesisTime: roughtime.Now().Add(-100 * time.Duration(params.BeaconConfig().SecondsPerSlot) * time.Second),
			},
			wantErr: false,
		},
		{
			name: "attestation.slot = current_slot-ATTESTATION_PROPAGATION_SLOT_RANGE, received 200ms late",
			args: args{
				attSlot: 100 - params.BeaconNetworkConfig().AttestationPropagationSlotRange,
				genesisTime: roughtime.Now().Add(
					-100 * time.Duration(params.BeaconConfig().SecondsPerSlot) * time.Second,
				).Add(200 * time.Millisecond),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := helpers.ValidateAttestationTime(tt.args.attSlot, tt.args.genesisTime); (err != nil) != tt.wantErr {
				t.Errorf("validateAggregateAttTime() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
