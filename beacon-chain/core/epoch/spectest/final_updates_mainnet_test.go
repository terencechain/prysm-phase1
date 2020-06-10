package spectest

import (
	"testing"
)

func TestFinalUpdatesMainnet(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runFinalUpdatesTests(t, "mainnet")
}
