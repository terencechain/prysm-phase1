package blockchain

import (
	"context"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
)

// PendingShardBlocks returns the canonical shard block branch that has not yet been crosslinked.
//
// Spec code:
// def get_pending_shard_blocks(store: Store, shard_store: ShardStore) -> Sequence[ShardBlock]:
//    """
//    Return the canonical shard block branch that has not yet been crosslinked.
//    """
//    shard = shard_store.shard
//
//    beacon_head_root = get_head(store)
//    beacon_head_state = store.block_states[beacon_head_root]
//    latest_shard_block_root = beacon_head_state.shard_states[shard].latest_block_root
//
//    shard_head_root = get_shard_head(store, shard_store)
//    root = shard_head_root
//    shard_blocks = []
//    while root != latest_shard_block_root:
//        shard_block = shard_store.blocks[root]
//        shard_blocks.append(shard_block)
//        root = shard_block.shard_parent_root
//
//    shard_blocks.reverse()
//    return shard_blocks
func (s *Service) PendingShardBlocks(ctx context.Context, shard uint64) ([]*ethpb.SignedShardBlock, error) {
	beaconHeadRoot := s.headRoot()
	beaconHeadState := s.headState()

	latestShardBlockRoot := bytesutil.ToBytes32(beaconHeadState.ShardStateAtIndex(shard).LatestBlockRoot)
	root, err := s.forkChoiceStore.ShardHead(ctx, beaconHeadRoot, s.justifiedBalances, shard)
	if err != nil {
		return nil, err
	}

	shardBlocks := make([]*ethpb.SignedShardBlock, 0)
	for root != latestShardBlockRoot {
		// TODO(0): Batch / use proto array fork choice
		shardBlock, err := s.beaconDB.ShardBlock(ctx, root)
		if err != nil {
			return nil, err
		}
		shardBlocks = append(shardBlocks, shardBlock)
		root = bytesutil.ToBytes32(shardBlock.Message.ShardParentRoot)
	}

	// Reverse the shard blocks ordering before it returns.
	for i, j := 0, len(shardBlocks)-1; i < j; i, j = i+1, j-1 {
		shardBlocks[i], shardBlocks[j] = shardBlocks[j], shardBlocks[i]
	}

	return shardBlocks, nil
}
