package validator

import (
	"context"
	"fmt"

	ptypes "github.com/gogo/protobuf/types"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-ssz"
	"github.com/prysmaticlabs/prysm/beacon-chain/cache"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/blocks"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/feed"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/feed/operation"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/state"
	stateTrie "github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/featureconfig"
	"github.com/prysmaticlabs/prysm/shared/params"
	"go.opencensus.io/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetAttestationData requests that the beacon node produce an attestation data object,
// which the validator acting as an attester will then sign.
func (vs *Server) GetAttestationData(ctx context.Context, req *ethpb.AttestationDataRequest) (*ethpb.AttestationData, error) {
	ctx, span := trace.StartSpan(ctx, "AttesterServer.RequestAttestation")
	defer span.End()
	span.AddAttributes(
		trace.Int64Attribute("slot", int64(req.Slot)),
		trace.Int64Attribute("committeeIndex", int64(req.CommitteeIndex)),
	)

	if vs.SyncChecker.Syncing() {
		return nil, status.Errorf(codes.Unavailable, "Syncing to latest head, not ready to respond")
	}

	if err := helpers.ValidateAttestationTime(req.Slot, vs.GenesisTimeFetcher.GenesisTime()); err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid request: %v", err))
	}

	res, err := vs.AttestationCache.Get(ctx, req)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not retrieve data from attestation cache: %v", err)
	}
	if res != nil {
		if featureconfig.Get().ReduceAttesterStateCopy {
			res.CommitteeIndex = req.CommitteeIndex
		}
		return res, nil
	}

	if err := vs.AttestationCache.MarkInProgress(req); err != nil {
		if err == cache.ErrAlreadyInProgress {
			res, err := vs.AttestationCache.Get(ctx, req)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "Could not retrieve data from attestation cache: %v", err)
			}
			if res == nil {
				return nil, status.Error(codes.DataLoss, "A request was in progress and resolved to nil")
			}
			if featureconfig.Get().ReduceAttesterStateCopy {
				res.CommitteeIndex = req.CommitteeIndex
			}
			return res, nil
		}
		return nil, status.Errorf(codes.Internal, "Could not mark attestation as in-progress: %v", err)
	}
	defer func() {
		if err := vs.AttestationCache.MarkNotInProgress(req); err != nil {
			log.WithError(err).Error("Failed to mark cache not in progress")
		}
	}()

	headState, err := vs.HeadFetcher.HeadState(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not retrieve head state: %v", err)
	}
	headRoot, err := vs.HeadFetcher.HeadRoot(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not retrieve head root: %v", err)
	}

	// In the case that we receive an attestation request after a newer state/block has been processed.
	if headState.Slot() > req.Slot {
		headRoot, err = helpers.BlockRootAtSlot(headState, req.Slot)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Could not get historical head root: %v", err)
		}
		headState, err = vs.StateGen.StateByRoot(ctx, bytesutil.ToBytes32(headRoot))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Could not get historical head state: %v", err)
		}
	}
	if headState == nil {
		return nil, status.Error(codes.Internal, "Failed to lookup parent state from head.")
	}

	if helpers.CurrentEpoch(headState) < helpers.SlotToEpoch(req.Slot) {
		headState, err = state.ProcessSlots(ctx, headState, helpers.StartSlot(helpers.SlotToEpoch(req.Slot)))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Could not process slots up to %d: %v", req.Slot, err)
		}
	}

	targetEpoch := helpers.CurrentEpoch(headState)
	epochStartSlot := helpers.StartSlot(targetEpoch)
	targetRoot := make([]byte, 32)
	if epochStartSlot == headState.Slot() {
		targetRoot = headRoot[:]
	} else {
		targetRoot, err = helpers.BlockRootAtSlot(headState, epochStartSlot)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Could not get target block for slot %d: %v", epochStartSlot, err)
		}
		if bytesutil.ToBytes32(targetRoot) == params.BeaconConfig().ZeroHash {
			targetRoot = headRoot
		}
	}

	res = &ethpb.AttestationData{
		Slot:            req.Slot,
		CommitteeIndex:  req.CommitteeIndex,
		BeaconBlockRoot: headRoot[:],
		Source:          headState.CurrentJustifiedCheckpoint(),
		Target: &ethpb.Checkpoint{
			Epoch: targetEpoch,
			Root:  targetRoot,
		},
	}

	if err := vs.AttestationCache.Put(ctx, req, res); err != nil {
		return nil, status.Errorf(codes.Internal, "Could not store attestation data in cache: %v", err)
	}
	return res, nil
}

// ProposeAttestation is a function called by an attester to vote
// on a block via an attestation object as defined in the Ethereum Serenity specification.
func (vs *Server) ProposeAttestation(ctx context.Context, att *ethpb.Attestation) (*ethpb.AttestResponse, error) {
	ctx, span := trace.StartSpan(ctx, "AttesterServer.ProposeAttestation")
	defer span.End()

	if _, err := bls.SignatureFromBytes(att.Signature); err != nil {
		return nil, status.Error(codes.InvalidArgument, "Incorrect attestation signature")
	}

	root, err := att.Data.HashTreeRoot()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not tree hash attestation: %v", err)
	}

	// Broadcast the unaggregated attestation on a feed to notify other services in the beacon node
	// of a received unaggregated attestation.
	vs.OperationNotifier.OperationFeed().Send(&feed.Event{
		Type: operation.UnaggregatedAttReceived,
		Data: &operation.UnAggregatedAttReceivedData{
			Attestation: att,
		},
	})

	// Determine subnet to broadcast attestation to
	wantedEpoch := helpers.SlotToEpoch(att.Data.Slot)
	vals, err := vs.HeadFetcher.HeadValidatorsIndices(ctx, wantedEpoch)
	if err != nil {
		return nil, err
	}
	subnet := helpers.ComputeSubnetFromCommitteeAndSlot(uint64(len(vals)), att.Data.CommitteeIndex, att.Data.Slot)

	// Broadcast the new attestation to the network.
	if err := vs.P2P.BroadcastAttestation(ctx, subnet, att); err != nil {
		return nil, status.Errorf(codes.Internal, "Could not broadcast attestation: %v", err)
	}

	go func() {
		ctx = trace.NewContext(context.Background(), trace.FromContext(ctx))
		attCopy := stateTrie.CopyAttestation(att)
		if err := vs.AttPool.SaveUnaggregatedAttestation(attCopy); err != nil {
			log.WithError(err).Error("Could not handle attestation in operations service")
			return
		}
	}()

	return &ethpb.AttestResponse{
		AttestationDataRoot: root[:],
	}, nil
}

// SubscribeCommitteeSubnets subscribes to the committee ID subnet given subscribe request.
func (vs *Server) SubscribeCommitteeSubnets(ctx context.Context, req *ethpb.CommitteeSubnetsSubscribeRequest) (*ptypes.Empty, error) {
	ctx, span := trace.StartSpan(ctx, "AttesterServer.SubscribeCommitteeSubnets")
	defer span.End()

	if len(req.Slots) != len(req.CommitteeIds) || len(req.CommitteeIds) != len(req.IsAggregator) {
		return nil, status.Error(codes.InvalidArgument, "request fields are not the same length")
	}
	if len(req.Slots) == 0 {
		return nil, status.Error(codes.InvalidArgument, "no attester slots provided")
	}

	fetchValsLen := func(slot uint64) (uint64, error) {
		wantedEpoch := helpers.SlotToEpoch(slot)
		vals, err := vs.HeadFetcher.HeadValidatorsIndices(ctx, wantedEpoch)
		if err != nil {
			return 0, err
		}
		return uint64(len(vals)), nil
	}

	// Request the head validator indices of epoch represented by the first requested
	// slot.
	currValsLen, err := fetchValsLen(req.Slots[0])
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not retrieve head validator length: %v", err)
	}
	currEpoch := helpers.SlotToEpoch(req.Slots[0])

	for i := 0; i < len(req.Slots); i++ {
		// If epoch has changed, re-request active validators length
		if currEpoch != helpers.SlotToEpoch(req.Slots[i]) {
			currValsLen, err = fetchValsLen(req.Slots[i])
			if err != nil {
				return nil, status.Errorf(codes.Internal, "Could not retrieve head validator length: %v", err)
			}
			currEpoch = helpers.SlotToEpoch(req.Slots[i])
		}
		subnet := helpers.ComputeSubnetFromCommitteeAndSlot(currValsLen, req.CommitteeIds[i], req.Slots[i])
		cache.SubnetIDs.AddAttesterSubnetID(req.Slots[i], subnet)
		if req.IsAggregator[i] {
			cache.SubnetIDs.AddAggregatorSubnetID(req.Slots[i], subnet)
		}
	}

	return &ptypes.Empty{}, nil
}

// This computes and returns the shard transition object given the list of shard blocks and the shard number.
//
// Spec code:
// def get_shard_transition(beacon_state: BeaconState,
//                         shard: Shard,
//                         shard_blocks: Sequence[SignedShardBlock]) -> ShardTransition:
//    offset_slots = compute_offset_slots(
//        get_latest_slot_for_shard(beacon_state, shard),
//        Slot(beacon_state.slot + 1),
//    )
//    shard_block_lengths, shard_data_roots, shard_states = (
//        get_shard_transition_fields(beacon_state, shard, shard_blocks)
//    )
//
//    if len(shard_blocks) > 0:
//        proposer_signatures = [shard_block.signature for shard_block in shard_blocks]
//        proposer_signature_aggregate = bls.Aggregate(proposer_signatures)
//    else:
//        proposer_signature_aggregate = NO_SIGNATURE
//
//    return ShardTransition(
//        start_slot=offset_slots[0],
//        shard_block_lengths=shard_block_lengths,
//        shard_data_roots=shard_data_roots,
//        shard_states=shard_states,
//        proposer_signature_aggregate=proposer_signature_aggregate,
//    )
func shardTransition(beaconState *stateTrie.BeaconState, shardBlocks []*ethpb.SignedShardBlock, shard uint64) (*ethpb.ShardTransition, error) {
	shardSlot := beaconState.ShardStateAtIndex(shard).Slot
	offsetSlots := helpers.ComputeOffsetSlots(shardSlot, beaconState.Slot()+1)

	shardBlockLengths, shardDataRoots, shardStates, err := shardTransitionFields(beaconState, shardBlocks, shard)
	if err != nil {
		return nil, err
	}

	aggregatedSignature := bytesutil.PadTo([]byte{}, params.BeaconConfig().BLSSignatureLength)
	if len(shardBlocks) > 0 {
		var sigs []bls.Signature
		for _, b := range shardBlocks {
			s, err := bls.SignatureFromBytes(b.Signature)
			if err != nil {
				return nil, err
			}
			sigs = append(sigs, s)
		}
		a := bls.AggregateSignatures(sigs)
		aggregatedSignature = a.Marshal()
	}

	startSlot := offsetSlots[0]
	return &ethpb.ShardTransition{
		StartSlot:                  startSlot,
		ShardBlockLengths:          shardBlockLengths,
		ShardDataRoots:             shardDataRoots,
		ShardStates:                shardStates,
		ProposerSignatureAggregate: aggregatedSignature,
	}, nil
}

// This returns the fields for shard transitions given shard blocks and shard.
//
// Spec code:
// def get_shard_transition_fields(
//    beacon_state: BeaconState,
//    shard: Shard,
//    shard_blocks: Sequence[SignedShardBlock],
//    validate_signature: bool=True,
//) -> Tuple[Sequence[uint64], Sequence[Root], Sequence[ShardState]]:
//    shard_states = []
//    shard_data_roots = []
//    shard_block_lengths = []
//
//    shard_state = beacon_state.shard_states[shard]
//    shard_block_slots = [shard_block.message.slot for shard_block in shard_blocks]
//    offset_slots = compute_offset_slots(
//        get_latest_slot_for_shard(beacon_state, shard),
//        Slot(beacon_state.slot + 1),
//    )
//    for slot in offset_slots:
//        if slot in shard_block_slots:
//            shard_block = shard_blocks[shard_block_slots.index(slot)]
//            shard_data_roots.append(hash_tree_root(shard_block.message.body))
//        else:
//            shard_block = SignedShardBlock(message=ShardBlock(slot=slot, shard=shard))
//            shard_data_roots.append(Root())
//        shard_state = get_post_shard_state(shard_state, shard_block.message)
//        shard_states.append(shard_state)
//        shard_block_lengths.append(len(shard_block.message.body))
//
//    return shard_block_lengths, shard_data_roots, shard_states
func shardTransitionFields(beaconState *stateTrie.BeaconState, shardBlocks []*ethpb.SignedShardBlock, shard uint64) ([]uint64, [][]byte, []*ethpb.ShardState, error) {
	shardBlockLength := make([]uint64, 0)
	shardStates := make([]*ethpb.ShardState, 0)
	shardDataRoots := make([][]byte, 0)

	// Note: Use proper getter
	shardState := beaconState.ShardStateAtIndex(shard)
	slotsToProcess := helpers.ShardOffSetSlots(beaconState, shard)
	shardBlockSlot := make(map[uint64]bool)
	for _, block := range shardBlocks {
		shardBlockSlot[block.Message.Slot] = true
	}

	for _, slot := range slotsToProcess {
		shardBlock := &ethpb.SignedShardBlock{Message: &ethpb.ShardBlock{Slot: slot, Shard: shard}}
		if shardBlockSlot[slot] {
			// TODO(0): Verify this works in skip slot situation.
			shardBlock = shardBlocks[slot]
			r, err := ssz.HashTreeRoot(shardBlock.Message.Body)
			if err != nil {
				return nil, nil, nil, err
			}
			shardDataRoots = append(shardDataRoots, r[:])
		} else {
			shardDataRoots = append(shardDataRoots, bytesutil.PadTo([]byte{}, 32))
		}

		copied := stateTrie.CopyShardState(shardState)
		shardState, err := blocks.ProcessShardBlock(copied, shardBlock.Message)
		if err != nil {
			return nil, nil, nil, err
		}
		shardStates = append(shardStates, shardState)
		// TODO(0): Is shard block length even necessary.
		shardBlockLength = append(shardBlockLength, uint64(len(shardBlock.Message.Body)))
	}

	return shardBlockLength, shardDataRoots, shardStates, nil
}
