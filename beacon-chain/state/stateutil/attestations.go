package stateutil

import (
	"bytes"
	"encoding/binary"

	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/featureconfig"
	"github.com/prysmaticlabs/prysm/shared/hashutil"
	"github.com/prysmaticlabs/prysm/shared/params"
)

// EpochAttestationsRoot computes the HashTreeRoot Merkleization of
// a list of pending attestation values according to the eth2
// Simple Serialize specification.
func EpochAttestationsRoot(atts []*pb.PendingAttestation) ([32]byte, error) {
	if featureconfig.Get().EnableSSZCache {
		return cachedHasher.epochAttestationsRoot(atts)
	}
	return nocachedHasher.epochAttestationsRoot(atts)
}

// PendingAttestationRoot describes a method from which the hash tree root
// of a pending attestation is returned.
func PendingAttestationRoot(hasher HashFn, att *pb.PendingAttestation) ([32]byte, error) {
	fieldRoots := [][32]byte{}
	if att != nil {
		// Bitfield.
		aggregationRoot, err := bitlistRoot(hasher, att.AggregationBits, params.BeaconConfig().MaxValidatorsPerCommittee)
		if err != nil {
			return [32]byte{}, err
		}
		// Attestation data.
		attDataRoot, err := attestationDataRoot(hasher, att.Data)
		if err != nil {
			return [32]byte{}, err
		}
		inclusionBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(inclusionBuf, att.InclusionDelay)
		// Inclusion delay.
		inclusionRoot := bytesutil.ToBytes32(inclusionBuf)

		proposerBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(proposerBuf, att.ProposerIndex)
		// Proposer index.
		proposerRoot := bytesutil.ToBytes32(proposerBuf)

		fieldRoots = [][32]byte{aggregationRoot, attDataRoot, inclusionRoot, proposerRoot}
	}
	return bitwiseMerkleizeArrays(hasher, fieldRoots, uint64(len(fieldRoots)), uint64(len(fieldRoots)))
}

// AttestationDataRoot describes a method that serves as a HashTreeRoot function for attestation data.
func AttestationDataRoot(data *ethpb.AttestationData) ([32]byte, error) {
	return attestationDataRoot(hashutil.CustomSHA256Hasher(), data)
}

func marshalAttestationData(data *ethpb.AttestationData) []byte {
	enc := make([]byte, 128)

	if data != nil {
		// Slot.
		slotBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(slotBuf, data.Slot)
		copy(enc[0:8], slotBuf)

		// Committee index.
		indexBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(indexBuf, data.CommitteeIndex)
		copy(enc[8:16], indexBuf)

		copy(enc[16:48], data.BeaconBlockRoot)

		// Source epoch and root.
		if data.Source != nil {
			sourceEpochBuf := make([]byte, 8)
			binary.LittleEndian.PutUint64(sourceEpochBuf, data.Source.Epoch)
			copy(enc[48:56], sourceEpochBuf)
			copy(enc[56:88], data.Source.Root)
		}

		// Target.
		if data.Target != nil {
			targetEpochBuf := make([]byte, 8)
			binary.LittleEndian.PutUint64(targetEpochBuf, data.Target.Epoch)
			copy(enc[88:96], targetEpochBuf)
			copy(enc[96:128], data.Target.Root)
		}
	}

	return enc
}

func attestationRoot(hasher HashFn, att *ethpb.Attestation) ([32]byte, error) {
	fieldRoots := make([][32]byte, 3)

	aggregationRoot, err := bitlistRoot(hasher, att.AggregationBits, params.BeaconConfig().MaxValidatorsPerCommittee)
	if err != nil {
		return [32]byte{}, err
	}
	fieldRoots[0] = aggregationRoot

	dataRoot, err := attestationDataRoot(hasher, att.Data)
	if err != nil {
		return [32]byte{}, err
	}
	fieldRoots[1] = dataRoot

	signatureBuf := bytesutil.ToBytes96(att.Signature)
	packedSig, err := pack([][]byte{signatureBuf[:]})
	if err != nil {
		return [32]byte{}, err
	}
	sigRoot, err := bitwiseMerkleize(hasher, packedSig, uint64(len(packedSig)), uint64(len(packedSig)))
	if err != nil {
		return [32]byte{}, err
	}
	fieldRoots[2] = sigRoot
	return bitwiseMerkleizeArrays(hasher, fieldRoots, uint64(len(fieldRoots)), uint64(len(fieldRoots)))
}

func blockAttestationRoot(atts []*ethpb.Attestation) ([32]byte, error) {
	hasher := hashutil.CustomSHA256Hasher()
	roots := make([][]byte, len(atts))
	for i := 0; i < len(atts); i++ {
		pendingRoot, err := attestationRoot(hasher, atts[i])
		if err != nil {
			return [32]byte{}, errors.Wrap(err, "could not attestation merkleization")
		}
		roots[i] = pendingRoot[:]
	}

	attsRootsRoot, err := bitwiseMerkleize(
		hasher,
		roots,
		uint64(len(roots)),
		params.BeaconConfig().MaxAttestations,
	)
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not compute block attestations merkleization")
	}
	attsLenBuf := new(bytes.Buffer)
	if err := binary.Write(attsLenBuf, binary.LittleEndian, uint64(len(atts))); err != nil {
		return [32]byte{}, errors.Wrap(err, "could not marshal epoch attestations length")
	}
	// We need to mix in the length of the slice.
	attsLenRoot := make([]byte, 32)
	copy(attsLenRoot, attsLenBuf.Bytes())
	res := mixInLength(attsRootsRoot, attsLenRoot)
	return res, nil
}

func attestationDataRoot(hasher HashFn, data *ethpb.AttestationData) ([32]byte, error) {
	fieldRoots := make([][]byte, 7)

	if data != nil {
		// Slot.
		slotBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(slotBuf, data.Slot)
		slotRoot := bytesutil.ToBytes32(slotBuf)
		fieldRoots[0] = slotRoot[:]

		// CommitteeIndex.
		indexBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(indexBuf, data.CommitteeIndex)
		interRoot := bytesutil.ToBytes32(indexBuf)
		fieldRoots[1] = interRoot[:]

		// Beacon block root.
		blockRoot := bytesutil.ToBytes32(data.BeaconBlockRoot)
		fieldRoots[2] = blockRoot[:]

		// Source.
		sourceRoot, err := CheckpointRoot(hasher, data.Source)
		if err != nil {
			return [32]byte{}, errors.Wrap(err, "could not compute source checkpoint merkleization")
		}
		fieldRoots[3] = sourceRoot[:]

		// Target.
		targetRoot, err := CheckpointRoot(hasher, data.Target)
		if err != nil {
			return [32]byte{}, errors.Wrap(err, "could not compute target checkpoint merkleization")
		}
		fieldRoots[4] = targetRoot[:]

		// Shard head root.
		shardHeadRoot := bytesutil.ToBytes32(data.ShardHeadRoot)
		fieldRoots[5] = shardHeadRoot[:]

		// Shard transition root.
		shardTransitionRoot := bytesutil.ToBytes32(data.ShardTransitionRoot)
		fieldRoots[6] = shardTransitionRoot[:]
	}

	return bitwiseMerkleize(hasher, fieldRoots, uint64(len(fieldRoots)), uint64(len(fieldRoots)))
}

func (h *stateRootHasher) pendingAttestationRoot(hasher HashFn, att *pb.PendingAttestation) ([32]byte, error) {
	// Marshal attestation to determine if it exists in the cache.
	enc := make([]byte, 2192)
	fieldRoots := make([][]byte, 4)

	if att != nil {
		copy(enc[0:2048], att.AggregationBits)

		inclusionBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(inclusionBuf, att.InclusionDelay)
		copy(enc[2048:2056], inclusionBuf)

		attDataBuf := marshalAttestationData(att.Data)
		copy(enc[2056:2184], attDataBuf)

		proposerBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(proposerBuf, att.ProposerIndex)
		copy(enc[2184:2192], proposerBuf)

		// Check if it exists in cache:
		if h.rootsCache != nil {
			if found, ok := h.rootsCache.Get(string(enc)); found != nil && ok {
				return found.([32]byte), nil
			}
		}

		// Bitfield.
		aggregationRoot, err := bitlistRoot(hasher, att.AggregationBits, 2048)
		if err != nil {
			return [32]byte{}, err
		}
		fieldRoots[0] = aggregationRoot[:]

		// Attestation data.
		attDataRoot, err := attestationDataRoot(hasher, att.Data)
		if err != nil {
			return [32]byte{}, err
		}
		fieldRoots[1] = attDataRoot[:]

		// Inclusion delay.
		inclusionRoot := bytesutil.ToBytes32(inclusionBuf)
		fieldRoots[2] = inclusionRoot[:]

		// Proposer index.
		proposerRoot := bytesutil.ToBytes32(proposerBuf)
		fieldRoots[3] = proposerRoot[:]
	}
	res, err := bitwiseMerkleize(hasher, fieldRoots, uint64(len(fieldRoots)), uint64(len(fieldRoots)))
	if err != nil {
		return [32]byte{}, err
	}
	if h.rootsCache != nil {
		h.rootsCache.Set(string(enc), res, 32)
	}
	return res, nil
}

func (h *stateRootHasher) epochAttestationsRoot(atts []*pb.PendingAttestation) ([32]byte, error) {
	hasher := hashutil.CustomSHA256Hasher()
	roots := make([][]byte, len(atts))
	for i := 0; i < len(atts); i++ {
		pendingRoot, err := h.pendingAttestationRoot(hasher, atts[i])
		if err != nil {
			return [32]byte{}, errors.Wrap(err, "could not attestation merkleization")
		}
		roots[i] = pendingRoot[:]
	}

	attsRootsRoot, err := bitwiseMerkleize(
		hasher,
		roots,
		uint64(len(roots)),
		params.BeaconConfig().MaxAttestations*params.BeaconConfig().SlotsPerEpoch,
	)
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not compute epoch attestations merkleization")
	}
	attsLenBuf := new(bytes.Buffer)
	if err := binary.Write(attsLenBuf, binary.LittleEndian, uint64(len(atts))); err != nil {
		return [32]byte{}, errors.Wrap(err, "could not marshal epoch attestations length")
	}
	// We need to mix in the length of the slice.
	attsLenRoot := make([]byte, 32)
	copy(attsLenRoot, attsLenBuf.Bytes())
	res := mixInLength(attsRootsRoot, attsLenRoot)
	return res, nil
}
