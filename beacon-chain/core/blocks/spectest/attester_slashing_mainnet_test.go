package spectest

import (
	"testing"
)

func TestAttesterSlashingMainnet(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runAttesterSlashingTest(t, "mainnet")
}
