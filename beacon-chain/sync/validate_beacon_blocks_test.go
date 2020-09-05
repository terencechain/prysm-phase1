package sync

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	lru "github.com/hashicorp/golang-lru"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pubsubpb "github.com/libp2p/go-libp2p-pubsub/pb"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	mock "github.com/prysmaticlabs/prysm/beacon-chain/blockchain/testing"
	"github.com/prysmaticlabs/prysm/beacon-chain/cache"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/state"
	dbtest "github.com/prysmaticlabs/prysm/beacon-chain/db/testing"
	"github.com/prysmaticlabs/prysm/beacon-chain/operations/attestations"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p"
	p2ptest "github.com/prysmaticlabs/prysm/beacon-chain/p2p/testing"
	"github.com/prysmaticlabs/prysm/beacon-chain/state/stategen"
	mockSync "github.com/prysmaticlabs/prysm/beacon-chain/sync/initial-sync/testing"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/testutil"
	"github.com/prysmaticlabs/prysm/shared/testutil/assert"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
	logTest "github.com/sirupsen/logrus/hooks/test"
)

// General note for writing validation tests: Use a random value for any field
// on the beacon block to avoid hitting shared global cache conditions across
// tests in this package.

func TestValidateBeaconBlockPubSub_InvalidSignature(t *testing.T) {
	ctx := context.Background()
	db, _ := dbtest.SetupDB(t)
	msg := testutil.NewBeaconBlock()
	msg.Block.Slot = 1
	msg.Block.ParentRoot = testutil.Random32Bytes(t)
	msg.Signature = bytesutil.PadTo([]byte("fake"), 96)

	p := p2ptest.NewTestP2P(t)

	c, err := lru.New(10)
	require.NoError(t, err)
	c2, err := lru.New(10)
	require.NoError(t, err)
	chainService := &mock.ChainService{Genesis: time.Now(),
		FinalizedCheckPoint: &ethpb.Checkpoint{
			Epoch: 0,
		}}
	r := &Service{
		db:             db,
		p2p:            p,
		initialSync:    &mockSync.Sync{IsSyncing: false},
		chain:          chainService,
		seenBlockCache: c,
		badBlockCache:  c2,
		blockNotifier:  chainService.BlockNotifier(),
	}

	buf := new(bytes.Buffer)
	_, err = p.Encoding().EncodeGossip(buf, msg)
	require.NoError(t, err)
	m := &pubsub.Message{
		Message: &pubsubpb.Message{
			Data: buf.Bytes(),
			TopicIDs: []string{
				p2p.GossipTypeMapping[reflect.TypeOf(msg)],
			},
		},
	}
	result := r.validateBeaconBlockPubSub(ctx, "", m) == pubsub.ValidationAccept
	assert.Equal(t, false, result)
}

func TestValidateBeaconBlockPubSub_BlockAlreadyPresentInDB(t *testing.T) {
	db, _ := dbtest.SetupDB(t)
	ctx := context.Background()

	p := p2ptest.NewTestP2P(t)
	msg := testutil.NewBeaconBlock()
	msg.Block.Slot = 100
	msg.Block.ParentRoot = testutil.Random32Bytes(t)
	require.NoError(t, db.SaveBlock(context.Background(), msg))

	c, err := lru.New(10)
	require.NoError(t, err)
	c2, err := lru.New(10)
	require.NoError(t, err)
	chainService := &mock.ChainService{Genesis: time.Now()}
	r := &Service{
		db:                db,
		p2p:               p,
		initialSync:       &mockSync.Sync{IsSyncing: false},
		chain:             chainService,
		seenBlockCache:    c,
		badBlockCache:     c2,
		stateSummaryCache: cache.NewStateSummaryCache(),
		blockNotifier:     chainService.BlockNotifier(),
	}

	buf := new(bytes.Buffer)
	_, err = p.Encoding().EncodeGossip(buf, msg)
	require.NoError(t, err)

	m := &pubsub.Message{
		Message: &pubsubpb.Message{
			Data: buf.Bytes(),
			TopicIDs: []string{
				p2p.GossipTypeMapping[reflect.TypeOf(msg)],
			},
		},
	}
	result := r.validateBeaconBlockPubSub(ctx, "", m) == pubsub.ValidationAccept
	assert.Equal(t, false, result)
}

func TestValidateBeaconBlockPubSub_ValidProposerSignature(t *testing.T) {
	db, stateSummaryCache := dbtest.SetupDB(t)
	p := p2ptest.NewTestP2P(t)
	ctx := context.Background()
	beaconState, privKeys := testutil.DeterministicGenesisState(t, 100)
	parentBlock := testutil.NewBeaconBlock()
	require.NoError(t, db.SaveBlock(ctx, parentBlock))
	bRoot, err := parentBlock.Block.HashTreeRoot()
	require.NoError(t, err)
	require.NoError(t, db.SaveState(ctx, beaconState, bRoot))
	require.NoError(t, db.SaveStateSummary(ctx, &pb.StateSummary{Root: bRoot[:]}))
	copied := beaconState.Copy()
	require.NoError(t, copied.SetSlot(1))
	proposerIdx, err := helpers.BeaconProposerIndex(copied)
	require.NoError(t, err)
	msg := testutil.NewBeaconBlock()
	msg.Block.ParentRoot = bRoot[:]
	msg.Block.Slot = 1
	msg.Block.ProposerIndex = proposerIdx
	msg.Signature, err = helpers.ComputeDomainAndSign(beaconState, 0, msg.Block, params.BeaconConfig().DomainBeaconProposer, privKeys[proposerIdx])
	require.NoError(t, err)

	c, err := lru.New(10)
	require.NoError(t, err)
	c2, err := lru.New(10)
	require.NoError(t, err)
	stateGen := stategen.New(db, stateSummaryCache)
	chainService := &mock.ChainService{Genesis: time.Unix(time.Now().Unix()-int64(params.BeaconConfig().SecondsPerSlot), 0),
		State: beaconState,
		FinalizedCheckPoint: &ethpb.Checkpoint{
			Epoch: 0,
			Root:  make([]byte, 32),
		},
	}
	r := &Service{
		db:                  db,
		p2p:                 p,
		initialSync:         &mockSync.Sync{IsSyncing: false},
		chain:               chainService,
		blockNotifier:       chainService.BlockNotifier(),
		seenBlockCache:      c,
		badBlockCache:       c2,
		slotToPendingBlocks: make(map[uint64][]*ethpb.SignedBeaconBlock),
		seenPendingBlocks:   make(map[[32]byte]bool),
		stateSummaryCache:   stateSummaryCache,
		stateGen:            stateGen,
	}
	buf := new(bytes.Buffer)
	_, err = p.Encoding().EncodeGossip(buf, msg)
	require.NoError(t, err)
	m := &pubsub.Message{
		Message: &pubsubpb.Message{
			Data: buf.Bytes(),
			TopicIDs: []string{
				p2p.GossipTypeMapping[reflect.TypeOf(msg)],
			},
		},
	}
	result := r.validateBeaconBlockPubSub(ctx, "", m) == pubsub.ValidationAccept
	assert.Equal(t, true, result)
	assert.NotNil(t, m.ValidatorData, "Decoded message was not set on the message validator data")
}

func TestValidateBeaconBlockPubSub_AdvanceEpochsForState(t *testing.T) {
	db, stateSummaryCache := dbtest.SetupDB(t)
	p := p2ptest.NewTestP2P(t)
	ctx := context.Background()
	beaconState, privKeys := testutil.DeterministicGenesisState(t, 100)
	parentBlock := testutil.NewBeaconBlock()
	require.NoError(t, db.SaveBlock(ctx, parentBlock))
	bRoot, err := parentBlock.Block.HashTreeRoot()
	require.NoError(t, err)
	require.NoError(t, db.SaveState(ctx, beaconState, bRoot))
	require.NoError(t, db.SaveStateSummary(ctx, &pb.StateSummary{Root: bRoot[:]}))
	copied := beaconState.Copy()
	// The next block is at least 2 epochs ahead to induce shuffling and a new seed.
	blkSlot := params.BeaconConfig().SlotsPerEpoch * 2
	copied, err = state.ProcessSlots(context.Background(), copied, blkSlot)
	require.NoError(t, err)
	proposerIdx, err := helpers.BeaconProposerIndex(copied)
	require.NoError(t, err)
	msg := testutil.NewBeaconBlock()
	msg.Block.ProposerIndex = proposerIdx
	msg.Block.Slot = blkSlot
	msg.Block.ParentRoot = bRoot[:]
	msg.Signature, err = helpers.ComputeDomainAndSign(beaconState, 0, msg.Block, params.BeaconConfig().DomainBeaconProposer, privKeys[proposerIdx])
	require.NoError(t, err)

	c, err := lru.New(10)
	require.NoError(t, err)
	c2, err := lru.New(10)
	require.NoError(t, err)
	stateGen := stategen.New(db, stateSummaryCache)
	chainService := &mock.ChainService{Genesis: time.Unix(time.Now().Unix()-int64(blkSlot*params.BeaconConfig().SecondsPerSlot), 0),
		State: beaconState,
		FinalizedCheckPoint: &ethpb.Checkpoint{
			Epoch: 0,
		}}
	r := &Service{
		db:                  db,
		p2p:                 p,
		initialSync:         &mockSync.Sync{IsSyncing: false},
		chain:               chainService,
		blockNotifier:       chainService.BlockNotifier(),
		seenBlockCache:      c,
		badBlockCache:       c2,
		slotToPendingBlocks: make(map[uint64][]*ethpb.SignedBeaconBlock),
		seenPendingBlocks:   make(map[[32]byte]bool),
		stateSummaryCache:   stateSummaryCache,
		stateGen:            stateGen,
	}
	buf := new(bytes.Buffer)
	_, err = p.Encoding().EncodeGossip(buf, msg)
	require.NoError(t, err)
	m := &pubsub.Message{
		Message: &pubsubpb.Message{
			Data: buf.Bytes(),
			TopicIDs: []string{
				p2p.GossipTypeMapping[reflect.TypeOf(msg)],
			},
		},
	}
	result := r.validateBeaconBlockPubSub(ctx, "", m) == pubsub.ValidationAccept
	assert.Equal(t, true, result)
	assert.NotNil(t, m.ValidatorData, "Decoded message was not set on the message validator data")
}

func TestValidateBeaconBlockPubSub_Syncing(t *testing.T) {
	db, _ := dbtest.SetupDB(t)
	p := p2ptest.NewTestP2P(t)
	ctx := context.Background()
	b := []byte("sk")
	b32 := bytesutil.ToBytes32(b)
	sk, err := bls.SecretKeyFromBytes(b32[:])
	require.NoError(t, err)
	msg := testutil.NewBeaconBlock()
	msg.Block.ParentRoot = testutil.Random32Bytes(t)
	msg.Signature = sk.Sign([]byte("data")).Marshal()
	chainService := &mock.ChainService{
		Genesis: time.Now(),
		FinalizedCheckPoint: &ethpb.Checkpoint{
			Epoch: 0,
		}}
	r := &Service{
		db:            db,
		p2p:           p,
		initialSync:   &mockSync.Sync{IsSyncing: true},
		chain:         chainService,
		blockNotifier: chainService.BlockNotifier(),
	}

	buf := new(bytes.Buffer)
	_, err = p.Encoding().EncodeGossip(buf, msg)
	require.NoError(t, err)
	m := &pubsub.Message{
		Message: &pubsubpb.Message{
			Data: buf.Bytes(),
			TopicIDs: []string{
				p2p.GossipTypeMapping[reflect.TypeOf(msg)],
			},
		},
	}
	result := r.validateBeaconBlockPubSub(ctx, "", m) == pubsub.ValidationAccept
	assert.Equal(t, false, result)
}

func TestValidateBeaconBlockPubSub_RejectBlocksFromFuture(t *testing.T) {
	db, _ := dbtest.SetupDB(t)
	p := p2ptest.NewTestP2P(t)
	ctx := context.Background()
	b := []byte("sk")
	b32 := bytesutil.ToBytes32(b)
	sk, err := bls.SecretKeyFromBytes(b32[:])
	require.NoError(t, err)
	msg := testutil.NewBeaconBlock()
	msg.Block.Slot = 1000
	msg.Block.ParentRoot = testutil.Random32Bytes(t)
	msg.Signature = sk.Sign([]byte("data")).Marshal()

	c, err := lru.New(10)
	require.NoError(t, err)
	c2, err := lru.New(10)
	require.NoError(t, err)
	chainService := &mock.ChainService{Genesis: time.Now()}
	r := &Service{
		p2p:                 p,
		db:                  db,
		initialSync:         &mockSync.Sync{IsSyncing: false},
		chain:               chainService,
		blockNotifier:       chainService.BlockNotifier(),
		seenBlockCache:      c,
		badBlockCache:       c2,
		slotToPendingBlocks: make(map[uint64][]*ethpb.SignedBeaconBlock),
		seenPendingBlocks:   make(map[[32]byte]bool),
	}

	buf := new(bytes.Buffer)
	_, err = p.Encoding().EncodeGossip(buf, msg)
	require.NoError(t, err)
	m := &pubsub.Message{
		Message: &pubsubpb.Message{
			Data: buf.Bytes(),
			TopicIDs: []string{
				p2p.GossipTypeMapping[reflect.TypeOf(msg)],
			},
		},
	}
	result := r.validateBeaconBlockPubSub(ctx, "", m) == pubsub.ValidationAccept
	assert.Equal(t, false, result)
}

func TestValidateBeaconBlockPubSub_RejectBlocksFromThePast(t *testing.T) {
	db, _ := dbtest.SetupDB(t)
	b := []byte("sk")
	b32 := bytesutil.ToBytes32(b)
	p := p2ptest.NewTestP2P(t)
	ctx := context.Background()
	sk, err := bls.SecretKeyFromBytes(b32[:])
	require.NoError(t, err)
	msg := testutil.NewBeaconBlock()
	msg.Block.ParentRoot = testutil.Random32Bytes(t)
	msg.Block.Slot = 10
	msg.Signature = sk.Sign([]byte("data")).Marshal()

	genesisTime := time.Now()
	c, err := lru.New(10)
	require.NoError(t, err)
	c2, err := lru.New(10)
	require.NoError(t, err)
	chainService := &mock.ChainService{
		Genesis: time.Unix(genesisTime.Unix()-1000, 0),
		FinalizedCheckPoint: &ethpb.Checkpoint{
			Epoch: 1,
		},
	}
	r := &Service{
		db:             db,
		p2p:            p,
		initialSync:    &mockSync.Sync{IsSyncing: false},
		chain:          chainService,
		blockNotifier:  chainService.BlockNotifier(),
		seenBlockCache: c,
		badBlockCache:  c2,
	}

	buf := new(bytes.Buffer)
	_, err = p.Encoding().EncodeGossip(buf, msg)
	require.NoError(t, err)
	m := &pubsub.Message{
		Message: &pubsubpb.Message{
			Data: buf.Bytes(),
			TopicIDs: []string{
				p2p.GossipTypeMapping[reflect.TypeOf(msg)],
			},
		},
	}
	result := r.validateBeaconBlockPubSub(ctx, "", m) == pubsub.ValidationAccept
	assert.Equal(t, false, result)
}

func TestValidateBeaconBlockPubSub_SeenProposerSlot(t *testing.T) {
	db, _ := dbtest.SetupDB(t)
	p := p2ptest.NewTestP2P(t)
	ctx := context.Background()
	beaconState, privKeys := testutil.DeterministicGenesisState(t, 100)
	parentBlock := testutil.NewBeaconBlock()
	require.NoError(t, db.SaveBlock(ctx, parentBlock))
	bRoot, err := parentBlock.Block.HashTreeRoot()
	require.NoError(t, err)
	require.NoError(t, db.SaveState(ctx, beaconState, bRoot))
	proposerIdx, err := helpers.BeaconProposerIndex(beaconState)
	require.NoError(t, err)

	msg := testutil.NewBeaconBlock()
	msg.Block.Slot = 1
	msg.Block.ProposerIndex = proposerIdx
	msg.Block.ParentRoot = bRoot[:]
	msg.Signature, err = helpers.ComputeDomainAndSign(beaconState, 0, msg.Block, params.BeaconConfig().DomainBeaconProposer, privKeys[proposerIdx])
	require.NoError(t, err)

	c, err := lru.New(10)
	require.NoError(t, err)
	c2, err := lru.New(10)
	require.NoError(t, err)
	chainService := &mock.ChainService{Genesis: time.Unix(time.Now().Unix()-int64(params.BeaconConfig().SecondsPerSlot), 0),
		State: beaconState,
		FinalizedCheckPoint: &ethpb.Checkpoint{
			Epoch: 0,
			Root:  make([]byte, 32),
		},
	}
	r := &Service{
		db:                  db,
		p2p:                 p,
		initialSync:         &mockSync.Sync{IsSyncing: false},
		chain:               chainService,
		blockNotifier:       chainService.BlockNotifier(),
		seenBlockCache:      c,
		badBlockCache:       c2,
		slotToPendingBlocks: make(map[uint64][]*ethpb.SignedBeaconBlock),
		seenPendingBlocks:   make(map[[32]byte]bool),
		stateSummaryCache:   cache.NewStateSummaryCache(),
	}

	buf := new(bytes.Buffer)
	_, err = p.Encoding().EncodeGossip(buf, msg)
	require.NoError(t, err)
	m := &pubsub.Message{
		Message: &pubsubpb.Message{
			Data: buf.Bytes(),
			TopicIDs: []string{
				p2p.GossipTypeMapping[reflect.TypeOf(msg)],
			},
		},
	}
	r.setSeenBlockIndexSlot(msg.Block.Slot, msg.Block.ProposerIndex)
	time.Sleep(10 * time.Millisecond) // Wait for cached value to pass through buffers.
	result := r.validateBeaconBlockPubSub(ctx, "", m) == pubsub.ValidationAccept
	assert.Equal(t, false, result)
}

func TestValidateBeaconBlockPubSub_FilterByFinalizedEpoch(t *testing.T) {
	hook := logTest.NewGlobal()
	db, _ := dbtest.SetupDB(t)
	p := p2ptest.NewTestP2P(t)

	parent := testutil.NewBeaconBlock()
	require.NoError(t, db.SaveBlock(context.Background(), parent))
	parentRoot, err := parent.Block.HashTreeRoot()
	require.NoError(t, err)
	chain := &mock.ChainService{Genesis: time.Unix(time.Now().Unix()-int64(params.BeaconConfig().SecondsPerSlot), 0),
		FinalizedCheckPoint: &ethpb.Checkpoint{
			Epoch: 1,
		}}
	c, err := lru.New(10)
	require.NoError(t, err)
	c2, err := lru.New(10)
	require.NoError(t, err)
	r := &Service{
		db:             db,
		p2p:            p,
		chain:          chain,
		blockNotifier:  chain.BlockNotifier(),
		attPool:        attestations.NewPool(),
		seenBlockCache: c,
		badBlockCache:  c2,
		initialSync:    &mockSync.Sync{IsSyncing: false},
	}

	b := testutil.NewBeaconBlock()
	b.Block.Slot = 1
	b.Block.ParentRoot = parentRoot[:]
	buf := new(bytes.Buffer)
	_, err = p.Encoding().EncodeGossip(buf, b)
	require.NoError(t, err)
	m := &pubsub.Message{
		Message: &pubsubpb.Message{
			Data: buf.Bytes(),
			TopicIDs: []string{
				p2p.GossipTypeMapping[reflect.TypeOf(b)],
			},
		},
	}

	r.validateBeaconBlockPubSub(context.Background(), "", m)
	require.LogsContain(t, hook, "Block slot older/equal than last finalized epoch start slot, rejecting it")

	hook.Reset()
	b.Block.Slot = params.BeaconConfig().SlotsPerEpoch
	buf = new(bytes.Buffer)
	_, err = p.Encoding().EncodeGossip(buf, b)
	require.NoError(t, err)
	m = &pubsub.Message{
		Message: &pubsubpb.Message{
			Data: buf.Bytes(),
			TopicIDs: []string{
				p2p.GossipTypeMapping[reflect.TypeOf(b)],
			},
		},
	}

	r.validateBeaconBlockPubSub(context.Background(), "", m)
	require.LogsDoNotContain(t, hook, "Block slot older/equal than last finalized epoch start slot, rejecting itt")
}

func TestValidateBeaconBlockPubSub_ParentNotFinalizedDescendant(t *testing.T) {
	hook := logTest.NewGlobal()
	db, stateSummaryCache := dbtest.SetupDB(t)
	p := p2ptest.NewTestP2P(t)
	ctx := context.Background()
	beaconState, privKeys := testutil.DeterministicGenesisState(t, 100)
	parentBlock := testutil.NewBeaconBlock()
	require.NoError(t, db.SaveBlock(ctx, parentBlock))
	bRoot, err := parentBlock.Block.HashTreeRoot()
	require.NoError(t, err)
	require.NoError(t, db.SaveState(ctx, beaconState, bRoot))
	require.NoError(t, db.SaveStateSummary(ctx, &pb.StateSummary{Root: bRoot[:]}))
	copied := beaconState.Copy()
	require.NoError(t, copied.SetSlot(1))
	proposerIdx, err := helpers.BeaconProposerIndex(copied)
	require.NoError(t, err)
	msg := testutil.NewBeaconBlock()
	msg.Block.Slot = 1
	msg.Block.ProposerIndex = proposerIdx
	msg.Block.ParentRoot = bRoot[:]
	msg.Signature, err = helpers.ComputeDomainAndSign(beaconState, 0, msg.Block, params.BeaconConfig().DomainBeaconProposer, privKeys[proposerIdx])
	require.NoError(t, err)

	c, err := lru.New(10)
	require.NoError(t, err)
	c2, err := lru.New(10)
	require.NoError(t, err)
	stateGen := stategen.New(db, stateSummaryCache)
	chainService := &mock.ChainService{Genesis: time.Unix(time.Now().Unix()-int64(params.BeaconConfig().SecondsPerSlot), 0),
		State: beaconState,
		FinalizedCheckPoint: &ethpb.Checkpoint{
			Epoch: 0,
			Root:  make([]byte, 32),
		},
		VerifyBlkDescendantErr: errors.New("not part of finalized chain"),
	}
	r := &Service{
		db:                  db,
		p2p:                 p,
		initialSync:         &mockSync.Sync{IsSyncing: false},
		chain:               chainService,
		blockNotifier:       chainService.BlockNotifier(),
		seenBlockCache:      c,
		badBlockCache:       c2,
		slotToPendingBlocks: make(map[uint64][]*ethpb.SignedBeaconBlock),
		seenPendingBlocks:   make(map[[32]byte]bool),
		stateSummaryCache:   stateSummaryCache,
		stateGen:            stateGen,
	}
	buf := new(bytes.Buffer)
	_, err = p.Encoding().EncodeGossip(buf, msg)
	require.NoError(t, err)
	m := &pubsub.Message{
		Message: &pubsubpb.Message{
			Data: buf.Bytes(),
			TopicIDs: []string{
				p2p.GossipTypeMapping[reflect.TypeOf(msg)],
			},
		},
	}
	assert.Equal(t, pubsub.ValidationReject, r.validateBeaconBlockPubSub(ctx, "", m), "Wrong validation result returned")
	require.LogsContain(t, hook, "not part of finalized chain")
}

func TestValidateBeaconBlockPubSub_InvalidParentBlock(t *testing.T) {
	db, stateSummaryCache := dbtest.SetupDB(t)
	p := p2ptest.NewTestP2P(t)
	ctx := context.Background()
	beaconState, privKeys := testutil.DeterministicGenesisState(t, 100)
	parentBlock := testutil.NewBeaconBlock()
	require.NoError(t, db.SaveBlock(ctx, parentBlock))
	bRoot, err := parentBlock.Block.HashTreeRoot()
	require.NoError(t, err)
	require.NoError(t, db.SaveState(ctx, beaconState, bRoot))
	require.NoError(t, db.SaveStateSummary(ctx, &pb.StateSummary{Root: bRoot[:]}))
	copied := beaconState.Copy()
	require.NoError(t, copied.SetSlot(1))
	proposerIdx, err := helpers.BeaconProposerIndex(copied)
	require.NoError(t, err)
	msg := testutil.NewBeaconBlock()
	msg.Block.ProposerIndex = proposerIdx
	msg.Block.Slot = 1
	msg.Block.ParentRoot = bRoot[:]
	msg.Signature, err = helpers.ComputeDomainAndSign(beaconState, 0, msg.Block, params.BeaconConfig().DomainBeaconProposer, privKeys[proposerIdx])
	require.NoError(t, err)

	// Mutate Signature
	copy(msg.Signature[:4], []byte{1, 2, 3, 4})
	currBlockRoot, err := msg.Block.HashTreeRoot()
	require.NoError(t, err)

	c, err := lru.New(10)
	require.NoError(t, err)
	c2, err := lru.New(10)
	require.NoError(t, err)
	stateGen := stategen.New(db, stateSummaryCache)
	chainService := &mock.ChainService{Genesis: time.Unix(time.Now().Unix()-int64(params.BeaconConfig().SecondsPerSlot), 0),
		State: beaconState,
		FinalizedCheckPoint: &ethpb.Checkpoint{
			Epoch: 0,
		}}
	r := &Service{
		db:                  db,
		p2p:                 p,
		initialSync:         &mockSync.Sync{IsSyncing: false},
		chain:               chainService,
		blockNotifier:       chainService.BlockNotifier(),
		seenBlockCache:      c,
		badBlockCache:       c2,
		slotToPendingBlocks: make(map[uint64][]*ethpb.SignedBeaconBlock),
		seenPendingBlocks:   make(map[[32]byte]bool),
		stateSummaryCache:   stateSummaryCache,
		stateGen:            stateGen,
	}
	buf := new(bytes.Buffer)
	_, err = p.Encoding().EncodeGossip(buf, msg)
	require.NoError(t, err)
	m := &pubsub.Message{
		Message: &pubsubpb.Message{
			Data: buf.Bytes(),
			TopicIDs: []string{
				p2p.GossipTypeMapping[reflect.TypeOf(msg)],
			},
		},
	}
	result := r.validateBeaconBlockPubSub(ctx, "", m) == pubsub.ValidationAccept
	assert.Equal(t, false, result)

	require.NoError(t, copied.SetSlot(2))
	proposerIdx, err = helpers.BeaconProposerIndex(copied)
	require.NoError(t, err)

	msg = testutil.NewBeaconBlock()
	msg.Block.Slot = 2
	msg.Block.ProposerIndex = proposerIdx
	msg.Block.ParentRoot = currBlockRoot[:]

	buf = new(bytes.Buffer)
	_, err = p.Encoding().EncodeGossip(buf, msg)
	require.NoError(t, err)
	m = &pubsub.Message{
		Message: &pubsubpb.Message{
			Data: buf.Bytes(),
			TopicIDs: []string{
				p2p.GossipTypeMapping[reflect.TypeOf(msg)],
			},
		},
	}

	// Expect block with bad parent to fail too
	result = r.validateBeaconBlockPubSub(ctx, "", m) == pubsub.ValidationAccept
	assert.Equal(t, false, result)
}

func TestValidateBeaconBlockPubSub_RejectEvilBlocksFromFuture(t *testing.T) {
	db, stateSummaryCache := dbtest.SetupDB(t)
	p := p2ptest.NewTestP2P(t)
	ctx := context.Background()

	beaconState, privKeys := testutil.DeterministicGenesisState(t, 100)
	parentBlock := testutil.NewBeaconBlock()
	require.NoError(t, db.SaveBlock(ctx, parentBlock))
	bRoot, err := parentBlock.Block.HashTreeRoot()
	require.NoError(t, err)
	require.NoError(t, db.SaveState(ctx, beaconState, bRoot))
	require.NoError(t, db.SaveStateSummary(ctx, &pb.StateSummary{Root: bRoot[:]}))

	copied := beaconState.Copy()
	// The next block is at least 2 epochs ahead to induce shuffling and a new seed.
	blkSlot := params.BeaconConfig().SlotsPerEpoch * 2
	copied, err = state.ProcessSlots(context.Background(), copied, blkSlot)
	require.NoError(t, err)
	proposerIdx, err := helpers.BeaconProposerIndex(copied)
	require.NoError(t, err)

	msg := testutil.NewBeaconBlock()
	msg.Block.ProposerIndex = proposerIdx
	msg.Block.Slot = blkSlot

	perSlot := params.BeaconConfig().SecondsPerSlot
	// current slot time
	slotsSinceGenesis := uint64(1000)
	// max uint, divided by slot time. But avoid losing precision too much.
	overflowBase := (1 << 63) / (perSlot >> 1)
	msg.Block.Slot = overflowBase + slotsSinceGenesis

	// valid block
	msg.Block.ParentRoot = bRoot[:]
	msg.Signature, err = helpers.ComputeDomainAndSign(beaconState, 0, msg.Block, params.BeaconConfig().DomainBeaconProposer, privKeys[proposerIdx])

	genesisTime := time.Now()
	c, err := lru.New(10)
	require.NoError(t, err)
	c2, err := lru.New(10)
	require.NoError(t, err)

	stateGen := stategen.New(db, stateSummaryCache)
	chainService := &mock.ChainService{
		Genesis: time.Unix(genesisTime.Unix()-int64(slotsSinceGenesis*perSlot), 0),
		FinalizedCheckPoint: &ethpb.Checkpoint{
			Epoch: 0,
		},
	}
	r := &Service{
		db:                  db,
		p2p:                 p,
		initialSync:         &mockSync.Sync{IsSyncing: false},
		chain:               chainService,
		blockNotifier:       chainService.BlockNotifier(),
		seenBlockCache:      c,
		badBlockCache:       c2,
		slotToPendingBlocks: make(map[uint64][]*ethpb.SignedBeaconBlock),
		seenPendingBlocks:   make(map[[32]byte]bool),
		stateSummaryCache:   stateSummaryCache,
		stateGen:            stateGen,
	}

	buf := new(bytes.Buffer)
	_, err = p.Encoding().EncodeGossip(buf, msg)
	require.NoError(t, err)
	m := &pubsub.Message{
		Message: &pubsubpb.Message{
			Data: buf.Bytes(),
			TopicIDs: []string{
				p2p.GossipTypeMapping[reflect.TypeOf(msg)],
			},
		},
	}
	result := r.validateBeaconBlockPubSub(ctx, "", m) == pubsub.ValidationAccept
	assert.Equal(t, false, result)
}

func TestService_setBadBlock_DoesntSetWithContextErr(t *testing.T) {
	s := Service{}
	require.NoError(t, s.initCaches())

	root := [32]byte{'b', 'a', 'd'}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.setBadBlock(ctx, root)
	if s.hasBadBlock(root) {
		t.Error("Set bad root with cancelled context")
	}
}
