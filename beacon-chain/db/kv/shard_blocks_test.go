package kv

import (
	"context"
	"testing"

	"github.com/gogo/protobuf/proto"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-ssz"
)

func TestStore_ShardBlocksCRUD(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	block := &ethpb.SignedShardBlock{
		Message: &ethpb.ShardBlock{
			Slot:            20,
			ShardParentRoot: []byte{1, 2, 3},
		},
	}
	blockRoot, err := ssz.HashTreeRoot(block.Message)
	if err != nil {
		t.Fatal(err)
	}
	retrievedBlock, err := db.ShardBlock(ctx, blockRoot)
	if err != nil {
		t.Fatal(err)
	}
	if retrievedBlock != nil {
		t.Errorf("Expected nil block, received %v", retrievedBlock)
	}
	if err := db.SaveShardBlock(ctx, block); err != nil {
		t.Fatal(err)
	}
	if !db.HasShardBlock(ctx, blockRoot) {
		t.Error("Expected block to exist in the db")
	}
	retrievedBlock, err = db.ShardBlock(ctx, blockRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(block, retrievedBlock) {
		t.Errorf("Wanted %v, received %v", block, retrievedBlock)
	}
}

func TestStore_HeadShardBlock(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	headblock := &ethpb.SignedShardBlock{
		Message: &ethpb.ShardBlock{
			Slot:            100,
			ShardParentRoot: []byte{10, 20, 30},
		},
	}
	blockRoot, err := ssz.HashTreeRoot(headblock.Message)
	if err != nil {
		t.Fatal(err)
	}
	shard := uint64(63)
	if err := db.SaveShardBlock(ctx, headblock); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveHeadShardBlockRoot(ctx, shard, blockRoot); err != nil {
		t.Fatal(err)
	}
	retrievedBlock, err := db.HeadShardBlock(ctx, shard)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(headblock, retrievedBlock) {
		t.Errorf("Wanted %v, received %v", headblock, retrievedBlock)
	}
}

func TestStore_GenesisShardBlock(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	genesisBlock := &ethpb.SignedShardBlock{
		Message: &ethpb.ShardBlock{
			Slot:            0,
			ShardParentRoot: []byte{1, 2, 3},
		},
	}
	blockRoot, err := ssz.HashTreeRoot(genesisBlock.Message)
	if err != nil {
		t.Fatal(err)
	}
	shard := uint64(63)
	if err := db.SaveShardBlock(ctx, genesisBlock); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveGenesisShardBlockRoot(ctx, shard, blockRoot); err != nil {
		t.Fatal(err)
	}
	retrievedBlock, err := db.GenesisShardBlock(ctx, shard)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(genesisBlock, retrievedBlock) {
		t.Errorf("Wanted %v, received %v", genesisBlock, retrievedBlock)
	}
}
