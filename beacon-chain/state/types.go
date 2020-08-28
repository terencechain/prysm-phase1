package state

import (
	"sync"

	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	coreutils "github.com/prysmaticlabs/prysm/beacon-chain/core/state/stateutils"
	pbp2p "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
)

func init() {
	fieldMap = make(map[fieldIndex]dataType, fieldCount)

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
	fieldMap[custodyChunkChallengeRecords] = compositeArray
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
	currentLightCommittee
	nextLightCommittee
	custodyChunkChallengeRecords
	custodyChunkChallengeIndex
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
	valMapHandler         *validatorMapHandler
	merkleLayers          [][][]byte
	sharedFieldReferences map[fieldIndex]*reference
}

// String returns the name of the field index.
func (f fieldIndex) String() string {
	switch f {
	case genesisTime:
		return "genesisTime"
	case genesisValidatorRoot:
		return "genesisValidatorRoot"
	case slot:
		return "slot"
	case fork:
		return "fork"
	case latestBlockHeader:
		return "latestBlockHeader"
	case blockRoots:
		return "blockRoots"
	case stateRoots:
		return "stateRoots"
	case historicalRoots:
		return "historicalRoots"
	case eth1Data:
		return "eth1Data"
	case eth1DataVotes:
		return "eth1DataVotes"
	case eth1DepositIndex:
		return "eth1DepositIndex"
	case validators:
		return "validators"
	case balances:
		return "balances"
	case randaoMixes:
		return "randaoMixes"
	case slashings:
		return "slashings"
	case previousEpochAttestations:
		return "previousEpochAttestations"
	case currentEpochAttestations:
		return "currentEpochAttestations"
	case justificationBits:
		return "justificationBits"
	case previousJustifiedCheckpoint:
		return "previousJustifiedCheckpoint"
	case currentJustifiedCheckpoint:
		return "currentJustifiedCheckpoint"
	case finalizedCheckpoint:
		return "finalizedCheckpoint"
	default:
		return ""
	}
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
	// Do not reduce further if object
	// already has 0 reference to prevent underflow.
	if r.refs > 0 {
		r.refs--
	}
	r.lock.Unlock()
}

// a container to hold the map and a reference tracker for how many
// states shared this.
type validatorMapHandler struct {
	valIdxMap map[[48]byte]uint64
	mapRef    *reference
}

// A constructor for the map handler.
func newValHandler(vals []*ethpb.Validator) *validatorMapHandler {
	return &validatorMapHandler{
		valIdxMap: coreutils.ValidatorIndexMap(vals),
		mapRef:    &reference{refs: 1},
	}
}

// copies the whole map and returns a map handler with the copied map.
func (v *validatorMapHandler) copy() *validatorMapHandler {
	if v == nil || v.valIdxMap == nil {
		return &validatorMapHandler{valIdxMap: map[[48]byte]uint64{}, mapRef: new(reference)}
	}
	m := make(map[[48]byte]uint64, len(v.valIdxMap))
	for k, v := range v.valIdxMap {
		m[k] = v
	}
	return &validatorMapHandler{
		valIdxMap: m,
		mapRef:    &reference{refs: 1},
	}
}
