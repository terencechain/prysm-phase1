// Package bls implements a go-wrapper around a library implementing the
// the BLS12-381 curve and signature scheme. This package exposes a public API for
// verifying and aggregating BLS signatures used by Ethereum 2.0.
package bls

import (
	"github.com/prysmaticlabs/prysm/shared/bls/blst"
	"github.com/prysmaticlabs/prysm/shared/bls/herumi"
	"github.com/prysmaticlabs/prysm/shared/bls/iface"
	"github.com/prysmaticlabs/prysm/shared/featureconfig"
)

// SecretKeyFromBytes creates a BLS private key from a BigEndian byte slice.
func SecretKeyFromBytes(privKey []byte) (SecretKey, error) {
	if featureconfig.Get().EnableBlst {
		return blst.SecretKeyFromBytes(privKey)
	}
	return herumi.SecretKeyFromBytes(privKey)
}

// PublicKeyFromBytes creates a BLS public key from a  BigEndian byte slice.
func PublicKeyFromBytes(pubKey []byte) (PublicKey, error) {
	if featureconfig.Get().EnableBlst {
		return blst.PublicKeyFromBytes(pubKey)
	}
	return herumi.PublicKeyFromBytes(pubKey)
}

// SignatureFromBytes creates a BLS signature from a LittleEndian byte slice.
func SignatureFromBytes(sig []byte) (Signature, error) {
	if featureconfig.Get().EnableBlst {
		return blst.SignatureFromBytes(sig)
	}
	return herumi.SignatureFromBytes(sig)
}

// AggregatePublicKeys aggregates the provided raw public keys into a single key.
func AggregatePublicKeys(pubs [][]byte) (PublicKey, error) {
	if featureconfig.Get().EnableBlst {
		return blst.AggregatePublicKeys(pubs)
	}
	return herumi.AggregatePublicKeys(pubs)
}

// AggregateSignatures converts a list of signatures into a single, aggregated sig.
func AggregateSignatures(sigs []iface.Signature) iface.Signature {
	if featureconfig.Get().EnableBlst {
		return blst.AggregateSignatures(sigs)
	}
	return herumi.AggregateSignatures(sigs)
}

// VerifyMultipleSignatures verifies multiple signatures for distinct messages securely.
func VerifyMultipleSignatures(sigs [][]byte, msgs [][32]byte, pubKeys []iface.PublicKey) (bool, error) {
	if featureconfig.Get().EnableBlst {
		return blst.VerifyMultipleSignatures(sigs, msgs, pubKeys)
	}
	// Manually decompress each signature as herumi does not
	// have a batch decompress method.
	rawSigs := make([]Signature, len(sigs))
	var err error
	for i, s := range sigs {
		rawSigs[i], err = herumi.SignatureFromBytes(s)
		if err != nil {
			return false, err
		}
	}
	return herumi.VerifyMultipleSignatures(rawSigs, msgs, pubKeys)
}

// NewAggregateSignature creates a blank aggregate signature.
func NewAggregateSignature() iface.Signature {
	if featureconfig.Get().EnableBlst {
		return blst.NewAggregateSignature()
	}
	return herumi.NewAggregateSignature()
}

// RandKey creates a new private key using a random input.
func RandKey() iface.SecretKey {
	if featureconfig.Get().EnableBlst {
		return blst.RandKey()
	}
	return herumi.RandKey()
}

// VerifyCompressed signature.
func VerifyCompressed(signature []byte, pub []byte, msg []byte) bool {
	if featureconfig.Get().EnableBlst {
		return blst.VerifyCompressed(signature, pub, msg)
	}
	sig, err := SignatureFromBytes(signature)
	if err != nil {
		return false
	}
	pk, err := PublicKeyFromBytes(pub)
	if err != nil {
		return false
	}
	return sig.Verify(pk, msg)
}
