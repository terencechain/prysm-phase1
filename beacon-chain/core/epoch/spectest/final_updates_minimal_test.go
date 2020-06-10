package spectest

import (
	"testing"
)

func TestFinalUpdatesMinimal(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runFinalUpdatesTests(t, "minimal")
}
