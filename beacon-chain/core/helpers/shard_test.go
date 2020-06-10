package helpers

import (
	"reflect"
	"sort"
	"testing"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	stateTrie "github.com/prysmaticlabs/prysm/beacon-chain/state"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/params"
)

func TestShardCommittee(t *testing.T) {
	ClearCache()

	shardCommitteeSizePerEpoch := uint64(4)
	validators := make([]*ethpb.Validator, shardCommitteeSizePerEpoch*params.BeaconConfig().SlotsPerEpoch)
	for i := 0; i < len(validators); i++ {
		validators[i] = &ethpb.Validator{
			ExitEpoch: params.BeaconConfig().FarFutureEpoch,
		}
	}
	beaconState, err := stateTrie.InitializeFromProto(&pb.BeaconState{
		Slot:        params.BeaconConfig().SlotsPerEpoch,
		Validators:  validators,
		RandaoMixes: make([][]byte, params.BeaconConfig().EpochsPerHistoricalVector),
		ShardStates: make([]*ethpb.ShardState, 64),
	})
	if err != nil {
		t.Fatal(err)
	}

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
		s := &pb.BeaconState{}
		state, err := stateTrie.InitializeFromProto(s)
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
		s := &pb.BeaconState{Slot: tt.endSlot}
		beaconState, err := stateTrie.InitializeFromProto(s)
		if err != nil {
			t.Fatal(err)
		}
		shardState := &ethpb.ShardState{Slot: tt.startSlot}
		if beaconState.SetShardState([]*ethpb.ShardState{shardState}) != nil {
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
	s := &pb.BeaconState{ShardStates: make([]*ethpb.ShardState, shardCount)}
	state, err := stateTrie.InitializeFromProto(s)
	if err != nil {
		t.Fatal(err)
	}

	if ActiveShardCount(state) != shardCount {
		t.Fatal("Did not get correct active shard count")
	}
}
