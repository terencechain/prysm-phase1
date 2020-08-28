package initialsync

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/paulbellamy/ratecounter"
	"github.com/pkg/errors"
	eth "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/state"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/featureconfig"
	"github.com/sirupsen/logrus"
)

const (
	// counterSeconds is an interval over which an average rate will be calculated.
	counterSeconds = 20
	// refreshTime defines an interval at which suitable peer is checked during 2nd phase of sync.
	refreshTime = 6 * time.Second
)

// blockReceiverFn defines block receiving function.
type blockReceiverFn func(ctx context.Context, block *eth.SignedBeaconBlock, blockRoot [32]byte) error

type batchBlockReceiverFn func(ctx context.Context, blks []*eth.SignedBeaconBlock, roots [][32]byte) error

// Round Robin sync looks at the latest peer statuses and syncs with the highest
// finalized peer.
//
// Step 1 - Sync to finalized epoch.
// Sync with peers of lowest finalized root with epoch greater than head state.
//
// Step 2 - Sync to head from finalized epoch.
// Using the finalized root as the head_block_root and the epoch start slot
// after the finalized epoch, request blocks to head from some subset of peers
// where step = 1.
func (s *Service) roundRobinSync(genesis time.Time) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer s.chain.ClearCachedStates()
	state.SkipSlotCache.Disable()
	defer state.SkipSlotCache.Enable()

	s.counter = ratecounter.NewRateCounter(counterSeconds * time.Second)
	s.lastProcessedSlot = s.chain.HeadSlot()
	highestFinalizedSlot := helpers.StartSlot(s.highestFinalizedEpoch() + 1)
	queue := newBlocksQueue(ctx, &blocksQueueConfig{
		p2p:                 s.p2p,
		headFetcher:         s.chain,
		finalizationFetcher: s.chain,
		highestExpectedSlot: highestFinalizedSlot,
		mode:                modeStopOnFinalizedEpoch,
	})
	if err := queue.start(); err != nil {
		return err
	}

	// Step 1 - Sync to end of finalized epoch.
	for data := range queue.fetchedData {
		s.processFetchedData(ctx, genesis, s.chain.HeadSlot(), data)
	}

	log.WithFields(logrus.Fields{
		"syncedSlot": s.chain.HeadSlot(),
		"headSlot":   helpers.SlotsSince(genesis),
	}).Debug("Synced to finalized epoch - now syncing blocks up to current head")
	if err := queue.stop(); err != nil {
		log.WithError(err).Debug("Error stopping queue")
	}

	if s.chain.HeadSlot() == helpers.SlotsSince(genesis) {
		return nil
	}

	// Step 2 - sync to head from any single peer.
	// This step might need to be improved for cases where there has been a long period since
	// finality. This step is less important than syncing to finality in terms of threat
	// mitigation. We are already convinced that we are on the correct finalized chain. Any blocks
	// we receive there after must build on the finalized chain or be considered invalid during
	// fork choice resolution / block processing.
	queue = newBlocksQueue(ctx, &blocksQueueConfig{
		p2p:                 s.p2p,
		headFetcher:         s.chain,
		finalizationFetcher: s.chain,
		highestExpectedSlot: helpers.SlotsSince(genesis),
		mode:                modeNonConstrained,
	})
	if err := queue.start(); err != nil {
		return err
	}
	for data := range queue.fetchedData {
		s.processFetchedDataRegSync(ctx, genesis, s.chain.HeadSlot(), data)
	}
	log.WithFields(logrus.Fields{
		"syncedSlot": s.chain.HeadSlot(),
		"headSlot":   helpers.SlotsSince(genesis),
	}).Debug("Synced to head of chain")
	if err := queue.stop(); err != nil {
		log.WithError(err).Debug("Error stopping queue")
	}

	return nil
}

// processFetchedData processes data received from queue.
func (s *Service) processFetchedData(
	ctx context.Context, genesis time.Time, startSlot uint64, data *blocksQueueFetchedData) {
	defer s.updatePeerScorerStats(data.pid, startSlot)

	blockReceiver := s.chain.ReceiveBlockInitialSync
	batchReceiver := s.chain.ReceiveBlockBatch

	// Use Batch Block Verify to process and verify batches directly.
	if featureconfig.Get().BatchBlockVerify {
		if err := s.processBatchedBlocks(ctx, genesis, data.blocks, batchReceiver); err != nil {
			log.WithError(err).Debug("Batch is not processed")
		}
		return
	}
	for _, blk := range data.blocks {
		if err := s.processBlock(ctx, genesis, blk, blockReceiver); err != nil {
			log.WithError(err).Debug("Block is not processed")
			continue
		}
	}
}

// processFetchedData processes data received from queue.
func (s *Service) processFetchedDataRegSync(
	ctx context.Context, genesis time.Time, startSlot uint64, data *blocksQueueFetchedData) {
	defer s.updatePeerScorerStats(data.pid, startSlot)

	blockReceiver := s.chain.ReceiveBlock

	for _, blk := range data.blocks {
		if err := s.processBlock(ctx, genesis, blk, blockReceiver); err != nil {
			log.WithError(err).Debug("Block is not processed")
			continue
		}
	}
}

// highestFinalizedEpoch returns the absolute highest finalized epoch of all connected peers.
// Note this can be lower than our finalized epoch if we have no peers or peers that are all behind us.
func (s *Service) highestFinalizedEpoch() uint64 {
	highest := uint64(0)
	for _, pid := range s.p2p.Peers().Connected() {
		peerChainState, err := s.p2p.Peers().ChainState(pid)
		if err == nil && peerChainState != nil && peerChainState.FinalizedEpoch > highest {
			highest = peerChainState.FinalizedEpoch
		}
	}

	return highest
}

// logSyncStatus and increment block processing counter.
func (s *Service) logSyncStatus(genesis time.Time, blk *eth.BeaconBlock, blkRoot [32]byte) {
	s.counter.Incr(1)
	rate := float64(s.counter.Rate()) / counterSeconds
	if rate == 0 {
		rate = 1
	}
	if featureconfig.Get().InitSyncVerbose || helpers.IsEpochStart(blk.Slot) {
		timeRemaining := time.Duration(float64(helpers.SlotsSince(genesis)-blk.Slot)/rate) * time.Second
		log.WithFields(logrus.Fields{
			"peers":           len(s.p2p.Peers().Connected()),
			"blocksPerSecond": fmt.Sprintf("%.1f", rate),
		}).Infof(
			"Processing block %s %d/%d - estimated time remaining %s",
			fmt.Sprintf("0x%s...", hex.EncodeToString(blkRoot[:])[:8]),
			blk.Slot, helpers.SlotsSince(genesis), timeRemaining,
		)
	}
}

// logBatchSyncStatus and increments the block processing counter.
func (s *Service) logBatchSyncStatus(genesis time.Time, blks []*eth.SignedBeaconBlock, blkRoot [32]byte) {
	s.counter.Incr(int64(len(blks)))
	rate := float64(s.counter.Rate()) / counterSeconds
	if rate == 0 {
		rate = 1
	}
	firstBlk := blks[0]
	timeRemaining := time.Duration(float64(helpers.SlotsSince(genesis)-firstBlk.Block.Slot)/rate) * time.Second
	log.WithFields(logrus.Fields{
		"peers":           len(s.p2p.Peers().Connected()),
		"blocksPerSecond": fmt.Sprintf("%.1f", rate),
	}).Infof(
		"Processing block batch of size %d starting from  %s %d/%d - estimated time remaining %s",
		len(blks), fmt.Sprintf("0x%s...", hex.EncodeToString(blkRoot[:])[:8]),
		firstBlk.Block.Slot, helpers.SlotsSince(genesis), timeRemaining,
	)
}

// processBlock performs basic checks on incoming block, and triggers receiver function.
func (s *Service) processBlock(
	ctx context.Context,
	genesis time.Time,
	blk *eth.SignedBeaconBlock,
	blockReceiver blockReceiverFn,
) error {
	blkRoot, err := blk.Block.HashTreeRoot()
	if err != nil {
		return err
	}
	if s.isProcessedBlock(ctx, blk, blkRoot) {
		return errors.Wrapf(errBlockAlreadyProcessed, "slot: %d , root %#x", blk.Block.Slot, blkRoot)
	}

	s.logSyncStatus(genesis, blk.Block, blkRoot)
	parentRoot := bytesutil.ToBytes32(blk.Block.ParentRoot)
	if !s.db.HasBlock(ctx, parentRoot) && !s.chain.HasInitSyncBlock(parentRoot) {
		return fmt.Errorf("beacon node doesn't have a block in db with root %#x", blk.Block.ParentRoot)
	}
	if err := blockReceiver(ctx, blk, blkRoot); err != nil {
		return err
	}
	s.lastProcessedSlot = blk.Block.Slot
	return nil
}

func (s *Service) processBatchedBlocks(ctx context.Context, genesis time.Time,
	blks []*eth.SignedBeaconBlock, bFunc batchBlockReceiverFn) error {
	if len(blks) == 0 {
		return errors.New("0 blocks provided into method")
	}
	firstBlock := blks[0]
	blkRoot, err := firstBlock.Block.HashTreeRoot()
	if err != nil {
		return err
	}
	for s.lastProcessedSlot >= firstBlock.Block.Slot && s.isProcessedBlock(ctx, firstBlock, blkRoot) {
		if len(blks) == 1 {
			return errors.New("no good blocks in batch")
		}
		blks = blks[1:]
		firstBlock = blks[0]
		blkRoot, err = firstBlock.Block.HashTreeRoot()
		if err != nil {
			return err
		}
	}
	s.logBatchSyncStatus(genesis, blks, blkRoot)
	parentRoot := bytesutil.ToBytes32(firstBlock.Block.ParentRoot)
	if !s.db.HasBlock(ctx, parentRoot) && !s.chain.HasInitSyncBlock(parentRoot) {
		return fmt.Errorf("beacon node doesn't have a block in db with root %#x", firstBlock.Block.ParentRoot)
	}
	blockRoots := make([][32]byte, len(blks))
	blockRoots[0] = blkRoot
	for i := 1; i < len(blks); i++ {
		b := blks[i]
		if !bytes.Equal(b.Block.ParentRoot, blockRoots[i-1][:]) {
			return fmt.Errorf("expected linear block list with parent root of %#x but received %#x",
				blockRoots[i-1][:], b.Block.ParentRoot)
		}
		blkRoot, err := b.Block.HashTreeRoot()
		if err != nil {
			return err
		}
		blockRoots[i] = blkRoot
	}
	if err := bFunc(ctx, blks, blockRoots); err != nil {
		return err
	}
	lastBlk := blks[len(blks)-1]
	s.lastProcessedSlot = lastBlk.Block.Slot
	return nil
}

// updatePeerScorerStats adjusts monitored metrics for a peer.
func (s *Service) updatePeerScorerStats(pid peer.ID, startSlot uint64) {
	if !featureconfig.Get().EnablePeerScorer || pid == "" {
		return
	}
	headSlot := s.chain.HeadSlot()
	if startSlot >= headSlot {
		return
	}
	if diff := s.chain.HeadSlot() - startSlot; diff > 0 {
		scorer := s.p2p.Peers().Scorers().BlockProviderScorer()
		scorer.IncrementProcessedBlocks(pid, diff)
	}
}

// isProcessedBlock checks DB and local cache for presence of a given block, to avoid duplicates.
func (s *Service) isProcessedBlock(ctx context.Context, blk *eth.SignedBeaconBlock, blkRoot [32]byte) bool {
	if blk.Block.Slot <= s.lastProcessedSlot && (s.db.HasBlock(ctx, blkRoot) || s.chain.HasInitSyncBlock(blkRoot)) {
		return true
	}
	return false
}
