package sync

import (
	"context"
	"io"

	libp2pcore "github.com/libp2p/go-libp2p-core"
	"github.com/libp2p/go-libp2p-core/helpers"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p"
	"github.com/prysmaticlabs/prysm/shared/params"
)

// sendRecentBeaconBlocksRequest sends a recent beacon blocks request to a peer to get
// those corresponding blocks from that peer.
func (s *Service) sendRecentBeaconBlocksRequest(ctx context.Context, blockRoots [][32]byte, id peer.ID) error {
	ctx, cancel := context.WithTimeout(ctx, respTimeout)
	defer cancel()

	stream, err := s.p2p.Send(ctx, blockRoots, p2p.RPCBlocksByRootTopic, id)
	if err != nil {
		return err
	}
	defer func() {
		if err := helpers.FullClose(stream); err != nil {
			log.WithError(err).Debugf("Failed to reset stream with protocol %s", stream.Protocol())
		}
	}()
	for i := 0; i < len(blockRoots); i++ {
		isFirstChunk := i == 0
		blk, err := ReadChunkedBlock(stream, s.p2p, isFirstChunk)
		if err == io.EOF {
			break
		}
		// Exit if peer sends more than max request blocks.
		if uint64(i) >= params.BeaconNetworkConfig().MaxRequestBlocks {
			break
		}
		if err != nil {
			log.WithError(err).Debug("Unable to retrieve block from stream")
			return err
		}

		blkRoot, err := blk.Block.HashTreeRoot()
		if err != nil {
			return err
		}
		s.pendingQueueLock.Lock()
		s.insertBlockToPendingQueue(blk.Block.Slot, blk, blkRoot)
		s.pendingQueueLock.Unlock()

	}
	return nil
}

// beaconBlocksRootRPCHandler looks up the request blocks from the database from the given block roots.
func (s *Service) beaconBlocksRootRPCHandler(ctx context.Context, msg interface{}, stream libp2pcore.Stream) error {
	defer func() {
		if err := stream.Close(); err != nil {
			log.WithError(err).Debug("Failed to close stream")
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, ttfbTimeout)
	defer cancel()
	SetRPCStreamDeadlines(stream)
	log := log.WithField("handler", "beacon_blocks_by_root")

	blockRoots, ok := msg.([][32]byte)
	if !ok {
		return errors.New("message is not type BeaconBlocksByRootRequest")
	}
	if len(blockRoots) == 0 {
		resp, err := s.generateErrorResponse(responseCodeInvalidRequest, "no block roots provided in request")
		if err != nil {
			log.WithError(err).Debug("Failed to generate a response error")
		} else if _, err := stream.Write(resp); err != nil {
			log.WithError(err).Debugf("Failed to write to stream")
		}
		return errors.New("no block roots provided")
	}
	if err := s.rateLimiter.validateRequest(stream, uint64(len(blockRoots))); err != nil {
		return err
	}

	if uint64(len(blockRoots)) > params.BeaconNetworkConfig().MaxRequestBlocks {
		resp, err := s.generateErrorResponse(responseCodeInvalidRequest, "requested more than the max block limit")
		if err != nil {
			log.WithError(err).Debug("Failed to generate a response error")
		} else if _, err := stream.Write(resp); err != nil {
			log.WithError(err).Debugf("Failed to write to stream")
		}
		return errors.New("requested more than the max block limit")
	}
	s.rateLimiter.add(stream, int64(len(blockRoots)))

	for _, root := range blockRoots {
		blk, err := s.db.Block(ctx, root)
		if err != nil {
			log.WithError(err).Debug("Failed to fetch block")
			resp, err := s.generateErrorResponse(responseCodeServerError, genericError)
			if err != nil {
				log.WithError(err).Debug("Failed to generate a response error")
			} else if _, err := stream.Write(resp); err != nil {
				log.WithError(err).Debugf("Failed to write to stream")
			}
			return err
		}
		if blk == nil {
			continue
		}
		if err := s.chunkWriter(stream, blk); err != nil {
			return err
		}
	}
	return nil
}
