package blockchain

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/state"
	"github.com/prysmaticlabs/prysm/beacon-chain/flags"
	stateTrie "github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/shared/attestationutil"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/featureconfig"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/sirupsen/logrus"
	"go.opencensus.io/trace"
)

// This defines size of the upper bound for initial sync block cache.
var initialSyncBlockCacheSize = 2 * params.BeaconConfig().SlotsPerEpoch

// onBlock is called when a gossip block is received. It runs regular state transition on the block.
// The block's signing root should be computed before calling this method to avoid redundant
// computation in this method and methods it calls into.
//
// Spec pseudocode definition:
//   def on_block(store: Store, block: BeaconBlock) -> None:
//    # Make a copy of the state to avoid mutability issues
//    assert block.parent_root in store.block_states
//    pre_state = store.block_states[block.parent_root].copy()
//    # Blocks cannot be in the future. If they are, their consideration must be delayed until the are in the past.
//    assert store.time >= pre_state.genesis_time + block.slot * SECONDS_PER_SLOT
//    # Add new block to the store
//    store.blocks[signing_root(block)] = block
//    # Check block is a descendant of the finalized block
//    assert (
//        get_ancestor(store, signing_root(block), store.blocks[store.finalized_checkpoint.root].slot) ==
//        store.finalized_checkpoint.root
//    )
//    # Check that block is later than the finalized epoch slot
//    assert block.slot > compute_start_slot_of_epoch(store.finalized_checkpoint.epoch)
//    # Check the block is valid and compute the post-state
//    state = state_transition(pre_state, block)
//    # Add new state for this block to the store
//    store.block_states[signing_root(block)] = state
//
//    # Update justified checkpoint
//    if state.current_justified_checkpoint.epoch > store.justified_checkpoint.epoch:
//        if state.current_justified_checkpoint.epoch > store.best_justified_checkpoint.epoch:
//            store.best_justified_checkpoint = state.current_justified_checkpoint
//
//    # Update finalized checkpoint
//    if state.finalized_checkpoint.epoch > store.finalized_checkpoint.epoch:
//        store.finalized_checkpoint = state.finalized_checkpoint
func (s *Service) onBlock(ctx context.Context, signed *ethpb.SignedBeaconBlock, blockRoot [32]byte) (*stateTrie.BeaconState, error) {
	ctx, span := trace.StartSpan(ctx, "blockchain.onBlock")
	defer span.End()

	if signed == nil || signed.Block == nil {
		return nil, errors.New("nil block")
	}

	b := signed.Block

	// Retrieve incoming block's pre state.
	preState, err := s.getBlockPreState(ctx, b)
	if err != nil {
		return nil, err
	}

	log.WithFields(logrus.Fields{
		"slot": b.Slot,
		"root": fmt.Sprintf("0x%s...", hex.EncodeToString(blockRoot[:])[:8]),
	}).Debug("Executing state transition on block")

	postState, err := state.ExecuteStateTransition(ctx, preState, signed)
	if err != nil {
		return nil, errors.Wrap(err, "could not execute state transition")
	}

	if err := s.beaconDB.SaveBlock(ctx, signed); err != nil {
		return nil, errors.Wrapf(err, "could not save block from slot %d", b.Slot)
	}

	if err := s.insertBlockToForkChoiceStore(ctx, b, blockRoot, postState); err != nil {
		return nil, errors.Wrapf(err, "could not insert block %d to fork choice store", b.Slot)
	}

	if featureconfig.Get().NewStateMgmt {
		if err := s.stateGen.SaveState(ctx, blockRoot, postState); err != nil {
			return nil, errors.Wrap(err, "could not save state")
		}
	} else {
		if err := s.beaconDB.SaveState(ctx, postState, blockRoot); err != nil {
			return nil, errors.Wrap(err, "could not save state")
		}
	}

	// Update justified check point.
	if postState.CurrentJustifiedCheckpoint().Epoch > s.justifiedCheckpt.Epoch {
		if err := s.updateJustified(ctx, postState); err != nil {
			return nil, err
		}
	}

	// Update finalized check point. Prune the block cache and helper caches on every new finalized epoch.
	if postState.FinalizedCheckpointEpoch() > s.finalizedCheckpt.Epoch {
		if !featureconfig.Get().NoInitSyncBatchSaveBlocks {
			if err := s.beaconDB.SaveBlocks(ctx, s.getInitSyncBlocks()); err != nil {
				return nil, err
			}
			s.clearInitSyncBlocks()
		}

		if err := s.beaconDB.SaveFinalizedCheckpoint(ctx, postState.FinalizedCheckpoint()); err != nil {
			return nil, errors.Wrap(err, "could not save finalized checkpoint")
		}

		if !featureconfig.Get().NewStateMgmt {
			startSlot := helpers.StartSlot(s.prevFinalizedCheckpt.Epoch)
			endSlot := helpers.StartSlot(s.finalizedCheckpt.Epoch)
			if endSlot > startSlot {
				if err := s.rmStatesOlderThanLastFinalized(ctx, startSlot, endSlot); err != nil {
					return nil, errors.Wrapf(err, "could not delete states prior to finalized check point, range: %d, %d",
						startSlot, endSlot)
				}
			}
		}
		fRoot := bytesutil.ToBytes32(postState.FinalizedCheckpoint().Root)

		// Prune proto array fork choice nodes, all nodes before finalized check point will
		// be pruned.
		if err := s.forkChoiceStore.Prune(ctx, fRoot); err != nil {
			return nil, errors.Wrap(err, "could not prune proto array fork choice nodes")
		}

		s.prevFinalizedCheckpt = s.finalizedCheckpt
		s.finalizedCheckpt = postState.FinalizedCheckpoint()

		if err := s.finalizedImpliesNewJustified(ctx, postState); err != nil {
			return nil, errors.Wrap(err, "could not save new justified")
		}

		if featureconfig.Get().NewStateMgmt {
			fRoot := bytesutil.ToBytes32(postState.FinalizedCheckpoint().Root)
			fBlock, err := s.beaconDB.Block(ctx, fRoot)
			if err != nil {
				return nil, errors.Wrap(err, "could not get finalized block to migrate")
			}
			if err := s.stateGen.MigrateToCold(ctx, fBlock.Block.Slot, fRoot); err != nil {
				return nil, errors.Wrap(err, "could not migrate to cold")
			}
		}
	}

	// Epoch boundary bookkeeping such as logging epoch summaries.
	if postState.Slot() >= s.nextEpochBoundarySlot {
		logEpochData(postState)
		reportEpochMetrics(postState)

		// Update committees cache at epoch boundary slot.
		if err := helpers.UpdateCommitteeCache(postState, helpers.CurrentEpoch(postState)); err != nil {
			return nil, err
		}
		if err := helpers.UpdateProposerIndicesInCache(postState, helpers.CurrentEpoch(postState)); err != nil {
			return nil, err
		}

		s.nextEpochBoundarySlot = helpers.StartSlot(helpers.NextEpoch(postState))
	}

	// Delete the processed block attestations from attestation pool.
	if err := s.deletePoolAtts(b.Body.Attestations); err != nil {
		return nil, err
	}

	// Delete the processed block attester slashings from slashings pool.
	for i := 0; i < len(b.Body.AttesterSlashings); i++ {
		s.slashingPool.MarkIncludedAttesterSlashing(b.Body.AttesterSlashings[i])
	}

	defer reportAttestationInclusion(b)

	return postState, nil
}

// onBlockInitialSyncStateTransition is called when an initial sync block is received.
// It runs state transition on the block and without any BLS verification. The excluded BLS verification
// includes attestation's aggregated signature. It also does not save attestations.
// The block's signing root should be computed before calling this method to avoid redundant
// computation in this method and methods it calls into.
func (s *Service) onBlockInitialSyncStateTransition(ctx context.Context, signed *ethpb.SignedBeaconBlock, blockRoot [32]byte) error {
	ctx, span := trace.StartSpan(ctx, "blockchain.onBlock")
	defer span.End()

	if signed == nil || signed.Block == nil {
		return errors.New("nil block")
	}

	b := signed.Block

	// Retrieve incoming block's pre state.
	preState, err := s.verifyBlkPreState(ctx, b)
	if err != nil {
		return err
	}
	// To invalidate cache for parent root because pre state will get mutated.
	s.stateGen.DeleteHotStateInCache(bytesutil.ToBytes32(b.ParentRoot))

	// Exit early if the pre state slot is higher than incoming block's slot.
	if preState.Slot() >= signed.Block.Slot {
		return nil
	}

	postState, err := state.ExecuteStateTransitionNoVerifyAttSigs(ctx, preState, signed)
	if err != nil {
		return errors.Wrap(err, "could not execute state transition")
	}

	if !featureconfig.Get().NoInitSyncBatchSaveBlocks {
		s.saveInitSyncBlock(blockRoot, signed)
	} else {
		if err := s.beaconDB.SaveBlock(ctx, signed); err != nil {
			return errors.Wrapf(err, "could not save block from slot %d", b.Slot)
		}
	}

	if err := s.insertBlockToForkChoiceStore(ctx, b, blockRoot, postState); err != nil {
		return errors.Wrapf(err, "could not insert block %d to fork choice store", b.Slot)
	}

	if featureconfig.Get().NewStateMgmt {
		if err := s.stateGen.SaveState(ctx, blockRoot, postState); err != nil {
			return errors.Wrap(err, "could not save state")
		}
	} else {
		s.initSyncStateLock.Lock()
		defer s.initSyncStateLock.Unlock()
		s.initSyncState[blockRoot] = postState.Copy()
		s.filterBoundaryCandidates(ctx, blockRoot, postState)
	}

	if flags.Get().EnableArchive {
		atts := signed.Block.Body.Attestations
		if err := s.beaconDB.SaveAttestations(ctx, atts); err != nil {
			return errors.Wrapf(err, "could not save block attestations from slot %d", b.Slot)
		}
	}

	// Update justified check point.
	if postState.CurrentJustifiedCheckpoint().Epoch > s.justifiedCheckpt.Epoch {
		if err := s.updateJustified(ctx, postState); err != nil {
			return err
		}
	}

	// Rate limit how many blocks (2 epochs worth of blocks) a node keeps in the memory.
	if len(s.getInitSyncBlocks()) > int(initialSyncBlockCacheSize) {
		if err := s.beaconDB.SaveBlocks(ctx, s.getInitSyncBlocks()); err != nil {
			return err
		}
		s.clearInitSyncBlocks()
	}

	// Update finalized check point. Prune the block cache and helper caches on every new finalized epoch.
	if postState.FinalizedCheckpointEpoch() > s.finalizedCheckpt.Epoch {
		if !featureconfig.Get().NewStateMgmt {
			startSlot := helpers.StartSlot(s.prevFinalizedCheckpt.Epoch)
			endSlot := helpers.StartSlot(s.finalizedCheckpt.Epoch)
			if endSlot > startSlot {
				if err := s.rmStatesOlderThanLastFinalized(ctx, startSlot, endSlot); err != nil {
					return errors.Wrapf(err, "could not delete states prior to finalized check point, range: %d, %d",
						startSlot, endSlot)
				}
			}

			if err := s.saveInitState(ctx, postState); err != nil {
				return errors.Wrap(err, "could not save init sync finalized state")
			}
		}

		if !featureconfig.Get().NoInitSyncBatchSaveBlocks {
			if err := s.beaconDB.SaveBlocks(ctx, s.getInitSyncBlocks()); err != nil {
				return err
			}
			s.clearInitSyncBlocks()
		}

		if err := s.beaconDB.SaveFinalizedCheckpoint(ctx, postState.FinalizedCheckpoint()); err != nil {
			return errors.Wrap(err, "could not save finalized checkpoint")
		}

		s.prevFinalizedCheckpt = s.finalizedCheckpt
		s.finalizedCheckpt = postState.FinalizedCheckpoint()

		if err := s.finalizedImpliesNewJustified(ctx, postState); err != nil {
			return errors.Wrap(err, "could not save new justified")
		}

		if featureconfig.Get().NewStateMgmt {
			fRoot := bytesutil.ToBytes32(postState.FinalizedCheckpoint().Root)
			fBlock, err := s.beaconDB.Block(ctx, fRoot)
			if err != nil {
				return errors.Wrap(err, "could not get finalized block to migrate")
			}
			if err := s.stateGen.MigrateToCold(ctx, fBlock.Block.Slot, fRoot); err != nil {
				return errors.Wrap(err, "could not migrate to cold")
			}
		}
	}

	if !featureconfig.Get().NewStateMgmt {
		numOfStates := len(s.boundaryRoots)
		if numOfStates > initialSyncCacheSize {
			if err = s.persistCachedStates(ctx, numOfStates); err != nil {
				return err
			}
		}
		if len(s.initSyncState) > maxCacheSize {
			s.pruneOldNonFinalizedStates()
		}
	}

	// Epoch boundary bookkeeping such as logging epoch summaries.
	if postState.Slot() >= s.nextEpochBoundarySlot {
		reportEpochMetrics(postState)
		s.nextEpochBoundarySlot = helpers.StartSlot(helpers.NextEpoch(postState))

		// Update committees cache at epoch boundary slot.
		if err := helpers.UpdateCommitteeCache(postState, helpers.CurrentEpoch(postState)); err != nil {
			return err
		}
		if err := helpers.UpdateProposerIndicesInCache(postState, helpers.CurrentEpoch(postState)); err != nil {
			return err
		}

		if !featureconfig.Get().NewStateMgmt && helpers.IsEpochStart(postState.Slot()) {
			if err := s.beaconDB.SaveState(ctx, postState, blockRoot); err != nil {
				return errors.Wrap(err, "could not save state")
			}
		}
	}

	return nil
}

// This feeds in the block and block's attestations to fork choice store. It's allows fork choice store
// to gain information on the most current chain.
func (s *Service) insertBlockToForkChoiceStore(ctx context.Context, blk *ethpb.BeaconBlock, root [32]byte, state *stateTrie.BeaconState) error {
	if err := s.fillInForkChoiceMissingBlocks(ctx, blk, state); err != nil {
		return err
	}

	// Feed in block to fork choice store.
	if err := s.forkChoiceStore.ProcessBlock(ctx,
		blk.Slot, root, bytesutil.ToBytes32(blk.ParentRoot), bytesutil.ToBytes32(blk.Body.Graffiti),
		state.CurrentJustifiedCheckpoint().Epoch,
		state.FinalizedCheckpointEpoch()); err != nil {
		return errors.Wrap(err, "could not process block for proto array fork choice")
	}

	// Feed in block's attestations to fork choice store.
	for _, a := range blk.Body.Attestations {
		committee, err := helpers.BeaconCommitteeFromState(state, a.Data.Slot, a.Data.CommitteeIndex)
		if err != nil {
			return err
		}
		indices := attestationutil.AttestingIndices(a.AggregationBits, committee)
		shard, err := helpers.ShardFromAttestation(state, a)
		if err != nil {
			return err
		}
		s.forkChoiceStore.ProcessAttestation(
			ctx,
			indices,
			bytesutil.ToBytes32(a.Data.BeaconBlockRoot),
			a.Data.Target.Epoch,
			bytesutil.ToBytes32(a.Data.ShardHeadRoot),
			shard,
			)
	}

	return nil
}
