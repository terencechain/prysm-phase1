package spectest

import (
	"testing"
)

func TestAttesterSlashingMinimal(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runAttesterSlashingTest(t, "minimal")
}
