package sync

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/protocol"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	mock "github.com/prysmaticlabs/prysm/beacon-chain/blockchain/testing"
	dbtest "github.com/prysmaticlabs/prysm/beacon-chain/db/testing"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p/peers"
	p2ptest "github.com/prysmaticlabs/prysm/beacon-chain/p2p/testing"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/rand"
	"github.com/prysmaticlabs/prysm/shared/testutil"
	"github.com/prysmaticlabs/prysm/shared/testutil/assert"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
)

//    /- b1 - b2
// b0
//    \- b3
// Test b1 was missing then received and we can process b0 -> b1 -> b2
func TestRegularSyncBeaconBlockSubscriber_ProcessPendingBlocks1(t *testing.T) {
	db, _ := dbtest.SetupDB(t)

	p1 := p2ptest.NewTestP2P(t)
	r := &Service{
		p2p: p1,
		db:  db,
		chain: &mock.ChainService{
			FinalizedCheckPoint: &ethpb.Checkpoint{
				Epoch: 0,
			},
		},
		slotToPendingBlocks: make(map[uint64][]*ethpb.SignedBeaconBlock),
		seenPendingBlocks:   make(map[[32]byte]bool),
	}
	err := r.initCaches()
	require.NoError(t, err)

	b0 := testutil.NewBeaconBlock()
	require.NoError(t, r.db.SaveBlock(context.Background(), b0))
	b0Root, err := b0.Block.HashTreeRoot()
	require.NoError(t, err)
	b3 := testutil.NewBeaconBlock()
	b3.Block.Slot = 3
	b3.Block.ParentRoot = b0Root[:]
	require.NoError(t, r.db.SaveBlock(context.Background(), b3))
	// Incomplete block link
	b1 := testutil.NewBeaconBlock()
	b1.Block.Slot = 1
	b1.Block.ParentRoot = b0Root[:]
	b1Root, err := b1.Block.HashTreeRoot()
	require.NoError(t, err)
	b2 := testutil.NewBeaconBlock()
	b2.Block.Slot = 2
	b2.Block.ParentRoot = b1Root[:]
	b2Root, err := b1.Block.HashTreeRoot()
	require.NoError(t, err)

	// Add b2 to the cache
	r.insertBlockToPendingQueue(b2.Block.Slot, b2, b2Root)

	require.NoError(t, r.processPendingBlocks(context.Background()))
	assert.Equal(t, 1, len(r.slotToPendingBlocks), "Incorrect size for slot to pending blocks cache")
	assert.Equal(t, 1, len(r.seenPendingBlocks), "Incorrect size for seen pending block")

	// Add b1 to the cache
	r.insertBlockToPendingQueue(b1.Block.Slot, b1, b1Root)
	require.NoError(t, r.db.SaveBlock(context.Background(), b1))

	// Insert bad b1 in the cache to verify the good one doesn't get replaced.
	r.insertBlockToPendingQueue(b1.Block.Slot, testutil.NewBeaconBlock(), [32]byte{})

	require.NoError(t, r.processPendingBlocks(context.Background()))
	assert.Equal(t, 1, len(r.slotToPendingBlocks), "Incorrect size for slot to pending blocks cache")
	assert.Equal(t, 2, len(r.seenPendingBlocks), "Incorrect size for seen pending block")
}

func TestRegularSync_InsertDuplicateBlocks(t *testing.T) {
	db, _ := dbtest.SetupDB(t)

	p1 := p2ptest.NewTestP2P(t)
	r := &Service{
		p2p: p1,
		db:  db,
		chain: &mock.ChainService{
			FinalizedCheckPoint: &ethpb.Checkpoint{
				Epoch: 0,
				Root:  make([]byte, 32),
			},
		},
		slotToPendingBlocks: make(map[uint64][]*ethpb.SignedBeaconBlock),
		seenPendingBlocks:   make(map[[32]byte]bool),
	}
	err := r.initCaches()
	require.NoError(t, err)

	b0 := testutil.NewBeaconBlock()
	b0r := [32]byte{'a'}
	require.NoError(t, r.db.SaveBlock(context.Background(), b0))
	b0Root, err := b0.Block.HashTreeRoot()
	b1 := testutil.NewBeaconBlock()
	b1.Block.Slot = 1
	b1.Block.ParentRoot = b0Root[:]
	b1r := [32]byte{'b'}

	r.insertBlockToPendingQueue(b0.Block.Slot, b0, b0r)
	require.Equal(t, int(1), len(r.slotToPendingBlocks[b0.Block.Slot]), "Block was not added to map")

	r.insertBlockToPendingQueue(b1.Block.Slot, b1, b1r)
	require.Equal(t, int(1), len(r.slotToPendingBlocks[b1.Block.Slot]), "Block was not added to map")

	// Add duplicate block which should not be saved.
	r.insertBlockToPendingQueue(b0.Block.Slot, b0, b0r)
	require.Equal(t, int(1), len(r.slotToPendingBlocks[b0.Block.Slot]), "Block was added to map")

	// Add duplicate block which should not be saved.
	r.insertBlockToPendingQueue(b1.Block.Slot, b1, b1r)
	require.Equal(t, int(1), len(r.slotToPendingBlocks[b1.Block.Slot]), "Block was added to map")

}

//    /- b1 - b2 - b5
// b0
//    \- b3 - b4
// Test b2 and b3 were missed, after receiving them we can process 2 chains.
func TestRegularSyncBeaconBlockSubscriber_ProcessPendingBlocks_2Chains(t *testing.T) {
	db, _ := dbtest.SetupDB(t)
	p1 := p2ptest.NewTestP2P(t)
	p2 := p2ptest.NewTestP2P(t)
	p1.Connect(p2)
	assert.Equal(t, 1, len(p1.BHost.Network().Peers()), "Expected peers to be connected")
	pcl := protocol.ID("/eth2/beacon_chain/req/hello/1/ssz_snappy")
	var wg sync.WaitGroup
	wg.Add(1)
	p2.BHost.SetStreamHandler(pcl, func(stream network.Stream) {
		defer wg.Done()
		code, errMsg, err := ReadStatusCode(stream, p1.Encoding())
		assert.NoError(t, err)
		if code == 0 {
			t.Error("Expected a non-zero code")
		}
		if errMsg != errWrongForkDigestVersion.Error() {
			t.Logf("Received error string len %d, wanted error string len %d", len(errMsg), len(errWrongForkDigestVersion.Error()))
			t.Errorf("Received unexpected message response in the stream: %s. Wanted %s.", errMsg, errWrongForkDigestVersion.Error())
		}
	})

	r := &Service{
		p2p: p1,
		db:  db,
		chain: &mock.ChainService{
			FinalizedCheckPoint: &ethpb.Checkpoint{
				Epoch: 0,
				Root:  make([]byte, 32),
			},
		},
		slotToPendingBlocks: make(map[uint64][]*ethpb.SignedBeaconBlock),
		seenPendingBlocks:   make(map[[32]byte]bool),
	}
	err := r.initCaches()
	require.NoError(t, err)
	p1.Peers().Add(new(enr.Record), p2.PeerID(), nil, network.DirOutbound)
	p1.Peers().SetConnectionState(p2.PeerID(), peers.PeerConnected)
	p1.Peers().SetChainState(p2.PeerID(), &pb.Status{})

	b0 := testutil.NewBeaconBlock()
	require.NoError(t, r.db.SaveBlock(context.Background(), b0))
	b0Root, err := b0.Block.HashTreeRoot()
	require.NoError(t, err)
	b1 := testutil.NewBeaconBlock()
	b1.Block.Slot = 1
	b1.Block.ParentRoot = b0Root[:]
	require.NoError(t, r.db.SaveBlock(context.Background(), b1))
	b1Root, err := b1.Block.HashTreeRoot()
	require.NoError(t, err)

	// Incomplete block links
	b2 := testutil.NewBeaconBlock()
	b2.Block.Slot = 2
	b2.Block.ParentRoot = b1Root[:]
	b2Root, err := b2.Block.HashTreeRoot()
	require.NoError(t, err)
	b5 := testutil.NewBeaconBlock()
	b5.Block.Slot = 5
	b5.Block.ParentRoot = b2Root[:]
	b5Root, err := b5.Block.HashTreeRoot()
	require.NoError(t, err)
	b3 := testutil.NewBeaconBlock()
	b3.Block.Slot = 3
	b3.Block.ParentRoot = b0Root[:]
	b3Root, err := b3.Block.HashTreeRoot()
	require.NoError(t, err)
	b4 := testutil.NewBeaconBlock()
	b4.Block.Slot = 4
	b4.Block.ParentRoot = b3Root[:]
	b4Root, err := b4.Block.HashTreeRoot()
	require.NoError(t, err)

	r.insertBlockToPendingQueue(b4.Block.Slot, b4, b4Root)
	r.insertBlockToPendingQueue(b5.Block.Slot, b5, b5Root)

	require.NoError(t, r.processPendingBlocks(context.Background()))
	assert.Equal(t, 2, len(r.slotToPendingBlocks), "Incorrect size for slot to pending blocks cache")
	assert.Equal(t, 2, len(r.seenPendingBlocks), "Incorrect size for seen pending block")

	// Add b3 to the cache
	r.insertBlockToPendingQueue(b3.Block.Slot, b3, b3Root)
	require.NoError(t, r.db.SaveBlock(context.Background(), b3))
	require.NoError(t, r.processPendingBlocks(context.Background()))
	assert.Equal(t, 1, len(r.slotToPendingBlocks), "Incorrect size for slot to pending blocks cache")
	assert.Equal(t, 3, len(r.seenPendingBlocks), "Incorrect size for seen pending block")

	// Add b2 to the cache
	r.insertBlockToPendingQueue(b2.Block.Slot, b2, b2Root)

	require.NoError(t, r.db.SaveBlock(context.Background(), b2))
	require.NoError(t, r.processPendingBlocks(context.Background()))
	assert.Equal(t, 0, len(r.slotToPendingBlocks), "Incorrect size for slot to pending blocks cache")
	assert.Equal(t, 4, len(r.seenPendingBlocks), "Incorrect size for seen pending block")
}

func TestRegularSyncBeaconBlockSubscriber_PruneOldPendingBlocks(t *testing.T) {
	db, _ := dbtest.SetupDB(t)
	p1 := p2ptest.NewTestP2P(t)
	p2 := p2ptest.NewTestP2P(t)
	p1.Connect(p2)
	assert.Equal(t, 1, len(p1.BHost.Network().Peers()), "Expected peers to be connected")

	r := &Service{
		p2p: p1,
		db:  db,
		chain: &mock.ChainService{
			FinalizedCheckPoint: &ethpb.Checkpoint{
				Epoch: 1,
				Root:  make([]byte, 32),
			},
		},
		slotToPendingBlocks: make(map[uint64][]*ethpb.SignedBeaconBlock),
		seenPendingBlocks:   make(map[[32]byte]bool),
	}
	err := r.initCaches()
	require.NoError(t, err)
	p1.Peers().Add(new(enr.Record), p1.PeerID(), nil, network.DirOutbound)
	p1.Peers().SetConnectionState(p1.PeerID(), peers.PeerConnected)
	p1.Peers().SetChainState(p1.PeerID(), &pb.Status{})

	b0 := testutil.NewBeaconBlock()
	require.NoError(t, r.db.SaveBlock(context.Background(), b0))
	b0Root, err := b0.Block.HashTreeRoot()
	require.NoError(t, err)
	b1 := testutil.NewBeaconBlock()
	b1.Block.Slot = 1
	b1.Block.ParentRoot = b0Root[:]
	require.NoError(t, r.db.SaveBlock(context.Background(), b1))
	b1Root, err := b1.Block.HashTreeRoot()
	require.NoError(t, err)

	// Incomplete block links
	b2 := testutil.NewBeaconBlock()
	b2.Block.Slot = 2
	b2.Block.ParentRoot = b1Root[:]
	b2Root, err := b2.Block.HashTreeRoot()
	require.NoError(t, err)
	b5 := testutil.NewBeaconBlock()
	b5.Block.Slot = 5
	b5.Block.ParentRoot = b2Root[:]
	b5Root, err := b5.Block.HashTreeRoot()
	require.NoError(t, err)
	b3 := testutil.NewBeaconBlock()
	b3.Block.Slot = 3
	b3.Block.ParentRoot = b0Root[:]
	b3Root, err := b3.Block.HashTreeRoot()
	require.NoError(t, err)
	b4 := testutil.NewBeaconBlock()
	b4.Block.Slot = 4
	b4.Block.ParentRoot = b3Root[:]
	b4Root, err := b4.Block.HashTreeRoot()
	require.NoError(t, err)

	r.insertBlockToPendingQueue(b2.Block.Slot, b2, b2Root)
	r.insertBlockToPendingQueue(b3.Block.Slot, b3, b3Root)
	r.insertBlockToPendingQueue(b4.Block.Slot, b4, b4Root)
	r.insertBlockToPendingQueue(b5.Block.Slot, b5, b5Root)

	require.NoError(t, r.processPendingBlocks(context.Background()))
	assert.Equal(t, 0, len(r.slotToPendingBlocks), "Incorrect size for slot to pending blocks cache")
	assert.Equal(t, 4, len(r.seenPendingBlocks), "Incorrect size for seen pending block")
}

func TestService_sortedPendingSlots(t *testing.T) {
	r := &Service{
		slotToPendingBlocks: make(map[uint64][]*ethpb.SignedBeaconBlock),
		seenPendingBlocks:   make(map[[32]byte]bool),
	}

	var lastSlot uint64 = math.MaxUint64
	r.insertBlockToPendingQueue(lastSlot, &ethpb.SignedBeaconBlock{}, [32]byte{1})
	r.insertBlockToPendingQueue(lastSlot-3, &ethpb.SignedBeaconBlock{}, [32]byte{2})
	r.insertBlockToPendingQueue(lastSlot-5, &ethpb.SignedBeaconBlock{}, [32]byte{3})
	r.insertBlockToPendingQueue(lastSlot-2, &ethpb.SignedBeaconBlock{}, [32]byte{4})

	want := []uint64{lastSlot - 5, lastSlot - 3, lastSlot - 2, lastSlot}
	assert.DeepEqual(t, want, r.sortedPendingSlots(), "Unexpected pending slots list")
}

func TestService_BatchRootRequest(t *testing.T) {
	db, _ := dbtest.SetupDB(t)
	p1 := p2ptest.NewTestP2P(t)
	p2 := p2ptest.NewTestP2P(t)
	p1.Connect(p2)
	assert.Equal(t, 1, len(p1.BHost.Network().Peers()), "Expected peers to be connected")

	r := &Service{
		p2p: p1,
		db:  db,
		chain: &mock.ChainService{
			FinalizedCheckPoint: &ethpb.Checkpoint{
				Epoch: 1,
				Root:  make([]byte, 32),
			},
		},
		slotToPendingBlocks: make(map[uint64][]*ethpb.SignedBeaconBlock),
		seenPendingBlocks:   make(map[[32]byte]bool),
	}

	err := r.initCaches()
	require.NoError(t, err)
	p1.Peers().Add(new(enr.Record), p2.PeerID(), nil, network.DirOutbound)
	p1.Peers().SetConnectionState(p2.PeerID(), peers.PeerConnected)
	p1.Peers().SetChainState(p2.PeerID(), &pb.Status{FinalizedEpoch: 2})

	b0 := testutil.NewBeaconBlock()
	require.NoError(t, r.db.SaveBlock(context.Background(), b0))
	b0Root, err := b0.Block.HashTreeRoot()
	require.NoError(t, err)
	b1 := testutil.NewBeaconBlock()
	b1.Block.Slot = 1
	b1.Block.ParentRoot = b0Root[:]
	require.NoError(t, r.db.SaveBlock(context.Background(), b1))
	b1Root, err := b1.Block.HashTreeRoot()
	require.NoError(t, err)

	b2 := testutil.NewBeaconBlock()
	b2.Block.Slot = 2
	b2.Block.ParentRoot = b1Root[:]
	b2Root, err := b2.Block.HashTreeRoot()
	require.NoError(t, err)
	b5 := testutil.NewBeaconBlock()
	b5.Block.Slot = 5
	b5.Block.ParentRoot = b2Root[:]
	b5Root, err := b5.Block.HashTreeRoot()
	require.NoError(t, err)
	b3 := testutil.NewBeaconBlock()
	b3.Block.Slot = 3
	b3.Block.ParentRoot = b0Root[:]
	b3Root, err := b3.Block.HashTreeRoot()
	require.NoError(t, err)
	b4 := testutil.NewBeaconBlock()
	b4.Block.Slot = 4
	b4.Block.ParentRoot = b3Root[:]
	b4Root, err := b4.Block.HashTreeRoot()
	require.NoError(t, err)

	// Send in duplicated roots to also test deduplicaton.
	sentRoots := [][32]byte{b2Root, b2Root, b3Root, b3Root, b4Root, b5Root}
	expectedRoots := [][32]byte{b2Root, b3Root, b4Root, b5Root}

	pcl := protocol.ID("/eth2/beacon_chain/req/beacon_blocks_by_root/1/ssz_snappy")
	var wg sync.WaitGroup
	wg.Add(1)
	p2.BHost.SetStreamHandler(pcl, func(stream network.Stream) {
		defer wg.Done()
		out := [][32]byte{}
		assert.NoError(t, p2.Encoding().DecodeWithMaxLength(stream, &out))
		assert.DeepEqual(t, expectedRoots, out, "Did not receive expected message")
		response := []*ethpb.SignedBeaconBlock{b2, b3, b4, b5}
		for _, blk := range response {
			_, err := stream.Write([]byte{responseCodeSuccess})
			assert.NoError(t, err, "Failed to write to stream")
			_, err = p2.Encoding().EncodeWithMaxLength(stream, blk)
			assert.NoError(t, err, "Could not send response back")
		}
		assert.NoError(t, stream.Close())
	})

	require.NoError(t, r.sendBatchRootRequest(context.Background(), sentRoots, rand.NewGenerator()))

	if testutil.WaitTimeout(&wg, 1*time.Second) {
		t.Fatal("Did not receive stream within 1 sec")
	}
	assert.Equal(t, 4, len(r.slotToPendingBlocks), "Incorrect size for slot to pending blocks cache")
	assert.Equal(t, 4, len(r.seenPendingBlocks), "Incorrect size for seen pending block")
}
