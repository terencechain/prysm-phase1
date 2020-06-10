package spectest

import (
	"testing"
)

func TestRegistryUpdatesMinimal(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runRegistryUpdatesTests(t, "minimal")
}
