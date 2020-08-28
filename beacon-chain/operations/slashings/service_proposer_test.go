package slashings

import (
	"context"
	"testing"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/testutil"
	"github.com/prysmaticlabs/prysm/shared/testutil/assert"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
)

func proposerSlashingForValIdx(valIdx uint64) *ethpb.ProposerSlashing {
	return &ethpb.ProposerSlashing{
		Header_1: &ethpb.SignedBeaconBlockHeader{
			Header: &ethpb.BeaconBlockHeader{ProposerIndex: valIdx},
		},
		Header_2: &ethpb.SignedBeaconBlockHeader{
			Header: &ethpb.BeaconBlockHeader{ProposerIndex: valIdx},
		},
	}
}

func TestPool_InsertProposerSlashing(t *testing.T) {
	type fields struct {
		wantedErr string
		pending   []*ethpb.ProposerSlashing
		included  map[uint64]bool
	}
	type args struct {
		slashings []*ethpb.ProposerSlashing
	}

	beaconState, privKeys := testutil.DeterministicGenesisState(t, 64)
	slashings := make([]*ethpb.ProposerSlashing, 20)
	for i := 0; i < len(slashings); i++ {
		sl, err := testutil.GenerateProposerSlashingForValidator(beaconState, privKeys[i], uint64(i))
		require.NoError(t, err)
		slashings[i] = sl
	}

	require.NoError(t, beaconState.SetSlot(helpers.StartSlot(1)))

	// We mark the following validators with some preconditions.
	exitedVal, err := beaconState.ValidatorAtIndex(uint64(2))
	require.NoError(t, err)
	exitedVal.WithdrawableEpoch = 0
	futureExitedVal, err := beaconState.ValidatorAtIndex(uint64(4))
	require.NoError(t, err)
	futureExitedVal.WithdrawableEpoch = 17
	slashedVal, err := beaconState.ValidatorAtIndex(uint64(5))
	require.NoError(t, err)
	slashedVal.Slashed = true
	require.NoError(t, beaconState.UpdateValidatorAtIndex(uint64(2), exitedVal))
	require.NoError(t, beaconState.UpdateValidatorAtIndex(uint64(4), futureExitedVal))
	require.NoError(t, beaconState.UpdateValidatorAtIndex(uint64(5), slashedVal))

	tests := []struct {
		name   string
		fields fields
		args   args
		want   []*ethpb.ProposerSlashing
	}{
		{
			name: "Empty list",
			fields: fields{
				pending:  make([]*ethpb.ProposerSlashing, 0),
				included: make(map[uint64]bool),
			},
			args: args{
				slashings: slashings[0:1],
			},
			want: slashings[0:1],
		},
		{
			name: "Duplicate identical slashing",
			fields: fields{
				pending:   slashings[0:1],
				included:  make(map[uint64]bool),
				wantedErr: "slashing object already exists in pending proposer slashings",
			},
			args: args{
				slashings: slashings[0:1],
			},
			want: slashings[0:1],
		},
		{
			name: "Slashing for exited validator",
			fields: fields{
				pending:   []*ethpb.ProposerSlashing{},
				included:  make(map[uint64]bool),
				wantedErr: "is not slashable",
			},
			args: args{
				slashings: slashings[2:3],
			},
			want: []*ethpb.ProposerSlashing{},
		},
		{
			name: "Slashing for exiting validator",
			fields: fields{
				pending:  []*ethpb.ProposerSlashing{},
				included: make(map[uint64]bool),
			},
			args: args{
				slashings: slashings[4:5],
			},
			want: slashings[4:5],
		},
		{
			name: "Slashing for slashed validator",
			fields: fields{
				pending:   []*ethpb.ProposerSlashing{},
				included:  make(map[uint64]bool),
				wantedErr: "not slashable",
			},
			args: args{
				slashings: slashings[5:6],
			},
			want: []*ethpb.ProposerSlashing{},
		},
		{
			name: "Already included",
			fields: fields{
				pending: []*ethpb.ProposerSlashing{},
				included: map[uint64]bool{
					1: true,
				},
				wantedErr: "cannot be slashed",
			},
			args: args{
				slashings: slashings[1:2],
			},
			want: []*ethpb.ProposerSlashing{},
		},
		{
			name: "Maintains sorted order",
			fields: fields{
				pending: []*ethpb.ProposerSlashing{
					slashings[0],
					slashings[2],
				},
				included: make(map[uint64]bool),
			},
			args: args{
				slashings: slashings[1:2],
			},
			want: []*ethpb.ProposerSlashing{
				slashings[0],
				slashings[1],
				slashings[2],
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Pool{
				pendingProposerSlashing: tt.fields.pending,
				included:                tt.fields.included,
			}
			var err error
			for i := 0; i < len(tt.args.slashings); i++ {
				err = p.InsertProposerSlashing(context.Background(), beaconState, tt.args.slashings[i])
			}
			if tt.fields.wantedErr != "" {
				require.ErrorContains(t, tt.fields.wantedErr, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, len(tt.want), len(p.pendingProposerSlashing))
			for i := range p.pendingAttesterSlashing {
				assert.Equal(t, p.pendingProposerSlashing[i].Header_1.Header.ProposerIndex, tt.want[i].Header_1.Header.ProposerIndex)
				assert.DeepEqual(t, tt.want[i], p.pendingProposerSlashing[i], "Proposer slashing at index %d does not match expected", i)
			}
		})
	}
}

func TestPool_InsertProposerSlashing_SigFailsVerify_ClearPool(t *testing.T) {
	params.SetupTestConfigCleanup(t)
	conf := params.BeaconConfig()
	conf.MaxAttesterSlashings = 2
	params.OverrideBeaconConfig(conf)
	beaconState, privKeys := testutil.DeterministicGenesisState(t, 64)
	slashings := make([]*ethpb.ProposerSlashing, 2)
	for i := 0; i < 2; i++ {
		sl, err := testutil.GenerateProposerSlashingForValidator(beaconState, privKeys[i], uint64(i))
		require.NoError(t, err)
		slashings[i] = sl
	}
	// We mess up the signature of the second slashing.
	badSig := make([]byte, 96)
	copy(badSig, "muahaha")
	slashings[1].Header_1.Signature = badSig
	p := &Pool{
		pendingProposerSlashing: make([]*ethpb.ProposerSlashing, 0),
	}
	// We only want a single slashing to remain.
	require.NoError(t, p.InsertProposerSlashing(context.Background(), beaconState, slashings[0]))
	err := p.InsertProposerSlashing(context.Background(), beaconState, slashings[1])
	require.ErrorContains(t, "could not verify proposer slashing", err, "Expected slashing with bad signature to fail")
	assert.Equal(t, 1, len(p.pendingProposerSlashing))
}

func TestPool_MarkIncludedProposerSlashing(t *testing.T) {
	type fields struct {
		pending  []*ethpb.ProposerSlashing
		included map[uint64]bool
	}
	type args struct {
		slashing *ethpb.ProposerSlashing
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   fields
	}{
		{
			name: "Included, does not exist in pending",
			fields: fields{
				pending: []*ethpb.ProposerSlashing{
					proposerSlashingForValIdx(1),
				},
				included: make(map[uint64]bool),
			},
			args: args{
				slashing: proposerSlashingForValIdx(3),
			},
			want: fields{
				pending: []*ethpb.ProposerSlashing{
					proposerSlashingForValIdx(1),
				},
				included: map[uint64]bool{
					3: true,
				},
			},
		},
		{
			name: "Removes from pending list",
			fields: fields{
				pending: []*ethpb.ProposerSlashing{
					proposerSlashingForValIdx(1),
					proposerSlashingForValIdx(2),
					proposerSlashingForValIdx(3),
				},
				included: map[uint64]bool{
					0: true,
				},
			},
			args: args{
				slashing: proposerSlashingForValIdx(2),
			},
			want: fields{
				pending: []*ethpb.ProposerSlashing{
					proposerSlashingForValIdx(1),
					proposerSlashingForValIdx(3),
				},
				included: map[uint64]bool{
					0: true,
					2: true,
				},
			},
		},
		{
			name: "Removes from pending long list",
			fields: fields{
				pending: []*ethpb.ProposerSlashing{
					proposerSlashingForValIdx(1),
					proposerSlashingForValIdx(2),
					proposerSlashingForValIdx(3),
					proposerSlashingForValIdx(4),
					proposerSlashingForValIdx(5),
					proposerSlashingForValIdx(6),
					proposerSlashingForValIdx(7),
					proposerSlashingForValIdx(8),
					proposerSlashingForValIdx(9),
					proposerSlashingForValIdx(10),
				},
				included: map[uint64]bool{
					0: true,
				},
			},
			args: args{
				slashing: proposerSlashingForValIdx(7),
			},
			want: fields{
				pending: []*ethpb.ProposerSlashing{
					proposerSlashingForValIdx(1),
					proposerSlashingForValIdx(2),
					proposerSlashingForValIdx(3),
					proposerSlashingForValIdx(4),
					proposerSlashingForValIdx(5),
					proposerSlashingForValIdx(6),
					proposerSlashingForValIdx(8),
					proposerSlashingForValIdx(9),
					proposerSlashingForValIdx(10),
				},
				included: map[uint64]bool{
					0: true,
					7: true,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Pool{
				pendingProposerSlashing: tt.fields.pending,
				included:                tt.fields.included,
			}
			p.MarkIncludedProposerSlashing(tt.args.slashing)
			assert.Equal(t, len(tt.want.pending), len(p.pendingProposerSlashing))
			for i := range p.pendingProposerSlashing {
				assert.DeepEqual(t, tt.want.pending[i], p.pendingProposerSlashing[i], "Unexpected pending proposer slashing at index %d", i)
			}
			assert.DeepEqual(t, tt.want.included, p.included)
		})
	}
}

func TestPool_PendingProposerSlashings(t *testing.T) {
	type fields struct {
		pending []*ethpb.ProposerSlashing
	}
	beaconState, privKeys := testutil.DeterministicGenesisState(t, 64)
	slashings := make([]*ethpb.ProposerSlashing, 20)
	for i := 0; i < len(slashings); i++ {
		sl, err := testutil.GenerateProposerSlashingForValidator(beaconState, privKeys[i], uint64(i))
		require.NoError(t, err)
		slashings[i] = sl
	}
	tests := []struct {
		name   string
		fields fields
		want   []*ethpb.ProposerSlashing
	}{
		{
			name: "Empty list",
			fields: fields{
				pending: []*ethpb.ProposerSlashing{},
			},
			want: []*ethpb.ProposerSlashing{},
		},
		{
			name: "All eligible",
			fields: fields{
				pending: slashings[:params.BeaconConfig().MaxProposerSlashings],
			},
			want: slashings[:params.BeaconConfig().MaxProposerSlashings],
		},
		{
			name: "Multiple indices",
			fields: fields{
				pending: slashings[3:6],
			},
			want: slashings[3:6],
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Pool{
				pendingProposerSlashing: tt.fields.pending,
			}
			assert.DeepEqual(t, tt.want, p.PendingProposerSlashings(context.Background(), beaconState))
		})
	}
}

func TestPool_PendingProposerSlashings_Slashed(t *testing.T) {
	type fields struct {
		pending []*ethpb.ProposerSlashing
	}
	beaconState, privKeys := testutil.DeterministicGenesisState(t, 64)
	val, err := beaconState.ValidatorAtIndex(0)
	require.NoError(t, err)
	val.Slashed = true
	require.NoError(t, beaconState.UpdateValidatorAtIndex(0, val))
	val, err = beaconState.ValidatorAtIndex(5)
	require.NoError(t, err)
	val.Slashed = true
	require.NoError(t, beaconState.UpdateValidatorAtIndex(5, val))
	slashings := make([]*ethpb.ProposerSlashing, 32)
	for i := 0; i < len(slashings); i++ {
		sl, err := testutil.GenerateProposerSlashingForValidator(beaconState, privKeys[i], uint64(i))
		require.NoError(t, err)
		slashings[i] = sl
	}
	result := make([]*ethpb.ProposerSlashing, 32)
	copy(result, slashings)
	tests := []struct {
		name   string
		fields fields
		want   []*ethpb.ProposerSlashing
	}{
		{
			name: "removes slashed",
			fields: fields{
				pending: slashings,
			},
			want: append(result[1:5], result[6:18]...),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Pool{
				pendingProposerSlashing: tt.fields.pending,
			}
			assert.DeepEqual(t, tt.want, p.PendingProposerSlashings(context.Background(), beaconState))
		})
	}
}
