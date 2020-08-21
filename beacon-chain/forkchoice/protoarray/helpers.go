package protoarray

import (
	"context"

	"github.com/prysmaticlabs/prysm/shared/params"
	"go.opencensus.io/trace"
)

// This computes validator balance delta from validator votes.
// It returns a list of deltas that represents the difference between old balances and new balances.
func computeDeltas(
	ctx context.Context,
	blockIndices map[[32]byte]uint64,
	votes []Vote,
	oldBalances []uint64,
	newBalances []uint64,
) ([]int, []Vote, error) {
	ctx, span := trace.StartSpan(ctx, "protoArrayForkChoice.computeDeltas")
	defer span.End()

	deltas := make([]int, len(blockIndices))

	for validatorIndex, vote := range votes {
		oldBalance := uint64(0)
		newBalance := uint64(0)

		// Skip if validator has never voted for current root and next root (ie. if the
		// votes are zero hash aka genesis block), there's nothing to compute.
		if vote.currentBeaconRoot == params.BeaconConfig().ZeroHash && vote.nextBeaconRoot == params.BeaconConfig().ZeroHash {
			continue
		}

		// If the validator index did not exist in `oldBalance` or `newBalance` list above, the balance is just 0.
		if validatorIndex < len(oldBalances) {
			oldBalance = oldBalances[validatorIndex]
		}
		if validatorIndex < len(newBalances) {
			newBalance = newBalances[validatorIndex]
		}

		// Perform delta only if the validator's balance or vote has changed.
		if vote.currentBeaconRoot != vote.nextBeaconRoot || oldBalance != newBalance {
			// Ignore the vote if it's not known in `blockIndices`,
			// that means we have not seen the block before.
			nextDeltaIndex, ok := blockIndices[vote.nextBeaconRoot]
			if ok {
				// Protection against out of bound, the `nextDeltaIndex` which defines
				// the block location in the dag can not exceed the total `delta` length.
				if int(nextDeltaIndex) >= len(deltas) {
					return nil, nil, errInvalidNodeDelta
				}
				deltas[nextDeltaIndex] += int(newBalance)
			}

			currentDeltaIndex, ok := blockIndices[vote.currentBeaconRoot]
			if ok {
				// Protection against out of bound (same as above)
				if int(currentDeltaIndex) >= len(deltas) {
					return nil, nil, errInvalidNodeDelta
				}
				deltas[currentDeltaIndex] -= int(oldBalance)
			}
		}

		// Rotate the validator vote.
		vote.currentBeaconRoot = vote.nextBeaconRoot
		votes[validatorIndex] = vote
	}

	return deltas, votes, nil
}

func computeShardDeltas(
	ctx context.Context,
	shardBlockIndices map[[32]byte]uint64,
	votes []Vote,
	oldBalances []uint64,
	newBalances []uint64,
	shard uint64,
) ([]int, []Vote, error) {
	ctx, span := trace.StartSpan(ctx, "protoArrayForkChoice.computeShardDeltas")
	defer span.End()

	deltas := make([]int, len(shardBlockIndices))
	for validatorIndex, vote := range votes {
		if vote.shard != shard {
			continue
		}

		oldBalance := uint64(0)
		newBalance := uint64(0)

		if vote.currentShardRoot == params.BeaconConfig().ZeroHash && vote.nextShardRoot == params.BeaconConfig().ZeroHash {
			continue
		}

		// If the validator index did not exist in `oldBalance` or `newBalance` list above, the balance is just 0.
		if validatorIndex < len(oldBalances) {
			oldBalance = oldBalances[validatorIndex]
		}
		if validatorIndex < len(newBalances) {
			newBalance = newBalances[validatorIndex]
		}

		// Perform delta only if the validator's balance or vote has changed.
		if vote.currentShardRoot != vote.nextShardRoot || oldBalance != newBalance {
			// Ignore the vote if it's not known in `blockIndices`,
			// that means we have not seen the block before.
			nextDeltaIndex, ok := shardBlockIndices[vote.nextBeaconRoot]
			if ok {
				// Protection against out of bound, the `nextDeltaIndex` which defines
				// the block location in the dag can not exceed the total `delta` length.
				if int(nextDeltaIndex) >= len(deltas) {
					return nil, nil, errInvalidNodeDelta
				}
				deltas[nextDeltaIndex] += int(newBalance)
			}

			currentDeltaIndex, ok := shardBlockIndices[vote.currentBeaconRoot]
			if ok {
				// Protection against out of bound (same as above)
				if int(currentDeltaIndex) >= len(deltas) {
					return nil, nil, errInvalidNodeDelta
				}
				deltas[currentDeltaIndex] -= int(oldBalance)
			}
		}

		// Rotate the validator vote.
		vote.currentShardRoot = vote.nextShardRoot
		votes[validatorIndex] = vote
	}

	return deltas, votes, nil
}

// This return a copy of the proto array node object.
func copyNode(node *Node) *Node {
	if node == nil {
		return &Node{}
	}

	copiedRoot := [32]byte{}
	copy(copiedRoot[:], node.root[:])

	return &Node{
		slot:           node.slot,
		root:           copiedRoot,
		parent:         node.parent,
		justifiedEpoch: node.justifiedEpoch,
		finalizedEpoch: node.finalizedEpoch,
		weight:         node.weight,
		bestChild:      node.bestChild,
		bestDescendant: node.bestDescendant,
	}
}
