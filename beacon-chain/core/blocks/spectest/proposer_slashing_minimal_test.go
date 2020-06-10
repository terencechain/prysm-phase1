package spectest

import (
	"testing"
)

func TestProposerSlashingMinimal(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runProposerSlashingTest(t, "minimal")
}
