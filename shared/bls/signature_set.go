package bls

// SignatureSet refers to the defined set of
// signatures and its respective public keys and
// messages required to verify it.
type SignatureSet struct {
	Signatures [][]byte
	PublicKeys []PublicKey
	Messages   [][32]byte
}

// NewSet constructs an empty signature set object.
func NewSet() *SignatureSet {
	return &SignatureSet{
		Signatures: [][]byte{},
		PublicKeys: []PublicKey{},
		Messages:   [][32]byte{},
	}
}

// Join merges the provided signature set to out current one.
func (s *SignatureSet) Join(set *SignatureSet) *SignatureSet {
	s.Signatures = append(s.Signatures, set.Signatures...)
	s.PublicKeys = append(s.PublicKeys, set.PublicKeys...)
	s.Messages = append(s.Messages, set.Messages...)
	return s
}

// Verify the current signature set using the batch verify algorithm.
func (s *SignatureSet) Verify() (bool, error) {
	return VerifyMultipleSignatures(s.Signatures, s.Messages, s.PublicKeys)
}
