package bls

// SignatureSet refers to the defined set of
// signatures and its respective public keys and
// messages required to verify it.
type SignatureSet struct {
	Signatures []Signature
	PublicKeys []PublicKey
	Messages   [][32]byte
}

// Join merges the provided signature set to out current one.
func (s *SignatureSet) Join(set *SignatureSet) {
	s.Signatures = append(s.Signatures, set.Signatures...)
	s.PublicKeys = append(s.PublicKeys, set.PublicKeys...)
	s.Messages = append(s.Messages, set.Messages...)
}
