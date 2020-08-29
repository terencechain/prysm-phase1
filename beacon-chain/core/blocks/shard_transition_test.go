package blocks

import (
	"context"
	"reflect"
	"testing"

	"github.com/gogo/protobuf/sortkeys"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/prysmaticlabs/go-ssz"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	stateTrie "github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/beacon-chain/state/stateutil"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
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

func TestVerifyShardBlockMessage(t *testing.T) {
	shardBlock := &ethpb.ShardBlock{
		Slot:            1,
		Shard:           1,
		ShardParentRoot: bytesutil.PadTo([]byte{'a'}, 32),
	}
	validators := make([]*ethpb.Validator, params.BeaconConfig().MaxValidatorsPerCommittee)
	for i := 0; i < len(validators); i++ {
		validators[i] = &ethpb.Validator{
			ExitEpoch: params.BeaconConfig().FarFutureEpoch,
		}
	}
	bh := &ethpb.BeaconBlockHeader{StateRoot: bytesutil.PadTo([]byte{'a'}, 32)}
	hr, err := stateutil.BlockHeaderRoot(bh)
	if err != nil {
		t.Fatal(err)
	}
	beaconState, err := stateTrie.InitializeFromProto(&pb.BeaconState{
		Slot:              0,
		Validators:        validators,
		RandaoMixes:       make([][]byte, params.BeaconConfig().EpochsPerHistoricalVector),
		ShardStates:       make([]*ethpb.ShardState, 64),
		LatestBlockHeader: bh,
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name             string
		slot             uint64
		shard            uint64
		proposerIndex    uint64
		latestBlockRoot  []byte
		beaconParentRoot []byte
		shardBodyLength  []byte
		body             []byte
		want             bool
	}{
		{
			name:             "All pass",
			slot:             1,
			proposerIndex:    38,
			latestBlockRoot:  bytesutil.PadTo([]byte{'a'}, 32),
			beaconParentRoot: hr[:],
			body:             make([]byte, 1),
			want:             true,
		},
		{
			name:             "Incorrect slot",
			slot:             100,
			shard:            1,
			proposerIndex:    38,
			latestBlockRoot:  bytesutil.PadTo([]byte{'a'}, 32),
			beaconParentRoot: hr[:],
			body:             make([]byte, 1),
			want:             false,
		},
		{
			name:             "Incorrect proposer index",
			slot:             1,
			proposerIndex:    39,
			latestBlockRoot:  bytesutil.PadTo([]byte{'a'}, 32),
			beaconParentRoot: hr[:],
			body:             make([]byte, 1),
			want:             false,
		},
		{
			name:             "Incorrect shard parent root",
			slot:             1,
			proposerIndex:    38,
			latestBlockRoot:  bytesutil.PadTo([]byte{'b'}, 32),
			beaconParentRoot: hr[:],
			body:             make([]byte, 1),
			want:             false,
		},
		{
			name:             "Incorrect body length",
			slot:             1,
			proposerIndex:    38,
			latestBlockRoot:  bytesutil.PadTo([]byte{'a'}, 32),
			beaconParentRoot: hr[:],
			body:             make([]byte, params.BeaconConfig().MaxShardBlockSize+1),
			want:             false,
		},
		{
			name:             "Incorrect beacon parent root length",
			slot:             1,
			proposerIndex:    38,
			latestBlockRoot:  bytesutil.PadTo([]byte{'a'}, 32),
			beaconParentRoot: bytesutil.PadTo([]byte{'b'}, 32),
			body:             make([]byte, params.BeaconConfig().MaxShardBlockSize+1),
			want:             false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			helpers.ClearCache()
			shardBlock.Slot = tt.slot
			shardBlock.ProposerIndex = tt.proposerIndex
			shardBlock.Body = tt.body
			shardBlock.BeaconParentRoot = tt.beaconParentRoot
			shardState := &ethpb.ShardState{
				LatestBlockRoot: tt.latestBlockRoot,
			}
			verified, err := verifyShardBlockMessage(context.Background(), beaconState, shardState, shardBlock)
			if err != nil {
				t.Fatal(err)
			}
			if verified != tt.want {
				t.Errorf("Wanted verified %v, got %v", tt.want, verified)
			}
		})
	}
}

func Test_VerifyShardBlockSignature(t *testing.T) {
	bs, err := testState(params.BeaconConfig().MaxValidatorsPerCommittee)
	require.NoError(t, err)
	priv := bls.RandKey()
	require.NoError(t, bs.UpdateValidatorAtIndex(0, &ethpb.Validator{PublicKey: priv.PublicKey().Marshal()}))
	sb := &ethpb.SignedShardBlock{Message: &ethpb.ShardBlock{ProposerIndex: 0, ShardParentRoot: make([]byte, 32), BeaconParentRoot: make([]byte, 32)}, Signature: make([]byte, 96)}
	sb.Signature, err = helpers.ComputeDomainAndSign(bs, 0, sb.Message, params.BeaconConfig().DomainShardProposal, priv)
	require.NoError(t, err)

	type args struct {
		beaconState *stateTrie.BeaconState
		shardBlock  *ethpb.SignedShardBlock
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "Sig does not verify",
			args: args{
				beaconState: bs,
				shardBlock: &ethpb.SignedShardBlock{
					Message:   &ethpb.ShardBlock{},
					Signature: []byte{},
				},
			},
			wantErr: true,
		},
		{
			name: "Sig verifies",
			args: args{
				beaconState: bs,
				shardBlock:  sb,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := verifyShardBlockSignature(tt.args.beaconState, tt.args.shardBlock); (err != nil) != tt.wantErr {
				t.Errorf("verifyShardBlockSignature() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestProcessShardBlock(t *testing.T) {
	sb := &ethpb.ShardBlock{Slot: 100, Body: bytesutil.PadTo([]byte{'a'}, 32), ShardParentRoot: make([]byte, 32), BeaconParentRoot: make([]byte, 32)}
	r, err := sb.HashTreeRoot()
	require.NoError(t, err)

	type args struct {
		shardState *ethpb.ShardState
		shardBlock *ethpb.ShardBlock
	}
	tests := []struct {
		name    string
		args    args
		want    *ethpb.ShardState
		wantErr bool
	}{
		{
			name: "Can process empty body",
			args: args{
				shardState: &ethpb.ShardState{},
				shardBlock: &ethpb.ShardBlock{Slot: 100}},
			want: &ethpb.ShardState{
				Slot:     100,
				GasPrice: 8,
			},
		},
		{
			name: "Can process non-empty body",
			args: args{
				shardState: &ethpb.ShardState{},
				shardBlock: sb,
			},
			want: &ethpb.ShardState{
				Slot:            100,
				GasPrice:        8,
				LatestBlockRoot: r[:],
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ProcessShardBlock(tt.args.shardState, tt.args.shardBlock)
			if (err != nil) != tt.wantErr {
				t.Errorf("ProcessShardBlock() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ProcessShardBlock() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShardStateTransition(t *testing.T) {
	bs, err := testState(params.BeaconConfig().MaxValidatorsPerCommittee)
	require.NoError(t, err)
	bh := &ethpb.BeaconBlockHeader{StateRoot: bytesutil.PadTo([]byte{'a'}, 32)}
	require.NoError(t, bs.SetLatestBlockHeader(bh))
	hr, err := stateutil.BlockHeaderRoot(bh)
	require.NoError(t, err)
	pIdx, err := helpers.ShardProposerIndex(bs, 1, 0)
	require.NoError(t, err)
	goodBlock := &ethpb.ShardBlock{
		Slot: 1, ProposerIndex: pIdx, Body: make([]byte, 1), BeaconParentRoot: hr[:], ShardParentRoot: bytesutil.PadTo([]byte{'a'}, 32),
	}
	priv := bls.RandKey()
	require.NoError(t, bs.UpdateValidatorAtIndex(pIdx, &ethpb.Validator{PublicKey: priv.PublicKey().Marshal()}))
	goodSig, err := helpers.ComputeDomainAndSign(bs, 0, goodBlock, params.BeaconConfig().DomainShardProposal, priv)
	require.NoError(t, err)
	r, err := goodBlock.HashTreeRoot()
	require.NoError(t, err)

	type args struct {
		bps        *stateTrie.BeaconState
		shardState *ethpb.ShardState
		block      *ethpb.SignedShardBlock
	}
	tests := []struct {
		name       string
		args       args
		want       *ethpb.ShardState
		wantErr    bool
		wantErrStr string
	}{
		{
			name: "Can't verify shard block message",
			args: args{
				shardState: &ethpb.ShardState{LatestBlockRoot: bytesutil.PadTo([]byte{'a'}, 32)},
				block:      &ethpb.SignedShardBlock{Message: &ethpb.ShardBlock{}},
				bps:        bs,
			},
			wantErr:    true,
			wantErrStr: "could not verify shard block message",
		},
		{
			name: "Can't verify shard block signature",
			args: args{
				shardState: &ethpb.ShardState{LatestBlockRoot: bytesutil.PadTo([]byte{'a'}, 32)},
				block:      &ethpb.SignedShardBlock{Message: goodBlock},
				bps:        bs.Copy(),
			},
			wantErr:    true,
			wantErrStr: "could not verify shard block signature",
		},
		{
			name: "Can process shard transition",
			args: args{
				shardState: &ethpb.ShardState{LatestBlockRoot: bytesutil.PadTo([]byte{'a'}, 32)},
				block:      &ethpb.SignedShardBlock{Message: goodBlock, Signature: goodSig},
				bps:        bs.Copy(),
			},
			wantErr: false,
			want: &ethpb.ShardState{
				Slot:            1,
				GasPrice:        8,
				LatestBlockRoot: r[:],
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ShardStateTransition(context.Background(), tt.args.bps, tt.args.shardState, tt.args.block)
			if tt.wantErr {
				require.ErrorContains(t, tt.wantErrStr, err)
			} else {
				require.NoError(t, err)
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("ShardStateTransition() got = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestCanCrosslink(t *testing.T) {
	helpers.ClearCache()
	indices := []uint64{0, 1, 2, 3, 4, 5}
	bs, err := testState(uint64(len(indices)))
	require.NoError(t, err)
	att1 := &ethpb.Attestation{AggregationBits: bitfield.Bitlist{0b11100}}
	att2 := &ethpb.Attestation{AggregationBits: bitfield.Bitlist{0b10011}}
	can, indices, err := CanCrosslink(bs, []*ethpb.Attestation{att1, att2}, indices)
	require.NoError(t, err)
	require.Equal(t, true, can)
	sortkeys.Uint64s(indices)
	require.DeepEqual(t, []uint64{0, 1, 2, 3}, indices)

	can, indices, err = CanCrosslink(bs, []*ethpb.Attestation{att1}, indices)
	require.NoError(t, err)
	require.Equal(t, false, can)
	sortkeys.Uint64s(indices)
	require.DeepEqual(t, []uint64{2, 3}, indices)
}

func TestVerifyAttShardHeadRoot(t *testing.T) {
	good := []byte{'a'}
	bad := []byte{'b'}
	s := []*ethpb.ShardState{{LatestBlockRoot: bad}}
	st := &ethpb.ShardTransition{ShardStates: s}
	atts := []*ethpb.Attestation{{Data: &ethpb.AttestationData{ShardHeadRoot: good}}}
	require.ErrorContains(t, "attestation shard head root is not consistent with shard state", verifyAttShardHeadRoot(st, atts))

	s = []*ethpb.ShardState{{LatestBlockRoot: bad}, {LatestBlockRoot: good}}
	st = &ethpb.ShardTransition{ShardStates: s}
	require.NoError(t, verifyAttShardHeadRoot(st, atts))
}

func TestVerifyAttTransitionRoot(t *testing.T) {
	st := &ethpb.ShardTransition{StartSlot: 999, ProposerSignatureAggregate: make([]byte, 96)}
	r, err := st.HashTreeRoot()
	require.NoError(t, err)
	require.NoError(t, verifyAttTransitionRoot(st, r))
	require.ErrorContains(t, "transition root missmatch", verifyAttTransitionRoot(st, [32]byte{'a'}))
}

func TestVerifyShardDataRootLength(t *testing.T) {
	type args struct {
		offsetSlots []uint64
		transition  *ethpb.ShardTransition
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
		err     string
	}{
		{
			name:    "incorrect shard blocks length",
			wantErr: true,
			err:     "data roots length != shard blocks length",
			args: args{
				transition: &ethpb.ShardTransition{
					ShardDataRoots:    make([][]byte, 1),
					ShardBlockLengths: make([]uint64, 2),
				},
				offsetSlots: []uint64{},
			},
		},
		{
			name:    "incorrect shard states length",
			wantErr: true,
			err:     "data roots length != shard states length",
			args: args{
				transition: &ethpb.ShardTransition{
					ShardDataRoots:    make([][]byte, 1),
					ShardBlockLengths: make([]uint64, 1),
					ShardStates:       make([]*ethpb.ShardState, 2),
				},
				offsetSlots: []uint64{},
			},
		},
		{
			name:    "incorrect offset length",
			wantErr: true,
			err:     "data roots length != offset length",
			args: args{
				transition: &ethpb.ShardTransition{
					ShardDataRoots:    make([][]byte, 1),
					ShardBlockLengths: make([]uint64, 1),
					ShardStates:       make([]*ethpb.ShardState, 1),
				},
				offsetSlots: []uint64{},
			},
		},
		{
			name:    "incorrect start slot",
			wantErr: true,
			err:     "offset start slot != transition start slot",
			args: args{
				transition: &ethpb.ShardTransition{
					ShardDataRoots:    make([][]byte, 1),
					ShardBlockLengths: make([]uint64, 1),
					ShardStates:       make([]*ethpb.ShardState, 1),
				},
				offsetSlots: []uint64{1},
			},
		},
		{
			name: "can verify",
			err:  "data roots length != shard blocks length",
			args: args{
				transition: &ethpb.ShardTransition{
					ShardDataRoots:    make([][]byte, 1),
					ShardBlockLengths: make([]uint64, 1),
					ShardStates:       make([]*ethpb.ShardState, 1),
					StartSlot:         1,
				},
				offsetSlots: []uint64{1},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyShardDataRootLength(tt.args.offsetSlots, tt.args.transition)
			if tt.wantErr {
				require.ErrorContains(t, tt.err, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func Test_ProcessCrosslink(t *testing.T) {
	helpers.ClearCache()
	bs, err := testState(params.BeaconConfig().MaxValidatorsPerCommittee)
	require.NoError(t, err)
	require.NoError(t, bs.SetSlot(3))
	st := &ethpb.ShardTransition{
		StartSlot:                  1,
		ShardBlockLengths:          []uint64{1000, 2000},
		ShardDataRoots:             [][]byte{bytesutil.PadTo([]byte{'d'}, 32), bytesutil.PadTo([]byte{'e'}, 32)},
		ShardStates:                []*ethpb.ShardState{{Slot: 1, GasPrice: 767, LatestBlockRoot: make([]byte, 32)}, {Slot: 2, GasPrice: 672, LatestBlockRoot: make([]byte, 32)}},
		ProposerSignatureAggregate: make([]byte, 96),
	}
	headers, indices, err := shardBlockProposersAndHeaders(bs, st, helpers.ShardOffSetSlots(bs, 3), 3)
	require.NoError(t, err)
	sigs := make([]bls.Signature, 0)
	for i, idx := range indices {
		sk := bls.RandKey()
		v, err := bs.ValidatorAtIndex(idx)
		require.NoError(t, err)
		v.PublicKey = sk.PublicKey().Marshal()
		require.NoError(t, bs.UpdateValidatorAtIndex(idx, v))
		s, err := helpers.ComputeDomainAndSign(bs, 0, headers[i], params.BeaconConfig().DomainShardProposal, sk)
		require.NoError(t, err)
		sig, err := bls.SignatureFromBytes(s)
		require.NoError(t, err)
		sigs = append(sigs, sig)
	}
	as := bls.AggregateSignatures(sigs)
	st.ProposerSignatureAggregate = as.Marshal()

	tr, err := st.HashTreeRoot()
	require.NoError(t, err)
	atts := []*ethpb.Attestation{
		{
			AggregationBits: bitfield.Bitlist{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			Data: &ethpb.AttestationData{
				Slot:                2,
				ShardTransitionRoot: tr[:],
				ShardHeadRoot:       make([]byte, 32),
			},
		},
	}
	a := atts[0]
	require.NoError(t, bs.SetCurrentEpochAttestations([]*pb.PendingAttestation{{
		Data: &ethpb.AttestationData{
			Slot:                a.Data.Slot,
			CommitteeIndex:      a.Data.CommitteeIndex,
			ShardTransitionRoot: a.Data.ShardTransitionRoot[:],
		}}}))
	shard, err := helpers.ShardFromCommitteeIndex(bs, a.Data.Slot, a.Data.CommitteeIndex)
	require.NoError(t, err)
	sts := make([]*ethpb.ShardTransition, 64)
	sts[shard] = st
	bs, err = processCrosslinks(bs, sts, atts)
	require.NoError(t, err)
	pa := bs.CurrentEpochAttestations()[0]
	require.Equal(t, true, pa.CrosslinkSuccess)
}

func Test_ProcessCrosslinkForShard(t *testing.T) {
	helpers.ClearCache()
	bs, err := testState(params.BeaconConfig().MaxValidatorsPerCommittee)
	require.NoError(t, err)
	require.NoError(t, bs.SetSlot(3))
	transition := &ethpb.ShardTransition{
		StartSlot:                  1,
		ShardBlockLengths:          []uint64{1000, 2000},
		ShardDataRoots:             [][]byte{bytesutil.PadTo([]byte{'d'}, 32), bytesutil.PadTo([]byte{'e'}, 32)},
		ShardStates:                []*ethpb.ShardState{{Slot: 1, GasPrice: 767, LatestBlockRoot: make([]byte, 32)}, {Slot: 2, GasPrice: 672, LatestBlockRoot: make([]byte, 32)}},
		ProposerSignatureAggregate: make([]byte, 96),
	}
	headers, indices, err := shardBlockProposersAndHeaders(bs, transition, helpers.ShardOffSetSlots(bs, 3), 3)
	require.NoError(t, err)
	sigs := make([]bls.Signature, 0)
	for i, idx := range indices {
		sk := bls.RandKey()
		v, err := bs.ValidatorAtIndex(idx)
		require.NoError(t, err)
		v.PublicKey = sk.PublicKey().Marshal()
		require.NoError(t, bs.UpdateValidatorAtIndex(idx, v))
		s, err := helpers.ComputeDomainAndSign(bs, 0, headers[i], params.BeaconConfig().DomainShardProposal, sk)
		require.NoError(t, err)
		sig, err := bls.SignatureFromBytes(s)
		require.NoError(t, err)
		sigs = append(sigs, sig)
	}
	as := bls.AggregateSignatures(sigs)
	transition.ProposerSignatureAggregate = as.Marshal()

	tr, err := transition.HashTreeRoot()
	require.NoError(t, err)
	atts := []*ethpb.Attestation{
		{
			AggregationBits: bitfield.Bitlist{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			Data: &ethpb.AttestationData{
				ShardTransitionRoot: tr[:],
				ShardHeadRoot:       make([]byte, 32),
			},
		},
	}
	wr, err := processCrosslinkForShard(bs, atts, transition, 0)
	require.NoError(t, err)
	require.Equal(t, tr, wr)
}

func Test_ApplyShardTransition(t *testing.T) {
	bs, err := testState(params.BeaconConfig().MaxValidatorsPerCommittee)
	require.NoError(t, err)
	require.NoError(t, bs.SetSlot(3))
	transition := &ethpb.ShardTransition{
		StartSlot:         1,
		ShardBlockLengths: []uint64{1000, 2000},
		ShardDataRoots:    [][]byte{bytesutil.PadTo([]byte{'d'}, 32), bytesutil.PadTo([]byte{'e'}, 32)},
		ShardStates:       []*ethpb.ShardState{{Slot: 1, GasPrice: 767}, {Slot: 2, GasPrice: 672}},
	}
	headers, indices, err := shardBlockProposersAndHeaders(bs, transition, helpers.ShardOffSetSlots(bs, 0), 0)
	require.NoError(t, err)
	sigs := make([]bls.Signature, 0)
	for i, idx := range indices {
		sk := bls.RandKey()
		v, err := bs.ValidatorAtIndex(idx)
		require.NoError(t, err)
		v.PublicKey = sk.PublicKey().Marshal()
		require.NoError(t, bs.UpdateValidatorAtIndex(idx, v))
		s, err := helpers.ComputeDomainAndSign(bs, 0, headers[i], params.BeaconConfig().DomainShardProposal, sk)
		require.NoError(t, err)
		sig, err := bls.SignatureFromBytes(s)
		require.NoError(t, err)
		sigs = append(sigs, sig)
	}
	as := bls.AggregateSignatures(sigs)
	transition.ProposerSignatureAggregate = as.Marshal()

	bs, err = applyShardTransition(bs, transition, 0)
	require.NoError(t, err)

	wanted := transition.ShardStates[len(transition.ShardStates)-1]
	require.DeepEqual(t, wanted, bs.ShardStateAtIndex(0))
}

func Test_IncBeaconProposerBal(t *testing.T) {
	bs, err := testState(params.BeaconConfig().MaxValidatorsPerCommittee)
	require.NoError(t, err)
	votedIndices := make([]uint64, 0)
	for i := uint64(0); i < params.BeaconConfig().MaxValidatorsPerCommittee; i++ {
		votedIndices = append(votedIndices, i)
	}
	require.NoError(t, err)

	bs, err = incBeaconProposerBal(bs, votedIndices)
	require.NoError(t, err)

	p, err := helpers.BeaconProposerIndex(bs)
	require.NoError(t, err)

	b, err := bs.BalanceAtIndex(p)
	require.NoError(t, err)
	require.Equal(t, true, b > params.BeaconConfig().MaxEffectiveBalance)
}

func Test_DecShardProposerBal(t *testing.T) {
	bs, err := testState(params.BeaconConfig().MaxValidatorsPerCommittee)
	require.NoError(t, err)
	require.NoError(t, bs.SetSlot(2))
	s := helpers.ShardOffSetSlots(bs, 0)
	p, err := helpers.ShardProposerIndex(bs, s[0], 0)
	require.NoError(t, err)
	l := uint64(100)
	gp := uint64(5)
	st := &ethpb.ShardTransition{
		ShardBlockLengths: []uint64{l},
		ShardStates:       []*ethpb.ShardState{{GasPrice: gp}},
	}
	bs, err = decShardProposerBal(bs, st, 0)
	require.NoError(t, err)
	b, err := bs.BalanceAtIndex(p)
	require.NoError(t, err)
	wanted := params.BeaconConfig().MaxEffectiveBalance - l*gp
	require.Equal(t, wanted, b)
}

func Test_ShardBlockProposersAndHeaders(t *testing.T) {
	bs, err := testState(params.BeaconConfig().MaxValidatorsPerCommittee)
	require.NoError(t, err)
	require.NoError(t, bs.SetSlot(3))
	offSets := []uint64{1, 2}
	transition := &ethpb.ShardTransition{
		StartSlot:         0,
		ShardBlockLengths: []uint64{1000, 2000},
		ShardDataRoots:    [][]byte{bytesutil.PadTo([]byte{'d'}, 32), bytesutil.PadTo([]byte{'e'}, 32)},
		ShardStates:       []*ethpb.ShardState{{Slot: 1, GasPrice: 767}, {Slot: 2, GasPrice: 672}},
	}
	headers, indices, err := shardBlockProposersAndHeaders(bs, transition, offSets, 0)
	require.NoError(t, err)

	p1, err := helpers.ShardProposerIndex(bs, 1, 0)
	require.NoError(t, err)
	p2, err := helpers.ShardProposerIndex(bs, 2, 0)
	require.NoError(t, err)
	require.DeepEqual(t, []uint64{p1, p2}, indices)
	h1 := &ethpb.ShardBlockHeader{Slot: 1, ProposerIndex: p1, BeaconParentRoot: bytesutil.PadTo([]byte{'b'}, 32),
		ShardParentRoot: bytesutil.PadTo([]byte{}, 32), BodyRoot: bytesutil.PadTo([]byte{'d'}, 32)}
	r1, err := ssz.HashTreeRoot(h1)
	require.NoError(t, err)
	h2 := &ethpb.ShardBlockHeader{Slot: 2, ProposerIndex: p2, BeaconParentRoot: bytesutil.PadTo([]byte{'c'}, 32),
		ShardParentRoot: r1[:], BodyRoot: bytesutil.PadTo([]byte{'e'}, 32)}
	wantedHeaders := []*ethpb.ShardBlockHeader{h1, h2}
	require.DeepEqual(t, wantedHeaders, headers)
}

func Test_VerifyProposerSignature(t *testing.T) {
	priv1 := bls.RandKey()
	priv2 := bls.RandKey()
	bs, err := testState(2)
	require.NoError(t, err)
	require.NoError(t, bs.UpdateValidatorAtIndex(0, &ethpb.Validator{PublicKey: priv1.PublicKey().Marshal()}))
	require.NoError(t, bs.UpdateValidatorAtIndex(1, &ethpb.Validator{PublicKey: priv2.PublicKey().Marshal()}))
	h1 := &ethpb.ShardBlockHeader{Slot: 1, ShardParentRoot: make([]byte, 32), BeaconParentRoot: make([]byte, 32), BodyRoot: make([]byte, 32)}
	h2 := &ethpb.ShardBlockHeader{Slot: 2, ShardParentRoot: make([]byte, 32), BeaconParentRoot: make([]byte, 32), BodyRoot: make([]byte, 32)}
	s1, err := helpers.ComputeDomainAndSign(bs, 0, h1, params.BeaconConfig().DomainShardProposal, priv1)
	require.NoError(t, err)
	s2, err := helpers.ComputeDomainAndSign(bs, 0, h2, params.BeaconConfig().DomainShardProposal, priv2)
	require.NoError(t, err)
	s1s, err := bls.SignatureFromBytes(s1)
	require.NoError(t, err)
	s2s, err := bls.SignatureFromBytes(s2)
	require.NoError(t, err)
	as := bls.AggregateSignatures([]bls.Signature{s1s, s2s})
	pIndices := []uint64{0, 1}
	require.NoError(t, verifyProposerSignature(bs, []*ethpb.ShardBlockHeader{h1, h2}, pIndices, as.Marshal()))
}

func testState(vCount uint64) (*stateTrie.BeaconState, error) {
	validators := make([]*ethpb.Validator, vCount)
	balances := make([]uint64, vCount)
	onlineCountdown := make([]uint64, vCount)
	for i := 0; i < len(validators); i++ {
		validators[i] = &ethpb.Validator{
			ExitEpoch:        params.BeaconConfig().FarFutureEpoch,
			EffectiveBalance: params.BeaconConfig().MaxEffectiveBalance,
		}
		balances[i] = params.BeaconConfig().MaxEffectiveBalance
		onlineCountdown[i] = 1
	}
	votedIndices := make([]uint64, 0)
	for i := uint64(0); i < params.BeaconConfig().MaxValidatorsPerCommittee; i++ {
		votedIndices = append(votedIndices, i)
	}
	shardState := make([]*ethpb.ShardState, helpers.ActiveShardCount())
	for i := 0; i < len(shardState); i++ {
		shardState[i] = &ethpb.ShardState{GasPrice: 876}
	}
	shardState[0].GasPrice = 876
	return stateTrie.InitializeFromProto(&pb.BeaconState{
		Fork: &pb.Fork{
			PreviousVersion: []byte{0, 0, 0, 0},
			CurrentVersion:  []byte{0, 0, 0, 0},
		},
		Validators:      validators,
		RandaoMixes:     make([][]byte, params.BeaconConfig().EpochsPerHistoricalVector),
		ShardStates:     shardState,
		OnlineCountdown: onlineCountdown,
		BlockRoots:      [][]byte{{'a'}, {'b'}, {'c'}},
		Balances:        balances,
	})
}
