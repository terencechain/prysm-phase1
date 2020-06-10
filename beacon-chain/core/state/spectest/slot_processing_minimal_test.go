package spectest

import (
	"testing"
)

func TestSlotProcessingMinimal(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runSlotProcessingTests(t, "minimal")
}
