package spectest

import (
	"testing"
)

func TestRewardsPenaltiesMainnet(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runPrecomputeRewardsAndPenaltiesTests(t, "mainnet")
}
