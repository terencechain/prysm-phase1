// Package state defines how the beacon chain state for eth2
// functions in the running beacon node, using an advanced,
// immutable implementation of the state data structure.
package state

import (
	"sync"

	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	pbp2p "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
)

func init() {
	fieldMap = make(map[fieldIndex]dataType)

	// Initialize the fixed sized arrays.
	fieldMap[blockRoots] = basicArray
	fieldMap[stateRoots] = basicArray
	fieldMap[randaoMixes] = basicArray

	// Initialize the composite arrays.
	fieldMap[eth1DataVotes] = compositeArray
	fieldMap[validators] = compositeArray
	fieldMap[previousEpochAttestations] = compositeArray
	fieldMap[currentEpochAttestations] = compositeArray
	fieldMap[shardStates] = compositeArray
}

type fieldIndex int

// dataType signifies the data type of the field.
type dataType int

// Below we define a set of useful enum values for the field
// indices of the beacon state. For example, genesisTime is the
// 0th field of the beacon state. This is helpful when we are
// updating the Merkle branches up the trie representation
// of the beacon state.
const (
	genesisTime fieldIndex = iota
	genesisValidatorRoot
	slot
	fork
	latestBlockHeader
	blockRoots
	stateRoots
	historicalRoots
	eth1Data
	eth1DataVotes
	eth1DepositIndex
	validators
	balances
	randaoMixes
	slashings
	previousEpochAttestations
	currentEpochAttestations
	justificationBits
	previousJustifiedCheckpoint
	currentJustifiedCheckpoint
	finalizedCheckpoint
	currentEpochStartShard
	shardStates
	onlineCountDown
)

// List of current data types the state supports.
const (
	basicArray dataType = iota
	compositeArray
)

// fieldMap keeps track of each field
// to its corresponding data type.
var fieldMap map[fieldIndex]dataType

// Reference structs are shared across BeaconState copies to understand when the state must use
// copy-on-write for shared fields or may modify a field in place when it holds the only reference
// to the field value. References are tracked in a map of fieldIndex -> *reference. Whenever a state
// releases their reference to the field value, they must decrement the refs. Likewise whenever a
// copy is performed then the state must increment the refs counter.
type reference struct {
	refs uint
	lock sync.RWMutex
}

// ErrNilInnerState returns when the inner state is nil and no copy set or get
// operations can be performed on state.
var ErrNilInnerState = errors.New("nil inner state")

// BeaconState defines a struct containing utilities for the eth2 chain state, defining
// getters and setters for its respective values and helpful functions such as HashTreeRoot().
type BeaconState struct {
	state                 *pbp2p.BeaconState
	lock                  sync.RWMutex
	dirtyFields           map[fieldIndex]interface{}
	dirtyIndices          map[fieldIndex][]uint64
	stateFieldLeaves      map[fieldIndex]*FieldTrie
	rebuildTrie           map[fieldIndex]bool
	valIdxMap             map[[48]byte]uint64
	merkleLayers          [][][]byte
	sharedFieldReferences map[fieldIndex]*reference
}

// ReadOnlyValidator returns a wrapper that only allows fields from a validator
// to be read, and prevents any modification of internal validator fields.
type ReadOnlyValidator struct {
	validator *ethpb.Validator
}

func (r *reference) Refs() uint {
	r.lock.RLock()
	defer r.lock.RUnlock()
	return r.refs
}

func (r *reference) AddRef() {
	r.lock.Lock()
	r.refs++
	r.lock.Unlock()
}

func (r *reference) MinusRef() {
	r.lock.Lock()
	defer r.lock.Unlock()
	// Do not reduce further if object
	// already has 0 reference to prevent overflow.
	if r.refs == 0 {
		return
	}
	r.refs--
}
