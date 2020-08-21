package protoarray

import "sync"

// ForkChoice defines the overall fork choice store which includes all block nodes, validator's latest votes and balances.
type ForkChoice struct {
	store      *Store
	votes      []Vote   // tracks individual validator's last vote.
	shardVotes [][]Vote // tracks individual validator's last vote on shards.
	balances   []uint64 // tracks individual validator's last justified balances.
}

// Store defines the fork choice store which includes block nodes and the last view of checkpoint information.
type Store struct {
	pruneThreshold       uint64              // do not prune tree unless threshold is reached.
	justifiedEpoch       uint64              // latest justified epoch in store.
	finalizedEpoch       uint64              // latest finalized epoch in store.
	finalizedRoot        [32]byte            // latest finalized root in store.
	nodes                []*Node             // list of block nodes, each node is a representation of one block.
	nodesIndices         map[[32]byte]uint64 // the root of block node and the nodes index in the list.
	nodeIndicesLock      sync.RWMutex
	shardNodes           [][]*ShardNode        // list of shard block nodes, each node is a representation of one block.
	shardNodeIndices     []map[[32]byte]uint64 // the root of shard block node and the Nodes index in the list.
	shardNodeIndicesLock sync.RWMutex
}

// Node defines the individual block which includes its block parent, ancestor and how much weight accounted for it.
// This is used as an array based stateful DAG for efficient fork choice look up.
type Node struct {
	slot           uint64   // slot of the block converted to the node.
	root           [32]byte // root of the block converted to the node.
	parent         uint64   // parent index of this node.
	justifiedEpoch uint64   // justifiedEpoch of this node.
	finalizedEpoch uint64   // finalizedEpoch of this node.
	weight         uint64   // weight of this node.
	bestChild      uint64   // bestChild index of this node.
	bestDescendant uint64   // bestDescendant of this node.
	graffiti       [32]byte // graffiti of the block node.
}

// ShardNode defines the individual shard block which includes its block parent, ancestor and how much weight accounted for it.
// This is used as an array based stateful DAG for efficient fork choice look up.
type ShardNode struct {
	Slot           uint64   // slot of the shard block converted to the node.
	Shard          uint64   // Shard of the shard node.
	Root           [32]byte // Root of the shard block converted to the node.
	Parent         uint64   // the parent index of this node.
	Weight         uint64   // weight of this node.
	BestChild      uint64   // best child index of this node.
	BestDescendent uint64   // head index of this node.
}

// Vote defines an individual validator's vote.
type Vote struct {
	currentBeaconRoot [32]byte // current voting root of beacon block.
	nextBeaconRoot    [32]byte // next voting root of beacon block.
	nextEpoch         uint64   // epoch of next voting period.
	currentShardRoot  [32]byte // current voting root of shard block.
	nextShardRoot     [32]byte // next voting root of shard block.
	shard             uint64   // the shard votes are for this shard.
}

// NonExistentNode defines an unknown node which is used for the array based stateful DAG.
const NonExistentNode = ^uint64(0)
