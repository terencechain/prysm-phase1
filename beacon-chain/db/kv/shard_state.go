package kv

import (
	"context"

	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	bolt "go.etcd.io/bbolt"
	"go.opencensus.io/trace"
)

// ShardState returns the saved shard state using block's signing root,
// this particular block was used to generate the state.
func (k *Store) ShardState(ctx context.Context, blockRoot [32]byte) (*ethpb.ShardState, error) {
	ctx, span := trace.StartSpan(ctx, "BeaconDB.ShardState")
	defer span.End()
	var s *ethpb.ShardState
	err := k.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(shardStateBucket)
		enc := bucket.Get(blockRoot[:])
		if enc == nil {
			return nil
		}

		var err error
		s, err = createShardState(ctx, enc)
		return err
	})
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, nil
	}

	return s, nil
}

// HeadShardState returns the latest canonical shard state of a given shard in beacon chain.
func (k *Store) HeadShardState(ctx context.Context, shard uint64) (*ethpb.ShardState, error) {
	ctx, span := trace.StartSpan(ctx, "BeaconDB.HeadShardState")
	defer span.End()
	var s *ethpb.ShardState
	err := k.db.View(func(tx *bolt.Tx) error {
		// Retrieve head block's signing root from blocks bucket,
		// to look up what the head state is.
		bucket := tx.Bucket(shardBlocksBucket)
		headBlkRoot := bucket.Get(headShardBlockRootKey(shard))

		bucket = tx.Bucket(shardStateBucket)
		enc := bucket.Get(headBlkRoot)
		if enc == nil {
			return nil
		}

		var err error
		s, err = createShardState(ctx, enc)
		return err
	})
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, nil
	}
	return s, nil
}

// GenesisShardState returns the genesis shard state of a given shard.
func (k *Store) GenesisShardState(ctx context.Context, shard uint64) (*ethpb.ShardState, error) {
	ctx, span := trace.StartSpan(ctx, "BeaconDB.GenesisShardState")
	defer span.End()
	var s *ethpb.ShardState
	err := k.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(shardBlocksBucket)
		genesisBlockRoot := bucket.Get(genesisShardBlockRootKey(shard))

		bucket = tx.Bucket(shardStateBucket)
		enc := bucket.Get(genesisBlockRoot)
		if enc == nil {
			return nil
		}

		var err error
		s, err = createShardState(ctx, enc)
		return err
	})
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, nil
	}
	return s, nil
}

// SaveShardState stores a state to the db using block's signing root which was used to generate the state.
func (k *Store) SaveShardState(ctx context.Context, state *ethpb.ShardState, blockRoot [32]byte) error {
	ctx, span := trace.StartSpan(ctx, "BeaconDB.SaveShardState")
	defer span.End()
	if state == nil {
		return errors.New("nil state")
	}
	enc, err := encode(ctx, state)
	if err != nil {
		return err
	}

	return k.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(shardStateBucket)
		return bucket.Put(blockRoot[:], enc)
	})
}

// HasShardState checks if a shard state by root exists in the db.
func (k *Store) HasShardState(ctx context.Context, blockRoot [32]byte) bool {
	ctx, span := trace.StartSpan(ctx, "BeaconDB.HasShardState")
	defer span.End()
	var exists bool
	if err := k.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(shardStateBucket)
		exists = bucket.Get(blockRoot[:]) != nil
		return nil
	}); err != nil { // This view never returns an error, but we'll handle anyway for sanity.
		panic(err)
	}
	return exists
}

// creates shard state from marshaled proto state bytes.
func createShardState(ctx context.Context, enc []byte) (*ethpb.ShardState, error) {
	protoState := &ethpb.ShardState{}
	err := decode(ctx, enc, protoState)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal encoding")
	}
	return protoState, nil
}
