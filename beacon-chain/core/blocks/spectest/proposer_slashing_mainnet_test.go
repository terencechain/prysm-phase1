package spectest

import (
	"testing"
)

func TestProposerSlashingMainnet(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runProposerSlashingTest(t, "mainnet")
}
