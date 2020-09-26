package sync

import (
	"bytes"
	"context"
	"time"

	libp2pcore "github.com/libp2p/go-libp2p-core"
	streamhelpers "github.com/libp2p/go-libp2p-core/helpers"
	"github.com/libp2p/go-libp2p-core/mux"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/flags"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p/peers"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/runutil"
	"github.com/prysmaticlabs/prysm/shared/slotutil"
	"github.com/prysmaticlabs/prysm/shared/timeutils"
	"github.com/sirupsen/logrus"
)

// maintainPeerStatuses by infrequently polling peers for their latest status.
func (s *Service) maintainPeerStatuses() {
	// Run twice per epoch.
	interval := time.Duration(params.BeaconConfig().SecondsPerSlot*params.BeaconConfig().SlotsPerEpoch/2) * time.Second
	runutil.RunEvery(s.ctx, interval, func() {
		for _, pid := range s.p2p.Peers().Connected() {
			go func(id peer.ID) {
				// If our peer status has not been updated correctly we disconnect over here
				// and set the connection state over here instead.
				if s.p2p.Host().Network().Connectedness(id) != network.Connected {
					s.p2p.Peers().SetConnectionState(id, peers.PeerDisconnecting)
					if err := s.p2p.Disconnect(id); err != nil {
						log.Debugf("Error when disconnecting with peer: %v", err)
					}
					s.p2p.Peers().SetConnectionState(id, peers.PeerDisconnected)
					return
				}
				if s.p2p.Peers().IsBad(id) {
					if err := s.sendGoodByeAndDisconnect(s.ctx, codeGenericError, id); err != nil {
						log.Debugf("Error when disconnecting with bad peer: %v", err)
					}
					return
				}
				// If the status hasn't been updated in the recent interval time.
				lastUpdated, err := s.p2p.Peers().ChainStateLastUpdated(id)
				if err != nil {
					// Peer has vanished; nothing to do.
					return
				}
				if timeutils.Now().After(lastUpdated.Add(interval)) {
					if err := s.reValidatePeer(s.ctx, id); err != nil {
						log.WithField("peer", id).WithError(err).Debug("Failed to revalidate peer")
						s.p2p.Peers().Scorers().BadResponsesScorer().Increment(id)
					}
				}
			}(pid)
		}
	})
}

// resyncIfBehind checks periodically to see if we are in normal sync but have fallen behind our peers by more than an epoch,
// in which case we attempt a resync using the initial sync method to catch up.
func (s *Service) resyncIfBehind() {
	millisecondsPerEpoch := params.BeaconConfig().SecondsPerSlot * params.BeaconConfig().SlotsPerEpoch * 1000
	// Run sixteen times per epoch.
	interval := time.Duration(int64(millisecondsPerEpoch)/16) * time.Millisecond
	runutil.RunEvery(s.ctx, interval, func() {
		if s.shouldReSync() {
			syncedEpoch := helpers.SlotToEpoch(s.chain.HeadSlot())
			// Factor number of expected minimum sync peers, to make sure that enough peers are
			// available to resync (some peers may go away between checking non-finalized peers and
			// actual resyncing).
			highestEpoch, _ := s.p2p.Peers().BestNonFinalized(flags.Get().MinimumSyncPeers*2, syncedEpoch)
			// Check if the current node is more than 1 epoch behind.
			if highestEpoch > (syncedEpoch + 1) {
				log.WithFields(logrus.Fields{
					"currentEpoch": helpers.SlotToEpoch(s.chain.CurrentSlot()),
					"syncedEpoch":  syncedEpoch,
					"peersEpoch":   highestEpoch,
				}).Info("Fallen behind peers; reverting to initial sync to catch up")
				numberOfTimesResyncedCounter.Inc()
				s.clearPendingSlots()
				if err := s.initialSync.Resync(); err != nil {
					log.Errorf("Could not resync chain: %v", err)
				}
			}
		}
	})
}

// shouldReSync returns true if the node is not syncing and falls behind two epochs.
func (s *Service) shouldReSync() bool {
	syncedEpoch := helpers.SlotToEpoch(s.chain.HeadSlot())
	currentEpoch := helpers.SlotToEpoch(s.chain.CurrentSlot())
	prevEpoch := uint64(0)
	if currentEpoch > 1 {
		prevEpoch = currentEpoch - 1
	}
	return s.initialSync != nil && !s.initialSync.Syncing() && syncedEpoch < prevEpoch
}

// sendRPCStatusRequest for a given topic with an expected protobuf message type.
func (s *Service) sendRPCStatusRequest(ctx context.Context, id peer.ID) error {
	ctx, cancel := context.WithTimeout(ctx, respTimeout)
	defer cancel()

	headRoot, err := s.chain.HeadRoot(ctx)
	if err != nil {
		return err
	}

	forkDigest, err := s.forkDigest()
	if err != nil {
		return err
	}
	resp := &pb.Status{
		ForkDigest:     forkDigest[:],
		FinalizedRoot:  s.chain.FinalizedCheckpt().Root,
		FinalizedEpoch: s.chain.FinalizedCheckpt().Epoch,
		HeadRoot:       headRoot,
		HeadSlot:       s.chain.HeadSlot(),
	}
	stream, err := s.p2p.Send(ctx, resp, p2p.RPCStatusTopic, id)
	if err != nil {
		return err
	}
	defer func() {
		if err := streamhelpers.FullClose(stream); err != nil && err.Error() != mux.ErrReset.Error() {
			log.WithError(err).Debugf("Failed to reset stream with protocol %s", stream.Protocol())
		}
	}()

	code, errMsg, err := ReadStatusCode(stream, s.p2p.Encoding())
	if err != nil {
		return err
	}

	if code != 0 {
		s.p2p.Peers().Scorers().BadResponsesScorer().Increment(stream.Conn().RemotePeer())
		return errors.New(errMsg)
	}

	msg := &pb.Status{}
	if err := s.p2p.Encoding().DecodeWithMaxLength(stream, msg); err != nil {
		return err
	}
	s.p2p.Peers().SetChainState(stream.Conn().RemotePeer(), msg)

	err = s.validateStatusMessage(ctx, msg)
	if err != nil {
		s.p2p.Peers().Scorers().BadResponsesScorer().Increment(stream.Conn().RemotePeer())
		// Disconnect if on a wrong fork.
		if err == errWrongForkDigestVersion {
			if err := s.sendGoodByeAndDisconnect(ctx, codeWrongNetwork, stream.Conn().RemotePeer()); err != nil {
				return err
			}
		}
	}
	return err
}

func (s *Service) reValidatePeer(ctx context.Context, id peer.ID) error {
	if err := s.sendRPCStatusRequest(ctx, id); err != nil {
		return err
	}
	// Do not return an error for ping requests.
	if err := s.sendPingRequest(ctx, id); err != nil {
		log.WithError(err).Debug("Could not ping peer")
	}
	return nil
}

// statusRPCHandler reads the incoming Status RPC from the peer and responds with our version of a status message.
// This handler will disconnect any peer that does not match our fork version.
func (s *Service) statusRPCHandler(ctx context.Context, msg interface{}, stream libp2pcore.Stream) error {
	defer func() {
		if err := stream.Close(); err != nil {
			log.WithError(err).Debug("Failed to close stream")
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, ttfbTimeout)
	defer cancel()
	SetRPCStreamDeadlines(stream)
	log := log.WithField("handler", "status")
	m, ok := msg.(*pb.Status)
	if !ok {
		return errors.New("message is not type *pb.Status")
	}
	if err := s.rateLimiter.validateRequest(stream, 1); err != nil {
		return err
	}
	s.rateLimiter.add(stream, 1)

	if err := s.validateStatusMessage(ctx, m); err != nil {
		log.WithFields(logrus.Fields{
			"peer":  stream.Conn().RemotePeer(),
			"error": err}).Debug("Invalid status message from peer")

		respCode := byte(0)
		switch err {
		case errGeneric:
			respCode = responseCodeServerError
		case errWrongForkDigestVersion:
			// Respond with our status and disconnect with the peer.
			s.p2p.Peers().SetChainState(stream.Conn().RemotePeer(), m)
			if err := s.respondWithStatus(ctx, stream); err != nil {
				return err
			}
			if err := stream.Close(); err != nil { // Close before disconnecting.
				log.WithError(err).Debug("Failed to close stream")
			}
			if err := s.sendGoodByeAndDisconnect(ctx, codeWrongNetwork, stream.Conn().RemotePeer()); err != nil {
				return err
			}
			return nil
		default:
			respCode = responseCodeInvalidRequest
			s.p2p.Peers().Scorers().BadResponsesScorer().Increment(stream.Conn().RemotePeer())
		}

		originalErr := err
		resp, err := s.generateErrorResponse(respCode, err.Error())
		if err != nil {
			log.WithError(err).Debug("Failed to generate a response error")
		} else if _, err := stream.Write(resp); err != nil {
			// The peer may already be ignoring us, as we disagree on fork version, so log this as debug only.
			log.WithError(err).Debug("Failed to write to stream")
		}
		if err := stream.Close(); err != nil { // Close before disconnecting.
			log.WithError(err).Debug("Failed to close stream")
		}
		time.Sleep(flushDelay)
		if err := s.p2p.Disconnect(stream.Conn().RemotePeer()); err != nil {
			log.WithError(err).Debug("Failed to disconnect from peer")
		}
		return originalErr
	}
	s.p2p.Peers().SetChainState(stream.Conn().RemotePeer(), m)

	return s.respondWithStatus(ctx, stream)
}

func (s *Service) respondWithStatus(ctx context.Context, stream network.Stream) error {
	headRoot, err := s.chain.HeadRoot(ctx)
	if err != nil {
		return err
	}

	forkDigest, err := s.forkDigest()
	if err != nil {
		return err
	}
	resp := &pb.Status{
		ForkDigest:     forkDigest[:],
		FinalizedRoot:  s.chain.FinalizedCheckpt().Root,
		FinalizedEpoch: s.chain.FinalizedCheckpt().Epoch,
		HeadRoot:       headRoot,
		HeadSlot:       s.chain.HeadSlot(),
	}

	if _, err := stream.Write([]byte{responseCodeSuccess}); err != nil {
		log.WithError(err).Debug("Failed to write to stream")
	}
	_, err = s.p2p.Encoding().EncodeWithMaxLength(stream, resp)
	return err
}

func (s *Service) validateStatusMessage(ctx context.Context, msg *pb.Status) error {
	forkDigest, err := s.forkDigest()
	if err != nil {
		return err
	}
	if !bytes.Equal(forkDigest[:], msg.ForkDigest) {
		return errWrongForkDigestVersion
	}
	genesis := s.chain.GenesisTime()
	finalizedEpoch := s.chain.FinalizedCheckpt().Epoch
	maxEpoch := slotutil.EpochsSinceGenesis(genesis)
	// It would take a minimum of 2 epochs to finalize a
	// previous epoch
	maxFinalizedEpoch := uint64(0)
	if maxEpoch > 2 {
		maxFinalizedEpoch = maxEpoch - 2
	}
	if msg.FinalizedEpoch > maxFinalizedEpoch {
		return errInvalidEpoch
	}
	// Exit early if the peer's finalized epoch
	// is less than that of the remote peer's.
	if finalizedEpoch < msg.FinalizedEpoch {
		return nil
	}
	finalizedAtGenesis := msg.FinalizedEpoch == 0
	rootIsEqual := bytes.Equal(params.BeaconConfig().ZeroHash[:], msg.FinalizedRoot)
	// If peer is at genesis with the correct genesis root hash we exit.
	if finalizedAtGenesis && rootIsEqual {
		return nil
	}
	if !s.db.IsFinalizedBlock(ctx, bytesutil.ToBytes32(msg.FinalizedRoot)) {
		return errInvalidFinalizedRoot
	}
	blk, err := s.db.Block(ctx, bytesutil.ToBytes32(msg.FinalizedRoot))
	if err != nil {
		return errGeneric
	}
	if blk == nil {
		return errGeneric
	}
	// TODO(#5827) Verify the finalized block with the epoch in the
	// status message
	return nil
}
