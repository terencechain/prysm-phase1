package stateutil

import (
	"bytes"
	"encoding/binary"

	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-ssz"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/hashutil"
	"github.com/prysmaticlabs/prysm/shared/htrutils"
	"github.com/prysmaticlabs/prysm/shared/params"
)

// CustodyChunkChallengeRecordRoot computes the HashTreeRoot Merkleization of
// a LightCommitteeRoot struct according to the eth2
// Simple Serialize specification.
func CustodyChunkChallengeRecordRoot(record *pb.CustodyChunkChallengeRecord) ([32]byte, error) {
	fieldRoots := make([][]byte, 6)
	if record != nil {
		challengeBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(challengeBuf, record.ChallengeIndex)
		challengeRoot := bytesutil.ToBytes32(challengeBuf)
		fieldRoots[0] = challengeRoot[:]

		challengerBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(challengerBuf, record.ChallengerIndex)
		challengerRoot := bytesutil.ToBytes32(challengerBuf)
		fieldRoots[1] = challengerRoot[:]

		responderBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(responderBuf, record.ResponderIndex)
		responderRoot := bytesutil.ToBytes32(responderBuf)
		fieldRoots[2] = responderRoot[:]

		inclusionBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(inclusionBuf, record.InclusionEpoch)
		inclusionRoot := bytesutil.ToBytes32(inclusionBuf)
		fieldRoots[3] = inclusionRoot[:]

		dataRoot := bytesutil.ToBytes32(record.DataRoot)
		fieldRoots[4] = dataRoot[:]

		chunkIndexBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(chunkIndexBuf, record.ChunkIndex)
		chunkRoot := bytesutil.ToBytes32(chunkIndexBuf)
		fieldRoots[5] = chunkRoot[:]
	}
	return htrutils.BitwiseMerkleize(hashutil.CustomSHA256Hasher(), fieldRoots, uint64(len(fieldRoots)), uint64(len(fieldRoots)))
}

// LightCommitteeRoot computes the HashTreeRoot Merkleization of
// a LightCommitteeRoot struct according to the eth2
// Simple Serialize specification.
func LightCommitteeRoot(committee *ethpb.CompactCommittee) ([32]byte, error) {
	// TODO(0): Implement light client root
	return ssz.HashTreeRoot(committee)
}

// ShardStateRoot computes the HashTreeRoot Merkleization of
// a ShardStateRoot struct according to the eth2
// Simple Serialize specification.
func ShardStateRoot(shardState *ethpb.ShardState) ([32]byte, error) {
	fieldRoots := make([][]byte, 3)
	if shardState != nil {
		stateSlotBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(stateSlotBuf, shardState.Slot)
		headerSlotRoot := bytesutil.ToBytes32(stateSlotBuf)
		fieldRoots[0] = headerSlotRoot[:]
		gasPrice := make([]byte, 8)
		binary.LittleEndian.PutUint64(gasPrice, shardState.GasPrice)
		gasPriceRoot := bytesutil.ToBytes32(gasPrice)
		fieldRoots[1] = gasPriceRoot[:]
		latestBlockRoot := bytesutil.ToBytes32(shardState.LatestBlockRoot)
		fieldRoots[2] = latestBlockRoot[:]
	}
	return htrutils.BitwiseMerkleize(hashutil.CustomSHA256Hasher(), fieldRoots, uint64(len(fieldRoots)), uint64(len(fieldRoots)))
}

// this returns the shard state root using an input hasher, it's used internally
// to be flexible and to be efficient.
func shardStateRoot(hasher htrutils.HashFn, shardState *ethpb.ShardState) ([32]byte, error) {
	fieldRoots := make([][32]byte, 3)
	if shardState != nil {
		slotBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(slotBuf, shardState.Slot)
		slotRoot := bytesutil.ToBytes32(slotBuf)
		fieldRoots[0] = slotRoot

		indexBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(indexBuf, shardState.GasPrice)
		gasPriceRoot := bytesutil.ToBytes32(indexBuf)
		fieldRoots[1] = gasPriceRoot

		latestBlockRoot := bytesutil.ToBytes32(shardState.LatestBlockRoot)
		fieldRoots[2] = latestBlockRoot
	}

	return htrutils.BitwiseMerkleizeArrays(hasher, fieldRoots, uint64(len(fieldRoots)), uint64(len(fieldRoots)))
}

// this returns the shard states root using the `shardStateRoot` input helper.
func shardStatesRoot(shardStates []*ethpb.ShardState) ([32]byte, error) {
	hasher := hashutil.CustomSHA256Hasher()
	roots := make([][]byte, len(shardStates))
	for i := 0; i < len(shardStates); i++ {
		shardStateRoot, err := shardStateRoot(hasher, shardStates[i])
		if err != nil {
			return [32]byte{}, errors.Wrap(err, "could not shard state merkleization")
		}
		roots[i] = shardStateRoot[:]
	}

	shardStatesRoot, err := htrutils.BitwiseMerkleize(
		hasher,
		roots,
		uint64(len(roots)),
		params.ShardConfig().MaxShard,
	)
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not compute shard states merkleization")
	}
	shardsBuf := new(bytes.Buffer)
	if err := binary.Write(shardsBuf, binary.LittleEndian, uint64(len(shardStates))); err != nil {
		return [32]byte{}, errors.Wrap(err, "could not marshal shard states length")
	}
	// We need to mix in the length of the slice.
	statesLenRoot := make([]byte, 32)
	copy(statesLenRoot, shardsBuf.Bytes())
	res := htrutils.MixInLength(shardStatesRoot, statesLenRoot)
	return res, nil
}

func custodyRecordRoot(hasher htrutils.HashFn, custodyRecord *pb.CustodyChunkChallengeRecord) ([32]byte, error) {
	fieldRoots := make([][32]byte, 6)
	if custodyRecord != nil {
		challengeBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(challengeBuf, custodyRecord.ChallengeIndex)
		challengeRoot := bytesutil.ToBytes32(challengeBuf)
		fieldRoots[0] = challengeRoot

		challengerBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(challengerBuf, custodyRecord.ChallengerIndex)
		challengerRoot := bytesutil.ToBytes32(challengerBuf)
		fieldRoots[1] = challengerRoot

		responderBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(responderBuf, custodyRecord.ResponderIndex)
		responderRoot := bytesutil.ToBytes32(responderBuf)
		fieldRoots[2] = responderRoot

		inclusionBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(inclusionBuf, custodyRecord.InclusionEpoch)
		inclusionRoot := bytesutil.ToBytes32(inclusionBuf)
		fieldRoots[3] = inclusionRoot

		dataRoot := bytesutil.ToBytes32(custodyRecord.DataRoot)
		fieldRoots[4] = dataRoot

		chunkIndexBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(chunkIndexBuf, custodyRecord.ChunkIndex)
		chunkRoot := bytesutil.ToBytes32(chunkIndexBuf)
		fieldRoots[5] = chunkRoot
	}
	return htrutils.BitwiseMerkleizeArrays(hasher, fieldRoots, uint64(len(fieldRoots)), uint64(len(fieldRoots)))
}

// this returns the custody records root using the `CustodyChunkChallengeRecord` input helper.
func custodyRecordsRoot(custodyRecords []*pb.CustodyChunkChallengeRecord) ([32]byte, error) {
	hasher := hashutil.CustomSHA256Hasher()
	roots := make([][]byte, len(custodyRecords))
	for i := 0; i < len(custodyRecords); i++ {
		custodyRecordRoot, err := custodyRecordRoot(hasher, custodyRecords[i])
		if err != nil {
			return [32]byte{}, errors.Wrap(err, "could not custody root merkleization")
		}
		roots[i] = custodyRecordRoot[:]
	}

	custodyRecordsRoot, err := htrutils.BitwiseMerkleize(
		hasher,
		roots,
		uint64(len(roots)),
		params.ShardConfig().MaxCustodyChunkChallengeRecords,
	)
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not compute custody records merkleization")
	}
	custodyBuf := new(bytes.Buffer)
	if err := binary.Write(custodyBuf, binary.LittleEndian, uint64(len(custodyRecords))); err != nil {
		return [32]byte{}, errors.Wrap(err, "could not marshal custody records length")
	}
	// We need to mix in the length of the slice.
	custodyRoot := make([]byte, 32)
	copy(custodyRoot, custodyBuf.Bytes())
	res := htrutils.MixInLength(custodyRecordsRoot, custodyRoot)
	return res, nil
}
