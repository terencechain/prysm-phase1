package stategen

import (
	"context"

	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/state"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"go.opencensus.io/trace"
)

// HasState returns true if the state exists in cache or in DB.
func (s *State) HasState(ctx context.Context, blockRoot [32]byte) bool {
	if s.hotStateCache.Has(blockRoot) {
		return true
	}

	return s.beaconDB.HasState(ctx, blockRoot)
}

// SaveStateSummary saves the relevant state summary for a block and its corresponding state slot in the
// state summary cache.
func (s *State) SaveStateSummary(ctx context.Context, blk *ethpb.SignedBeaconBlock, blockRoot [32]byte) {
	// Save State summary
	s.stateSummaryCache.Put(blockRoot, &pb.StateSummary{
		Slot: blk.Block.Slot,
		Root: blockRoot[:],
	})
}

// This saves a post finalized beacon state in the hot section of the DB. On the epoch boundary,
// it saves a full state. On an intermediate slot, it saves a back pointer to the
// nearest epoch boundary state.
func (s *State) saveHotState(ctx context.Context, blockRoot [32]byte, state *state.BeaconState) error {
	ctx, span := trace.StartSpan(ctx, "stateGen.saveHotState")
	defer span.End()

	// If the hot state is already in cache, one can be sure the state was processed and in the DB.
	if s.hotStateCache.Has(blockRoot) {
		return nil
	}

	// Only on an epoch boundary slot, saves epoch boundary state in epoch boundary root state cache.
	if helpers.IsEpochStart(state.Slot()) {
		if err := s.epochBoundaryStateCache.put(blockRoot, state); err != nil {
			return err
		}
	}

	// On an intermediate slots, save the hot state summary.
	s.stateSummaryCache.Put(blockRoot, &pb.StateSummary{
		Slot: state.Slot(),
		Root: blockRoot[:],
	})

	// Store the copied state in the hot state cache.
	s.hotStateCache.Put(blockRoot, state)

	return nil
}

// This loads a post finalized beacon state from the hot section of the DB. If necessary it will
// replay blocks starting from the nearest epoch boundary state or last finalized state. It returns the beacon state that
// corresponds to the input block root.
func (s *State) loadHotStateByRoot(ctx context.Context, blockRoot [32]byte) (*state.BeaconState, error) {
	ctx, span := trace.StartSpan(ctx, "stateGen.loadHotStateByRoot")
	defer span.End()

	// First, it checks if the state exists in hot state cache.
	cachedState := s.hotStateCache.Get(blockRoot)
	if cachedState != nil {
		return cachedState, nil
	}

	// Second, it checks if the state exits in epoch boundary state cache.
	cachedInfo, ok, err := s.epochBoundaryStateCache.getByRoot(blockRoot)
	if err != nil {
		return nil, err
	}
	if ok {
		return cachedInfo.state, nil
	}

	summary, err := s.stateSummary(ctx, blockRoot)
	if err != nil {
		return nil, errors.Wrap(err, "could not get state summary")
	}
	targetSlot := summary.Slot

	// Since the requested state is not in caches, start replaying using the last available ancestor state which is
	// retrieved using input block's parent root.
	startState, err := s.lastAncestorState(ctx, blockRoot)
	if err != nil {
		return nil, errors.Wrap(err, "could not get ancestor state")
	}
	if startState == nil {
		return nil, errUnknownBoundaryState
	}

	blks, err := s.LoadBlocks(ctx, startState.Slot()+1, targetSlot, bytesutil.ToBytes32(summary.Root))
	if err != nil {
		return nil, errors.Wrap(err, "could not load blocks for hot state using root")
	}

	return s.ReplayBlocks(ctx, startState, blks, targetSlot)
}

// This loads a hot state by slot where the slot lies between the epoch boundary points.
// This is a slower implementation (versus ByRoot) as slot is the only argument. It require fetching
// all the blocks between the epoch boundary points for playback.
// Use `loadHotStateByRoot` unless you really don't know the root.
func (s *State) loadHotStateBySlot(ctx context.Context, slot uint64) (*state.BeaconState, error) {
	ctx, span := trace.StartSpan(ctx, "stateGen.loadHotStateBySlot")
	defer span.End()

	// Return genesis state if slot is 0.
	if slot == 0 {
		return s.beaconDB.GenesisState(ctx)
	}

	// Gather last saved state, that is where node starts to replay the blocks.
	startState, err := s.lastSavedState(ctx, slot)

	// Gather the last saved block root and the slot number.
	lastValidRoot, lastValidSlot, err := s.lastSavedBlock(ctx, slot)
	if err != nil {
		return nil, errors.Wrap(err, "could not get last valid block for hot state using slot")
	}

	// Load and replay blocks to get the intermediate state.
	replayBlks, err := s.LoadBlocks(ctx, startState.Slot()+1, lastValidSlot, lastValidRoot)
	if err != nil {
		return nil, err
	}

	return s.ReplayBlocks(ctx, startState, replayBlks, slot)
}

// This returns the highest available ancestor state of the input block root.
// It recursively look up block's parent until a corresponding state of the block root
// is found in the caches or DB.
//
// There's three ways to derive block parent state:
// 1.) block parent state is the last finalized state
// 2.) block parent state is the epoch boundary state and exists in epoch boundary cache.
// 3.) block parent state is in DB.
func (s *State) lastAncestorState(ctx context.Context, root [32]byte) (*state.BeaconState, error) {
	ctx, span := trace.StartSpan(ctx, "stateGen.lastAncestorState")
	defer span.End()

	if s.isFinalizedRoot(root) && s.finalizedState() != nil {
		return s.finalizedState(), nil
	}

	b, err := s.beaconDB.Block(ctx, root)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, errUnknownBlock
	}

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Is the state a genesis state.
		parentRoot := bytesutil.ToBytes32(b.Block.ParentRoot)
		if parentRoot == params.BeaconConfig().ZeroHash {
			return s.beaconDB.GenesisState(ctx)
		}

		// Does the state exist in finalized info cache.
		if s.isFinalizedRoot(parentRoot) {
			return s.finalizedState(), nil
		}

		// Does the state exist in epoch boundary cache.
		cachedInfo, ok, err := s.epochBoundaryStateCache.getByRoot(parentRoot)
		if err != nil {
			return nil, err
		}
		if ok {
			return cachedInfo.state, nil
		}

		// Does the state exists in DB.
		if s.beaconDB.HasState(ctx, parentRoot) {
			return s.beaconDB.State(ctx, parentRoot)
		}
		b, err = s.beaconDB.Block(ctx, parentRoot)
		if err != nil {
			return nil, err
		}
		if b == nil {
			return nil, errUnknownBlock
		}
	}
}
