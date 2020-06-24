package sync

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/kevinms/leakybucket-go"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/protocol"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-ssz"
	mock "github.com/prysmaticlabs/prysm/beacon-chain/blockchain/testing"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/state"
	db "github.com/prysmaticlabs/prysm/beacon-chain/db/testing"
	p2ptest "github.com/prysmaticlabs/prysm/beacon-chain/p2p/testing"
	"github.com/prysmaticlabs/prysm/beacon-chain/state/stateutil"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/testutil"
)

func TestRecentBeaconBlocksRPCHandler_ReturnsBlocks(t *testing.T) {
	p1 := p2ptest.NewTestP2P(t)
	p2 := p2ptest.NewTestP2P(t)
	p1.Connect(p2)
	if len(p1.BHost.Network().Peers()) != 1 {
		t.Error("Expected peers to be connected")
	}
	d, _ := db.SetupDB(t)

	var blkRoots [][]byte
	// Populate the database with blocks that would match the request.
	for i := 1; i < 11; i++ {
		blk := &ethpb.BeaconBlock{
			Slot: uint64(i),
		}
		root, err := ssz.HashTreeRoot(blk)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.SaveBlock(context.Background(), &ethpb.SignedBeaconBlock{Block: blk}); err != nil {
			t.Fatal(err)
		}
		blkRoots = append(blkRoots, root[:])
	}

	r := &Service{p2p: p1, db: d, blocksRateLimiter: leakybucket.NewCollector(10000, 10000, false)}
	pcl := protocol.ID("/testing")

	var wg sync.WaitGroup
	wg.Add(1)
	p2.BHost.SetStreamHandler(pcl, func(stream network.Stream) {
		defer wg.Done()
		for i := range blkRoots {
			expectSuccess(t, r, stream)
			res := &ethpb.SignedBeaconBlock{}
			if err := r.p2p.Encoding().DecodeWithLength(stream, &res); err != nil {
				t.Error(err)
			}
			if res.Block.Slot != uint64(i+1) {
				t.Errorf("Received unexpected block slot %d but wanted %d", res.Block.Slot, i+1)
			}
		}
	})

	stream1, err := p1.BHost.NewStream(context.Background(), p2.BHost.ID(), pcl)
	if err != nil {
		t.Fatal(err)
	}
	req := &pb.BeaconBlocksByRootRequest{BlockRoots: blkRoots}
	err = r.beaconBlocksRootRPCHandler(context.Background(), req, stream1)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if testutil.WaitTimeout(&wg, 1*time.Second) {
		t.Fatal("Did not receive stream within 1 sec")
	}
}

func TestRecentBeaconBlocks_RPCRequestSent(t *testing.T) {
	p1 := p2ptest.NewTestP2P(t)
	p2 := p2ptest.NewTestP2P(t)
	p1.DelaySend = true

	blockA := &ethpb.SignedBeaconBlock{Block: &ethpb.BeaconBlock{Slot: 111}}
	blockB := &ethpb.SignedBeaconBlock{Block: &ethpb.BeaconBlock{Slot: 40}}
	// Set up a head state with data we expect.
	blockARoot, err := stateutil.BlockRoot(blockA.Block)
	if err != nil {
		t.Fatal(err)
	}
	blockBRoot, err := stateutil.BlockRoot(blockB.Block)
	if err != nil {
		t.Fatal(err)
	}
	genesisState, err := state.GenesisBeaconState(nil, 0, &ethpb.Eth1Data{})
	if err != nil {
		t.Fatal(err)
	}
	if err := genesisState.SetSlot(111); err != nil {
		t.Fatal(err)
	}
	if err := genesisState.UpdateBlockRootAtIndex(111%params.BeaconConfig().SlotsPerHistoricalRoot, blockARoot); err != nil {
		t.Fatal(err)
	}
	finalizedCheckpt := &ethpb.Checkpoint{
		Epoch: 5,
		Root:  blockBRoot[:],
	}

	expectedRoots := [][]byte{blockBRoot[:], blockARoot[:]}

	r := &Service{
		p2p: p1,
		chain: &mock.ChainService{
			State:               genesisState,
			FinalizedCheckPoint: finalizedCheckpt,
			Root:                blockARoot[:],
		},
		slotToPendingBlocks: make(map[uint64]*ethpb.SignedBeaconBlock),
		seenPendingBlocks:   make(map[[32]byte]bool),
		ctx:                 context.Background(),
		blocksRateLimiter:   leakybucket.NewCollector(10000, 10000, false),
	}

	// Setup streams
	pcl := protocol.ID("/eth2/beacon_chain/req/beacon_blocks_by_root/1/ssz")
	var wg sync.WaitGroup
	wg.Add(1)
	p2.BHost.SetStreamHandler(pcl, func(stream network.Stream) {
		defer wg.Done()
		out := &pb.BeaconBlocksByRootRequest{BlockRoots: [][]byte{}}
		if err := p2.Encoding().DecodeWithLength(stream, out); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(out.BlockRoots, expectedRoots) {
			t.Fatalf("Did not receive expected message. Got %+v wanted %+v", out.BlockRoots, expectedRoots)
		}
		response := []*ethpb.SignedBeaconBlock{blockB, blockA}
		for _, blk := range response {
			if _, err := stream.Write([]byte{responseCodeSuccess}); err != nil {
				t.Fatalf("Failed to write to stream: %v", err)
			}
			_, err := p2.Encoding().EncodeWithLength(stream, blk)
			if err != nil {
				t.Errorf("Could not send response back: %v ", err)
			}
		}
	})

	p1.Connect(p2)
	if err := r.sendRecentBeaconBlocksRequest(context.Background(), expectedRoots, p2.PeerID()); err != nil {
		t.Fatal(err)
	}

	if testutil.WaitTimeout(&wg, 1*time.Second) {
		t.Fatal("Did not receive stream within 1 sec")
	}
}
