package spectest

import (
	"testing"
)

func TestRewardsAndPenaltiesMinimal(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runRewardsAndPenaltiesTests(t, "minimal")
}
