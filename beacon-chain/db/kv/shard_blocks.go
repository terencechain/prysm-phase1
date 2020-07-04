package kv

import (
	"context"
	"encoding/binary"

	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-ssz"
	bolt "go.etcd.io/bbolt"
	"go.opencensus.io/trace"
)

// ShardBlock retrieval by root.
func (k *Store) ShardBlock(ctx context.Context, blockRoot [32]byte) (*ethpb.SignedShardBlock, error) {
	ctx, span := trace.StartSpan(ctx, "BeaconDB.ShardBlock")
	defer span.End()

	var block *ethpb.SignedShardBlock
	err := k.db.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(shardBlocksBucket)
		enc := bkt.Get(blockRoot[:])
		if enc == nil {
			return nil
		}
		block = &ethpb.SignedShardBlock{}
		return decode(ctx, enc, block)
	})

	return block, err
}

// HeadShardBlock returns the latest canonical shard block of a given shard in eth2.
func (k *Store) HeadShardBlock(ctx context.Context, shard uint64) (*ethpb.SignedShardBlock, error) {
	ctx, span := trace.StartSpan(ctx, "BeaconDB.HeadShardBlock")
	defer span.End()
	var headBlock *ethpb.SignedShardBlock
	err := k.db.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(shardBlocksBucket)
		headRoot := bkt.Get(headShardBlockRootKey(shard))
		if headRoot == nil {
			return nil
		}
		enc := bkt.Get(headRoot)
		if enc == nil {
			return nil
		}
		headBlock = &ethpb.SignedShardBlock{}
		return decode(ctx, enc, headBlock)
	})
	return headBlock, err
}

// HasShardBlock checks if a shard block by root exists in the db.
func (k *Store) HasShardBlock(ctx context.Context, blockRoot [32]byte) bool {
	ctx, span := trace.StartSpan(ctx, "BeaconDB.HasShardBlock")
	defer span.End()

	exists := false
	if err := k.db.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(shardBlocksBucket)
		exists = bkt.Get(blockRoot[:]) != nil
		return nil
	}); err != nil { // This view never returns an error, but we'll handle anyway for sanity.
		panic(err)
	}
	return exists
}

// SaveShardBlock to the db.
func (k *Store) SaveShardBlock(ctx context.Context, signed *ethpb.SignedShardBlock) error {
	ctx, span := trace.StartSpan(ctx, "BeaconDB.SaveShardBlock")
	defer span.End()
	blockRoot, err := ssz.HashTreeRoot(signed.Message)
	if err != nil {
		return err
	}
	return k.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(shardBlocksBucket)

		if existingBlock := bkt.Get(blockRoot[:]); existingBlock != nil {
			return nil
		}
		enc, err := encode(ctx, signed)
		if err != nil {
			return err
		}
		return bkt.Put(blockRoot[:], enc)
	})
}

// SaveHeadShardBlockRoot to the db.
func (k *Store) SaveHeadShardBlockRoot(ctx context.Context, shard uint64, blockRoot [32]byte) error {
	ctx, span := trace.StartSpan(ctx, "BeaconDB.SaveHeadShardBlockRoot")
	defer span.End()
	return k.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(shardBlocksBucket)
		if bucket.Get(blockRoot[:]) == nil {
			return errors.New("no block found with head block root")
		}

		return bucket.Put(headShardBlockRootKey(shard), blockRoot[:])
	})
}

// GenesisShardBlock retrieves the genesis shard block of the shard chain chain.
func (k *Store) GenesisShardBlock(ctx context.Context, shard uint64) (*ethpb.SignedShardBlock, error) {
	ctx, span := trace.StartSpan(ctx, "BeaconDB.GenesisShardBlock")
	defer span.End()
	var block *ethpb.SignedShardBlock
	err := k.db.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(shardBlocksBucket)
		root := bkt.Get(genesisShardBlockRootKey(shard))
		enc := bkt.Get(root)
		if enc == nil {
			return nil
		}
		block = &ethpb.SignedShardBlock{}
		return decode(ctx, enc, block)
	})
	return block, err
}

// SaveGenesisShardBlockRoot to the db.
func (k *Store) SaveGenesisShardBlockRoot(ctx context.Context, shard uint64, blockRoot [32]byte) error {
	ctx, span := trace.StartSpan(ctx, "BeaconDB.SaveGenesisShardBlockRoot")
	defer span.End()
	return k.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(shardBlocksBucket)
		if bucket.Get(blockRoot[:]) == nil {
			return errors.New("no block found with genesis block root")
		}

		return bucket.Put(genesisShardBlockRootKey(shard), blockRoot[:])
	})
}

func headShardBlockRootKey(shard uint64) []byte {
	shardBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(shardBuf, shard)
	return append([]byte("head-shard-block-root-"), shardBuf...)
}

func genesisShardBlockRootKey(shard uint64) []byte {
	shardBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(shardBuf, shard)
	return append([]byte("genesis-shard-block-root-"), shardBuf...)
}
