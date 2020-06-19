package kv

import (
	"context"
	"reflect"
	"testing"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-ssz"
	"gopkg.in/d4l3k/messagediff.v1"
)

func TestShardState_CanSaveRetrieve(t *testing.T) {
	db := setupDB(t)

	r := [32]byte{'A'}
	if db.HasShardState(context.Background(), r) {
		t.Fatal("Wanted false")
	}

	st := &ethpb.ShardState{Slot: 100}
	if err := db.SaveShardState(context.Background(), st, r); err != nil {
		t.Fatal(err)
	}

	if !db.HasShardState(context.Background(), r) {
		t.Fatal("Wanted true")
	}
	savedS, err := db.ShardState(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(st, savedS) {
		diff, _ := messagediff.PrettyDiff(st, savedS)
		t.Errorf("Did not retrieve saved state: %v", diff)
	}

	savedS, err = db.ShardState(context.Background(), [32]byte{'B'})
	if err != nil {
		t.Fatal(err)
	}
	if savedS != nil {
		t.Error("Unsaved state should've been nil")
	}
}

func TestHeadShardState_CanSaveRetrieve(t *testing.T) {
	db := setupDB(t)

	st := &ethpb.ShardState{Slot: 100}
	headBlock := &ethpb.SignedShardBlock{
		Message: &ethpb.ShardBlock{
			Shard:           5,
			Slot:            100,
			ShardParentRoot: []byte{10, 20, 30},
		},
	}
	headRoot, err := ssz.HashTreeRoot(headBlock.Message)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveShardBlock(context.Background(), headBlock); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveShardState(context.Background(), st, headRoot); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveHeadShardBlockRoot(context.Background(), headBlock.Message.Shard, headRoot); err != nil {
		t.Fatal(err)
	}

	savedHeadS, err := db.HeadShardState(context.Background(), headBlock.Message.Shard)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(st, savedHeadS) {
		t.Error("Did not retrieve saved state")
	}

	savedHeadS, err = db.HeadShardState(context.Background(), headBlock.Message.Shard+1)
	if err != nil {
		t.Fatal(err)
	}
	if savedHeadS != nil {
		t.Error("Unsaved state should've been nil")
	}
}

func TestGenesisShardState_CanSaveRetrieve(t *testing.T) {
	db := setupDB(t)

	st := &ethpb.ShardState{GasPrice: 1}
	genesisBlk := &ethpb.SignedShardBlock{
		Message: &ethpb.ShardBlock{
			Shard: 3,
		},
	}
	genesisRoot, err := ssz.HashTreeRoot(genesisBlk.Message)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveShardBlock(context.Background(), genesisBlk); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveShardState(context.Background(), st, genesisRoot); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveGenesisShardBlockRoot(context.Background(), genesisBlk.Message.Shard, genesisRoot); err != nil {
		t.Fatal(err)
	}

	savedGenesisS, err := db.GenesisShardState(context.Background(), genesisBlk.Message.Shard)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(st, savedGenesisS) {
		t.Error("did not retrieve saved state")
	}

	savedGenesisS, err = db.GenesisShardState(context.Background(), genesisBlk.Message.Shard+1)
	if err != nil {
		t.Fatal(err)
	}
	if savedGenesisS != nil {
		t.Error("unsaved genesis state should've been nil")
	}
}
