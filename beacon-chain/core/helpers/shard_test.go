package helpers

import (
	"reflect"
	"sort"
	"testing"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	stateTrie "github.com/prysmaticlabs/prysm/beacon-chain/state"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
)

func TestShardCommittee(t *testing.T) {
	ClearCache()

	shardCommitteeSizePerEpoch := uint64(4)
	beaconState, err := testState(shardCommitteeSizePerEpoch * params.BeaconConfig().SlotsPerEpoch)
	require.NoError(t, err)
	tests := []struct {
		epoch     uint64
		shard     uint64
		committee []uint64
	}{
		{
			epoch:     0,
			shard:     0,
			committee: []uint64{111, 67},
		},
		{
			epoch:     0,
			shard:     1,
			committee: []uint64{78, 114},
		},
		{
			epoch:     params.ShardConfig().ShardCommitteePeriod,
			shard:     0,
			committee: []uint64{111, 67},
		},
	}

	for _, tt := range tests {
		committee, err := ShardCommittee(beaconState, tt.epoch, tt.shard)
		if err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(tt.committee, committee) {
			t.Errorf(
				"Result committee was an unexpected value. Wanted %d, got %d",
				tt.committee,
				committee,
			)
		}
	}
}

func TestUpdatedGasPrice(t *testing.T) {
	tests := []struct {
		prevGasPrice     uint64
		shardBlockLength uint64
		finalGasPrice    uint64
	}{
		{
			// Test max gas price is the upper bound.
			prevGasPrice:     params.ShardConfig().MaxGasPrice + 1,
			shardBlockLength: params.ShardConfig().TargetShardBlockSize + 1,
			finalGasPrice:    params.ShardConfig().MaxGasPrice,
		},
		{
			// Test min gas price is the lower bound.
			prevGasPrice:     0,
			shardBlockLength: params.ShardConfig().TargetShardBlockSize - 1,
			finalGasPrice:    params.ShardConfig().MinGasPrice,
		},
		{
			// Test max gas price is the upper bound.
			prevGasPrice:     10000,
			shardBlockLength: params.ShardConfig().TargetShardBlockSize + 10000,
			finalGasPrice:    10047,
		},
		{
			// Test decreasing gas price.
			prevGasPrice:     100000000,
			shardBlockLength: params.ShardConfig().TargetShardBlockSize - 1,
			finalGasPrice:    99999953,
		},
	}

	for _, tt := range tests {
		if UpdatedGasPrice(tt.prevGasPrice, tt.shardBlockLength) != tt.finalGasPrice {
			t.Errorf("UpdatedGasPrice(%d, %d) = %d, wanted: %d", tt.prevGasPrice, tt.shardBlockLength,
				UpdatedGasPrice(tt.prevGasPrice, tt.shardBlockLength), tt.finalGasPrice)
		}
	}
}

func TestOnlineValidatorIndices(t *testing.T) {
	tests := []struct {
		totalIndices  []uint64
		onlineIndices map[int]bool
		wantedIndices []uint64
	}{
		{
			totalIndices:  []uint64{0, 1, 2, 3},
			wantedIndices: []uint64{},
		},
		{
			totalIndices:  []uint64{0, 1, 2, 3},
			onlineIndices: map[int]bool{0: true},
			wantedIndices: []uint64{0},
		},
		{
			totalIndices:  []uint64{0, 1, 2, 3},
			onlineIndices: map[int]bool{0: true, 1: true, 2: true, 3: true},
			wantedIndices: []uint64{0, 1, 2, 3},
		},
		{
			totalIndices:  []uint64{0, 1, 2, 3, 4, 5},
			onlineIndices: map[int]bool{1: true, 3: true, 5: true},
			wantedIndices: []uint64{1, 3, 5},
		},
	}

	for _, tt := range tests {
		ClearCache()
		state, err := stateTrie.InitializeFromProto(&pb.BeaconState{})
		if err != nil {
			t.Fatal(err)
		}
		validators := make([]*ethpb.Validator, len(tt.totalIndices))
		for i := 0; i < len(validators); i++ {
			validators[i] = &ethpb.Validator{ExitEpoch: params.BeaconConfig().FarFutureEpoch}
		}
		if err := state.SetValidators(validators); err != nil {
			t.Fatal(err)
		}
		onlineCountDown := make([]uint64, len(tt.totalIndices))
		for i := 0; i < len(onlineCountDown); i++ {
			if tt.onlineIndices[i] {
				onlineCountDown[i] = 1
			}
		}
		if err := state.SetOnlineCountdowns(onlineCountDown); err != nil {
			t.Fatal(err)
		}
		onlineValidators, err := OnlineValidatorIndices(state)
		if err != nil {
			t.Fatal(err)
		}

		sort.Slice(onlineValidators, func(i, j int) bool {
			return onlineValidators[i] < onlineValidators[j]
		})
		if !reflect.DeepEqual(onlineValidators, tt.wantedIndices) {
			t.Fatalf("online indices was not an expected value. Wanted: %v, got: %v", tt.wantedIndices, onlineValidators)
		}
	}
}

func TestShardOffSetSlots(t *testing.T) {
	tests := []struct {
		startSlot   uint64
		endSlot     uint64
		offsetSlots []uint64
	}{
		{
			startSlot:   0,
			endSlot:     0,
			offsetSlots: []uint64{},
		},
		{
			startSlot:   0,
			endSlot:     1,
			offsetSlots: []uint64{},
		},
		{
			startSlot:   0,
			endSlot:     2,
			offsetSlots: []uint64{1},
		},
		{
			startSlot:   0,
			endSlot:     100,
			offsetSlots: []uint64{1, 2, 3, 5, 8, 13, 21, 34, 55, 89},
		},
		{
			startSlot:   50,
			endSlot:     100,
			offsetSlots: []uint64{51, 52, 53, 55, 58, 63, 71, 84},
		},
		{
			startSlot:   90,
			endSlot:     100,
			offsetSlots: []uint64{91, 92, 93, 95, 98},
		},
	}

	for _, tt := range tests {
		beaconState, err := stateTrie.InitializeFromProto(&pb.BeaconState{Slot: tt.endSlot})
		if err != nil {
			t.Fatal(err)
		}
		shardState := &ethpb.ShardState{Slot: tt.startSlot}
		if beaconState.SetShardStates([]*ethpb.ShardState{shardState}) != nil {
			t.Fatal(err)
		}
		offsetSlots := ShardOffSetSlots(beaconState, 0)
		if !reflect.DeepEqual(offsetSlots, tt.offsetSlots) {
			t.Errorf("offset slot was not an expected value. Wanted: %v, got: %v", tt.offsetSlots, offsetSlots)
		}
	}
}

func TestComputeOffsetSlot(t *testing.T) {
	tests := []struct {
		startSlot   uint64
		endSlot     uint64
		offsetSlots []uint64
	}{
		{
			startSlot:   0,
			endSlot:     0,
			offsetSlots: []uint64{},
		},
		{
			startSlot:   0,
			endSlot:     1,
			offsetSlots: []uint64{},
		},
		{
			startSlot:   0,
			endSlot:     2,
			offsetSlots: []uint64{1},
		},
		{
			startSlot:   0,
			endSlot:     100,
			offsetSlots: []uint64{1, 2, 3, 5, 8, 13, 21, 34, 55, 89},
		},
		{
			startSlot:   50,
			endSlot:     100,
			offsetSlots: []uint64{51, 52, 53, 55, 58, 63, 71, 84},
		},
		{
			startSlot:   90,
			endSlot:     100,
			offsetSlots: []uint64{91, 92, 93, 95, 98},
		},
	}

	for _, tt := range tests {
		offsetSlots := ComputeOffsetSlots(tt.startSlot, tt.endSlot)
		if !reflect.DeepEqual(offsetSlots, tt.offsetSlots) {
			t.Errorf("offset slot was not an expected value. Wanted: %v, got: %v", tt.offsetSlots, offsetSlots)
		}
	}
}

func TestIsEmptyShardTransition(t *testing.T) {
	tests := []struct {
		transition *ethpb.ShardTransition
		wanted     bool
	}{
		{&ethpb.ShardTransition{}, true},
		{&ethpb.ShardTransition{StartSlot: 1}, false},
		{&ethpb.ShardTransition{ShardBlockLengths: []uint64{1}}, false},
		{&ethpb.ShardTransition{ShardDataRoots: [][]byte{{}}}, false},
		{&ethpb.ShardTransition{ShardStates: []*ethpb.ShardState{{}}}, false},
	}
	for _, tt := range tests {
		if IsEmptyShardTransition(tt.transition) != tt.wanted {
			t.Errorf("IsEmptyShardTransition reported: %v", tt.transition)
		}
	}
}

func TestCommitteeCountDelta(t *testing.T) {
	tests := []struct {
		startSlot  uint64
		endSlot    uint64
		countDelta uint64
	}{
		{0, 0, 0},
		{0, 1, 4},
		{0, 2, 8},
		{0, 31, 124},
		{0, 32, 128},
	}
	validators := make([]*ethpb.Validator, params.BeaconConfig().MinGenesisActiveValidatorCount)
	for i := 0; i < len(validators); i++ {
		validators[i] = &ethpb.Validator{
			ExitEpoch: params.BeaconConfig().FarFutureEpoch,
		}
	}
	beaconState, err := stateTrie.InitializeFromProto(&pb.BeaconState{
		Validators: validators,
		Slot:       33,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		ClearCache()
		count, err := CommitteeCountDelta(beaconState, tt.startSlot, tt.endSlot)
		if err != nil {
			t.Fatal(err)
		}
		if count != tt.countDelta {
			t.Errorf("CommitteeCountDelta(%d, %d) = %d, wanted %d", tt.startSlot, tt.endSlot, count, tt.countDelta)
		}
	}
}

func TestActiveShardCount(t *testing.T) {
	shardCount := uint64(64)
	if ActiveShardCount() != shardCount {
		t.Fatal("Did not get correct active shard count")
	}
}

func TestIsOntimeAttestation(t *testing.T) {
	tests := []struct {
		att    *ethpb.Attestation
		slot   uint64
		wanted bool
	}{
		{&ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 2}}, 2, false},
		{&ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 1}}, 2, true},
		{&ethpb.Attestation{Data: &ethpb.AttestationData{}}, 1, true},
		{&ethpb.Attestation{Data: &ethpb.AttestationData{}}, 0, true},
	}
	for _, tt := range tests {
		if IsOnTimeAttData(tt.att.Data, tt.slot) != tt.wanted {
			t.Errorf("isOnTimeAttestation verification fails: %v", IsOnTimeAttData(tt.att.Data, tt.slot))
		}
	}
}

func TestOnTimeAttsByCommitteeID(t *testing.T) {
	tests := []struct {
		inputAtts []*ethpb.Attestation
		goodAtts  []*ethpb.Attestation
	}{
		{
			inputAtts: []*ethpb.Attestation{
				{Data: &ethpb.AttestationData{CommitteeIndex: 1}},
				{Data: &ethpb.AttestationData{CommitteeIndex: 2}},
				{Data: &ethpb.AttestationData{CommitteeIndex: 3, BeaconBlockRoot: []byte{'a'}}},
				{Data: &ethpb.AttestationData{CommitteeIndex: 3, BeaconBlockRoot: []byte{'b'}}},
				{Data: &ethpb.AttestationData{CommitteeIndex: 3, Slot: 2}}, // Not on time.
			},
			goodAtts: []*ethpb.Attestation{
				{Data: &ethpb.AttestationData{CommitteeIndex: 1}},
				{Data: &ethpb.AttestationData{CommitteeIndex: 2}},
				{Data: &ethpb.AttestationData{CommitteeIndex: 3, BeaconBlockRoot: []byte{'a'}}},
				{Data: &ethpb.AttestationData{CommitteeIndex: 3, BeaconBlockRoot: []byte{'b'}}}},
		},
		{
			inputAtts: []*ethpb.Attestation{
				{Data: &ethpb.AttestationData{CommitteeIndex: 60}},
				{Data: &ethpb.AttestationData{CommitteeIndex: 60, Slot: 1}}, // Not on time.
				{Data: &ethpb.AttestationData{CommitteeIndex: 61}},
				{Data: &ethpb.AttestationData{CommitteeIndex: 61, BeaconBlockRoot: []byte{'a'}}},
				{Data: &ethpb.AttestationData{CommitteeIndex: 61, BeaconBlockRoot: []byte{'a'}}},
			},
			goodAtts: []*ethpb.Attestation{
				{Data: &ethpb.AttestationData{CommitteeIndex: 60}},
				{Data: &ethpb.AttestationData{CommitteeIndex: 61}},
				{Data: &ethpb.AttestationData{CommitteeIndex: 61, BeaconBlockRoot: []byte{'a'}}},
				{Data: &ethpb.AttestationData{CommitteeIndex: 61, BeaconBlockRoot: []byte{'a'}}},
			},
		},
	}
	for _, tt := range tests {
		wanted := make([][]*ethpb.Attestation, params.BeaconConfig().MaxCommitteesPerSlot)
		for _, a := range tt.goodAtts {
			wanted[a.Data.CommitteeIndex] = append(wanted[a.Data.CommitteeIndex], a)
		}
		received := OnTimeAttsByCommitteeID(tt.inputAtts, 1)
		if !reflect.DeepEqual(received, wanted) {
			t.Error("Did not receive wanted atts")
		}
	}
}

func TestAttsByTransitionRoot(t *testing.T) {
	tests := []struct {
		inputAtts []*ethpb.Attestation
	}{
		{
			inputAtts: []*ethpb.Attestation{
				{Data: &ethpb.AttestationData{ShardTransitionRoot: []byte{'a'}}},
				{Data: &ethpb.AttestationData{ShardTransitionRoot: []byte{'a'}}},
				{Data: &ethpb.AttestationData{ShardTransitionRoot: []byte{'a'}}},
				{Data: &ethpb.AttestationData{ShardTransitionRoot: []byte{'b'}}},
				{Data: &ethpb.AttestationData{ShardTransitionRoot: []byte{'b'}}},
				{Data: &ethpb.AttestationData{ShardTransitionRoot: []byte{'c'}}},
			},
		},
		{
			inputAtts: []*ethpb.Attestation{
				{Data: &ethpb.AttestationData{ShardTransitionRoot: []byte{'z'}}},
				{Data: &ethpb.AttestationData{ShardTransitionRoot: []byte{'x'}}},
				{Data: &ethpb.AttestationData{ShardTransitionRoot: []byte{'y'}}},
			},
		},
		{
			inputAtts: []*ethpb.Attestation{
				{Data: &ethpb.AttestationData{}},
				{Data: &ethpb.AttestationData{}},
				{Data: &ethpb.AttestationData{ShardTransitionRoot: []byte{}}},
				{Data: &ethpb.AttestationData{ShardTransitionRoot: []byte{}}},
			},
		},
	}
	for _, tt := range tests {
		wanted := make(map[[32]byte][]*ethpb.Attestation)
		for _, a := range tt.inputAtts {
			r := bytesutil.ToBytes32(a.Data.ShardTransitionRoot)
			atts, ok := wanted[r]
			if ok {
				wanted[r] = []*ethpb.Attestation{a}
			} else {
				wanted[r] = append(atts, a)
			}
		}
		received := AttsByTransitionRoot(tt.inputAtts)
		if !reflect.DeepEqual(received, wanted) {
			t.Error("Did not receive wanted atts")
		}
	}
}

func TestShardFromCommitteeIndex(t *testing.T) {
	bs, err := testState(params.BeaconConfig().MaxValidatorsPerCommittee)
	require.NoError(t, err)
	type args struct {
		beaconState *stateTrie.BeaconState
		slot        uint64
		committeeID uint64
	}
	tests := []struct {
		name string
		args args
		want uint64
	}{
		{
			name: "slot 1, committee 0",
			args: args{beaconState: bs, slot: 1, committeeID: 0},
			want: 1,
		},
		{
			name: "slot 1, committee 1",
			args: args{beaconState: bs, slot: 1, committeeID: 1},
			want: 2,
		},
		{
			name: "slot 2, committee 0",
			args: args{beaconState: bs, slot: 2, committeeID: 3},
			want: 5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ShardFromCommitteeIndex(tt.args.beaconState, tt.args.slot, tt.args.committeeID)
			require.NoError(t, err)
			require.Equal(t, got, tt.want)
		})
	}
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
	return stateTrie.InitializeFromProto(&pb.BeaconState{
		Fork: &pb.Fork{
			PreviousVersion: []byte{0, 0, 0, 0},
			CurrentVersion:  []byte{0, 0, 0, 0},
		},
		Validators:      validators,
		RandaoMixes:     make([][]byte, params.BeaconConfig().EpochsPerHistoricalVector),
		ShardStates:     make([]*ethpb.ShardState, 64),
		OnlineCountdown: onlineCountdown,
		BlockRoots:      [][]byte{{'a'}, {'b'}, {'c'}},
		Balances:        balances,
	})
}
