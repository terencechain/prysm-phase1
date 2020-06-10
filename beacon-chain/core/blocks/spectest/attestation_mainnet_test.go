package spectest

import (
	"testing"
)

func TestAttestationMainnet(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runAttestationTest(t, "mainnet")
}
