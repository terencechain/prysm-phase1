package kv

import (
	"context"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/testutil"
)

func TestStore_ShardBlocksCRUD(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	block := testutil.NewShardblock()
	block.Message.Slot = 20
	block.Message.ShardParentRoot = bytesutil.PadTo([]byte{1, 2, 3}, 32)
	blockRoot, err := block.Message.HashTreeRoot()
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
	headBlock := testutil.NewShardblock()
	headBlock.Message.Slot = 100
	headBlock.Message.ShardParentRoot = bytesutil.PadTo([]byte{10, 20, 33}, 32)
	blockRoot, err := headBlock.Message.HashTreeRoot()
	if err != nil {
		t.Fatal(err)
	}
	shard := uint64(63)
	if err := db.SaveShardBlock(ctx, headBlock); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveHeadShardBlockRoot(ctx, shard, blockRoot); err != nil {
		t.Fatal(err)
	}
	retrievedBlock, err := db.HeadShardBlock(ctx, shard)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(headBlock, retrievedBlock) {
		t.Errorf("Wanted %v, received %v", headBlock, retrievedBlock)
	}
}

func TestStore_GenesisShardBlock(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	genesisBlock := testutil.NewShardblock()
	genesisBlock.Message.Body = []byte{1, 2, 3}
	blockRoot, err := genesisBlock.Message.HashTreeRoot()
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
