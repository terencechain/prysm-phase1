package spectest

import (
	"testing"
)

func TestRewardsPenaltiesMinimal(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runPrecomputeRewardsAndPenaltiesTests(t, "minimal")
}
